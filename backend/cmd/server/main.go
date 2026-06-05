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

	"foundry-tx-simulator/backend/internal/config"
	"foundry-tx-simulator/backend/internal/httpapi"
)

func main() {
	cfg, configPath, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	server := httpapi.NewServer(cfg, configPath)
	defer server.Close()
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("foundry tx simulator backend listening", "url", "http://"+cfg.ListenAddr)
	slog.Info("config loaded", "path", configPath)
	slog.Info(
		"simulation workers configured",
		"max_concurrent_runs", cfg.MaxConcurrent,
		"forge_bin", cfg.ForgeBin,
		"cast_bin", cfg.CastBin,
		"anvil_bin", cfg.AnvilBin,
		"anvil_host", cfg.AnvilHost,
		"anvil_port_start", cfg.AnvilPortStart,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown server", "error", err)
			os.Exit(1)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}
	if ctx.Err() != nil {
		slog.Info("server stopped")
	}
}
