package mapillary

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImagesInBBox(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth tok" {
			t.Errorf("auth header = %q", got)
		}
		if r.URL.Query().Get("access_token") != "" {
			t.Error("token leaked into URL")
		}
		if r.URL.Query().Get("is_pano") != "true" {
			t.Error("is_pano filter missing")
		}
		if !strings.Contains(r.URL.Query().Get("fields"), "computed_geometry") {
			t.Error("fields missing computed_geometry")
		}
		w.Write([]byte(`{"data":[
			{"id":"1","computed_geometry":{"type":"Point","coordinates":[106.8,-6.2]},"is_pano":true,"captured_at":1700000000000},
			{"id":"2","geometry":{"type":"Point","coordinates":[107.6,-6.9]},"is_pano":true},
			{"id":"3"}
		]}`))
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.BaseURL = srv.URL
	imgs, err := c.ImagesInBBox(context.Background(), BBox{106.7, -6.3, 106.8, -6.2}, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("got %d images, want 2 (one has no geometry)", len(imgs))
	}
	if imgs[0].Lat != -6.2 || imgs[0].Lng != 106.8 {
		t.Fatalf("computed geometry parsed wrong: %+v", imgs[0])
	}
	if imgs[1].Lat != -6.9 {
		t.Fatalf("fallback geometry parsed wrong: %+v", imgs[1])
	}
}

func TestImagesInBBoxErrors(t *testing.T) {
	if _, err := NewClient("").ImagesInBBox(context.Background(), BBox{0, 0, 0.01, 0.01}, false, 1); !errors.Is(err, ErrNoToken) {
		t.Fatalf("no token err = %v", err)
	}
	if _, err := NewClient("t").ImagesInBBox(context.Background(), BBox{0, 0, 1, 1}, false, 1); err == nil {
		t.Fatal("expected bbox too large error")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid OAuth access token","type":"OAuthException","code":190}}`))
	}))
	defer srv.Close()
	c := NewClient("bad")
	c.BaseURL = srv.URL
	_, err := c.ImagesInBBox(context.Background(), BBox{0, 0, 0.01, 0.01}, false, 1)
	if err == nil || !strings.Contains(err.Error(), "Invalid OAuth") {
		t.Fatalf("err = %v, want API message", err)
	}
}

func TestFinderFallsBackToNonPano(t *testing.T) {
	var panoCalls, anyCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("is_pano") == "true" {
			panoCalls++
			w.Write([]byte(`{"data":[]}`))
			return
		}
		anyCalls++
		w.Write([]byte(`{"data":[{"id":"9","computed_geometry":{"coordinates":[110.36,-7.79]}}]}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	f := NewFinder(c, DefaultSeeds)
	f.Rand = rand.New(rand.NewPCG(1, 2))
	loc, err := f.Find(context.Background(), "indonesia")
	if err != nil {
		t.Fatal(err)
	}
	if loc.ImageID != "9" || loc.Country != "ID" || loc.Pano {
		t.Fatalf("loc = %+v", loc)
	}
	if panoCalls == 0 || anyCalls != 1 {
		t.Fatalf("pano calls %d, any calls %d", panoCalls, anyCalls)
	}
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
