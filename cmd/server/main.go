// Command server runs the game locally: API + embedded frontend.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rizface/geoguessr/internal/envfile"
	"github.com/rizface/geoguessr/internal/game"
	"github.com/rizface/geoguessr/internal/mapillary"
	"github.com/rizface/geoguessr/internal/pool"
	"github.com/rizface/geoguessr/internal/server"
	"github.com/rizface/geoguessr/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	poolPath := flag.String("pool", "data/locations.json", "location pool file")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := envfile.Load(".env"); err != nil {
		log.Warn("could not read .env", "err", err)
	}

	p, err := pool.Open(*poolPath)
	if err != nil {
		log.Error("open pool", "err", err)
		os.Exit(1)
	}

	token := os.Getenv("MAPILLARY_TOKEN")
	var finder game.Finder
	if token != "" {
		f := mapillary.NewFinder(mapillary.NewClient(token), mapillary.DefaultSeeds)
		f.Log = log
		finder = f
	} else {
		log.Warn("MAPILLARY_TOKEN not set: the street view won't load. Get a free token at https://www.mapillary.com/dashboard/developers")
	}

	srv := &server.Server{
		Games:  game.NewService(p, finder, log),
		Pool:   p,
		Token:  token,
		Static: web.Static(),
		Log:    log,
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("geoguessr is up", "url", "http://"+*addr, "pool", p.Len(), "liveSearch", finder != nil)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Info("bye")
}
