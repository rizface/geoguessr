# Wherever

A GeoGuessr-style game that runs on your own machine and doesn't touch the Google Maps API.

You're dropped somewhere on Earth in a 360° street view. Look around, read the signs, then pin where you think you are on the map. The closer you are, the more points you get (5,000 max per round).

- **Street view:** [Mapillary](https://www.mapillary.com) imagery through [MapillaryJS](https://github.com/mapillary/mapillary-js) (free token)
- **Guess map:** [Leaflet](https://leafletjs.com) with [OpenStreetMap](https://www.openstreetmap.org) tiles (no key)
- **Sound:** synthesized in the browser with the Web Audio API (no audio files)
- **Backend:** Go standard library only; the frontend is embedded, so it's one binary

## Quick start

1. **Get a free Mapillary token.** Sign in at [mapillary.com/dashboard/developers](https://www.mapillary.com/dashboard/developers), register an app, and copy its **client token** (starts with `MLY|`).

2. **Put it in `.env`:**

   ```sh
   cp .env.example .env
   # then edit .env and paste your token
   ```

3. **Run it:**

   ```sh
   make run          # or: go run ./cmd/server
   ```

   Open <http://127.0.0.1:8080>.

The first game in a region searches Mapillary live, which takes a few seconds. Every location found is saved to `data/locations.json`, so the game gets faster the more you play.

### Optional: pre-fill locations

To make every game start instantly, curate a pool up front:

```sh
make curate                                   # 200 locations worldwide
go run ./cmd/curate -n 100 -region indonesia  # just Indonesia
go run ./cmd/curate -n 50 -any                # allow normal photos, not only 360° panoramas
```

## How to play

| Key | Action |
| --- | --- |
| Click map | Drop / move your pin |
| Space or Enter | Guess, then go to the next round |
| R | Jump back to where the round started |
| M | Mute / unmute |

Hover the small map to enlarge it. The pin icon above it keeps it large.

**Settings:** region (World, Indonesia, or a continent), 3/5/10 rounds, and an optional 30s / 60s / 2m timer. When the timer runs out, a pin you've already placed still counts.

**Scoring:** `5000 × e^(−distance / 2000 km)`. A guess within 25 m gets the full 5,000.

## How it works

```
cmd/server         HTTP server (API + embedded frontend)
cmd/curate         CLI that fills data/locations.json from Mapillary
internal/game      game sessions, rounds, guesses
internal/geo       haversine distance and scoring
internal/mapillary coverage-tile client, random-location finder, seed cities
internal/mvt       minimal Mapbox Vector Tile decoder (points + properties)
internal/pool      file-backed location pool
web/static         HTML / CSS / JS (Leaflet, MapillaryJS, Web Audio)
```

**Finding locations.** Mapillary has no "random place" endpoint, so the finder picks a random seed city (around 150 of them, in places with good coverage) and jitters to a random point within the city's radius. It then downloads the zoom-14 **coverage vector tile** under that point (about 2 km across). That's the same tile Mapillary's own map draws its green dots from, and it lists every image there with its ID, position and whether it's a 360° panorama. If the tile has no panoramas it tries another spot, and after a few misses it accepts normal photos too.

Earlier versions used the Graph API's `/images?bbox=` search instead. It runs a database query per request and regularly times out in dense cities, which made new games hang. Tiles are pre-rendered and CDN-served, so they're fast and reliable. The 50,000-tiles-per-day limit is far more than one person can use.

**No cheating via devtools.** The browser only ever receives the Mapillary image ID for the current round. The real coordinates stay on the server until you guess, and you can't request future rounds.

**API**

| Method | Path | |
| --- | --- | --- |
| GET | `/api/config` | token, regions and how many saved locations each has |
| POST | `/api/games` | `{"rounds":5,"region":"world","timeLimitSec":0}` |
| GET | `/api/games/{id}/rounds/{n}` | image ID for the current round |
| POST | `/api/games/{id}/rounds/{n}/guess` | `{"lat":..,"lng":..}`, or `{}` for a timeout |
| GET | `/api/games/{id}/summary` | every round, once the game is over |

Games live in memory and are dropped after 6 hours.

## Development

```sh
make test   # go vet + go test
make build  # ./bin/wherever and ./bin/curate
```

Flags: `-addr` (default `127.0.0.1:8080`) and `-pool` (default `data/locations.json`).

## Notes

- You still need internet: imagery and map tiles load online.
- Coverage depends on what people have uploaded to Mapillary. It's dense in Europe, North America and Japan, and thinner elsewhere. If an image fails to load, there's a **Skip round** button.
- Imagery is © Mapillary contributors, CC BY-SA. Map data is © OpenStreetMap contributors. Both are credited in the UI.
- This is meant for personal, local use. OpenStreetMap's public tile servers are fine for that, but don't use them for a deployed app with real traffic.
