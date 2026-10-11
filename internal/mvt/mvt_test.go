package mvt

import (
	"math"
	"os"
	"testing"
)

// testdata/image_layer.pbf was encoded by Python's mapbox-vector-tile
// (an independent implementation) with a "sequence" line layer and an
// "image" point layer shaped like Mapillary's coverage tiles.

func TestDecodeImageLayer(t *testing.T) {
	for _, file := range []string{"testdata/image_layer.pbf", "testdata/image_layer.pbf.gz"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			l, err := DecodeLayer(data, "image")
			if err != nil {
				t.Fatal(err)
			}
			if l == nil || l.Name != "image" || l.Extent != 4096 {
				t.Fatalf("layer = %+v", l)
			}
			if len(l.Features) != 3 {
				t.Fatalf("features = %d, want 3", len(l.Features))
			}

			f0 := l.Features[0]
			if f0.Type != GeomPoint || len(f0.Points) != 1 || f0.Points[0] != [2]float64{100, 200} {
				t.Fatalf("f0 geometry = %v %v", f0.Type, f0.Points)
			}
			if f0.Props["id"] != int64(1234567890123) && f0.Props["id"] != uint64(1234567890123) {
				t.Fatalf("f0 id = %#v", f0.Props["id"])
			}
			if f0.Props["is_pano"] != true || f0.Props["sequence_id"] != "abc" {
				t.Fatalf("f0 props = %#v", f0.Props)
			}
			if q, ok := f0.Props["quality_score"].(float64); !ok || math.Abs(q-0.75) > 1e-6 {
				t.Fatalf("f0 quality = %#v", f0.Props["quality_score"])
			}

			f1 := l.Features[1]
			if f1.Points[0] != [2]float64{4000, 50} || f1.Props["id"] != "998877" || f1.Props["is_pano"] != false {
				t.Fatalf("f1 = %+v", f1)
			}

			// Points may sit in the tile buffer, outside 0..extent.
			f2 := l.Features[2]
			if f2.Points[0] != [2]float64{-5, 4100} {
				t.Fatalf("f2 points = %v", f2.Points)
			}
			if c, ok := f2.Props["compass_angle"].(float64); !ok || c != -12.5 {
				t.Fatalf("f2 compass = %#v", f2.Props["compass_angle"])
			}
		})
	}
}

func TestDecodeMissingLayer(t *testing.T) {
	data, _ := os.ReadFile("testdata/image_layer.pbf")
	l, err := DecodeLayer(data, "nope")
	if err != nil || l != nil {
		t.Fatalf("got %v, %v; want nil, nil", l, err)
	}
	if l, err := DecodeLayer(nil, "image"); err != nil || l != nil {
		t.Fatalf("empty tile: %v, %v", l, err)
	}
}

func TestDecodeGarbage(t *testing.T) {
	data, _ := os.ReadFile("testdata/image_layer.pbf")
	// Every truncation must fail cleanly or decode, never panic.
	for i := 0; i < len(data); i++ {
		_, _ = DecodeLayer(data[:i], "image")
	}
	if _, err := DecodeLayer([]byte("<html>not a tile</html>"), "image"); err == nil {
		t.Log("html body decoded without error (acceptable as long as no panic)")
	}
}

func TestTileMath(t *testing.T) {
	// Jakarta, Monas.
	lng, lat := 106.8272, -6.1754
	x, y := LngLatToTile(lng, lat, 14)
	if x != 13053 || y != 8473 {
		t.Fatalf("tile = %d/%d, want 13053/8473", x, y)
	}
	// Top-left corner of that tile must be north-west of the point,
	// bottom-right corner south-east.
	l0, t0 := TileToLngLat(14, x, y, 0, 0, 4096)
	l1, t1 := TileToLngLat(14, x, y, 4096, 4096, 4096)
	if !(l0 <= lng && lng < l1 && t1 < lat && lat <= t0) {
		t.Fatalf("point %v,%v not inside tile [%v,%v]..[%v,%v]", lng, lat, l0, t0, l1, t1)
	}
	// Round trip through the tile centre.
	cl, ct := TileToLngLat(14, x, y, 2048, 2048, 4096)
	if bx, by := LngLatToTile(cl, ct, 14); bx != x || by != y {
		t.Fatalf("round trip %d/%d", bx, by)
	}
	// Clamping at the edges.
	if x, y := LngLatToTile(180, -89, 2); x != 3 || y != 3 {
		t.Fatalf("edge tile = %d/%d", x, y)
	}
}
