// Package pool stores pre-curated playable locations in a JSON file,
// so games start instantly instead of searching Mapillary every round.
package pool

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
)

// Location is one playable round: a Mapillary image and where it really is.
type Location struct {
	ImageID string  `json:"imageId"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Pano    bool    `json:"pano"`
	Country string  `json:"country,omitempty"`
	Region  string  `json:"region,omitempty"`
	Near    string  `json:"near,omitempty"`
}

// MatchesRegion mirrors mapillary.FilterSeeds so pool and live search agree.
func (l Location) MatchesRegion(region string) bool {
	switch region {
	case "", "world":
		return true
	case "indonesia":
		return l.Country == "ID"
	default:
		return l.Region == region
	}
}

// Store is a concurrency-safe, file-backed set of locations.
type Store struct {
	mu   sync.RWMutex
	path string
	locs []Location
	ids  map[string]struct{}
}

// Open loads the pool from path. A missing file is an empty pool, not an error.
func Open(path string) (*Store, error) {
	s := &Store{path: path, ids: map[string]struct{}{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pool: read %s: %w", path, err)
	}
	var locs []Location
	if err := json.Unmarshal(b, &locs); err != nil {
		return nil, fmt.Errorf("pool: parse %s: %w", path, err)
	}
	for _, l := range locs {
		s.addLocked(l)
	}
	return s, nil
}

// Len returns how many locations are stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.locs)
}

// CountRegion returns how many stored locations match region.
func (s *Store) CountRegion(region string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, l := range s.locs {
		if l.MatchesRegion(region) {
			n++
		}
	}
	return n
}

// Add inserts l unless its image is already stored. Reports whether it was new.
func (s *Store) Add(l Location) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(l)
}

func (s *Store) addLocked(l Location) bool {
	if l.ImageID == "" {
		return false
	}
	if _, ok := s.ids[l.ImageID]; ok {
		return false
	}
	s.ids[l.ImageID] = struct{}{}
	s.locs = append(s.locs, l)
	return true
}

// Pick returns up to n distinct random locations in region.
func (s *Store) Pick(r *rand.Rand, region string, n int) []Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var candidates []Location
	for _, l := range s.locs {
		if l.MatchesRegion(region) {
			candidates = append(candidates, l)
		}
	}
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	if len(candidates) > n {
		candidates = candidates[:n]
	}
	return candidates
}

// Save writes the pool atomically (temp file + rename).
func (s *Store) Save() error {
	s.mu.RLock()
	b, err := json.MarshalIndent(s.locs, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
