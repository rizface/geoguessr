// Command curate pre-fills the location pool from Mapillary so games
// start instantly. Run it once (or whenever you want fresh places):
//
//	go run ./cmd/curate -n 200
//	go run ./cmd/curate -n 100 -region indonesia
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"

	"github.com/rizface/geoguessr/internal/envfile"
	"github.com/rizface/geoguessr/internal/mapillary"
	"github.com/rizface/geoguessr/internal/pool"
)

func main() {
	n := flag.Int("n", 100, "how many new locations to add")
	region := flag.String("region", "world", "region to curate (world, indonesia, asia, europe, north-america, south-america, africa, oceania)")
	poolPath := flag.String("pool", "data/locations.json", "location pool file")
	workers := flag.Int("workers", 6, "parallel searches")
	anyImage := flag.Bool("any", false, "accept normal photos too, not just 360° panoramas (more coverage, less fun)")
	flag.Parse()

	if err := run(*n, *region, *poolPath, *workers, *anyImage); err != nil {
		fmt.Fprintln(os.Stderr, "curate:", err)
		os.Exit(1)
	}
}

func run(n int, region, poolPath string, workers int, anyImage bool) error {
	_ = envfile.Load(".env")
	token := os.Getenv("MAPILLARY_TOKEN")
	if token == "" {
		return errors.New("MAPILLARY_TOKEN is not set (put it in .env)")
	}
	if !mapillary.ValidRegion(region) {
		return fmt.Errorf("unknown region %q", region)
	}

	p, err := pool.Open(poolPath)
	if err != nil {
		return err
	}
	fmt.Printf("pool has %d locations (%d in %s). finding %d more...\n", p.Len(), p.CountRegion(region), region, n)

	finder := mapillary.NewFinder(mapillary.NewClient(token), mapillary.DefaultSeeds)
	finder.Log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	if !anyImage {
		finder.PanoShare = 1 // panoramas only
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var (
		mu       sync.Mutex
		added    int
		misses   int
		wg       sync.WaitGroup
		maxMiss  = n * 3
		firstErr error
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				done := added >= n || misses >= maxMiss || ctx.Err() != nil
				mu.Unlock()
				if done {
					return
				}
				loc, err := finder.Find(ctx, region)
				mu.Lock()
				switch {
				case errors.Is(err, mapillary.ErrNoToken):
					firstErr = err
					misses = maxMiss
				case err != nil:
					misses++
					if firstErr == nil {
						firstErr = err
					}
				case p.Add(loc):
					added++
					fmt.Printf("\r  %d/%d  (latest: near %s)          ", added, n, loc.Near)
					if added%10 == 0 {
						_ = p.Save()
					}
				default:
					misses++ // duplicate
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Println()

	if err := p.Save(); err != nil {
		return fmt.Errorf("save pool: %w", err)
	}
	fmt.Printf("added %d locations, pool now has %d (%s)\n", added, p.Len(), poolPath)
	if added == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}
