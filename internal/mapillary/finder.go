package mapillary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"

	"github.com/rizface/geoguessr/internal/mvt"
	"github.com/rizface/geoguessr/internal/pool"
)

// Finder hunts for a random playable location: pick a random point near
// a seed city, fetch the z14 coverage tile under it, pick an image.
type Finder struct {
	Client      *Client
	Seeds       []Seed
	MaxAttempts int
	// PanoShare is the fraction of attempts that require a 360° panorama
	// before falling back to any image. 0.7 means the first 70% of attempts.
	PanoShare float64
	Rand      *rand.Rand
	Log       *slog.Logger // optional; request errors are logged here

	randMu sync.Mutex // Rand is not goroutine-safe; Find runs in parallel
}

// NewFinder returns a Finder with defaults.
func NewFinder(c *Client, seeds []Seed) *Finder {
	return &Finder{
		Client:      c,
		Seeds:       seeds,
		MaxAttempts: 12,
		PanoShare:   0.7,
		Rand:        rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
}

// ErrNotFound means every attempt came back empty.
var ErrNotFound = errors.New("mapillary: no imagery found after all attempts")

// Find returns one random location. Seeds are filtered by region first.
// Safe for concurrent use.
func (f *Finder) Find(ctx context.Context, region string) (pool.Location, error) {
	seeds := FilterSeeds(f.Seeds, region)
	if len(seeds) == 0 {
		return pool.Location{}, fmt.Errorf("mapillary: no seed cities for region %q", region)
	}

	panoAttempts := int(math.Ceil(float64(f.MaxAttempts) * f.PanoShare))
	var lastErr error
	for i := 0; i < f.MaxAttempts; i++ {
		if err := ctx.Err(); err != nil {
			return pool.Location{}, err
		}
		f.randMu.Lock()
		seed := seeds[f.Rand.IntN(len(seeds))]
		p := randomPointNear(f.Rand, seed.Lat, seed.Lng, seed.RadiusKm)
		f.randMu.Unlock()
		x, y := mvt.LngLatToTile(p[1], p[0], ImageZoom)

		images, err := f.Client.ImagesInTile(ctx, x, y)
		if err != nil {
			if errors.Is(err, ErrNoToken) {
				return pool.Location{}, err
			}
			if f.Log != nil && ctx.Err() == nil {
				f.Log.Warn("mapillary request failed", "err", err)
			}
			lastErr = err
			continue
		}

		if i < panoAttempts {
			images = panosOnly(images)
		}
		if len(images) == 0 {
			continue
		}
		f.randMu.Lock()
		img := images[f.Rand.IntN(len(images))]
		f.randMu.Unlock()
		return pool.Location{
			ImageID: img.ID,
			Lat:     img.Lat,
			Lng:     img.Lng,
			Pano:    img.IsPano,
			Country: seed.Country,
			Region:  seed.Region,
			Near:    seed.Name,
		}, nil
	}
	if lastErr != nil {
		return pool.Location{}, fmt.Errorf("%w (last error: %v)", ErrNotFound, lastErr)
	}
	return pool.Location{}, ErrNotFound
}

func panosOnly(images []Image) []Image {
	out := images[:0:0]
	for _, img := range images {
		if img.IsPano {
			out = append(out, img)
		}
	}
	return out
}

// randomPointNear picks a uniformly random point inside a circle of
// radiusKm around (lat, lng). Returns [lat, lng].
func randomPointNear(r *rand.Rand, lat, lng, radiusKm float64) [2]float64 {
	if radiusKm <= 0 {
		radiusKm = 10
	}
	// sqrt for uniform distribution over the disk area.
	dist := radiusKm * math.Sqrt(r.Float64())
	angle := 2 * math.Pi * r.Float64()
	dLat := (dist * math.Cos(angle)) / 111.32
	cosLat := math.Max(0.01, math.Cos(lat*math.Pi/180))
	dLng := (dist * math.Sin(angle)) / (111.32 * cosLat)
	return [2]float64{lat + dLat, lng + dLng}
}
