// Package mapillary is a tiny client for the Mapillary Graph API,
// just enough to find street-level images inside a bounding box.
package mapillary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBaseURL is the Mapillary Graph API root.
const DefaultBaseURL = "https://graph.mapillary.com"

// MaxBBoxArea is the largest bbox (in square degrees) the /images endpoint accepts.
const MaxBBoxArea = 0.01

// ErrNoToken is returned when the client has no access token configured.
var ErrNoToken = errors.New("mapillary: no access token (set MAPILLARY_TOKEN)")

// BBox is a lon/lat bounding box.
type BBox struct {
	MinLng, MinLat, MaxLng, MaxLat float64
}

// Area returns the bbox area in square degrees.
func (b BBox) Area() float64 { return (b.MaxLng - b.MinLng) * (b.MaxLat - b.MinLat) }

func (b BBox) String() string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }
	return f(b.MinLng) + "," + f(b.MinLat) + "," + f(b.MaxLng) + "," + f(b.MaxLat)
}

// Image is one Mapillary image with its best-known position.
type Image struct {
	ID         string
	Lat, Lng   float64
	IsPano     bool
	CapturedAt time.Time
}

// Client calls the Mapillary Graph API.
type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

// NewClient builds a client with sane defaults.
func NewClient(token string) *Client {
	return &Client{
		Token:   token,
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

type geometry struct {
	Coordinates []float64 `json:"coordinates"` // [lng, lat]
}

type apiImage struct {
	ID               string    `json:"id"`
	ComputedGeometry *geometry `json:"computed_geometry"`
	Geometry         *geometry `json:"geometry"`
	IsPano           bool      `json:"is_pano"`
	CapturedAt       int64     `json:"captured_at"` // unix millis
}

type apiResponse struct {
	Data  []apiImage `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// ImagesInBBox lists images inside bbox. When panoOnly is true only
// 360° panoramas are returned.
func (c *Client) ImagesInBBox(ctx context.Context, bbox BBox, panoOnly bool, limit int) ([]Image, error) {
	if c.Token == "" {
		return nil, ErrNoToken
	}
	if bbox.Area() >= MaxBBoxArea {
		return nil, fmt.Errorf("mapillary: bbox area %.4f exceeds limit %.2f", bbox.Area(), MaxBBoxArea)
	}

	q := url.Values{}
	q.Set("fields", "id,computed_geometry,geometry,is_pano,captured_at")
	q.Set("bbox", bbox.String())
	q.Set("limit", strconv.Itoa(limit))
	if panoOnly {
		q.Set("is_pano", "true")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/images?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// Header auth keeps the token out of URLs and logs.
	req.Header.Set("Authorization", "OAuth "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mapillary: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("mapillary: read body: %w", err)
	}

	var out apiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("mapillary: HTTP %d, bad JSON: %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("mapillary: HTTP %d: %s (%s)", resp.StatusCode, out.Error.Message, out.Error.Type)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mapillary: HTTP %d", resp.StatusCode)
	}

	images := make([]Image, 0, len(out.Data))
	for _, d := range out.Data {
		g := d.ComputedGeometry
		if g == nil || len(g.Coordinates) < 2 {
			g = d.Geometry
		}
		if d.ID == "" || g == nil || len(g.Coordinates) < 2 {
			continue
		}
		images = append(images, Image{
			ID:         d.ID,
			Lng:        g.Coordinates[0],
			Lat:        g.Coordinates[1],
			IsPano:     d.IsPano,
			CapturedAt: time.UnixMilli(d.CapturedAt),
		})
	}
	return images, nil
}
