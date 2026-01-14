// Package llm provides OpenAI implementation of the Provider interface.
package llm

import (
	"context"
	"fmt"

	"github.com/factchecker/verity/internal/config"
	openai "github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements Provider using OpenAI API.
type OpenAIProvider struct {
	client         *openai.Client
	model          string
	embeddingModel string
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(cfg *config.LLMConfig) (*OpenAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}

	client := openai.NewClient(cfg.APIKey)

	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	embeddingModel := cfg.EmbeddingModel
	if embeddingModel == "" {
		embeddingModel = "text-embedding-ada-002"
	}

	return &OpenAIProvider{
		client:         client,
		model:          model,
		embeddingModel: embeddingModel,
	}, nil
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// SupportsEmbeddings returns true as OpenAI supports embeddings.
func (p *OpenAIProvider) SupportsEmbeddings() bool {
	return true
}

// Complete generates a completion for the given prompt.
func (p *OpenAIProvider) Complete(ctx context.Context, prompt string, opts CompletionOptions) (string, error) {
	return p.CompleteWithSystem(ctx, "", prompt, opts)
}

// CompleteWithSystem generates a completion with a system prompt.
func (p *OpenAIProvider) CompleteWithSystem(ctx context.Context, system, user string, opts CompletionOptions) (string, error) {
	model := opts.Model
	if model == "" {
		model = p.model
	}

	messages := []openai.ChatCompletionMessage{}
	if system != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: system,
		})
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: user,
	})

	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: float32(opts.Temperature),
	})
	if err != nil {
		return "", fmt.Errorf("OpenAI completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI returned no choices")
	}

	return resp.Choices[0].Message.Content, nil
}

// Embed generates embeddings for the given text.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(p.embeddingModel),
		Input: []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI embedding failed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("OpenAI returned no embeddings")
	}

	return resp.Data[0].Embedding, nil
}
