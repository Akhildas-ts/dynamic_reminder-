package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"dynamic-reminder/internal/models"
	"dynamic-reminder/internal/repository"
	"dynamic-reminder/internal/service"
)

type Handler struct {
	rules  *service.RuleService
	audit  *service.AuditService
	tasks  *repository.TaskRepository
	logger *slog.Logger
}

// NewRouter wires up the chi router with every endpoint the service exposes.
// Middleware is attached once here, in dependency order: request ID → real-IP
// → recoverer → access log. The actual handlers are pure thin shells over
// services — no business logic lives in this layer.
func NewRouter(
	rules *service.RuleService,
	audit *service.AuditService,
	tasks *repository.TaskRepository,
	logger *slog.Logger,
) http.Handler {
	h := &Handler{rules: rules, audit: audit, tasks: tasks, logger: logger}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(loggerMiddleware(logger))

	r.Get("/healthz", h.health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/tasks", h.listTasks)

		r.Route("/rules", func(r chi.Router) {
			r.Get("/", h.listRules)
			r.Post("/", h.createRule)
			r.Get("/{id}", h.getRule)
			r.Patch("/{id}", h.updateRule)
			r.Delete("/{id}", h.deleteRule)
			r.Patch("/{id}/status", h.setStatus)
		})

		r.Get("/audit-logs", h.listAudit)
	})

	return r
}

// ----- health -----

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ----- tasks -----

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.tasks.List(r.Context())
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tasks})
}

// ----- rules -----

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.rules.List(r.Context())
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rules})
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rule, err := h.rules.Get(r.Context(), id)
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	var in service.CreateRuleInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rule, err := h.rules.Create(r.Context(), in)
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var in service.UpdateRuleInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rule, err := h.rules.Update(r.Context(), id, in)
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.rules.Delete(r.Context(), id); err != nil {
		translateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type statusRequest struct {
	Active *bool `json:"active"`
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var in statusRequest
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if in.Active == nil {
		writeError(w, http.StatusBadRequest, errors.New("'active' is required"))
		return
	}
	rule, err := h.rules.SetActive(r.Context(), id, *in.Active)
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// ----- audit -----

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.AuditFilter{}

	if v := q.Get("event_type"); v != "" {
		if !models.AuditEventType(v).Valid() {
			writeError(w, http.StatusBadRequest, errors.New("invalid event_type"))
			return
		}
		f.EventType = v
	}
	if v := q.Get("rule_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid rule_id"))
			return
		}
		f.RuleID = &id
	}
	if v := q.Get("task_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid task_id"))
			return
		}
		f.TaskID = &id
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, errors.New("invalid limit"))
			return
		}
		f.Limit = n
	}

	logs, err := h.audit.List(r.Context(), f)
	if err != nil {
		translateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": logs})
}

func parseID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, errors.New("invalid id (must be UUID)")
	}
	return id, nil
}
