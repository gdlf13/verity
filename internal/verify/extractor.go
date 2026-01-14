// Package verify provides claim extraction and verification functionality.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/factchecker/verity/internal/config"
	"github.com/factchecker/verity/internal/llm"
	"github.com/factchecker/verity/internal/models"
	"github.com/google/uuid"
)

// ClaimExtractor extracts atomic factual claims from text.
type ClaimExtractor struct {
	provider         llm.Provider
	customClaimTypes map[string]config.ClaimTypeConfig
}

// NewClaimExtractor creates a new claim extractor.
func NewClaimExtractor(provider llm.Provider, customTypes map[string]config.ClaimTypeConfig) *ClaimExtractor {
	return &ClaimExtractor{
		provider:         provider,
		customClaimTypes: customTypes,
	}
}

type extractedClaim struct {
	Text          string `json:"text"`
	Type          string `json:"type"`
	SentenceIndex int    `json:"sentence_index"`
}

type extractionResult struct {
	Claims []extractedClaim `json:"claims"`
}

// Extract extracts atomic factual claims from text.
func (e *ClaimExtractor) Extract(ctx context.Context, text string) ([]models.Claim, error) {
	systemPrompt := e.buildSystemPrompt()
	userPrompt := fmt.Sprintf("Text to analyze:\n\n%s", text)

	opts := llm.DefaultCompletionOptions()
	opts.MaxTokens = 4096

	response, err := e.provider.CompleteWithSystem(ctx, systemPrompt, userPrompt, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	// Parse JSON response
	claims, err := e.parseResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse extraction response: %w", err)
	}

	return claims, nil
}

func (e *ClaimExtractor) buildSystemPrompt() string {
	customTypesDesc := ""
	if len(e.customClaimTypes) > 0 {
		customTypesDesc = "\n\nCustom claim types:\n"
		for name, cfg := range e.customClaimTypes {
			customTypesDesc += fmt.Sprintf("- %s: %s. %s\n", name, cfg.Description, cfg.PromptHint)
		}
	}

	return fmt.Sprintf(`You are an expert fact-checker specialized in decomposing text into atomic, verifiable claims.

Your task:
1. Break down the text into individual, atomic factual claims
2. Each claim should be independently verifiable
3. Classify each claim by type
4. Preserve the original meaning and context
5. Number each claim by its position in the original text (0-indexed)

Claim types:
- statistical: Claims involving numbers, percentages, quantities
- factual: General factual statements
- temporal: Claims about dates, times, durations
- geographic: Claims about locations, places
- citation: References to other sources, quotes
- comparative: Claims comparing entities (X is larger/better than Y)
- causal: Claims about cause and effect relationships%s

Rules:
- Ignore opinions, questions, and subjective statements
- Focus only on objective, verifiable facts
- Each claim must be a complete, standalone statement
- Do not merge multiple facts into one claim

Respond with a JSON object containing an array of claims:
{
  "claims": [
    {"text": "The claim text", "type": "statistical", "sentence_index": 0},
    {"text": "Another claim", "type": "factual", "sentence_index": 1}
  ]
}

Only respond with the JSON object, no other text.`, customTypesDesc)
}

func (e *ClaimExtractor) parseResponse(response string) ([]models.Claim, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)

	// Handle markdown code blocks
	if strings.HasPrefix(response, "```") {
		re := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)\\s*```")
		matches := re.FindStringSubmatch(response)
		if len(matches) > 1 {
			response = matches[1]
		}
	}

	var result extractionResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// Try to find JSON object in response
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start >= 0 && end > start {
			response = response[start : end+1]
			if err := json.Unmarshal([]byte(response), &result); err != nil {
				return nil, fmt.Errorf("invalid JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no JSON found in response")
		}
	}

	claims := make([]models.Claim, len(result.Claims))
	for i, ec := range result.Claims {
		claims[i] = models.Claim{
			ID:            uuid.New().String(),
			Text:          ec.Text,
			Type:          models.ClaimType(ec.Type),
			SentenceIndex: ec.SentenceIndex,
			Status:        models.StatusPending,
		}
	}

	return claims, nil
}
