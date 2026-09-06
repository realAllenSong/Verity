package verity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxBatchBody = 12 << 20

type HTTPConfig struct {
	APIToken       string
	AllowedOrigin  map[string]struct{}
	OpenAPIPath    string
	RequestTimeout time.Duration
}

type HTTPServer struct {
	store  *Store
	cfg    HTTPConfig
	logger *slog.Logger
}

type contextKey string

const requestIDKey contextKey = "request-id"

func NewHandler(store *Store, cfg HTTPConfig, logger *slog.Logger) http.Handler {
	server := &HTTPServer{store: store, cfg: cfg, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("GET /ready", server.ready)
	mux.HandleFunc("GET /openapi.json", server.openAPI)
	mux.HandleFunc("GET /api/v1/workspace", server.workspace)
	mux.HandleFunc("POST /api/v1/runs", server.run)
	mux.HandleFunc("POST /api/v1/datasets/{dataset_id}/batches", server.stageBatch)
	mux.HandleFunc("GET /api/v1/review-queue", server.reviewQueue)
	mux.HandleFunc("PATCH /api/v1/reviews/{record_id}", server.updateReview)
	mux.HandleFunc("GET /api/v1/stages/{stage_id}/preview", server.stagePreview)
	mux.HandleFunc("OPTIONS /{path...}", server.options)
	var handler http.Handler = mux
	handler = server.withTimeout(handler)
	handler = server.withRecovery(handler)
	handler = server.withAuth(handler)
	handler = server.withCORS(handler)
	handler = server.withAccessLog(handler)
	handler = server.withRequestID(handler)
	return handler
}

func (s *HTTPServer) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "control_plane": "go", "version": "0.2.0",
	})
}

func (s *HTTPServer) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.IsReady(); err != nil {
		s.problem(w, r, http.StatusServiceUnavailable, "not_ready", "Service is not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *HTTPServer) openAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, s.cfg.OpenAPIPath)
}

func (s *HTTPServer) workspace(w http.ResponseWriter, r *http.Request) {
	etag := s.store.ETag()
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, s.store.Workspace())
}

func (s *HTTPServer) run(w http.ResponseWriter, r *http.Request) {
	response, err := s.store.Run(r.Context())
	if err != nil {
		if errors.Is(err, ErrConflict) {
			s.problem(w, r, http.StatusConflict, "run_in_progress", "A pipeline run is already active")
			return
		}
		s.logger.Error("pipeline run failed", "request_id", requestID(r.Context()), "error", err)
		s.problem(w, r, http.StatusInternalServerError, "run_failed", "The data-plane engine did not complete")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *HTTPServer) stageBatch(w http.ResponseWriter, r *http.Request) {
	var request BatchCreate
	if err := decodeJSON(w, r, &request, maxBatchBody); err != nil {
		s.problem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := validateBatch(request); err != nil {
		s.problem(w, r, http.StatusUnprocessableEntity, "invalid_batch", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) > 200 {
		s.problem(w, r, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be 200 characters or fewer")
		return
	}
	response, duplicate, err := s.store.StageBatch(r.PathValue("dataset_id"), request, idempotencyKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.problem(w, r, http.StatusNotFound, "dataset_not_found", "Dataset not found")
			return
		}
		s.problem(w, r, http.StatusInternalServerError, "stage_failed", "The batch could not be staged")
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, response)
}

func (s *HTTPServer) reviewQueue(w http.ResponseWriter, r *http.Request) {
	total, records := s.store.ReviewQueue()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": total, "returned": len(records), "records": records,
	})
}

func (s *HTTPServer) updateReview(w http.ResponseWriter, r *http.Request) {
	var update ReviewUpdate
	if err := decodeJSON(w, r, &update, 32<<10); err != nil {
		s.problem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if update.Decision != "accepted" && update.Decision != "rejected" && update.Decision != "modified" {
		s.problem(w, r, http.StatusUnprocessableEntity, "invalid_decision", "Decision must be accepted, modified, or rejected")
		return
	}
	if len(update.Note) > 500 {
		s.problem(w, r, http.StatusUnprocessableEntity, "invalid_note", "Review note must be 500 characters or fewer")
		return
	}
	workspace, err := s.store.UpdateReview(r.PathValue("record_id"), update)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.problem(w, r, http.StatusNotFound, "review_not_found", "Review record not found")
			return
		}
		s.problem(w, r, http.StatusInternalServerError, "review_failed", "The review decision could not be saved")
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s *HTTPServer) stagePreview(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			s.problem(w, r, http.StatusUnprocessableEntity, "invalid_limit", "Limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	stageID := r.PathValue("stage_id")
	rows, err := s.store.Preview(r.Context(), stageID, limit)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.problem(w, r, http.StatusNotFound, "stage_not_found", "Pipeline stage not found")
			return
		}
		s.logger.Error("stage preview failed", "request_id", requestID(r.Context()), "error", err)
		s.problem(w, r, http.StatusInternalServerError, "preview_failed", "The stage artifact could not be read")
		return
	}
	writeJSON(w, http.StatusOK, PreviewResponse{StageID: stageID, Count: len(rows), Rows: rows})
}

func (s *HTTPServer) options(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) problem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type":       "https://verity.local/problems/" + code,
		"title":      http.StatusText(status),
		"status":     status,
		"detail":     detail,
		"request_id": requestID(r.Context()),
	})
}

func (s *HTTPServer) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" || len(id) > 128 {
			bytes := make([]byte, 12)
			if _, err := rand.Read(bytes); err != nil {
				id = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
			} else {
				id = hex.EncodeToString(bytes)
			}
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func (s *HTTPServer) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("http request",
			"request_id", requestID(r.Context()), "method", r.Method, "path", r.URL.Path,
			"status", recorder.status, "duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func (s *HTTPServer) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, allowed := s.cfg.AllowedOrigin[origin]; origin != "" && allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIToken == "" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.APIToken)) != 1 {
			s.problem(w, r, http.StatusUnauthorized, "unauthorized", "A valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "request_id", requestID(r.Context()), "panic", recovered)
				s.problem(w, r, http.StatusInternalServerError, "internal_error", "The server could not complete the request")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) withTimeout(next http.Handler) http.Handler {
	timeout := s.cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 4 * time.Minute
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validateBatch(request BatchCreate) error {
	filename := strings.TrimSpace(request.Filename)
	if filename == "" || len(filename) > 180 || filename == "." {
		return errors.New("filename must contain between 1 and 180 characters")
	}
	if len(request.Records) < 1 || len(request.Records) > 10_000 {
		return errors.New("records must contain between 1 and 10,000 items")
	}
	for index, record := range request.Records {
		if len(record) == 0 {
			return fmt.Errorf("record %d must contain at least one field", index)
		}
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON document")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
