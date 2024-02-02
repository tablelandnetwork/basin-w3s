package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"golang.org/x/exp/slog"
)

func main() {
	cfg, err := initConfig()
	if err != nil {
		slog.Error("failed to init config", err)
		os.Exit(1)
	}

	handlers, err := initHandlers(cfg)
	if err != nil {
		slog.Error("failed to init handlers", err)
		os.Exit(1)
	}

	router := newRouter()
	router.post("/api/v1/upload", handlers.Upload)
	router.get("/api/v1/health", handlers.Health)

	srv := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%s", cfg.HTTP.Port),
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      router.r,
	}

	// Run our server in a goroutine so that it doesn't block.
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("failed to start HTTP server", err)
		}
	}()

	slog.Info("server running", "port", cfg.HTTP.Port)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
	os.Exit(0)
}
