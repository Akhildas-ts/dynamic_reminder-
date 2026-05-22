package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dynamic-reminder/internal/models"
	"dynamic-reminder/internal/repository"
)

// AuditService is a thin read-side façade over the audit repository.
// Writes happen inside other services' transactions, never here.
type AuditService struct {
	pool *pgxpool.Pool
}

func NewAuditService(pool *pgxpool.Pool) *AuditService {
	return &AuditService{pool: pool}
}

func (s *AuditService) List(ctx context.Context, f repository.AuditFilter) ([]models.AuditLog, error) {
	return repository.NewAuditRepository(s.pool).List(ctx, f)
}
