// Package geo holds the distance and scoring math for the game.
package geo

import "math"

// EarthRadiusKm is the mean Earth radius used for great-circle distance.
const EarthRadiusKm = 6371.0088

// MaxScore is the best possible score for a single round.
const MaxScore = 5000

// perfectRadiusKm is how close a guess must be to earn the full score.
const perfectRadiusKm = 0.025

// scoreFalloffKm controls how fast the score decays with distance.
// At this distance a guess earns ~37% of MaxScore.
const scoreFalloffKm = 2000.0

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// HaversineKm returns the great-circle distance between a and b in kilometers.
func HaversineKm(a, b Point) float64 {
	lat1 := toRad(a.Lat)
	lat2 := toRad(b.Lat)
	dLat := lat2 - lat1
	dLng := toRad(b.Lng - a.Lng)

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	// Guard against tiny float overshoots above 1.
	h = math.Min(1, h)
	return 2 * EarthRadiusKm * math.Asin(math.Sqrt(h))
}

// Score turns a distance into round points, GeoGuessr style:
// full points inside a few meters, exponential decay after that.
func Score(distanceKm float64) int {
	if distanceKm < perfectRadiusKm {
		return MaxScore
	}
	return int(math.Round(MaxScore * math.Exp(-distanceKm/scoreFalloffKm)))
}

// NormalizeLng wraps a longitude into [-180, 180). Leaflet can return
// values outside that range when the user pans across the antimeridian.
func NormalizeLng(lng float64) float64 {
	l := math.Mod(lng+180, 360)
	if l < 0 {
		l += 360
	}
	return l - 180
}

// Valid reports whether p is a usable coordinate.
func (p Point) Valid() bool {
	return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lng) &&
		p.Lat >= -90 && p.Lat <= 90 &&
		!math.IsInf(p.Lng, 0)
}

func toRad(deg float64) float64 { return deg * math.Pi / 180 }
