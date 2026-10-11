// Package game runs game sessions: it picks locations, records guesses
// and keeps the true answers on the server until each round is played.
package game

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"sync"
	"time"

	"github.com/rizface/geoguessr/internal/geo"
	"github.com/rizface/geoguessr/internal/pool"
)

// Limits for game settings.
const (
	MinRounds     = 1
	MaxRounds     = 10
	DefaultRounds = 5
	MaxTimeLimit  = 600 // seconds
	gameTTL       = 6 * time.Hour
	liveWorkers   = 6
)

// Errors the HTTP layer maps to status codes.
var (
	ErrGameNotFound   = errors.New("game not found")
	ErrRoundNotFound  = errors.New("round not found")
	ErrRoundLocked    = errors.New("finish the current round first")
	ErrAlreadyGuessed = errors.New("round already guessed")
	ErrGameNotOver    = errors.New("game is not finished yet")
	ErrBadSettings    = errors.New("invalid game settings")
	ErrNoLocations    = errors.New("could not find enough locations")
)

// Finder finds a fresh random location on demand (Mapillary live search).
type Finder interface {
	Find(ctx context.Context, region string) (pool.Location, error)
}

// Settings chosen on the start screen.
type Settings struct {
	Rounds       int    `json:"rounds"`
	Region       string `json:"region"`
	TimeLimitSec int    `json:"timeLimitSec"` // 0 = no timer
}

// Round is one location plus what the player did with it.
type Round struct {
	Location   pool.Location
	Guess      *geo.Point
	DistanceKm float64
	Score      int
	Played     bool
	TimedOut   bool
}

// Game is one playthrough.
type Game struct {
	ID        string
	Settings  Settings
	Rounds    []Round
	Current   int // index of the round being played; == len(Rounds) when over
	CreatedAt time.Time
}

// Total sums the score of played rounds.
func (g *Game) Total() int {
	t := 0
	for _, r := range g.Rounds {
		t += r.Score
	}
	return t
}

// Over reports whether every round has been played.
func (g *Game) Over() bool { return g.Current >= len(g.Rounds) }

// Service owns all live games.
type Service struct {
	mu     sync.Mutex
	games  map[string]*Game
	pool   *pool.Store
	finder Finder // may be nil: pool-only mode
	rand   *mrand.Rand
	randMu sync.Mutex
	now    func() time.Time
	log    *slog.Logger
	// SavePool persists locations found by live search into the pool file,
	// so the pool grows the more you play.
	SavePool bool
}

// NewService builds a Service. finder may be nil.
func NewService(p *pool.Store, f Finder, log *slog.Logger) *Service {
	return &Service{
		games:    map[string]*Game{},
		pool:     p,
		finder:   f,
		rand:     mrand.New(mrand.NewPCG(mrand.Uint64(), mrand.Uint64())),
		now:      time.Now,
		log:      log,
		SavePool: true,
	}
}

// Normalize fills defaults and validates settings.
func (s Settings) Normalize(validRegion func(string) bool) (Settings, error) {
	if s.Rounds == 0 {
		s.Rounds = DefaultRounds
	}
	if s.Region == "" {
		s.Region = "world"
	}
	if s.Rounds < MinRounds || s.Rounds > MaxRounds {
		return s, fmt.Errorf("%w: rounds must be %d-%d", ErrBadSettings, MinRounds, MaxRounds)
	}
	if s.TimeLimitSec < 0 || s.TimeLimitSec > MaxTimeLimit {
		return s, fmt.Errorf("%w: time limit must be 0-%d seconds", ErrBadSettings, MaxTimeLimit)
	}
	if validRegion != nil && !validRegion(s.Region) {
		return s, fmt.Errorf("%w: unknown region %q", ErrBadSettings, s.Region)
	}
	return s, nil
}

// NewGame picks locations (pool first, then live search) and starts a game.
func (s *Service) NewGame(ctx context.Context, settings Settings) (*Game, error) {
	s.randMu.Lock()
	locs := s.pool.Pick(s.rand, settings.Region, settings.Rounds)
	s.randMu.Unlock()

	if missing := settings.Rounds - len(locs); missing > 0 {
		live, err := s.findLive(ctx, settings.Region, missing, locs)
		locs = append(locs, live...)
		// A slightly shorter game beats no game: accept at least half.
		if len(locs) < (settings.Rounds+1)/2 {
			if err == nil {
				err = errors.New("live search returned too few results")
			}
			return nil, fmt.Errorf("%w (%d/%d): %v", ErrNoLocations, len(locs), settings.Rounds, err)
		}
		if len(locs) < settings.Rounds {
			s.log.Warn("starting a shorter game", "wanted", settings.Rounds, "got", len(locs), "err", err)
		}
	}

	s.randMu.Lock()
	s.rand.Shuffle(len(locs), func(i, j int) { locs[i], locs[j] = locs[j], locs[i] })
	s.randMu.Unlock()

	g := &Game{
		ID:        newID(),
		Settings:  settings,
		Rounds:    make([]Round, len(locs)),
		CreatedAt: s.now(),
	}
	for i, l := range locs {
		g.Rounds[i] = Round{Location: l}
	}

	s.mu.Lock()
	s.evictLocked()
	s.games[g.ID] = g
	s.mu.Unlock()
	return g, nil
}

// findLive asks the finder for n new locations in parallel, skipping
// images already chosen.
func (s *Service) findLive(ctx context.Context, region string, n int, have []pool.Location) ([]pool.Location, error) {
	if s.finder == nil {
		return nil, errors.New("location pool is empty and no MAPILLARY_TOKEN is set for live search")
	}
	seen := map[string]bool{}
	for _, l := range have {
		seen[l.ImageID] = true
	}

	var (
		mu      sync.Mutex
		found   []pool.Location
		lastErr error
		wg      sync.WaitGroup
	)
	// Over-request a little: duplicates and failures are common.
	jobs := make(chan struct{}, n*3)
	for i := 0; i < n*3; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	for w := 0; w < min(liveWorkers, n*3); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				mu.Lock()
				done := len(found) >= n
				mu.Unlock()
				if done || ctx.Err() != nil {
					return
				}
				loc, err := s.finder.Find(ctx, region)
				mu.Lock()
				if err != nil {
					lastErr = err
				} else if !seen[loc.ImageID] && len(found) < n {
					seen[loc.ImageID] = true
					found = append(found, loc)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if s.SavePool && len(found) > 0 {
		added := 0
		for _, l := range found {
			if s.pool.Add(l) {
				added++
			}
		}
		if added > 0 {
			if err := s.pool.Save(); err != nil {
				s.log.Warn("could not save location pool", "err", err)
			}
		}
	}
	return found, lastErr
}

// Get returns a game by ID.
func (s *Service) Get(id string) (*Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[id]
	if !ok {
		return nil, ErrGameNotFound
	}
	return g, nil
}

// RoundView is what the client may see before guessing: no coordinates.
type RoundView struct {
	Index        int    `json:"index"`
	Total        int    `json:"total"`
	ImageID      string `json:"imageId"`
	TimeLimitSec int    `json:"timeLimitSec"`
	Score        int    `json:"score"` // running total before this round
}

// Round returns the playable view of round n. Only the current round
// can be fetched, so players can't peek ahead.
func (s *Service) Round(id string, n int) (RoundView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[id]
	if !ok {
		return RoundView{}, ErrGameNotFound
	}
	if n < 0 || n >= len(g.Rounds) {
		return RoundView{}, ErrRoundNotFound
	}
	if n > g.Current {
		return RoundView{}, ErrRoundLocked
	}
	return RoundView{
		Index:        n,
		Total:        len(g.Rounds),
		ImageID:      g.Rounds[n].Location.ImageID,
		TimeLimitSec: g.Settings.TimeLimitSec,
		Score:        g.Total() - g.Rounds[n].Score,
	}, nil
}

// GuessResult is revealed after a guess.
type GuessResult struct {
	Index      int        `json:"index"`
	Actual     geo.Point  `json:"actual"`
	Guess      *geo.Point `json:"guess"`
	DistanceKm float64    `json:"distanceKm"`
	Score      int        `json:"score"`
	TotalScore int        `json:"totalScore"`
	TimedOut   bool       `json:"timedOut"`
	Near       string     `json:"near,omitempty"`
	Country    string     `json:"country,omitempty"`
	GameOver   bool       `json:"gameOver"`
}

// Guess records a guess for the current round. A nil guess means the
// timer ran out: zero points.
func (s *Service) Guess(id string, n int, guess *geo.Point) (GuessResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[id]
	if !ok {
		return GuessResult{}, ErrGameNotFound
	}
	if n < 0 || n >= len(g.Rounds) {
		return GuessResult{}, ErrRoundNotFound
	}
	if n < g.Current || g.Rounds[n].Played {
		return GuessResult{}, ErrAlreadyGuessed
	}
	if n > g.Current {
		return GuessResult{}, ErrRoundLocked
	}

	r := &g.Rounds[n]
	actual := geo.Point{Lat: r.Location.Lat, Lng: r.Location.Lng}
	if guess == nil {
		r.TimedOut = true
	} else {
		p := geo.Point{Lat: guess.Lat, Lng: geo.NormalizeLng(guess.Lng)}
		r.Guess = &p
		r.DistanceKm = geo.HaversineKm(p, actual)
		r.Score = geo.Score(r.DistanceKm)
	}
	r.Played = true
	g.Current++

	return resultFor(g, n), nil
}

func resultFor(g *Game, n int) GuessResult {
	r := g.Rounds[n]
	return GuessResult{
		Index:      n,
		Actual:     geo.Point{Lat: r.Location.Lat, Lng: r.Location.Lng},
		Guess:      r.Guess,
		DistanceKm: r.DistanceKm,
		Score:      r.Score,
		TotalScore: g.Total(),
		TimedOut:   r.TimedOut,
		Near:       r.Location.Near,
		Country:    r.Location.Country,
		GameOver:   g.Over(),
	}
}

// Summary is the end-of-game recap.
type Summary struct {
	ID         string        `json:"id"`
	Settings   Settings      `json:"settings"`
	Rounds     []GuessResult `json:"rounds"`
	TotalScore int           `json:"totalScore"`
	MaxScore   int           `json:"maxScore"`
}

// Summary returns all rounds once the game is over.
func (s *Service) Summary(id string) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[id]
	if !ok {
		return Summary{}, ErrGameNotFound
	}
	if !g.Over() {
		return Summary{}, ErrGameNotOver
	}
	out := Summary{
		ID:         g.ID,
		Settings:   g.Settings,
		TotalScore: g.Total(),
		MaxScore:   geo.MaxScore * len(g.Rounds),
	}
	for i := range g.Rounds {
		out.Rounds = append(out.Rounds, resultFor(g, i))
	}
	return out, nil
}

func (s *Service) evictLocked() {
	cutoff := s.now().Add(-gameTTL)
	for id, g := range s.games {
		if g.CreatedAt.Before(cutoff) {
			delete(s.games, id)
		}
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
