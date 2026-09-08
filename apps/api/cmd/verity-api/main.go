package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		healthcheck()
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	repoRoot := env("VERITY_REPO_ROOT", findRepoRoot())
	artifacts := env("VERITY_ARTIFACT_ROOT", filepath.Join(repoRoot, "artifacts"))
	seed := env(
		"VERITY_SEED_WORKSPACE",
		filepath.Join(repoRoot, "apps", "web", "src", "data", "demo-workspace.json"),
	)
	engine, closeEngine, err := configuredEngine(repoRoot, artifacts)
	if err != nil {
		logger.Error("initialize engine", "error", err)
		os.Exit(1)
	}
	defer closeEngine()
	maxUploadBytes, err := configuredByteLimit("VERITY_MAX_UPLOAD_BYTES", 5<<30)
	if err != nil {
		logger.Error("configure upload limit", "error", err)
		os.Exit(1)
	}
	store, err := verity.NewStore(verity.StoreConfig{
		RepoRoot: repoRoot, SeedWorkspace: seed,
		StatePath:      filepath.Join(artifacts, "control", "state.json"),
		ArtifactsDir:   artifacts,
		MaxUploadBytes: maxUploadBytes,
	}, engine)
	if err != nil {
		logger.Error("initialize store", "error", err)
		os.Exit(1)
	}

	airbyte, err := configuredAirbyte()
	if err != nil {
		logger.Error("initialize Airbyte adapter", "error", err)
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
		Airbyte:        airbyte,
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

func configuredEngine(repoRoot, artifacts string) (verity.Engine, func(), error) {
	rawDir := env("VERITY_RAW_DIR", filepath.Join(repoRoot, "sample_data", "raw"))
	switch strings.ToLower(env("VERITY_ORCHESTRATOR", "local")) {
	case "local":
		return verity.LocalEngine{RepoRoot: repoRoot, ArtifactsDir: artifacts, RawDir: rawDir}, func() {}, nil
	case "temporal":
		engine, err := verity.NewTemporalEngine(verity.TemporalConfig{
			Address:   env("VERITY_TEMPORAL_ADDRESS", "127.0.0.1:7233"),
			Namespace: env("VERITY_TEMPORAL_NAMESPACE", "default"),
			TaskQueue: env("VERITY_TEMPORAL_TASK_QUEUE", verity.DefaultTemporalTaskQueue),
			RepoRoot:  repoRoot, ArtifactsDir: artifacts, RawDir: rawDir,
		})
		if err != nil {
			return nil, func() {}, err
		}
		return engine, engine.Close, nil
	default:
		return nil, func() {}, fmt.Errorf("VERITY_ORCHESTRATOR must be local or temporal")
	}
}

func configuredAirbyte() (*verity.AirbyteClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("VERITY_AIRBYTE_BASE_URL"))
	if baseURL == "" {
		return nil, nil
	}
	return verity.NewAirbyteClient(baseURL, os.Getenv("VERITY_AIRBYTE_TOKEN"), nil)
}

func healthcheck() {
	address := env("VERITY_HEALTHCHECK_URL", "http://127.0.0.1:8000/ready")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "readiness returned %s\n", response.Status)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func configuredByteLimit(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer number of bytes", name)
	}
	return parsed, nil
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
