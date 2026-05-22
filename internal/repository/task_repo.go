package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"dynamic-reminder/internal/models"
)

type TaskRepository struct {
	db Querier
}

func NewTaskRepository(db Querier) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) GetByID(ctx context.Context, id uuid.UUID) (models.Task, error) {
	var t models.Task
	err := r.db.QueryRow(ctx, `
		SELECT id, title, description, due_date, created_at, updated_at
		FROM tasks
		WHERE id = $1
	`, id).Scan(&t.ID, &t.Title, &t.Description, &t.DueDate, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

func (r *TaskRepository) List(ctx context.Context) ([]models.Task, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, title, description, due_date, created_at, updated_at
		FROM tasks
		ORDER BY due_date ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]models.Task, 0)
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.DueDate, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
