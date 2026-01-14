// Package verify provides the main verification engine.
package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/factchecker/verity/internal/config"
	"github.com/factchecker/verity/internal/database"
	"github.com/factchecker/verity/internal/llm"
	"github.com/factchecker/verity/internal/models"
	"github.com/factchecker/verity/internal/search"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Engine orchestrates the complete fact-checking pipeline.
type Engine struct {
	extractor    *ClaimExtractor
	verifier     *ClaimVerifier
	searchClient *search.AggregatedSearchClient
	store        database.Store
	airGapped    bool
}

// NewEngine creates a new verification engine.
func NewEngine(cfg *config.Config, provider llm.Provider, store database.Store) *Engine {
	// Create search clients based on configuration
	var clients []search.SearchClient

	if cfg.Search.DuckDuckGo {
		clients = append(clients, search.NewDuckDuckGoClient())
	}
	// Wikipedia disabled - not considered a reliable source
	// if cfg.Search.Wikipedia {
	// 	clients = append(clients, search.NewWikipediaClient())
	// }
	if cfg.Search.PubMed {
		clients = append(clients, search.NewPubMedClient())
	}

	searchClient := search.NewAggregatedSearchClient(clients...)
	airGapped := !searchClient.HasClients()

	if airGapped {
		log.Warn().Msg("No search sources configured - running in air-gapped mode")
	}

	return &Engine{
		extractor:    NewClaimExtractor(provider, cfg.CustomClaimTypes),
		verifier:     NewClaimVerifier(provider),
		searchClient: searchClient,
		store:        store,
		airGapped:    airGapped,
	}
}

// VerifyText processes text through the complete fact-checking pipeline.
func (e *Engine) VerifyText(ctx context.Context, text string) (*models.VerificationResponse, error) {
	startTime := time.Now()

	// Calculate document hash
	hash := sha256.Sum256([]byte(text))
	docHash := hex.EncodeToString(hash[:])

	// Check for existing analysis
	existing, err := e.store.GetAnalysisByHash(ctx, docHash)
	if err != nil {
		log.Error().Err(err).Msg("Failed to check for existing analysis")
	}
	if existing != nil {
		log.Info().Str("id", existing.ID).Msg("Returning cached analysis")
		claims, _ := e.store.GetClaimsByAnalysis(ctx, existing.ID)
		return &models.VerificationResponse{
			ID:           existing.ID,
			DocumentHash: docHash,
			Analysis:     *existing,
			Claims:       claims,
		}, nil
	}

	// Step 1: Extract claims
	log.Info().Msg("Step 1: Extracting claims")
	claims, err := e.extractor.Extract(ctx, text)
	if err != nil {
		return nil, err
	}
	log.Info().Int("count", len(claims)).Msg("Claims extracted")

	// Step 2: Verify claims (concurrently with limited parallelism)
	log.Info().Msg("Step 2: Verifying claims")
	var warnings []models.Warning
	claims, claimWarnings := e.verifyClaims(ctx, claims)
	warnings = append(warnings, claimWarnings...)

	// Step 3: Calculate scores
	log.Info().Msg("Step 3: Calculating scores")
	analysis := e.calculateAnalysis(docHash, claims, time.Since(startTime))

	// Step 4: Persist results
	log.Info().Msg("Step 4: Persisting results")
	if err := e.store.SaveAnalysis(ctx, &analysis); err != nil {
		log.Error().Err(err).Msg("Failed to save analysis")
	}
	if err := e.store.SaveClaims(ctx, analysis.ID, claims); err != nil {
		log.Error().Err(err).Msg("Failed to save claims")
	}

	log.Info().
		Str("id", analysis.ID).
		Float64("score", analysis.OverallScore).
		Int("claims", analysis.TotalClaims).
		Int64("duration_ms", analysis.ProcessingTimeMs).
		Msg("Verification complete")

	return &models.VerificationResponse{
		ID:           analysis.ID,
		DocumentHash: docHash,
		Analysis:     analysis,
		Claims:       claims,
		Warnings:     warnings,
	}, nil
}

func (e *Engine) verifyClaims(ctx context.Context, claims []models.Claim) ([]models.Claim, []models.Warning) {
	var warnings []models.Warning
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrent verifications
	semaphore := make(chan struct{}, 5)

	for i := range claims {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			claim := &claims[idx]

			var status models.VerificationStatus
			var confidence float64
			var reasoning string
			var evidences []models.Evidence

			if e.airGapped {
				// Air-gapped mode: verify using LLM knowledge only
				var err error
				status, confidence, reasoning, err = e.verifier.VerifyWithoutEvidence(ctx, *claim)
				if err != nil {
					log.Error().Err(err).Str("claim", claim.Text[:min(50, len(claim.Text))]).Msg("Verification failed")
					status = models.StatusUnsupported
					reasoning = "Verification error"
				}
				claim.SourceType = models.SourceTypeModelBased
			} else {
				// Normal mode: search for evidence and verify
				searchResults, searchWarnings := e.searchClient.Search(ctx, claim.Text, 6)

				mu.Lock()
				warnings = append(warnings, searchWarnings...)
				mu.Unlock()

				evidences = searchResults

				// If no evidence found, fallback to LLM-based verification
				if len(evidences) == 0 {
					log.Info().Str("claim", claim.Text[:min(50, len(claim.Text))]).Msg("No evidence found, using LLM fallback")
					var err error
					status, confidence, reasoning, err = e.verifier.VerifyWithoutEvidence(ctx, *claim)
					if err != nil {
						log.Error().Err(err).Msg("LLM fallback verification failed")
						status = models.StatusUnsupported
						reasoning = "Verification error - no evidence found"
					}
					claim.SourceType = models.SourceTypeModelBased
				} else {
					var err error
					status, confidence, reasoning, err = e.verifier.Verify(ctx, *claim, evidences)
					if err != nil {
						log.Error().Err(err).Str("claim", claim.Text[:min(50, len(claim.Text))]).Msg("Verification failed")
						status = models.StatusUnsupported
						reasoning = "Verification error"
					}
					claim.SourceType = models.SourceTypeEvidenceBacked
				}
			}

			claim.Status = status
			claim.Confidence = confidence
			claim.Reasoning = reasoning
			claim.Evidences = evidences
			claim.CreatedAt = time.Now()
		}(i)
	}

	wg.Wait()
	return claims, warnings
}

func (e *Engine) calculateAnalysis(docHash string, claims []models.Claim, duration time.Duration) models.AnalysisResult {
	var verified, mixed, unsupported int
	for _, claim := range claims {
		switch claim.Status {
		case models.StatusVerified:
			verified++
		case models.StatusMixed:
			mixed++
		case models.StatusUnsupported:
			unsupported++
		}
	}

	// Calculate overall score (0-10)
	var score float64
	if len(claims) > 0 {
		// Weighted: verified=1.0, mixed=0.5, unsupported=0.0
		scoreSum := float64(verified) + float64(mixed)*0.5
		score = (scoreSum / float64(len(claims))) * 10
	}

	return models.AnalysisResult{
		ID:                uuid.New().String(),
		DocumentHash:      docHash,
		OverallScore:      score,
		TotalClaims:       len(claims),
		VerifiedClaims:    verified,
		MixedClaims:       mixed,
		UnsupportedClaims: unsupported,
		ProcessingTimeMs:  duration.Milliseconds(),
		Status:            "completed",
		CreatedAt:         time.Now(),
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
