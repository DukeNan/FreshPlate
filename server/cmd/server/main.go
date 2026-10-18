package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"freshplate/server/internal/api"
	"freshplate/server/internal/logic"
	appRuntime "freshplate/server/internal/runtime"
	"freshplate/server/internal/store"
)

func run() error {
	cfg := appRuntime.Flags(flag.CommandLine)
	flag.Parse()
	if err := cfg.Validate(); err != nil {
		return err
	}
	lock, err := appRuntime.Lock(cfg.Database)
	if err != nil {
		return err
	}
	defer appRuntime.Unlock(lock)
	key, err := appRuntime.Key(cfg.Database, cfg.KeyFile)
	if err != nil {
		return err
	}
	cfg.JWTKey, err = appRuntime.JWTKey(cfg.Database, cfg.JWTKeyFile)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()
	if err = os.Chmod(cfg.Database, 0600); err != nil {
		return err
	}
	service, err := logic.New(st)
	if err != nil {
		return err
	}
	handler, err := api.New(service, *cfg, key)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", handler)
	mux.Handle("/swagger/", handler)
	mux.Handle("/", appRuntime.Pages(*cfg))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = st.CleanupSessions(ctx, time.Now()); err != nil {
		return err
	}
	cleanupDone := make(chan struct{})
	defer func() { stop(); <-cleanupDone }()
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if e := st.CleanupSessions(ctx, now); e != nil {
					log.Printf("session cleanup failed (%T)", e)
				}
			}
		}
	}()
	srv := &http.Server{Addr: cfg.Address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { log.Printf("FreshPlate listening on %s", cfg.Address); done <- srv.ListenAndServe() }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
