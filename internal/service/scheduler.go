package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dynamic-reminder/internal/models"
	"dynamic-reminder/internal/repository"
)

// errAlreadyClaimed signals that another overlapping tick/worker already fired
// this rule; the current transaction rolls back and the worker skips it.
var errAlreadyClaimed = errors.New("rule already claimed")

type SchedulerOptions struct {
	TickInterval time.Duration
	Workers      int
	QueueSize    int
	Logger       *slog.Logger
}

// Scheduler is a fan-out reminder dispatcher.
//
//	┌──────────┐    every TickInterval      ┌─────────┐ buffered ┌──────────┐
//	│ time.    │ ─────── ListDue ─────────► │ jobs ch │ ───────► │ workers  │
//	│ Ticker   │                            │ (cap N) │          │ goroutine│
//	└──────────┘                            └─────────┘          └──────────┘
//
// A slow worker can never block the ticker beyond the channel's buffer,
// and a slow DB tick can never starve a worker that is currently processing.
type Scheduler struct {
	pool   *pgxpool.Pool
	opts   SchedulerOptions
	logger *slog.Logger
}

func NewScheduler(pool *pgxpool.Pool, opts SchedulerOptions) *Scheduler {
	if opts.TickInterval <= 0 {
		opts.TickInterval = time.Minute
	}
	if opts.Workers <= 0 {
		opts.Workers = 5
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 100
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Scheduler{pool: pool, opts: opts, logger: opts.Logger}
}

// Run blocks until ctx is cancelled. It owns the lifecycle of the worker
// pool: starts N workers, fires an immediate tick, then ticks on a schedule.
// On cancellation it closes the jobs channel and waits for workers to drain.
func (s *Scheduler) Run(ctx context.Context) {
	jobs := make(chan repository.DueRule, s.opts.QueueSize)
	var workers sync.WaitGroup

	for i := 0; i < s.opts.Workers; i++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			s.workerLoop(ctx, id, jobs)
		}(i + 1)
	}

	s.logger.Info("scheduler started",
		"tick", s.opts.TickInterval,
		"workers", s.opts.Workers,
		"queue", s.opts.QueueSize,
	)

	ticker := time.NewTicker(s.opts.TickInterval)
	defer ticker.Stop()

	s.tick(ctx, jobs) // fire once immediately for snappy demos

	for {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			s.logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx, jobs)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, jobs chan<- repository.DueRule) {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	due, err := repository.NewRuleRepository(s.pool).ListDue(fetchCtx, time.Now())
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.logger.Error("scheduler: list due rules failed", "err", err)
		}
		return
	}
	if len(due) == 0 {
		return
	}
	s.logger.Info("scheduler: dispatching reminders", "count", len(due))

	for _, d := range due {
		select {
		case jobs <- d:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) workerLoop(ctx context.Context, id int, jobs <-chan repository.DueRule) {
	for d := range jobs {
		s.process(ctx, id, d)
	}
}

// process commits the state change (last_triggered_at) and the audit entry
// in a single transaction. If either step fails, neither is persisted —
// the rule will simply be re-picked on the next tick.
func (s *Scheduler) process(ctx context.Context, workerID int, d repository.DueRule) {
	procCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	triggeredAt := time.Now()
	err := pgx.BeginFunc(procCtx, s.pool, func(tx pgx.Tx) error {
		claimed, err := repository.NewRuleRepository(tx).MarkTriggered(
			procCtx, d.Rule.ID, triggeredAt, d.Rule.LastTriggeredAt)
		if err != nil {
			return err
		}
		if !claimed {
			// Another tick already advanced last_triggered_at — roll back and
			// skip so the reminder is not delivered twice.
			return errAlreadyClaimed
		}
		return repository.NewAuditRepository(tx).Insert(procCtx,
			models.EventReminderTriggered, &d.Rule.ID, &d.Rule.TaskID,
			map[string]any{
				"task_title":       d.Title,
				"task_due_date":    d.DueDate,
				"trigger_interval": d.Rule.TriggerInterval,
				"triggered_at":     triggeredAt,
				"worker_id":        workerID,
			},
		)
	})
	if err != nil {
		if errors.Is(err, errAlreadyClaimed) || errors.Is(err, context.Canceled) {
			return
		}
		s.logger.Error("scheduler: failed to commit reminder",
			"err", err, "rule_id", d.Rule.ID, "task_id", d.Rule.TaskID)
		return
	}

	// Simulated "send": surfaces the reminder on stdout once the row is durable.
	s.logger.Info("⏰ REMINDER",
		"worker", workerID,
		"task", d.Title,
		"due_in", time.Until(d.DueDate).Round(time.Second).String(),
		"rule_id", d.Rule.ID,
		"interval", d.Rule.TriggerInterval,
	)
}
