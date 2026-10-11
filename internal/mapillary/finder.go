package mapillary

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/rizface/geoguessr/internal/pool"
)

// bboxHalfDeg is half the side of the search box. 0.04° → 0.08° square,
// area 0.0064 deg², safely under the API's 0.01 deg² limit.
const bboxHalfDeg = 0.04

// Finder hunts for a random playable location by sampling points near
// seed cities and asking Mapillary what imagery exists there.
type Finder struct {
	Client      *Client
	Seeds       []Seed
	MaxAttempts int
	// PanoShare is the fraction of attempts that require a 360° panorama
	// before falling back to any image. 0.7 means the first 70% of attempts.
	PanoShare float64
	Rand      *rand.Rand
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
		seed := seeds[f.Rand.IntN(len(seeds))]
		center := randomPointNear(f.Rand, seed.Lat, seed.Lng, seed.RadiusKm)
		bbox := BBox{
			MinLng: center[1] - bboxHalfDeg, MinLat: center[0] - bboxHalfDeg,
			MaxLng: center[1] + bboxHalfDeg, MaxLat: center[0] + bboxHalfDeg,
		}

		images, err := f.Client.ImagesInBBox(ctx, bbox, i < panoAttempts, 100)
		if err != nil {
			if errors.Is(err, ErrNoToken) {
				return pool.Location{}, err
			}
			lastErr = err
			continue
		}
		if len(images) == 0 {
			continue
		}
		img := images[f.Rand.IntN(len(images))]
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
