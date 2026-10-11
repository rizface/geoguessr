package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rizface/geoguessr/internal/game"
	"github.com/rizface/geoguessr/internal/pool"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	p, err := pool.Open(filepath.Join(t.TempDir(), "locations.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		p.Add(pool.Location{ImageID: fmt.Sprintf("img-%d", i), Lat: -7.79, Lng: 110.36, Country: "ID", Region: "asia", Near: "Yogyakarta"})
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		Games:  game.NewService(p, nil, log),
		Pool:   p,
		Token:  "tok",
		Static: fstest.MapFS{"index.html": {Data: []byte("<html>hi</html>")}},
		Log:    log,
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func call(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

func TestGameOverHTTP(t *testing.T) {
	ts := newTestServer(t)

	var cfg configResponse
	if code := call(t, "GET", ts.URL+"/api/config", "", &cfg); code != 200 || cfg.MapillaryToken != "tok" {
		t.Fatalf("config: %d %+v", code, cfg)
	}

	var created newGameResponse
	if code := call(t, "POST", ts.URL+"/api/games", `{"rounds":2,"region":"indonesia","timeLimitSec":60}`, &created); code != 201 {
		t.Fatalf("create: %d", code)
	}
	base := ts.URL + "/api/games/" + created.ID

	// Round view must not leak coordinates.
	resp, err := http.Get(base + "/rounds/0")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "110.36") || strings.Contains(string(raw), "lat") {
		t.Fatalf("round view leaks location: %s", raw)
	}

	if code := call(t, "GET", base+"/rounds/1", "", nil); code != http.StatusConflict {
		t.Fatalf("peek ahead: %d, want 409", code)
	}

	var res game.GuessResult
	if code := call(t, "POST", base+"/rounds/0/guess", `{"lat":-7.79,"lng":110.36}`, &res); code != 200 || res.Score != 5000 {
		t.Fatalf("guess: %d %+v", code, res)
	}
	if code := call(t, "POST", base+"/rounds/1/guess", `{"lat":-7.79}`, nil); code != 400 {
		t.Fatalf("half guess: %d, want 400", code)
	}
	if code := call(t, "POST", base+"/rounds/1/guess", `{}`, &res); code != 200 || !res.TimedOut || !res.GameOver {
		t.Fatalf("timeout guess: %d %+v", code, res)
	}

	var sum game.Summary
	if code := call(t, "GET", base+"/summary", "", &sum); code != 200 || sum.TotalScore != 5000 || len(sum.Rounds) != 2 {
		t.Fatalf("summary: %d %+v", code, sum)
	}

	if code := call(t, "POST", ts.URL+"/api/games", `{"rounds":99}`, nil); code != 400 {
		t.Fatalf("bad rounds: %d", code)
	}
	if code := call(t, "GET", ts.URL+"/api/games/nope/summary", "", nil); code != 404 {
		t.Fatalf("missing game: %d", code)
	}
	if code := call(t, "GET", ts.URL+"/", "", nil); code != 200 {
		t.Fatalf("static: %d", code)
	}
}
