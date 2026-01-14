// Package verify provides claim verification against evidence.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/factchecker/verity/internal/llm"
	"github.com/factchecker/verity/internal/models"
)

// ClaimVerifier verifies claims against evidence.
type ClaimVerifier struct {
	provider llm.Provider
}

// NewClaimVerifier creates a new claim verifier.
func NewClaimVerifier(provider llm.Provider) *ClaimVerifier {
	return &ClaimVerifier{provider: provider}
}

type verificationResult struct {
	Status     string  `json:"verification_status"`
	Confidence float64 `json:"confidence_score"`
	Reasoning  string  `json:"reasoning"`
}

// Verify verifies a claim against provided evidence.
func (v *ClaimVerifier) Verify(ctx context.Context, claim models.Claim, evidences []models.Evidence) (models.VerificationStatus, float64, string, error) {
	if len(evidences) == 0 {
		return models.StatusUnsupported, 0.0, "No evidence found to support this claim", nil
	}

	systemPrompt := `You are a fact-checking expert. Analyze the claim against the provided evidence.

Your task:
1. Compare the claim with each piece of evidence
2. Determine if the evidence supports, contradicts, or is neutral to the claim
3. Assign a confidence score (0-1) based on:
   - Quality and authority of sources
   - Consistency across multiple sources
   - Recency of information
   - Specificity of evidence

Respond with a JSON object:
{
  "verification_status": "verified|mixed|unsupported",
  "confidence_score": 0.0-1.0,
  "reasoning": "Brief explanation of your decision"
}

Status meanings:
- verified: Evidence strongly supports the claim
- mixed: Evidence is conflicting or partially supports
- unsupported: No evidence supports the claim or evidence contradicts it

Only respond with the JSON object, no other text.`

	// Format evidence for the prompt
	var evidenceText strings.Builder
	for i, e := range evidences {
		evidenceText.WriteString(fmt.Sprintf("\nEvidence %d:\n", i+1))
		evidenceText.WriteString(fmt.Sprintf("Source: %s (%s)\n", e.SourceName, e.SourceType))
		evidenceText.WriteString(fmt.Sprintf("URL: %s\n", e.SourceURL))
		evidenceText.WriteString(fmt.Sprintf("Text: %s\n", e.Snippet))
	}

	userPrompt := fmt.Sprintf("Claim: %s\n\nEvidence found:%s\n\nAnalyze and provide verification result.", claim.Text, evidenceText.String())

	opts := llm.DefaultCompletionOptions()
	response, err := v.provider.CompleteWithSystem(ctx, systemPrompt, userPrompt, opts)
	if err != nil {
		return models.StatusUnsupported, 0.0, "", fmt.Errorf("verification failed: %w", err)
	}

	result, err := v.parseResponse(response)
	if err != nil {
		return models.StatusUnsupported, 0.0, "", fmt.Errorf("failed to parse verification response: %w", err)
	}

	status := models.StatusUnsupported
	switch result.Status {
	case "verified":
		status = models.StatusVerified
	case "mixed":
		status = models.StatusMixed
	case "unsupported":
		status = models.StatusUnsupported
	}

	return status, result.Confidence, result.Reasoning, nil
}

// VerifyWithoutEvidence uses LLM knowledge to verify a claim (air-gapped mode).
func (v *ClaimVerifier) VerifyWithoutEvidence(ctx context.Context, claim models.Claim) (models.VerificationStatus, float64, string, error) {
	systemPrompt := `You are a fact-checking expert. Analyze the claim using your training knowledge.

IMPORTANT: You are operating without external evidence sources. Base your assessment only on your training data.

Your task:
1. Assess whether the claim is likely to be true based on your knowledge
2. Be conservative - if uncertain, mark as unsupported
3. Assign a confidence score (0-1), keeping in mind that without external verification, confidence should generally be lower

Respond with a JSON object:
{
  "verification_status": "verified|mixed|unsupported",
  "confidence_score": 0.0-1.0,
  "reasoning": "Brief explanation including any caveats about relying on model knowledge"
}

Status meanings:
- verified: You are confident the claim is factually correct
- mixed: The claim is partially correct or you have some uncertainty
- unsupported: You cannot verify the claim or believe it may be incorrect

Only respond with the JSON object, no other text.`

	userPrompt := fmt.Sprintf("Claim to verify: %s", claim.Text)

	opts := llm.DefaultCompletionOptions()
	response, err := v.provider.CompleteWithSystem(ctx, systemPrompt, userPrompt, opts)
	if err != nil {
		return models.StatusUnsupported, 0.0, "", fmt.Errorf("verification failed: %w", err)
	}

	result, err := v.parseResponse(response)
	if err != nil {
		return models.StatusUnsupported, 0.0, "", fmt.Errorf("failed to parse verification response: %w", err)
	}

	status := models.StatusUnsupported
	switch result.Status {
	case "verified":
		status = models.StatusVerified
	case "mixed":
		status = models.StatusMixed
	case "unsupported":
		status = models.StatusUnsupported
	}

	// Add disclaimer to reasoning
	reasoning := result.Reasoning + " [Note: Verified using model knowledge only, without external evidence sources]"

	return status, result.Confidence, reasoning, nil
}

func (v *ClaimVerifier) parseResponse(response string) (*verificationResult, error) {
	response = strings.TrimSpace(response)

	// Handle markdown code blocks
	if strings.HasPrefix(response, "```") {
		re := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)\\s*```")
		matches := re.FindStringSubmatch(response)
		if len(matches) > 1 {
			response = matches[1]
		}
	}

	var result verificationResult
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

	return &result, nil
}
