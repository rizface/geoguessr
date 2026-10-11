// Package server exposes the game over a small JSON API and serves the
// static frontend.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rizface/geoguessr/internal/game"
	"github.com/rizface/geoguessr/internal/geo"
	"github.com/rizface/geoguessr/internal/mapillary"
	"github.com/rizface/geoguessr/internal/pool"
)

// Server wires HTTP handlers to the game service.
type Server struct {
	Games  *game.Service
	Pool   *pool.Store
	Token  string // Mapillary token; the viewer in the browser needs it too
	Static fs.FS
	Log    *slog.Logger
}

// Handler returns the full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("POST /api/games", s.newGame)
	mux.HandleFunc("GET /api/games/{id}/rounds/{n}", s.round)
	mux.HandleFunc("POST /api/games/{id}/rounds/{n}/guess", s.guess)
	mux.HandleFunc("GET /api/games/{id}/summary", s.summary)
	mux.Handle("GET /", http.FileServerFS(s.Static))
	return s.logRequests(mux)
}

type configResponse struct {
	MapillaryToken string         `json:"mapillaryToken"`
	Regions        []regionInfo   `json:"regions"`
	Limits         map[string]int `json:"limits"`
}

type regionInfo struct {
	ID         string `json:"id"`
	PoolSize   int    `json:"poolSize"`
	LiveSearch bool   `json:"liveSearch"`
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	regions := make([]regionInfo, 0, len(mapillary.Regions))
	for _, id := range mapillary.Regions {
		regions = append(regions, regionInfo{ID: id, PoolSize: s.Pool.CountRegion(id), LiveSearch: s.Token != ""})
	}
	writeJSON(w, http.StatusOK, configResponse{
		MapillaryToken: s.Token,
		Regions:        regions,
		Limits: map[string]int{
			"minRounds":    game.MinRounds,
			"maxRounds":    game.MaxRounds,
			"maxTimeLimit": game.MaxTimeLimit,
		},
	})
}

type newGameResponse struct {
	ID       string        `json:"id"`
	Settings game.Settings `json:"settings"`
	Rounds   int           `json:"rounds"`
}

func (s *Server) newGame(w http.ResponseWriter, r *http.Request) {
	var settings game.Settings
	if err := decodeJSON(r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, err := settings.Normalize(mapillary.ValidRegion)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Live search can take a while; don't hang forever.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	g, err := s.Games.NewGame(ctx, settings)
	if err != nil {
		s.Log.Warn("new game failed", "err", err, "region", settings.Region)
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newGameResponse{ID: g.ID, Settings: g.Settings, Rounds: len(g.Rounds)})
}

func (s *Server) round(w http.ResponseWriter, r *http.Request) {
	n, ok := roundIndex(w, r)
	if !ok {
		return
	}
	view, err := s.Games.Round(r.PathValue("id"), n)
	if err != nil {
		writeGameError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type guessRequest struct {
	// Lat/Lng are pointers so "no guess" (timer ran out) is explicit.
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
}

func (s *Server) guess(w http.ResponseWriter, r *http.Request) {
	n, ok := roundIndex(w, r)
	if !ok {
		return
	}
	var req guessRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var p *geo.Point
	if req.Lat != nil && req.Lng != nil {
		p = &geo.Point{Lat: *req.Lat, Lng: *req.Lng}
		if !p.Valid() {
			writeError(w, http.StatusBadRequest, "invalid coordinates")
			return
		}
	} else if req.Lat != nil || req.Lng != nil {
		writeError(w, http.StatusBadRequest, "send both lat and lng, or neither for a timeout")
		return
	}
	res, err := s.Games.Guess(r.PathValue("id"), n, p)
	if err != nil {
		writeGameError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.Games.Summary(r.PathValue("id"))
	if err != nil {
		writeGameError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func roundIndex(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "round must be a number")
		return 0, false
	}
	return n, true
}

func writeGameError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, game.ErrGameNotFound), errors.Is(err, game.ErrRoundNotFound):
		status = http.StatusNotFound
	case errors.Is(err, game.ErrRoundLocked), errors.Is(err, game.ErrAlreadyGuessed), errors.Is(err, game.ErrGameNotOver):
		status = http.StatusConflict
	}
	writeError(w, status, err.Error())
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
			s.Log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", time.Since(start).Round(time.Millisecond))
		}
	})
}
