// Package api provides HTTP API handlers and middleware.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/factchecker/verity/internal/database"
	"github.com/factchecker/verity/internal/models"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type contextKey string

const (
	apiKeyContextKey contextKey = "apiKey"
	requestIDKey     contextKey = "requestID"
)

// AuthMiddleware validates API keys.
func AuthMiddleware(store database.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health endpoint
			if r.URL.Path == "/api/v1/health" {
				next.ServeHTTP(w, r)
				return
			}

			// Get API key from header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error": "Missing Authorization header"}`, http.StatusUnauthorized)
				return
			}

			// Parse Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, `{"error": "Invalid Authorization header format"}`, http.StatusUnauthorized)
				return
			}

			apiKey := parts[1]

			// Hash the key for lookup
			hash := sha256.Sum256([]byte(apiKey))
			keyHash := hex.EncodeToString(hash[:])

			// Look up API key
			key, err := store.GetAPIKeyByHash(r.Context(), keyHash)
			if err != nil {
				log.Error().Err(err).Msg("Failed to look up API key")
				http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
				return
			}
			if key == nil {
				http.Error(w, `{"error": "Invalid API key"}`, http.StatusUnauthorized)
				return
			}

			// Update last used
			go func() {
				_ = store.UpdateAPIKeyLastUsed(context.Background(), key.ID, time.Now())
			}()

			// Store key in context for rate limiting and auditing
			ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDMiddleware adds a unique request ID to each request.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs all requests.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", wrapped.status).
			Dur("duration", duration).
			Str("request_id", getRequestID(r.Context())).
			Msg("Request completed")
	})
}

// AuditMiddleware logs API requests to the database.
func AuditMiddleware(store database.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			// Get API key from context
			apiKeyID := ""
			if key := getAPIKey(r.Context()); key != nil {
				apiKeyID = key.ID
			}

			// Log asynchronously
			go func() {
				auditLog := &models.AuditLog{
					ID:           uuid.New().String(),
					APIKeyID:     apiKeyID,
					Endpoint:     r.URL.Path,
					Method:       r.Method,
					RequestSize:  r.ContentLength,
					ResponseCode: wrapped.status,
					DurationMs:   duration.Milliseconds(),
					Timestamp:    start,
				}
				if err := store.LogRequest(context.Background(), auditLog); err != nil {
					log.Error().Err(err).Msg("Failed to log audit entry")
				}
			}()
		})
	}
}

// RateLimitMiddleware applies per-key rate limiting.
func RateLimitMiddleware(defaultLimit int) func(http.Handler) http.Handler {
	// Use httprate with custom key function
	limiter := httprate.Limit(
		defaultLimit,
		time.Minute,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
			if key := getAPIKey(r.Context()); key != nil {
				return key.ID, nil
			}
			return r.RemoteAddr, nil
		}),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error": "Rate limit exceeded"}`, http.StatusTooManyRequests)
		}),
	)
	return limiter
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Helper functions to get context values
func getAPIKey(ctx context.Context) *models.APIKey {
	if key, ok := ctx.Value(apiKeyContextKey).(*models.APIKey); ok {
		return key
	}
	return nil
}

func getRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
