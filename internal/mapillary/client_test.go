package mapillary

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rizface/geoguessr/internal/mvt"
)

// The fixture has 3 image points: a pano (id 1234567890123) at (100,200),
// a non-pano ("998877") at (4000,50), and a pano in the buffer zone at
// (-5,4100) that belongs to a neighbouring tile and must be skipped.
func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../mvt/testdata/image_layer.pbf")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tileServer(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient("MLY|tok")
	c.TilesURL = srv.URL
	return c
}

func TestImagesInTile(t *testing.T) {
	tile := fixture(t)
	c := tileServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/14/13053/8473" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("access_token") != "MLY|tok" {
			t.Errorf("token = %q", r.URL.Query().Get("access_token"))
		}
		w.Write(tile)
	})

	imgs, err := c.ImagesInTile(context.Background(), 13053, 8473)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("got %d images, want 2 (buffer point skipped): %+v", len(imgs), imgs)
	}
	if imgs[0].ID != "1234567890123" || !imgs[0].IsPano || imgs[1].ID != "998877" || imgs[1].IsPano {
		t.Fatalf("images = %+v", imgs)
	}
	wantLng, wantLat := mvt.TileToLngLat(14, 13053, 8473, 100, 200, 4096)
	if math.Abs(imgs[0].Lng-wantLng) > 1e-9 || math.Abs(imgs[0].Lat-wantLat) > 1e-9 {
		t.Fatalf("position = %v,%v want %v,%v", imgs[0].Lng, imgs[0].Lat, wantLng, wantLat)
	}
	// Sanity: that tile is central Jakarta.
	if imgs[0].Lat > -6.1 || imgs[0].Lat < -6.3 || imgs[0].Lng < 106.7 || imgs[0].Lng > 106.9 {
		t.Fatalf("not in Jakarta: %v,%v", imgs[0].Lat, imgs[0].Lng)
	}
	if imgs[0].CapturedAt.UnixMilli() != 1700000000000 {
		t.Fatalf("captured_at = %v", imgs[0].CapturedAt)
	}
}

func TestImagesInTileEmptyAndErrors(t *testing.T) {
	if _, err := NewClient("").ImagesInTile(context.Background(), 0, 0); !errors.Is(err, ErrNoToken) {
		t.Fatalf("no token err = %v", err)
	}

	empty := tileServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if imgs, err := empty.ImagesInTile(context.Background(), 1, 1); err != nil || len(imgs) != 0 {
		t.Fatalf("204: %v %v", imgs, err)
	}

	// A tile with other layers but no "image" layer is just empty.
	none := tileServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write(nil) })
	if imgs, err := none.ImagesInTile(context.Background(), 1, 1); err != nil || len(imgs) != 0 {
		t.Fatalf("empty body: %v %v", imgs, err)
	}

	denied := tileServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid OAuth access token"}}`))
	})
	_, err := denied.ImagesInTile(context.Background(), 1, 1)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Invalid OAuth") {
		t.Fatalf("401 err = %v", err)
	}
	if strings.Contains(err.Error(), "MLY|tok") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

func TestFinderPrefersPanoramas(t *testing.T) {
	tile := fixture(t)
	c := tileServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write(tile) })
	f := NewFinder(c, DefaultSeeds)
	f.PanoShare = 1
	for i := 0; i < 20; i++ {
		loc, err := f.Find(context.Background(), "indonesia")
		if err != nil {
			t.Fatal(err)
		}
		if loc.ImageID != "1234567890123" || !loc.Pano || loc.Country != "ID" {
			t.Fatalf("loc = %+v", loc)
		}
	}
}

func TestFinderFallsBackAfterPanoAttempts(t *testing.T) {
	// Non-pano-only world: panorama attempts find nothing, later ones succeed.
	tile := fixture(t)
	var calls atomic.Int64
	c := tileServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write(tile)
	})
	f := NewFinder(c, DefaultSeeds)
	f.Rand = rand.New(rand.NewPCG(7, 7))
	f.PanoShare = 0 // accept anything from the first attempt
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		loc, err := f.Find(context.Background(), "world")
		if err != nil {
			t.Fatal(err)
		}
		seen[loc.ImageID] = true
	}
	if !seen["998877"] {
		t.Fatalf("non-pano never chosen with PanoShare=0: %v", seen)
	}
}

func TestFinderReportsLastError(t *testing.T) {
	c := tileServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("slow down"))
	})
	f := NewFinder(c, DefaultSeeds)
	f.MaxAttempts = 3
	_, err := f.Find(context.Background(), "world")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v", err)
	}
}

func TestFinderConcurrent(t *testing.T) {
	tile := fixture(t)
	c := tileServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write(tile) })
	f := NewFinder(c, DefaultSeeds)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := f.Find(context.Background(), "world"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestSeedsAreSane(t *testing.T) {
	for _, s := range DefaultSeeds {
		if s.Lat < -90 || s.Lat > 90 || s.Lng < -180 || s.Lng > 180 || s.RadiusKm <= 0 {
			t.Errorf("bad seed %+v", s)
		}
		if !ValidRegion(s.Region) {
			t.Errorf("seed %s has unknown region %q", s.Name, s.Region)
		}
	}
	for _, r := range Regions {
		if len(FilterSeeds(DefaultSeeds, r)) == 0 {
			t.Errorf("region %q has no seeds", r)
		}
	}
}

func TestRandomPointNearStaysInRadius(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 1000; i++ {
		p := randomPointNear(r, -6.2, 106.8, 20)
		// ~0.2° is comfortably more than 20 km at the equator.
		if p[0] < -6.4 || p[0] > -6.0 || p[1] < 106.6 || p[1] > 107.0 {
			t.Fatalf("point out of range: %v", p)
		}
	}
}
