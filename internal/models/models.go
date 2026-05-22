package models

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DueDate     time.Time `json:"due_date"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ReminderRule struct {
	ID              uuid.UUID  `json:"id"`
	TaskID          uuid.UUID  `json:"task_id"`
	TriggerInterval string     `json:"trigger_interval"`
	Active          bool       `json:"active"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type AuditEventType string

const (
	EventRuleCreate        AuditEventType = "RULE_CREATE"
	EventRuleUpdate        AuditEventType = "RULE_UPDATE"
	EventRuleDelete        AuditEventType = "RULE_DELETE"
	EventStatusChange      AuditEventType = "STATUS_CHANGE"
	EventReminderTriggered AuditEventType = "REMINDER_TRIGGERED"
)

// Valid reports whether e is one of the known audit event types. Used to
// reject bad ?event_type= filter values before they reach the DB enum cast.
func (e AuditEventType) Valid() bool {
	switch e {
	case EventRuleCreate, EventRuleUpdate, EventRuleDelete,
		EventStatusChange, EventReminderTriggered:
		return true
	default:
		return false
	}
}

type AuditLog struct {
	ID        uuid.UUID      `json:"id"`
	EventType AuditEventType `json:"event_type"`
	RuleID    *uuid.UUID     `json:"rule_id,omitempty"`
	TaskID    *uuid.UUID     `json:"task_id,omitempty"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}
