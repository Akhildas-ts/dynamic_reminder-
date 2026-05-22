package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"dynamic-reminder/internal/repository"
	"dynamic-reminder/internal/service"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// translateError maps domain errors to HTTP status codes. Keeping this in one
// place means handlers stay small and consistent.
func translateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrTaskNotFound),
		errors.Is(err, service.ErrRuleNotFound),
		errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, service.ErrInvalidInterval),
		errors.Is(err, service.ErrEmptyUpdate):
		writeError(w, http.StatusBadRequest, err)
	default:
		// Log the real error server-side, but don't leak internal/DB detail
		// (table names, SQL, driver messages) back to the client.
		slog.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, errors.New("internal server error"))
	}
}

func loggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).Round(time.Microsecond).String(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
