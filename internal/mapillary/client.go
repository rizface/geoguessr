// Package mapillary finds street-level images using Mapillary's
// coverage vector tiles.
//
// Why tiles and not the Graph API's /images?bbox= search: bbox search
// runs a database query per request and regularly times out in dense
// cities. Coverage tiles are pre-rendered and CDN-served, one request
// returns every image in a ~2 km square, and it is what Mapillary's own
// map uses.
package mapillary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rizface/geoguessr/internal/mvt"
)

// DefaultTilesURL is the coverage tile root.
const DefaultTilesURL = "https://tiles.mapillary.com/maps/vtp/mly1_public/2"

// ImageZoom is the only zoom level whose tiles carry individual images.
const ImageZoom = 14

// ErrNoToken is returned when the client has no access token configured.
var ErrNoToken = errors.New("mapillary: no access token (set MAPILLARY_TOKEN)")

// Image is one Mapillary image with its position.
type Image struct {
	ID         string
	Lat, Lng   float64
	IsPano     bool
	CapturedAt time.Time
}

// Client fetches Mapillary coverage tiles.
type Client struct {
	Token    string
	TilesURL string
	HTTP     *http.Client
}

// NewClient builds a client with sane defaults.
func NewClient(token string) *Client {
	return &Client{
		Token:    token,
		TilesURL: DefaultTilesURL,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
	}
}

// ImagesInTile returns every image in z14 tile (x, y). An empty slice
// means the tile has no coverage.
func (c *Client) ImagesInTile(ctx context.Context, x, y int) ([]Image, error) {
	if c.Token == "" {
		return nil, ErrNoToken
	}
	// Tiles take the token as a query parameter (per Mapillary's docs);
	// it never leaves this process except to Mapillary.
	u := fmt.Sprintf("%s/%d/%d/%d?access_token=%s", c.TilesURL, ImageZoom, x, y, url.QueryEscape(c.Token))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Strip the URL (and token) from transport errors.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("mapillary: tile %d/%d after %s: %w", x, y, time.Since(start).Round(time.Millisecond), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("mapillary: tile %d/%d: read: %w", x, y, err)
	}
	switch {
	case resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound:
		return nil, nil // no coverage
	case resp.StatusCode != http.StatusOK:
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("mapillary: tile %d/%d: HTTP %d: %s", x, y, resp.StatusCode, msg)
	}

	layer, err := mvt.DecodeLayer(body, "image")
	if err != nil {
		return nil, fmt.Errorf("mapillary: tile %d/%d: %w", x, y, err)
	}
	if layer == nil {
		return nil, nil
	}

	images := make([]Image, 0, len(layer.Features))
	for _, f := range layer.Features {
		id := idString(f.Props["id"])
		if id == "" || len(f.Points) == 0 {
			continue
		}
		p := f.Points[0]
		// Skip points in the tile's buffer zone; they belong to a neighbour.
		if p[0] < 0 || p[1] < 0 || p[0] > float64(layer.Extent) || p[1] > float64(layer.Extent) {
			continue
		}
		lng, lat := mvt.TileToLngLat(ImageZoom, x, y, p[0], p[1], layer.Extent)
		pano, _ := f.Props["is_pano"].(bool)
		images = append(images, Image{
			ID:         id,
			Lat:        lat,
			Lng:        lng,
			IsPano:     pano,
			CapturedAt: time.UnixMilli(toInt64(f.Props["captured_at"])),
		})
	}
	return images, nil
}

// The docs disagree on whether ids are numbers or strings; accept both.
func idString(v any) string {
	switch id := v.(type) {
	case string:
		return id
	case int64:
		return strconv.FormatInt(id, 10)
	case uint64:
		return strconv.FormatUint(id, 10)
	case float64:
		return strconv.FormatFloat(id, 'f', 0, 64)
	}
	return ""
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
