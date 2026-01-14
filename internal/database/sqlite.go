// Package database provides SQLite implementation of the Store interface.
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/factchecker/verity/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite store.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// Migrate runs database migrations.
func (s *SQLiteStore) Migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS analysis_results (
			id TEXT PRIMARY KEY,
			document_hash TEXT NOT NULL,
			overall_score REAL NOT NULL,
			total_claims INTEGER NOT NULL,
			verified_claims INTEGER NOT NULL,
			mixed_claims INTEGER NOT NULL,
			unsupported_claims INTEGER NOT NULL,
			processing_time_ms INTEGER NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_hash ON analysis_results(document_hash)`,
		`CREATE TABLE IF NOT EXISTS claims (
			id TEXT PRIMARY KEY,
			analysis_id TEXT NOT NULL,
			text TEXT NOT NULL,
			type TEXT NOT NULL,
			sentence_index INTEGER NOT NULL,
			status TEXT NOT NULL,
			confidence REAL NOT NULL,
			source_type TEXT NOT NULL,
			evidences TEXT NOT NULL,
			reasoning TEXT,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (analysis_id) REFERENCES analysis_results(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_claims_analysis ON claims(analysis_id)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			key_hash TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			requests_per_minute INTEGER NOT NULL,
			tokens_per_day INTEGER NOT NULL,
			created_at DATETIME NOT NULL,
			last_used_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			api_key_id TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			method TEXT NOT NULL,
			request_size INTEGER NOT NULL,
			response_code INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			timestamp DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_logs(timestamp)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// SaveAnalysis stores an analysis result.
func (s *SQLiteStore) SaveAnalysis(ctx context.Context, result *models.AnalysisResult) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analysis_results (id, document_hash, overall_score, total_claims, verified_claims,
			mixed_claims, unsupported_claims, processing_time_ms, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.DocumentHash, result.OverallScore, result.TotalClaims,
		result.VerifiedClaims, result.MixedClaims, result.UnsupportedClaims,
		result.ProcessingTimeMs, result.Status, result.CreatedAt,
	)
	return err
}

// GetAnalysis retrieves an analysis by ID.
func (s *SQLiteStore) GetAnalysis(ctx context.Context, id string) (*models.AnalysisResult, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, document_hash, overall_score, total_claims, verified_claims, mixed_claims,
			unsupported_claims, processing_time_ms, status, created_at
		FROM analysis_results WHERE id = ?`, id)

	var result models.AnalysisResult
	err := row.Scan(&result.ID, &result.DocumentHash, &result.OverallScore, &result.TotalClaims,
		&result.VerifiedClaims, &result.MixedClaims, &result.UnsupportedClaims,
		&result.ProcessingTimeMs, &result.Status, &result.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAnalysisByHash retrieves an analysis by document hash.
func (s *SQLiteStore) GetAnalysisByHash(ctx context.Context, hash string) (*models.AnalysisResult, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, document_hash, overall_score, total_claims, verified_claims, mixed_claims,
			unsupported_claims, processing_time_ms, status, created_at
		FROM analysis_results WHERE document_hash = ? ORDER BY created_at DESC LIMIT 1`, hash)

	var result models.AnalysisResult
	err := row.Scan(&result.ID, &result.DocumentHash, &result.OverallScore, &result.TotalClaims,
		&result.VerifiedClaims, &result.MixedClaims, &result.UnsupportedClaims,
		&result.ProcessingTimeMs, &result.Status, &result.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListAnalyses returns paginated analysis results.
func (s *SQLiteStore) ListAnalyses(ctx context.Context, limit, offset int) ([]*models.AnalysisResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_hash, overall_score, total_claims, verified_claims, mixed_claims,
			unsupported_claims, processing_time_ms, status, created_at
		FROM analysis_results ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.AnalysisResult
	for rows.Next() {
		var r models.AnalysisResult
		if err := rows.Scan(&r.ID, &r.DocumentHash, &r.OverallScore, &r.TotalClaims,
			&r.VerifiedClaims, &r.MixedClaims, &r.UnsupportedClaims,
			&r.ProcessingTimeMs, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, &r)
	}
	return results, rows.Err()
}

// SaveClaims stores claims for an analysis.
func (s *SQLiteStore) SaveClaims(ctx context.Context, analysisID string, claims []models.Claim) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO claims (id, analysis_id, text, type, sentence_index, status, confidence,
			source_type, evidences, reasoning, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, claim := range claims {
		evidencesJSON, _ := json.Marshal(claim.Evidences)
		_, err := stmt.ExecContext(ctx, claim.ID, analysisID, claim.Text, claim.Type,
			claim.SentenceIndex, claim.Status, claim.Confidence, claim.SourceType,
			string(evidencesJSON), claim.Reasoning, claim.CreatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetClaimsByAnalysis retrieves all claims for an analysis.
func (s *SQLiteStore) GetClaimsByAnalysis(ctx context.Context, analysisID string) ([]models.Claim, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, text, type, sentence_index, status, confidence, source_type, evidences, reasoning, created_at
		FROM claims WHERE analysis_id = ? ORDER BY sentence_index`, analysisID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claims []models.Claim
	for rows.Next() {
		var c models.Claim
		var evidencesJSON string
		if err := rows.Scan(&c.ID, &c.Text, &c.Type, &c.SentenceIndex, &c.Status,
			&c.Confidence, &c.SourceType, &evidencesJSON, &c.Reasoning, &c.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(evidencesJSON), &c.Evidences)
		claims = append(claims, c)
	}
	return claims, rows.Err()
}

// CreateAPIKey stores a new API key.
func (s *SQLiteStore) CreateAPIKey(ctx context.Context, key *models.APIKey) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, key_hash, name, requests_per_minute, tokens_per_day, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		key.ID, key.KeyHash, key.Name, key.RequestsPerMinute, key.TokensPerDay, key.CreatedAt)
	return err
}

// GetAPIKeyByHash retrieves an API key by its hash.
func (s *SQLiteStore) GetAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, key_hash, name, requests_per_minute, tokens_per_day, created_at, last_used_at
		FROM api_keys WHERE key_hash = ?`, hash)

	var key models.APIKey
	err := row.Scan(&key.ID, &key.KeyHash, &key.Name, &key.RequestsPerMinute,
		&key.TokensPerDay, &key.CreatedAt, &key.LastUsedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// UpdateAPIKeyLastUsed updates the last used timestamp.
func (s *SQLiteStore) UpdateAPIKeyLastUsed(ctx context.Context, id string, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, t, id)
	return err
}

// DeleteAPIKey removes an API key.
func (s *SQLiteStore) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

// ListAPIKeys returns all API keys.
func (s *SQLiteStore) ListAPIKeys(ctx context.Context) ([]*models.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, requests_per_minute, tokens_per_day, created_at, last_used_at
		FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.RequestsPerMinute,
			&k.TokensPerDay, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, &k)
	}
	return keys, rows.Err()
}

// LogRequest stores an audit log entry.
func (s *SQLiteStore) LogRequest(ctx context.Context, log *models.AuditLog) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, api_key_id, endpoint, method, request_size, response_code, duration_ms, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.APIKeyID, log.Endpoint, log.Method, log.RequestSize,
		log.ResponseCode, log.DurationMs, log.Timestamp)
	return err
}

// GetAuditLogs returns paginated audit logs.
func (s *SQLiteStore) GetAuditLogs(ctx context.Context, limit, offset int) ([]*models.AuditLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, api_key_id, endpoint, method, request_size, response_code, duration_ms, timestamp
		FROM audit_logs ORDER BY timestamp DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		var l models.AuditLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.Endpoint, &l.Method,
			&l.RequestSize, &l.ResponseCode, &l.DurationMs, &l.Timestamp); err != nil {
			return nil, err
		}
		logs = append(logs, &l)
	}
	return logs, rows.Err()
}
