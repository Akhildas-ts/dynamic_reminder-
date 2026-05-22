package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"dynamic-reminder/internal/models"
)

type AuditRepository struct {
	db Querier
}

// Store history of important actions in database
func NewAuditRepository(db Querier) *AuditRepository {
	return &AuditRepository{db: db}
}

type AuditFilter struct {
	EventType string
	RuleID    *uuid.UUID
	TaskID    *uuid.UUID
	Limit     int
}

func (r *AuditRepository) Insert(
	ctx context.Context,
	event models.AuditEventType,
	ruleID, taskID *uuid.UUID,
	payload map[string]any,
) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal audit payload: %w", err)
	}
	if _, err := r.db.Exec(ctx, `
		INSERT INTO audit_logs (event_type, rule_id, task_id, payload)
		VALUES ($1, $2, $3, $4::jsonb)
	`, event, ruleID, taskID, string(raw)); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

func (r *AuditRepository) List(ctx context.Context, f AuditFilter) ([]models.AuditLog, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var eventArg any
	if f.EventType != "" {
		eventArg = f.EventType
	}

	rows, err := r.db.Query(ctx, `
		SELECT id, event_type, rule_id, task_id, payload, created_at
		FROM audit_logs
		WHERE ($1::audit_event_type IS NULL OR event_type = $1::audit_event_type)
		  AND ($2::uuid IS NULL OR rule_id = $2)
		  AND ($3::uuid IS NULL OR task_id = $3)
		ORDER BY created_at DESC
		LIMIT $4
	`, eventArg, f.RuleID, f.TaskID, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	logs := make([]models.AuditLog, 0)
	for rows.Next() {
		var (
			l   models.AuditLog
			raw []byte
		)
		if err := rows.Scan(&l.ID, &l.EventType, &l.RuleID, &l.TaskID, &raw, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &l.Payload); err != nil {
				return nil, fmt.Errorf("unmarshal audit payload: %w", err)
			}
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
