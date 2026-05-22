package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dynamic-reminder/internal/models"
	"dynamic-reminder/internal/repository"
)

var (
	ErrInvalidInterval = errors.New("trigger_interval must be a positive Go duration (e.g. '15m', '1h', '24h')")
	ErrEmptyUpdate     = errors.New("at least one of trigger_interval or active must be provided")
	ErrTaskNotFound    = errors.New("task not found")
	ErrRuleNotFound    = errors.New("rule not found")
)

// RuleService owns every mutation that touches a reminder rule. It is the
// single place where an audit log entry is paired with a state change inside
// the same transaction — that pairing is the contract that gives this system
// its "audit trail" guarantee.
type RuleService struct {
	pool *pgxpool.Pool
}

func NewRuleService(pool *pgxpool.Pool) *RuleService {
	return &RuleService{pool: pool}
}

type CreateRuleInput struct {
	TaskID   uuid.UUID `json:"task_id"`
	Interval string    `json:"trigger_interval"`
	Active   *bool     `json:"active,omitempty"`
}

type UpdateRuleInput struct {
	Interval *string `json:"trigger_interval,omitempty"`
	Active   *bool   `json:"active,omitempty"`
}

func validateInterval(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return ErrInvalidInterval
	}
	return nil
}

func (s *RuleService) Create(ctx context.Context, in CreateRuleInput) (models.ReminderRule, error) {
	if err := validateInterval(in.Interval); err != nil {
		return models.ReminderRule{}, err
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}

	var created models.ReminderRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := repository.NewTaskRepository(tx).GetByID(ctx, in.TaskID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrTaskNotFound
			}
			return err
		}
		rule, err := repository.NewRuleRepository(tx).Create(ctx, in.TaskID, in.Interval, active)
		if err != nil {
			return err
		}
		if err := repository.NewAuditRepository(tx).Insert(ctx,
			models.EventRuleCreate, &rule.ID, &rule.TaskID,
			map[string]any{
				"trigger_interval": rule.TriggerInterval,
				"active":           rule.Active,
			},
		); err != nil {
			return err
		}
		created = rule
		return nil
	})
	if err != nil {
		return models.ReminderRule{}, err
	}
	return created, nil
}

func (s *RuleService) Update(ctx context.Context, id uuid.UUID, in UpdateRuleInput) (models.ReminderRule, error) {
	if in.Interval == nil && in.Active == nil {
		return models.ReminderRule{}, ErrEmptyUpdate
	}
	if in.Interval != nil {
		if err := validateInterval(*in.Interval); err != nil {
			return models.ReminderRule{}, err
		}
	}

	var updated models.ReminderRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rules := repository.NewRuleRepository(tx)
		existing, err := rules.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrRuleNotFound
			}
			return err
		}
		interval := existing.TriggerInterval
		if in.Interval != nil {
			interval = *in.Interval
		}
		active := existing.Active
		if in.Active != nil {
			active = *in.Active
		}
		rule, err := rules.Update(ctx, id, interval, active)
		if err != nil {
			return err
		}
		if err := repository.NewAuditRepository(tx).Insert(ctx,
			models.EventRuleUpdate, &rule.ID, &rule.TaskID,
			map[string]any{
				"before": map[string]any{
					"trigger_interval": existing.TriggerInterval,
					"active":           existing.Active,
				},
				"after": map[string]any{
					"trigger_interval": rule.TriggerInterval,
					"active":           rule.Active,
				},
			},
		); err != nil {
			return err
		}
		updated = rule
		return nil
	})
	if err != nil {
		return models.ReminderRule{}, err
	}
	return updated, nil
}

func (s *RuleService) Delete(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rules := repository.NewRuleRepository(tx)
		existing, err := rules.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrRuleNotFound
			}
			return err
		}
		if err := rules.Delete(ctx, id); err != nil {
			return err
		}
		return repository.NewAuditRepository(tx).Insert(ctx,
			models.EventRuleDelete, &existing.ID, &existing.TaskID,
			map[string]any{
				"trigger_interval": existing.TriggerInterval,
				"active":           existing.Active,
			},
		)
	})
}

func (s *RuleService) SetActive(ctx context.Context, id uuid.UUID, active bool) (models.ReminderRule, error) {
	var result models.ReminderRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rules := repository.NewRuleRepository(tx)
		existing, err := rules.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrRuleNotFound
			}
			return err
		}
		if existing.Active == active {
			result = existing
			return nil
		}
		rule, err := rules.SetActive(ctx, id, active)
		if err != nil {
			return err
		}
		if err := repository.NewAuditRepository(tx).Insert(ctx,
			models.EventStatusChange, &rule.ID, &rule.TaskID,
			map[string]any{"from": existing.Active, "to": rule.Active},
		); err != nil {
			return err
		}
		result = rule
		return nil
	})
	if err != nil {
		return models.ReminderRule{}, err
	}
	return result, nil
}

func (s *RuleService) List(ctx context.Context) ([]models.ReminderRule, error) {
	return repository.NewRuleRepository(s.pool).List(ctx)
}

func (s *RuleService) Get(ctx context.Context, id uuid.UUID) (models.ReminderRule, error) {
	rule, err := repository.NewRuleRepository(s.pool).GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return rule, ErrRuleNotFound
	}
	return rule, err
}
