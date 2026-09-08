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
	"path/filepath"
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
	Airbyte        *AirbyteClient
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
	mux.HandleFunc("POST /api/v1/imports", server.createImport)
	mux.HandleFunc("HEAD /api/v1/uploads/{upload_id}", server.uploadOffset)
	mux.HandleFunc("PATCH /api/v1/uploads/{upload_id}", server.appendUpload)
	mux.HandleFunc("POST /api/v1/imports/{import_id}/complete", server.completeImport)
	mux.HandleFunc("GET /api/v1/jobs/{job_id}", server.job)
	mux.HandleFunc("GET /api/v1/jobs/{job_id}/events", server.jobEvents)
	mux.HandleFunc("POST /api/v1/runs", server.run)
	mux.HandleFunc("POST /api/v1/datasets/{dataset_id}/batches", server.stageBatch)
	mux.HandleFunc("GET /api/v1/review-queue", server.reviewQueue)
	mux.HandleFunc("PATCH /api/v1/reviews/{record_id}", server.updateReview)
	mux.HandleFunc("GET /api/v1/stages/{stage_id}/preview", server.stagePreview)
	mux.HandleFunc("GET /api/v1/stages/{stage_id}/records", server.stageRecords)
	mux.HandleFunc("GET /api/v1/stages/{stage_id}/comparison", server.stageComparison)
	mux.HandleFunc("GET /api/v1/outputs/{output_id}", server.downloadOutput)
	mux.HandleFunc("POST /api/v1/integrations/airbyte/syncs", server.triggerAirbyteSync)
	mux.HandleFunc("GET /api/v1/integrations/airbyte/jobs/{job_id}", server.airbyteJob)
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

func (s *HTTPServer) createImport(w http.ResponseWriter, r *http.Request) {
	var request ImportCreate
	if err := decodeJSON(w, r, &request, 64<<10); err != nil {
		s.problem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	created, _, err := s.store.CreateImport(request, r.Header.Get("Idempotency-Key"))
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			s.problem(w, r, http.StatusNotFound, "dataset_not_found", "Dataset not found")
		case errors.Is(err, ErrUploadTooLarge):
			s.problem(w, r, http.StatusRequestEntityTooLarge, "upload_too_large", err.Error())
		default:
			s.problem(w, r, http.StatusUnprocessableEntity, "invalid_import", err.Error())
		}
		return
	}
	w.Header().Set("Location", created.UploadURL)
	writeJSON(w, http.StatusAccepted, created)
}

func (s *HTTPServer) uploadOffset(w http.ResponseWriter, r *http.Request) {
	item, ok := s.store.ImportByUpload(r.PathValue("upload_id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, "upload_not_found", "Upload not found")
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(item.Offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(item.SizeBytes, 10))
	w.Header().Set("Upload-State", string(item.State))
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) appendUpload(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("Upload-Offset")), 10, 64)
	if err != nil || offset < 0 {
		s.problem(w, r, http.StatusBadRequest, "invalid_upload_offset", "Upload-Offset must be a non-negative integer")
		return
	}
	next, err := s.store.AppendUpload(r.PathValue("upload_id"), offset, r.Body, r.ContentLength)
	if err != nil {
		w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
		switch {
		case errors.Is(err, ErrNotFound):
			s.problem(w, r, http.StatusNotFound, "upload_not_found", "Upload not found")
		case errors.Is(err, ErrUploadOffset), errors.Is(err, ErrConflict):
			s.problem(w, r, http.StatusConflict, "upload_offset_conflict", "Resume from the acknowledged Upload-Offset")
		case errors.Is(err, ErrUploadTooLarge):
			s.problem(w, r, http.StatusRequestEntityTooLarge, "upload_too_large", err.Error())
		default:
			s.problem(w, r, http.StatusInternalServerError, "upload_failed", "The upload chunk could not be committed")
		}
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) completeImport(w http.ResponseWriter, r *http.Request) {
	job, _, err := s.store.CompleteImport(r.PathValue("import_id"), r.Header.Get("Idempotency-Key"))
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			s.problem(w, r, http.StatusNotFound, "import_not_found", "Import not found")
		case errors.Is(err, ErrUploadIncomplete), errors.Is(err, ErrConflict):
			s.problem(w, r, http.StatusConflict, "upload_incomplete", "Upload must be complete before processing starts")
		default:
			s.problem(w, r, http.StatusUnprocessableEntity, "import_not_ready", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *HTTPServer) job(w http.ResponseWriter, r *http.Request) {
	job, ok := s.store.Job(r.PathValue("job_id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, "job_not_found", "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *HTTPServer) jobEvents(w http.ResponseWriter, r *http.Request) {
	after := uint64(0)
	if value := strings.TrimSpace(r.Header.Get("Last-Event-ID")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			s.problem(w, r, http.StatusBadRequest, "invalid_event_cursor", "Last-Event-ID must be an integer")
			return
		}
		after = parsed
	}
	events, ok := s.store.JobEvents(r.PathValue("job_id"), after)
	if !ok {
		s.problem(w, r, http.StatusNotFound, "job_not_found", "Job not found")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	for _, event := range events {
		payload, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.EventType, payload)
	}
}

func (s *HTTPServer) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "control_plane": "go", "data_plane": "go", "version": "0.3.0",
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
	total, records, err := s.store.ReviewPage(r.Context(), 50)
	if err != nil {
		s.problem(w, r, http.StatusInternalServerError, "review_unavailable", "The review queue could not be read")
		return
	}
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

func (s *HTTPServer) stageRecords(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			s.problem(w, r, http.StatusUnprocessableEntity, "invalid_limit", "Limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	page, err := s.store.StageRecords(r.Context(), r.PathValue("stage_id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.problem(w, r, http.StatusNotFound, "stage_not_found", "Pipeline stage not found")
			return
		}
		s.problem(w, r, http.StatusUnprocessableEntity, "invalid_stage_page", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *HTTPServer) downloadOutput(w http.ResponseWriter, r *http.Request) {
	path, output, err := s.store.OutputPath(r.PathValue("output_id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.problem(w, r, http.StatusNotFound, "output_not_found", "Output not found")
			return
		}
		s.problem(w, r, http.StatusInternalServerError, "output_unavailable", "Output could not be opened")
		return
	}
	checksum, err := fileSHA256(path)
	if err != nil {
		s.problem(w, r, http.StatusInternalServerError, "output_unavailable", "Output checksum could not be verified")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	w.Header().Set("X-Verity-Output-ID", output.ID)
	w.Header().Set("X-Verity-SHA256", checksum)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

func (s *HTTPServer) stageComparison(w http.ResponseWriter, r *http.Request) {
	limit := 8
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 20 {
			s.problem(w, r, http.StatusUnprocessableEntity, "invalid_limit", "Limit must be between 1 and 20")
			return
		}
		limit = parsed
	}
	response, err := s.store.Compare(r.Context(), r.PathValue("stage_id"), limit)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, ErrNotFound) {
			s.problem(w, r, http.StatusNotFound, "stage_not_found", "Pipeline stage not found")
			return
		}
		s.logger.Error("stage comparison failed", "request_id", requestID(r.Context()), "error", err)
		s.problem(w, r, http.StatusInternalServerError, "comparison_failed", "The stage comparison could not be read")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *HTTPServer) triggerAirbyteSync(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Airbyte == nil {
		s.problem(w, r, http.StatusServiceUnavailable, "integration_not_configured", "Airbyte is not configured for this deployment")
		return
	}
	var request AirbyteSyncRequest
	if err := decodeJSON(w, r, &request, 32<<10); err != nil {
		s.problem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request.ConnectionID = strings.TrimSpace(request.ConnectionID)
	if request.ConnectionID == "" || len(request.ConnectionID) > 128 {
		s.problem(w, r, http.StatusUnprocessableEntity, "invalid_connection", "connection_id must contain between 1 and 128 characters")
		return
	}
	job, err := s.cfg.Airbyte.TriggerSync(r.Context(), request.ConnectionID)
	if err != nil {
		s.logger.Error("Airbyte sync failed", "request_id", requestID(r.Context()), "error", err)
		s.problem(w, r, http.StatusBadGateway, "airbyte_unavailable", "Airbyte could not start the sync")
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *HTTPServer) airbyteJob(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Airbyte == nil {
		s.problem(w, r, http.StatusServiceUnavailable, "integration_not_configured", "Airbyte is not configured for this deployment")
		return
	}
	jobID, err := strconv.ParseInt(r.PathValue("job_id"), 10, 64)
	if err != nil || jobID < 1 {
		s.problem(w, r, http.StatusUnprocessableEntity, "invalid_job", "job_id must be a positive integer")
		return
	}
	job, err := s.cfg.Airbyte.Job(r.Context(), jobID)
	if err != nil {
		s.logger.Error("Airbyte job lookup failed", "request_id", requestID(r.Context()), "error", err)
		s.problem(w, r, http.StatusBadGateway, "airbyte_unavailable", "Airbyte could not return the job")
		return
	}
	writeJSON(w, http.StatusOK, job)
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
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID, Upload-Offset, Upload-Length, Last-Event-ID")
			w.Header().Set("Access-Control-Expose-Headers", "ETag, Location, Upload-Offset, Upload-Length, Upload-State, X-Request-ID, X-Verity-Output-ID, X-Verity-SHA256")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PATCH, OPTIONS")
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
