// Package llm provides a pluggable interface for LLM providers.
package llm

import (
	"context"
	"fmt"

	"github.com/factchecker/verity/internal/config"
)

// CompletionOptions contains options for completion requests.
type CompletionOptions struct {
	MaxTokens   int
	Temperature float64
	Model       string
}

// DefaultCompletionOptions returns sensible defaults.
func DefaultCompletionOptions() CompletionOptions {
	return CompletionOptions{
		MaxTokens:   2048,
		Temperature: 0.0,
	}
}

// Provider defines the interface for LLM providers.
type Provider interface {
	// Complete generates a completion for the given prompt.
	Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error)

	// CompleteWithSystem generates a completion with a system prompt.
	CompleteWithSystem(ctx context.Context, system, user string, opts CompletionOptions) (string, error)

	// Embed generates embeddings for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Name returns the provider name.
	Name() string

	// SupportsEmbeddings returns whether this provider supports embeddings.
	SupportsEmbeddings() bool
}

// NewProvider creates a new LLM provider based on configuration.
func NewProvider(cfg *config.LLMConfig) (Provider, error) {
	switch cfg.Provider {
	case "openai":
		return NewOpenAIProvider(cfg)
	case "anthropic":
		return NewAnthropicProvider(cfg)
	case "gemini":
		return NewGeminiProvider(cfg)
	case "ollama":
		return NewOllamaProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}
}
