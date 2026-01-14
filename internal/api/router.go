// Package api provides HTTP router setup.
package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/factchecker/verity/internal/config"
	"github.com/factchecker/verity/internal/database"
	"github.com/factchecker/verity/internal/verify"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates a new HTTP router with all routes configured.
func NewRouter(cfg *config.Config, engine *verify.Engine, store database.Store, staticFS embed.FS) http.Handler {
	r := chi.NewRouter()

	handler := NewHandler(engine, store)

	// Global middleware
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check (no auth required)
		r.Get("/health", handler.HealthCheck)

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(store))
			r.Use(AuditMiddleware(store))
			r.Use(RateLimitMiddleware(cfg.RateLimits.RequestsPerMinute))

			// Verification endpoints
			r.Post("/verify/text", handler.VerifyText)

			// Results
			r.Get("/results", handler.ListResults)
			r.Get("/results/{id}", handler.GetResult)

			// Audit logs
			r.Get("/audit", handler.GetAuditLogs)
		})

		// Admin routes (API key management)
		// In production, these should be protected differently
		r.Route("/admin", func(r chi.Router) {
			r.Post("/keys", handler.CreateAPIKey)
			r.Get("/keys", handler.ListAPIKeys)
			r.Delete("/keys/{id}", handler.DeleteAPIKey)
		})
	})

	// Serve static frontend if enabled
	if cfg.Server.EnableUI {
		// Try to serve embedded files
		staticContent, err := fs.Sub(staticFS, "static")
		if err == nil {
			fileServer := http.FileServer(http.FS(staticContent))
			r.Handle("/*", fileServer)
		} else {
			// Serve a simple placeholder if no static files
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Verity - Fact Checker</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        h1 { color: #2563eb; }
        code { background: #f1f5f9; padding: 2px 6px; border-radius: 4px; }
        .endpoint { margin: 10px 0; }
    </style>
</head>
<body>
    <h1>Verity API</h1>
    <p>Fact-checking API is running. Use the API endpoints below:</p>

    <h2>Endpoints</h2>
    <div class="endpoint"><code>GET /api/v1/health</code> - Health check</div>
    <div class="endpoint"><code>POST /api/v1/verify/text</code> - Verify text content</div>
    <div class="endpoint"><code>GET /api/v1/results</code> - List verification results</div>
    <div class="endpoint"><code>GET /api/v1/results/{id}</code> - Get specific result</div>

    <h2>Authentication</h2>
    <p>Use <code>Authorization: Bearer your-api-key</code> header for all requests except health check.</p>

    <h2>Create API Key</h2>
    <p><code>POST /api/v1/admin/keys</code> with body <code>{"name": "my-key"}</code></p>
</body>
</html>`))
			})
		}
	}

	return r
}
