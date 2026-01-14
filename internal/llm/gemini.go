// Package llm provides Google Gemini implementation of the Provider interface.
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

// GeminiProvider implements Provider using Google Gemini API.
type GeminiProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGeminiProvider creates a new Gemini provider.
func NewGeminiProvider(cfg *config.LLMConfig) (*GeminiProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("Gemini API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-1.5-flash"
	}

	return &GeminiProvider{
		apiKey:     cfg.APIKey,
		model:      model,
		httpClient: &http.Client{},
	}, nil
}

// Name returns the provider name.
func (p *GeminiProvider) Name() string {
	return "gemini"
}

// SupportsEmbeddings returns true as Gemini supports embeddings.
func (p *GeminiProvider) SupportsEmbeddings() bool {
	return true
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	SystemInstruction *geminiContent        `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

type geminiEmbedRequest struct {
	Model   string `json:"model"`
	Content struct {
		Parts []geminiPart `json:"parts"`
	} `json:"content"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete generates a completion for the given prompt.
func (p *GeminiProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	return p.CompleteWithSystem(ctx, "", prompt, opts)
}

// CompleteWithSystem generates a completion with a system prompt.
func (p *GeminiProvider) CompleteWithSystem(ctx context.Context, system, user string, opts CompletionOptions) (string, error) {
	model := opts.Model
	if model == "" {
		model = p.model
	}

	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{{Text: user}},
				Role:  "user",
			},
		},
		GenerationConfig: &geminiGenerationConfig{
			Temperature:     opts.Temperature,
			MaxOutputTokens: maxTokens,
		},
	}

	// Add system instruction if provided
	if system != "" {
		reqBody.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: system}},
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, p.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result geminiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("Gemini error: %s (code %d)", result.Error.Message, result.Error.Code)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini returned no content")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}

// Embed generates embeddings for the given text.
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddingModel := "text-embedding-004" // Gemini's embedding model

	reqBody := geminiEmbedRequest{
		Model: fmt.Sprintf("models/%s", embeddingModel),
	}
	reqBody.Content.Parts = []geminiPart{{Text: text}}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent?key=%s",
		embeddingModel, p.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini embed request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result geminiEmbedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("Gemini error: %s", result.Error.Message)
	}

	if len(result.Embedding.Values) == 0 {
		return nil, fmt.Errorf("Gemini returned no embedding")
	}

	return result.Embedding.Values, nil
}
