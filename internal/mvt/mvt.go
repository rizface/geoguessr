// Package mvt decodes Mapbox Vector Tiles, just far enough to read the
// point features (and their properties) of one layer.
//
// Spec: https://github.com/mapbox/vector-tile-spec/tree/master/2.1
//
// A tile is a protobuf message:
//
//	Tile    { repeated Layer layers = 3; }
//	Layer   { string name = 1; repeated Feature features = 2;
//	          repeated string keys = 3; repeated Value values = 4;
//	          uint32 extent = 5; uint32 version = 15; }
//	Feature { uint64 id = 1; repeated uint32 tags = 2 [packed];
//	          GeomType type = 3; repeated uint32 geometry = 4 [packed]; }
//	Value   { string=1 | float=2 | double=3 | int64=4 | uint64=5 | sint64=6 | bool=7 }
package mvt

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Geometry types from the spec.
const (
	GeomUnknown    = 0
	GeomPoint      = 1
	GeomLineString = 2
	GeomPolygon    = 3
)

// Feature is one feature with its properties and, for point features,
// its positions in tile pixel coordinates (0..Extent).
type Feature struct {
	ID     uint64
	Type   int
	Props  map[string]any
	Points [][2]float64
}

// Layer is a decoded layer.
type Layer struct {
	Name     string
	Extent   uint32
	Features []Feature
}

var errTruncated = errors.New("mvt: truncated data")

// DecodeLayer finds the layer called name and decodes it. Returns
// (nil, nil) when the tile has no such layer, which is normal for
// tiles with no imagery. Gzip-compressed tiles are handled transparently.
func DecodeLayer(data []byte, name string) (*Layer, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("mvt: gzip: %w", err)
		}
		data, err = io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("mvt: gzip: %w", err)
		}
	}

	r := reader{buf: data}
	for !r.done() {
		field, wire, err := r.key()
		if err != nil {
			return nil, err
		}
		if field == 3 && wire == wireBytes {
			msg, err := r.bytes()
			if err != nil {
				return nil, err
			}
			// Peek at the name before doing the full decode.
			if layerName(msg) != name {
				continue
			}
			return decodeLayer(msg)
		}
		if err := r.skip(wire); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func layerName(msg []byte) string {
	r := reader{buf: msg}
	for !r.done() {
		field, wire, err := r.key()
		if err != nil {
			return ""
		}
		if field == 1 && wire == wireBytes {
			b, err := r.bytes()
			if err != nil {
				return ""
			}
			return string(b)
		}
		if r.skip(wire) != nil {
			return ""
		}
	}
	return ""
}

type rawFeature struct {
	id   uint64
	typ  int
	tags []uint32
	geom []uint32
}

func decodeLayer(msg []byte) (*Layer, error) {
	l := &Layer{Extent: 4096} // spec default
	var (
		keys   []string
		values []any
		raws   []rawFeature
	)
	r := reader{buf: msg}
	for !r.done() {
		field, wire, err := r.key()
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			l.Name = string(b)
		case field == 2 && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			f, err := decodeFeature(b)
			if err != nil {
				return nil, err
			}
			raws = append(raws, f)
		case field == 3 && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			keys = append(keys, string(b))
		case field == 4 && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			v, err := decodeValue(b)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		case field == 5 && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return nil, err
			}
			l.Extent = uint32(v)
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}

	// Keys and values can appear after features, so resolve tags last.
	for _, rf := range raws {
		f := Feature{ID: rf.id, Type: rf.typ, Props: make(map[string]any, len(rf.tags)/2)}
		for i := 0; i+1 < len(rf.tags); i += 2 {
			k, v := int(rf.tags[i]), int(rf.tags[i+1])
			if k < len(keys) && v < len(values) {
				f.Props[keys[k]] = values[v]
			}
		}
		if rf.typ == GeomPoint {
			pts, err := decodePoints(rf.geom)
			if err != nil {
				return nil, err
			}
			f.Points = pts
		}
		l.Features = append(l.Features, f)
	}
	return l, nil
}

func decodeFeature(msg []byte) (rawFeature, error) {
	var f rawFeature
	r := reader{buf: msg}
	for !r.done() {
		field, wire, err := r.key()
		if err != nil {
			return f, err
		}
		switch {
		case field == 1 && wire == wireVarint:
			f.id, err = r.varint()
		case field == 2 && wire == wireBytes:
			f.tags, err = r.packedUint32()
		case field == 3 && wire == wireVarint:
			var v uint64
			v, err = r.varint()
			f.typ = int(v)
		case field == 4 && wire == wireBytes:
			f.geom, err = r.packedUint32()
		default:
			err = r.skip(wire)
		}
		if err != nil {
			return f, err
		}
	}
	return f, nil
}

// decodePoints runs the geometry command stream for a point feature.
// Points use only MoveTo commands; each carries a zigzag-encoded delta
// from the previous cursor position.
func decodePoints(geom []uint32) ([][2]float64, error) {
	var (
		pts  [][2]float64
		x, y int64
	)
	for i := 0; i < len(geom); {
		cmd := geom[i] & 0x7
		count := int(geom[i] >> 3)
		i++
		switch cmd {
		case 1, 2: // MoveTo, LineTo: count pairs of params
			if i+2*count > len(geom) {
				return nil, errTruncated
			}
			for j := 0; j < count; j++ {
				x += unzigzag(geom[i])
				y += unzigzag(geom[i+1])
				i += 2
				if cmd == 1 {
					pts = append(pts, [2]float64{float64(x), float64(y)})
				}
			}
		case 7: // ClosePath: no params
		default:
			return nil, fmt.Errorf("mvt: unknown geometry command %d", cmd)
		}
	}
	return pts, nil
}

func decodeValue(msg []byte) (any, error) {
	r := reader{buf: msg}
	var out any
	for !r.done() {
		field, wire, err := r.key()
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireBytes:
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			out = string(b)
		case field == 2 && wire == wireFixed32:
			v, err := r.fixed32()
			if err != nil {
				return nil, err
			}
			out = float64(math.Float32frombits(v))
		case field == 3 && wire == wireFixed64:
			v, err := r.fixed64()
			if err != nil {
				return nil, err
			}
			out = math.Float64frombits(v)
		case (field == 4 || field == 5 || field == 6 || field == 7) && wire == wireVarint:
			v, err := r.varint()
			if err != nil {
				return nil, err
			}
			switch field {
			case 4:
				out = int64(v)
			case 5:
				out = v
			case 6:
				out = unzigzag64(v)
			case 7:
				out = v != 0
			}
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func unzigzag(n uint32) int64   { return int64(int32(n>>1) ^ -int32(n&1)) }
func unzigzag64(n uint64) int64 { return int64(n>>1) ^ -int64(n&1) }

// ---- minimal protobuf reader ----

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

type reader struct {
	buf []byte
	pos int
}

func (r *reader) done() bool { return r.pos >= len(r.buf) }

func (r *reader) varint() (uint64, error) {
	v, n := binary.Uvarint(r.buf[r.pos:])
	if n <= 0 {
		return 0, errTruncated
	}
	r.pos += n
	return v, nil
}

func (r *reader) key() (field int, wire int, err error) {
	v, err := r.varint()
	if err != nil {
		return 0, 0, err
	}
	return int(v >> 3), int(v & 7), nil
}

func (r *reader) bytes() ([]byte, error) {
	n, err := r.varint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.buf)-r.pos) {
		return nil, errTruncated
	}
	b := r.buf[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return b, nil
}

func (r *reader) fixed32() (uint32, error) {
	if len(r.buf)-r.pos < 4 {
		return 0, errTruncated
	}
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *reader) fixed64() (uint64, error) {
	if len(r.buf)-r.pos < 8 {
		return 0, errTruncated
	}
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *reader) packedUint32() ([]uint32, error) {
	b, err := r.bytes()
	if err != nil {
		return nil, err
	}
	sub := reader{buf: b}
	var out []uint32
	for !sub.done() {
		v, err := sub.varint()
		if err != nil {
			return nil, err
		}
		out = append(out, uint32(v))
	}
	return out, nil
}

func (r *reader) skip(wire int) error {
	switch wire {
	case wireVarint:
		_, err := r.varint()
		return err
	case wireFixed64:
		_, err := r.fixed64()
		return err
	case wireBytes:
		_, err := r.bytes()
		return err
	case wireFixed32:
		_, err := r.fixed32()
		return err
	default:
		return fmt.Errorf("mvt: unsupported wire type %d", wire)
	}
}

// TileToLngLat converts a position inside tile (z, x, y) — in pixel
// units out of extent — to longitude/latitude (Web Mercator).
func TileToLngLat(z, x, y int, px, py float64, extent uint32) (lng, lat float64) {
	n := math.Exp2(float64(z))
	wx := (float64(x) + px/float64(extent)) / n
	wy := (float64(y) + py/float64(extent)) / n
	lng = wx*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*wy))) * 180 / math.Pi
	return lng, lat
}

// LngLatToTile returns the tile containing (lng, lat) at zoom z.
func LngLatToTile(lng, lat float64, z int) (x, y int) {
	n := math.Exp2(float64(z))
	lat = math.Max(-85.0511, math.Min(85.0511, lat))
	latRad := lat * math.Pi / 180
	x = int(math.Floor((lng + 180) / 360 * n))
	y = int(math.Floor((1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * n))
	last := int(n) - 1
	x = min(max(x, 0), last)
	y = min(max(y, 0), last)
	return x, y
}
