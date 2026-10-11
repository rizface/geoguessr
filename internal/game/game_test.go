package game

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rizface/geoguessr/internal/geo"
	"github.com/rizface/geoguessr/internal/pool"
)

type fakeFinder struct {
	n   atomic.Int64
	err error
}

func (f *fakeFinder) Find(_ context.Context, region string) (pool.Location, error) {
	if f.err != nil {
		return pool.Location{}, f.err
	}
	i := f.n.Add(1)
	return pool.Location{ImageID: fmt.Sprintf("live-%d", i), Lat: -6.2, Lng: 106.8, Country: "ID", Region: "asia"}, nil
}

func newTestService(t *testing.T, poolLocs []pool.Location, f Finder) *Service {
	t.Helper()
	p, err := pool.Open(filepath.Join(t.TempDir(), "locations.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range poolLocs {
		p.Add(l)
	}
	return NewService(p, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func samplePool(n int) []pool.Location {
	var out []pool.Location
	for i := 0; i < n; i++ {
		out = append(out, pool.Location{ImageID: fmt.Sprintf("pool-%d", i), Lat: 51.5, Lng: -0.12, Country: "GB", Region: "europe"})
	}
	return out
}

func TestFullGameFromPool(t *testing.T) {
	s := newTestService(t, samplePool(10), nil)
	g, err := s.NewGame(context.Background(), Settings{Rounds: 3, Region: "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Rounds) != 3 {
		t.Fatalf("rounds = %d, want 3", len(g.Rounds))
	}

	// Can't peek ahead.
	if _, err := s.Round(g.ID, 1); !errors.Is(err, ErrRoundLocked) {
		t.Fatalf("peek ahead err = %v, want ErrRoundLocked", err)
	}
	// Summary blocked until done.
	if _, err := s.Summary(g.ID); !errors.Is(err, ErrGameNotOver) {
		t.Fatalf("early summary err = %v", err)
	}

	// Perfect guess.
	rv, err := s.Round(g.ID, 0)
	if err != nil || rv.ImageID == "" {
		t.Fatalf("round 0: %+v %v", rv, err)
	}
	res, err := s.Guess(g.ID, 0, &geo.Point{Lat: 51.5, Lng: -0.12})
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != geo.MaxScore {
		t.Fatalf("perfect guess score = %d", res.Score)
	}
	// Double guess rejected.
	if _, err := s.Guess(g.ID, 0, &geo.Point{}); !errors.Is(err, ErrAlreadyGuessed) {
		t.Fatalf("double guess err = %v", err)
	}

	// Timed out round.
	res, err = s.Guess(g.ID, 1, nil)
	if err != nil || !res.TimedOut || res.Score != 0 {
		t.Fatalf("timeout: %+v %v", res, err)
	}

	// Far guess, with an un-normalized longitude from Leaflet.
	res, err = s.Guess(g.ID, 2, &geo.Point{Lat: -6.2, Lng: 106.8 + 360})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(res.Guess.Lng-106.8) > 1e-9 {
		t.Fatalf("lng not normalized: %v", res.Guess.Lng)
	}
	if !res.GameOver || res.TotalScore != geo.MaxScore+res.Score {
		t.Fatalf("end state wrong: %+v", res)
	}

	sum, err := s.Summary(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Rounds) != 3 || sum.MaxScore != 15000 || sum.TotalScore != res.TotalScore {
		t.Fatalf("summary wrong: %+v", sum)
	}
}

func TestRegionFilterAndLiveFallback(t *testing.T) {
	f := &fakeFinder{}
	s := newTestService(t, samplePool(10), f) // pool is all Europe
	g, err := s.NewGame(context.Background(), Settings{Rounds: 4, Region: "indonesia"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rounds {
		if r.Location.Country != "ID" {
			t.Fatalf("got non-Indonesia location %+v", r.Location)
		}
	}
	// Live finds were saved into the pool.
	if got := s.pool.CountRegion("indonesia"); got != 4 {
		t.Fatalf("pool indonesia count = %d, want 4", got)
	}
}

func TestNoTokenEmptyPool(t *testing.T) {
	s := newTestService(t, nil, nil)
	_, err := s.NewGame(context.Background(), Settings{Rounds: 5, Region: "world"})
	if !errors.Is(err, ErrNoLocations) {
		t.Fatalf("err = %v, want ErrNoLocations", err)
	}
}

func TestFinderFailure(t *testing.T) {
	s := newTestService(t, samplePool(2), &fakeFinder{err: errors.New("boom")})
	_, err := s.NewGame(context.Background(), Settings{Rounds: 5})
	if !errors.Is(err, ErrNoLocations) {
		t.Fatalf("err = %v, want ErrNoLocations", err)
	}
}

func TestSettingsNormalize(t *testing.T) {
	ok := func(r string) bool { return r == "world" || r == "europe" }
	s, err := Settings{}.Normalize(ok)
	if err != nil || s.Rounds != DefaultRounds || s.Region != "world" {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	for _, bad := range []Settings{{Rounds: 11}, {Rounds: -1}, {TimeLimitSec: -5}, {TimeLimitSec: 9999}, {Region: "mars"}} {
		if _, err := bad.Normalize(ok); !errors.Is(err, ErrBadSettings) {
			t.Errorf("Normalize(%+v) err = %v, want ErrBadSettings", bad, err)
		}
	}
}
