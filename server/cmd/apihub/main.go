package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/elykia/apihub/server/internal/app"
	"github.com/elykia/apihub/server/internal/config"
	"github.com/elykia/apihub/server/internal/webui"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("starting APIHub", "version", version, "revision", revision)
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime, err := app.Build(root, cfg, logger, webui.FS())
	if err != nil {
		logger.Error("failed to build application", "error", err)
		os.Exit(1)
	}
	serverErrors := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				serverErrors <- fmt.Errorf("HTTP server panic: %v", recovered)
			}
		}()
		serverErrors <- runtime.Server.ListenAndServe()
	}()
	var serveErr error
	select {
	case <-root.Done():
		logger.Info("shutting down", "signal", "context canceled")
	case err := <-serverErrors:
		serveErr = err
		if err == nil || err == http.ErrServerClosed {
			serveErr = fmt.Errorf("HTTP server stopped unexpectedly")
		}
		logger.Error("HTTP server failed", "error", serveErr)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
	if serveErr != nil {
		os.Exit(1)
	}
}

func healthcheck() error {
	target, err := healthcheckTarget(os.LookupEnv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return fmt.Errorf("create healthcheck request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("healthcheck request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("healthcheck returned HTTP %d", response.StatusCode)
	}
	return nil
}

func healthcheckTarget(lookup func(string) (string, bool)) (string, error) {
	if target, _ := lookup("HEALTHCHECK_URL"); target != "" {
		return target, nil
	}
	host, present := lookup("HOST")
	host = strings.TrimSpace(host)
	if !present || host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	port, err := config.ParsePort(lookup("PORT"))
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprint(port)), Path: "/health/ready"}).String(), nil
}
