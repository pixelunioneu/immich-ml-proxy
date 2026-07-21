// Command proxy runs immich-ml-proxy: a reverse proxy that routes
// immich-machine-learning /predict requests to either a GPU or an OCR
// (CPU) backend based on the request's top-level task key.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/pixelunioneu/immich-ml-proxy/internal/config"
	"github.com/pixelunioneu/immich-ml-proxy/internal/proxy"
	"github.com/pixelunioneu/immich-ml-proxy/internal/router"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	r := router.New(cfg.OCRTaskKeys)
	handler := proxy.New(proxy.Config{
		Router:            r,
		DefaultBackendURL: cfg.DefaultBackendURL,
		OCRBackendURL:     cfg.OCRBackendURL,
		MaxBodyBytes:      cfg.MaxBodyBytes,
		RequestTimeout:    cfg.RequestTimeout,
		Logger:            logger,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", proxy.Healthz)
	mux.HandleFunc("/readyz", proxy.Readyz)
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", handler)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("starting immich-ml-proxy",
		slog.String("listen_addr", cfg.ListenAddr),
		slog.String("default_backend", cfg.DefaultBackendURL.String()),
		slog.String("ocr_backend", cfg.OCRBackendURL.String()),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		logger.Info("shutting down", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(ctx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
