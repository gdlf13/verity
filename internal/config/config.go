// Package config handles application configuration from YAML files and environment variables.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	LLM      LLMConfig      `yaml:"llm"`
	Search   SearchConfig   `yaml:"search_sources"`
	RateLimits RateLimitConfig `yaml:"rate_limits"`
	Logging  LoggingConfig  `yaml:"logging"`
	CustomClaimTypes map[string]ClaimTypeConfig `yaml:"custom_claim_types"`
}

type ServerConfig struct {
	Port     int  `yaml:"port"`
	EnableUI bool `yaml:"enable_ui"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"` // sqlite, postgres
	Path   string `yaml:"path"`   // for sqlite
	URL    string `yaml:"url"`    // for postgres
}

type LLMConfig struct {
	Provider        string `yaml:"provider"` // openai, azure, anthropic, ollama
	Model           string `yaml:"model"`
	APIKey          string `yaml:"api_key"`
	AzureEndpoint   string `yaml:"azure_endpoint"`
	AzureDeployment string `yaml:"azure_deployment"`
	OllamaURL       string `yaml:"ollama_url"`
	EmbeddingModel  string `yaml:"embedding_model"`
}

type SearchConfig struct {
	DuckDuckGo bool         `yaml:"duckduckgo"`
	Wikipedia  bool         `yaml:"wikipedia"`
	PubMed     bool         `yaml:"pubmed"`
	Google     GoogleConfig `yaml:"google"`
}

type GoogleConfig struct {
	Enabled        bool   `yaml:"enabled"`
	APIKey         string `yaml:"api_key"`
	SearchEngineID string `yaml:"search_engine_id"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `yaml:"default_requests_per_minute"`
	TokensPerDay      int `yaml:"default_tokens_per_day"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
}

type ClaimTypeConfig struct {
	Description string `yaml:"description"`
	PromptHint  string `yaml:"prompt_hint"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:     8080,
			EnableUI: true,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "./data/verity.db",
		},
		LLM: LLMConfig{
			Provider:       "openai",
			Model:          "gpt-4o-mini",
			EmbeddingModel: "text-embedding-ada-002",
		},
		Search: SearchConfig{
			DuckDuckGo: true,
			Wikipedia:  true,
			PubMed:     true,
		},
		RateLimits: RateLimitConfig{
			RequestsPerMinute: 60,
			TokensPerDay:      100000,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		CustomClaimTypes: make(map[string]ClaimTypeConfig),
	}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s (run with --generate-config to create one)", path)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Interpolate environment variables
	content := interpolateEnvVars(string(data))

	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(content), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// GenerateSample creates a sample configuration file.
func GenerateSample(path string) error {
	sample := `# Verity Configuration
# See documentation for all options

server:
  port: 8080
  enable_ui: true

database:
  driver: sqlite  # sqlite or postgres
  path: ./data/verity.db
  # url: postgresql://user:pass@localhost:5432/verity

llm:
  provider: openai  # openai, anthropic, gemini, ollama
  model: gpt-4o-mini
  api_key: ${OPENAI_API_KEY}
  embedding_model: text-embedding-ada-002

  # For Anthropic Claude:
  # provider: anthropic
  # model: claude-3-haiku-20240307
  # api_key: ${ANTHROPIC_API_KEY}

  # For Google Gemini:
  # provider: gemini
  # model: gemini-1.5-flash
  # api_key: ${GEMINI_API_KEY}

  # For Ollama (local):
  # provider: ollama
  # model: llama3
  # ollama_url: http://localhost:11434

search_sources:
  duckduckgo: true
  wikipedia: true
  pubmed: true
  google:
    enabled: false
    api_key: ${GOOGLE_API_KEY}
    search_engine_id: ${GOOGLE_CX}

rate_limits:
  default_requests_per_minute: 60
  default_tokens_per_day: 100000

logging:
  level: info  # debug, info, warn, error
  format: json # json or text

# Custom claim types (optional)
custom_claim_types:
  # regulatory:
  #   description: "Claims about regulatory compliance"
  #   prompt_hint: "Look for references to laws and regulations"
`
	return os.WriteFile(path, []byte(sample), 0644)
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Server.Port)
	}

	if c.Database.Driver != "sqlite" && c.Database.Driver != "postgres" {
		return fmt.Errorf("unsupported database driver: %s", c.Database.Driver)
	}

	validProviders := map[string]bool{"openai": true, "azure": true, "anthropic": true, "gemini": true, "ollama": true}
	if !validProviders[c.LLM.Provider] {
		return fmt.Errorf("unsupported LLM provider: %s", c.LLM.Provider)
	}

	// Validate API key requirements
	switch c.LLM.Provider {
	case "openai":
		if c.LLM.APIKey == "" {
			return fmt.Errorf("OpenAI API key is required")
		}
	case "anthropic":
		if c.LLM.APIKey == "" {
			return fmt.Errorf("Anthropic API key is required")
		}
	case "gemini":
		if c.LLM.APIKey == "" {
			return fmt.Errorf("Gemini API key is required")
		}
	}

	return nil
}

// interpolateEnvVars replaces ${VAR_NAME} with environment variable values.
func interpolateEnvVars(content string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(content, func(match string) string {
		varName := strings.TrimPrefix(strings.TrimSuffix(match, "}"), "${")
		if value := os.Getenv(varName); value != "" {
			return value
		}
		return match // Keep original if not set
	})
}
