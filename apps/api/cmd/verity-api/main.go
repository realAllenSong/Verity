package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/realAllenSong/OurData/apps/api/internal/verity"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	repoRoot := env("VERITY_REPO_ROOT", findRepoRoot())
	artifacts := env("VERITY_ARTIFACT_ROOT", filepath.Join(repoRoot, "artifacts"))
	seed := env(
		"VERITY_SEED_WORKSPACE",
		filepath.Join(repoRoot, "apps", "web", "src", "data", "demo-workspace.json"),
	)
	engineDir := env("VERITY_ENGINE_DIR", filepath.Join(repoRoot, "apps", "engine"))
	store, err := verity.NewStore(verity.StoreConfig{
		RepoRoot: repoRoot, SeedWorkspace: seed,
		StatePath:    filepath.Join(artifacts, "control", "state.json"),
		ArtifactsDir: artifacts,
	}, verity.CommandEngine{
		RepoRoot: repoRoot, ArtifactsDir: artifacts, EngineDir: engineDir,
		Timeout: 3 * time.Minute,
	})
	if err != nil {
		logger.Error("initialize store", "error", err)
		os.Exit(1)
	}

	handler := verity.NewHandler(store, verity.HTTPConfig{
		APIToken: os.Getenv("VERITY_API_TOKEN"),
		AllowedOrigin: origins(env(
			"VERITY_ALLOWED_ORIGINS",
			"http://127.0.0.1:3000,http://localhost:3000",
		)),
		OpenAPIPath: env(
			"VERITY_OPENAPI_PATH",
			filepath.Join(repoRoot, "packages", "contracts", "openapi.json"),
		),
		RequestTimeout: 4 * time.Minute,
	}, logger)

	server := &http.Server{
		Addr:              env("VERITY_API_ADDR", "127.0.0.1:8000"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       4 * time.Minute,
		WriteTimeout:      4 * time.Minute,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("verity api listening", "address", server.Addr, "control_plane", "go")
		errCh <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server stopped", "error", err)
			os.Exit(1)
		}
	}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func origins(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func findRepoRoot() string {
	current, err := os.Getwd()
	if err != nil {
		return "."
	}
	for directory := current; ; directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "packages", "contracts", "openapi.json")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return current
		}
	}
}
