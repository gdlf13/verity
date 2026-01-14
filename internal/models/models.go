// Package models defines the core data structures used throughout the application.
package models

import (
	"time"
)

// ClaimType represents the type of a factual claim.
type ClaimType string

const (
	ClaimTypeStatistical ClaimType = "statistical"
	ClaimTypeFactual     ClaimType = "factual"
	ClaimTypeTemporal    ClaimType = "temporal"
	ClaimTypeGeographic  ClaimType = "geographic"
	ClaimTypeCitation    ClaimType = "citation"
	ClaimTypeComparative ClaimType = "comparative"
	ClaimTypeCausal      ClaimType = "causal"
	ClaimTypeCustom      ClaimType = "custom"
)

// VerificationStatus represents the result of claim verification.
type VerificationStatus string

const (
	StatusVerified    VerificationStatus = "verified"
	StatusMixed       VerificationStatus = "mixed"
	StatusUnsupported VerificationStatus = "unsupported"
	StatusPending     VerificationStatus = "pending"
)

// SourceType indicates how the claim was verified.
type SourceType string

const (
	SourceTypeEvidenceBacked SourceType = "evidence_backed"
	SourceTypeModelBased     SourceType = "model_based"
)

// Claim represents an atomic factual claim extracted from text.
type Claim struct {
	ID                 string             `json:"id"`
	Text               string             `json:"text"`
	Type               ClaimType          `json:"type"`
	SentenceIndex      int                `json:"sentence_index"`
	Status             VerificationStatus `json:"status"`
	Confidence         float64            `json:"confidence"`
	SourceType         SourceType         `json:"source_type"`
	Evidences          []Evidence         `json:"evidences"`
	Reasoning          string             `json:"reasoning,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
}

// Evidence represents a piece of evidence found for a claim.
type Evidence struct {
	ID             string    `json:"id"`
	SourceName     string    `json:"source_name"`
	SourceURL      string    `json:"source_url"`
	SourceType     string    `json:"source_type"` // web, academic, encyclopedia
	Snippet        string    `json:"snippet"`
	RelevanceScore float64   `json:"relevance_score"`
	RetrievedAt    time.Time `json:"retrieved_at"`
}

// AnalysisResult represents the overall result of fact-checking a document.
type AnalysisResult struct {
	ID                  string    `json:"id"`
	DocumentHash        string    `json:"document_hash"`
	OverallScore        float64   `json:"overall_score"`
	TotalClaims         int       `json:"total_claims"`
	VerifiedClaims      int       `json:"verified_claims"`
	MixedClaims         int       `json:"mixed_claims"`
	UnsupportedClaims   int       `json:"unsupported_claims"`
	ProcessingTimeMs    int64     `json:"processing_time_ms"`
	Status              string    `json:"status"` // pending, processing, completed, failed
	CreatedAt           time.Time `json:"created_at"`
}

// VerificationResponse is the API response for a verification request.
type VerificationResponse struct {
	ID           string         `json:"id"`
	DocumentHash string         `json:"document_hash"`
	Analysis     AnalysisResult `json:"analysis"`
	Claims       []Claim        `json:"claims"`
	Warnings     []Warning      `json:"warnings,omitempty"`
}

// Warning represents a non-fatal issue during processing.
type Warning struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

// APIKey represents an API key for authentication.
type APIKey struct {
	ID                string    `json:"id"`
	KeyHash           string    `json:"-"` // Never expose
	Name              string    `json:"name"`
	RequestsPerMinute int       `json:"requests_per_minute"`
	TokensPerDay      int       `json:"tokens_per_day"`
	CreatedAt         time.Time `json:"created_at"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
}

// AuditLog represents an API request audit entry.
type AuditLog struct {
	ID            string    `json:"id"`
	APIKeyID      string    `json:"api_key_id"`
	Endpoint      string    `json:"endpoint"`
	Method        string    `json:"method"`
	RequestSize   int64     `json:"request_size"`
	ResponseCode  int       `json:"response_code"`
	DurationMs    int64     `json:"duration_ms"`
	Timestamp     time.Time `json:"timestamp"`
}

// VerifyRequest is the request body for verification endpoints.
type VerifyRequest struct {
	Text        string `json:"text"`
	ModelSource string `json:"model_source,omitempty"` // Optional: GPT-4, Claude, etc.
}

// BatchVerifyRequest is the request body for batch verification.
type BatchVerifyRequest struct {
	Documents []VerifyRequest `json:"documents"`
}
