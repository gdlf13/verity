// Package api provides HTTP API handlers.
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/factchecker/verity/internal/database"
	"github.com/factchecker/verity/internal/models"
	"github.com/factchecker/verity/internal/verify"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Handler contains all HTTP handlers.
type Handler struct {
	engine *verify.Engine
	store  database.Store
}

// NewHandler creates a new handler.
func NewHandler(engine *verify.Engine, store database.Store) *Handler {
	return &Handler{
		engine: engine,
		store:  store,
	}
}

// HealthCheck returns the service health status.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"version":   "1.0.0",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, response)
}

// VerifyText handles text verification requests.
func (h *Handler) VerifyText(w http.ResponseWriter, r *http.Request) {
	var req models.VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "Text is required")
		return
	}

	result, err := h.engine.VerifyText(r.Context(), req.Text)
	if err != nil {
		log.Error().Err(err).Msg("Verification failed")
		writeError(w, http.StatusInternalServerError, "Verification failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// GetResult returns a verification result by ID.
func (h *Handler) GetResult(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "ID is required")
		return
	}

	analysis, err := h.store.GetAnalysis(r.Context(), id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get analysis")
		writeError(w, http.StatusInternalServerError, "Failed to get result")
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "Result not found")
		return
	}

	claims, err := h.store.GetClaimsByAnalysis(r.Context(), id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get claims")
		writeError(w, http.StatusInternalServerError, "Failed to get claims")
		return
	}

	response := models.VerificationResponse{
		ID:           analysis.ID,
		DocumentHash: analysis.DocumentHash,
		Analysis:     *analysis,
		Claims:       claims,
	}

	writeJSON(w, http.StatusOK, response)
}

// ListResults returns paginated verification results.
func (h *Handler) ListResults(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	results, err := h.store.ListAnalyses(r.Context(), limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list results")
		writeError(w, http.StatusInternalServerError, "Failed to list results")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"limit":   limit,
		"offset":  offset,
	})
}

// GetAuditLogs returns paginated audit logs.
func (h *Handler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	logs, err := h.store.GetAuditLogs(r.Context(), limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get audit logs")
		writeError(w, http.StatusInternalServerError, "Failed to get audit logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"limit":  limit,
		"offset": offset,
	})
}

// CreateAPIKey creates a new API key.
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name              string `json:"name"`
		RequestsPerMinute int    `json:"requests_per_minute"`
		TokensPerDay      int    `json:"tokens_per_day"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// Generate random API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate key")
		return
	}
	rawKey := "vrt_" + base64.URLEncoding.EncodeToString(keyBytes)

	// Hash for storage
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	// Set defaults
	if req.RequestsPerMinute <= 0 {
		req.RequestsPerMinute = 60
	}
	if req.TokensPerDay <= 0 {
		req.TokensPerDay = 100000
	}

	apiKey := &models.APIKey{
		ID:                uuid.New().String(),
		KeyHash:           keyHash,
		Name:              req.Name,
		RequestsPerMinute: req.RequestsPerMinute,
		TokensPerDay:      req.TokensPerDay,
		CreatedAt:         time.Now(),
	}

	if err := h.store.CreateAPIKey(r.Context(), apiKey); err != nil {
		log.Error().Err(err).Msg("Failed to create API key")
		writeError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	// Return the raw key only once
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":                  apiKey.ID,
		"key":                 rawKey, // Only returned on creation
		"name":                apiKey.Name,
		"requests_per_minute": apiKey.RequestsPerMinute,
		"tokens_per_day":      apiKey.TokensPerDay,
		"created_at":          apiKey.CreatedAt,
	})
}

// ListAPIKeys lists all API keys (without the actual keys).
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListAPIKeys(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list API keys")
		writeError(w, http.StatusInternalServerError, "Failed to list API keys")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys": keys,
	})
}

// DeleteAPIKey deletes an API key.
func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "ID is required")
		return
	}

	if err := h.store.DeleteAPIKey(r.Context(), id); err != nil {
		log.Error().Err(err).Msg("Failed to delete API key")
		writeError(w, http.StatusInternalServerError, "Failed to delete API key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Helper functions
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
