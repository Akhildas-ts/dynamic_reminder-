package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"dynamic-reminder/internal/models"
)

type RuleRepository struct {
	db Querier
}

func NewRuleRepository(db Querier) *RuleRepository {
	return &RuleRepository{db: db}
}

const ruleColumns = `id, task_id, trigger_interval, active, last_triggered_at, created_at, updated_at`

func scanRule(row pgx.Row, rule *models.ReminderRule) error {
	return row.Scan(
		&rule.ID, &rule.TaskID, &rule.TriggerInterval, &rule.Active,
		&rule.LastTriggeredAt, &rule.CreatedAt, &rule.UpdatedAt,
	)
}

func (r *RuleRepository) Create(ctx context.Context, taskID uuid.UUID, interval string, active bool) (models.ReminderRule, error) {
	var rule models.ReminderRule
	err := scanRule(r.db.QueryRow(ctx, `
		INSERT INTO reminder_rules (task_id, trigger_interval, active)
		VALUES ($1, $2, $3)
		RETURNING `+ruleColumns,
		taskID, interval, active,
	), &rule)
	if err != nil {
		return rule, fmt.Errorf("insert rule: %w", err)
	}
	return rule, nil
}

func (r *RuleRepository) GetByID(ctx context.Context, id uuid.UUID) (models.ReminderRule, error) {
	var rule models.ReminderRule
	err := scanRule(r.db.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM reminder_rules WHERE id = $1`, id,
	), &rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return rule, ErrNotFound
	}
	if err != nil {
		return rule, fmt.Errorf("get rule: %w", err)
	}
	return rule, nil
}

func (r *RuleRepository) Update(ctx context.Context, id uuid.UUID, interval string, active bool) (models.ReminderRule, error) {
	var rule models.ReminderRule
	err := scanRule(r.db.QueryRow(ctx, `
		UPDATE reminder_rules
		SET trigger_interval = $2, active = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING `+ruleColumns,
		id, interval, active,
	), &rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return rule, ErrNotFound
	}
	if err != nil {
		return rule, fmt.Errorf("update rule: %w", err)
	}
	return rule, nil
}

func (r *RuleRepository) SetActive(ctx context.Context, id uuid.UUID, active bool) (models.ReminderRule, error) {
	var rule models.ReminderRule
	err := scanRule(r.db.QueryRow(ctx, `
		UPDATE reminder_rules
		SET active = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING `+ruleColumns,
		id, active,
	), &rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return rule, ErrNotFound
	}
	if err != nil {
		return rule, fmt.Errorf("set active: %w", err)
	}
	return rule, nil
}

func (r *RuleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM reminder_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *RuleRepository) List(ctx context.Context) ([]models.ReminderRule, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+ruleColumns+` FROM reminder_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()

	rules := make([]models.ReminderRule, 0)
	for rows.Next() {
		var rule models.ReminderRule
		if err := scanRule(rows, &rule); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// DueRule pairs a rule with the task fields the scheduler needs when
// dispatching a reminder. Joining at the DB layer avoids a second round-trip.
type DueRule struct {
	Rule    models.ReminderRule
	Title   string
	DueDate time.Time
}

// ListDue returns active rules whose next firing time is at or before `now`.
// Rules for tasks that have already passed their due_date are skipped: we stop
// reminding once the work is overdue from the database's point of view.
//
// The duration arithmetic happens in Go because trigger_interval is stored
// in Go's duration syntax ("15m", "1h"); decoding it here keeps the schema
// simple and the parsing logic in one place.
func (r *RuleRepository) ListDue(ctx context.Context, now time.Time) ([]DueRule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			rr.id, rr.task_id, rr.trigger_interval, rr.active,
			rr.last_triggered_at, rr.created_at, rr.updated_at,
			t.title, t.due_date
		FROM reminder_rules rr
		INNER JOIN tasks t ON t.id = rr.task_id
		WHERE rr.active = TRUE
		  AND t.due_date > $1
	`, now)
	if err != nil {
		return nil, fmt.Errorf("list due rules: %w", err)
	}
	defer rows.Close()

	var due []DueRule
	for rows.Next() {
		var d DueRule
		if err := rows.Scan(
			&d.Rule.ID, &d.Rule.TaskID, &d.Rule.TriggerInterval, &d.Rule.Active,
			&d.Rule.LastTriggeredAt, &d.Rule.CreatedAt, &d.Rule.UpdatedAt,
			&d.Title, &d.DueDate,
		); err != nil {
			return nil, fmt.Errorf("scan due rule: %w", err)
		}
		interval, err := time.ParseDuration(d.Rule.TriggerInterval)
		if err != nil {
			// Malformed interval slipped past validation — skip rather than abort the batch.
			continue
		}
		if d.Rule.LastTriggeredAt == nil || now.Sub(*d.Rule.LastTriggeredAt) >= interval {
			due = append(due, d)
		}
	}
	return due, rows.Err()
}

// MarkTriggered atomically advances last_triggered_at — but only if the column
// still holds the value the scheduler observed when it decided the rule was due
// (`prev`). This compare-and-swap means that if two overlapping ticks dispatch
// the same rule, only the first worker claims it: the second sees
// RowsAffected == 0 and gets claimed == false, so the caller can skip it
// instead of sending a duplicate reminder.
func (r *RuleRepository) MarkTriggered(ctx context.Context, id uuid.UUID, at time.Time, prev *time.Time) (claimed bool, err error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE reminder_rules
		SET last_triggered_at = $2, updated_at = NOW()
		WHERE id = $1
		  AND last_triggered_at IS NOT DISTINCT FROM $3
	`, id, at, prev)
	if err != nil {
		return false, fmt.Errorf("mark triggered: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
