// Package llm provides Anthropic Claude implementation of the Provider interface.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/factchecker/verity/internal/config"
)

// AnthropicProvider implements Provider using Anthropic Claude API.
type AnthropicProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(cfg *config.LLMConfig) (*AnthropicProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "claude-3-haiku-20240307"
	}

	return &AnthropicProvider{
		apiKey:     cfg.APIKey,
		model:      model,
		httpClient: &http.Client{},
	}, nil
}

// Name returns the provider name.
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// SupportsEmbeddings returns false as Anthropic doesn't provide embeddings API.
func (p *AnthropicProvider) SupportsEmbeddings() bool {
	return false
}

type anthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	System    string              `json:"system,omitempty"`
	Messages  []anthropicMessage  `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete generates a completion for the given prompt.
func (p *AnthropicProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	return p.CompleteWithSystem(ctx, "", prompt, opts)
}

// CompleteWithSystem generates a completion with a system prompt.
func (p *AnthropicProvider) CompleteWithSystem(ctx context.Context, system, user string, opts CompletionOptions) (string, error) {
	model := opts.Model
	if model == "" {
		model = p.model
	}

	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages: []anthropicMessage{
			{Role: "user", Content: user},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("Anthropic error: %s", result.Error.Message)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("Anthropic returned no content")
	}

	return result.Content[0].Text, nil
}

// Embed is not supported by Anthropic.
func (p *AnthropicProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("Anthropic does not support embeddings")
}
