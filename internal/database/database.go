// Package database provides the data access layer with support for multiple backends.
package database

import (
	"context"
	"time"

	"github.com/factchecker/verity/internal/models"
)

// Store defines the interface for data persistence.
type Store interface {
	// Analysis results
	SaveAnalysis(ctx context.Context, result *models.AnalysisResult) error
	GetAnalysis(ctx context.Context, id string) (*models.AnalysisResult, error)
	GetAnalysisByHash(ctx context.Context, hash string) (*models.AnalysisResult, error)
	ListAnalyses(ctx context.Context, limit, offset int) ([]*models.AnalysisResult, error)

	// Claims
	SaveClaims(ctx context.Context, analysisID string, claims []models.Claim) error
	GetClaimsByAnalysis(ctx context.Context, analysisID string) ([]models.Claim, error)

	// API Keys
	CreateAPIKey(ctx context.Context, key *models.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error)
	UpdateAPIKeyLastUsed(ctx context.Context, id string, t time.Time) error
	DeleteAPIKey(ctx context.Context, id string) error
	ListAPIKeys(ctx context.Context) ([]*models.APIKey, error)

	// Audit logs
	LogRequest(ctx context.Context, log *models.AuditLog) error
	GetAuditLogs(ctx context.Context, limit, offset int) ([]*models.AuditLog, error)

	// Lifecycle
	Close() error
	Migrate() error
}
