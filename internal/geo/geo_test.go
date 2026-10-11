package geo

import (
	"math"
	"testing"
)

func TestHaversineKm(t *testing.T) {
	jakarta := Point{Lat: -6.2088, Lng: 106.8456}
	bandung := Point{Lat: -6.9175, Lng: 107.6191}
	london := Point{Lat: 51.5074, Lng: -0.1278}
	paris := Point{Lat: 48.8566, Lng: 2.3522}

	tests := []struct {
		name   string
		a, b   Point
		wantKm float64
		tolKm  float64
	}{
		{"same point", jakarta, jakarta, 0, 0.0001},
		{"jakarta-bandung", jakarta, bandung, 116, 3},
		{"london-paris", london, paris, 344, 3},
		{"antipodal-ish across antimeridian", Point{0, 179.5}, Point{0, -179.5}, 111.2, 1},
		{"pole to pole", Point{90, 0}, Point{-90, 0}, math.Pi * EarthRadiusKm, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HaversineKm(tt.a, tt.b)
			if math.Abs(got-tt.wantKm) > tt.tolKm {
				t.Fatalf("HaversineKm = %.2f, want %.2f ± %.2f", got, tt.wantKm, tt.tolKm)
			}
		})
	}
}

func TestScore(t *testing.T) {
	if got := Score(0); got != MaxScore {
		t.Fatalf("Score(0) = %d, want %d", got, MaxScore)
	}
	if got := Score(0.01); got != MaxScore {
		t.Fatalf("Score(10m) = %d, want %d", got, MaxScore)
	}
	if got := Score(2000); got != 1839 {
		t.Fatalf("Score(2000km) = %d, want 1839", got)
	}
	if got := Score(20000); got != 0 {
		t.Fatalf("Score(20000km) = %d, want 0", got)
	}
	// Monotonic: farther never scores higher.
	prev := Score(0)
	for d := 0.0; d < 20000; d += 37.5 {
		s := Score(d)
		if s > prev {
			t.Fatalf("score increased at %.1f km: %d > %d", d, s, prev)
		}
		prev = s
	}
}

func TestNormalizeLng(t *testing.T) {
	cases := map[float64]float64{
		0:     0,
		179:   179,
		181:   -179,
		-181:  179,
		540:   -180,
		-360:  0,
		466.8: 106.8,
	}
	for in, want := range cases {
		if got := NormalizeLng(in); math.Abs(got-want) > 1e-9 {
			t.Errorf("NormalizeLng(%v) = %v, want %v", in, got, want)
		}
	}
}
