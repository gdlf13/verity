// Package search provides Wikipedia search implementation.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/factchecker/verity/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// WikipediaClient searches using Wikipedia API.
type WikipediaClient struct {
	httpClient *http.Client
	languages  []string // Languages to search (e.g., "pt", "en")
}

// NewWikipediaClient creates a new Wikipedia client that searches PT and EN.
func NewWikipediaClient() *WikipediaClient {
	return &WikipediaClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		languages:  []string{"pt", "en"}, // Search Portuguese first, then English
	}
}

// Name returns the source name.
func (c *WikipediaClient) Name() string {
	return "Wikipedia"
}

// Available returns true as Wikipedia requires no API key.
func (c *WikipediaClient) Available() bool {
	return true
}

type wikiSearchResponse struct {
	Query struct {
		Search []struct {
			PageID int    `json:"pageid"`
			Title  string `json:"title"`
		} `json:"search"`
	} `json:"query"`
}

type wikiExtractResponse struct {
	Query struct {
		Pages map[string]struct {
			Title   string `json:"title"`
			Extract string `json:"extract"`
		} `json:"pages"`
	} `json:"query"`
}

// Search searches Wikipedia for evidence in multiple languages.
func (c *WikipediaClient) Search(ctx context.Context, query string, maxResults int) ([]models.Evidence, error) {
	keywords := extractKeywords(query)
	log.Debug().Str("original", query).Str("keywords", keywords).Msg("Wikipedia: Searching")

	var allEvidences []models.Evidence

	// Search in each configured language
	for _, lang := range c.languages {
		if len(allEvidences) >= maxResults {
			break
		}

		remaining := maxResults - len(allEvidences)
		evidences, err := c.searchLanguage(ctx, lang, keywords, remaining)
		if err != nil {
			log.Warn().Str("lang", lang).Err(err).Msg("Wikipedia search failed for language")
			continue
		}
		allEvidences = append(allEvidences, evidences...)
	}

	log.Debug().Int("count", len(allEvidences)).Msg("Wikipedia: Search completed")
	return allEvidences, nil
}

// searchLanguage searches Wikipedia in a specific language.
func (c *WikipediaClient) searchLanguage(ctx context.Context, lang, keywords string, maxResults int) ([]models.Evidence, error) {
	baseURL := fmt.Sprintf("https://%s.wikipedia.org/w/api.php", lang)

	// First, search for relevant pages
	searchURL := fmt.Sprintf("%s?action=query&list=search&srsearch=%s&format=json&srlimit=%d",
		baseURL, url.QueryEscape(keywords), maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Verity/1.0 (Fact-checking tool)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Wikipedia search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Wikipedia returned status %d", resp.StatusCode)
	}

	var searchData wikiSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchData); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	if len(searchData.Query.Search) == 0 {
		return nil, nil
	}

	// Get extracts for each page
	var pageIDs []string
	for _, result := range searchData.Query.Search {
		pageIDs = append(pageIDs, fmt.Sprintf("%d", result.PageID))
	}

	extractURL := fmt.Sprintf("%s?action=query&prop=extracts&exintro=true&explaintext=true&pageids=%s&format=json",
		baseURL, strings.Join(pageIDs, "|"))

	req, err = http.NewRequestWithContext(ctx, "GET", extractURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create extract request: %w", err)
	}
	req.Header.Set("User-Agent", "Verity/1.0 (Fact-checking tool)")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Wikipedia extract failed: %w", err)
	}
	defer resp.Body.Close()

	var extractData wikiExtractResponse
	if err := json.NewDecoder(resp.Body).Decode(&extractData); err != nil {
		return nil, fmt.Errorf("failed to decode extract response: %w", err)
	}

	now := time.Now()
	var evidences []models.Evidence

	langName := "Wikipedia"
	if lang != "en" {
		langName = fmt.Sprintf("Wikipedia (%s)", strings.ToUpper(lang))
	}

	for _, page := range extractData.Query.Pages {
		if page.Extract == "" {
			continue
		}

		// Truncate long extracts
		snippet := page.Extract
		if len(snippet) > 600 {
			snippet = snippet[:600] + "..."
		}

		evidences = append(evidences, models.Evidence{
			ID:          uuid.New().String(),
			SourceName:  langName,
			SourceURL:   fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.PathEscape(strings.ReplaceAll(page.Title, " ", "_"))),
			SourceType:  "encyclopedia",
			Snippet:     snippet,
			RetrievedAt: now,
		})
	}

	return evidences, nil
}
