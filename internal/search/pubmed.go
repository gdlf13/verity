// Package search provides PubMed search implementation.
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
)

// PubMedClient searches using NCBI PubMed API.
type PubMedClient struct {
	httpClient *http.Client
}

// NewPubMedClient creates a new PubMed client.
func NewPubMedClient() *PubMedClient {
	return &PubMedClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the source name.
func (c *PubMedClient) Name() string {
	return "PubMed"
}

// Available returns true as PubMed requires no API key for basic usage.
func (c *PubMedClient) Available() bool {
	return true
}

type pubmedSearchResponse struct {
	ESearchResult struct {
		IDList []string `json:"idlist"`
	} `json:"esearchresult"`
}

type pubmedSummaryResponse struct {
	Result map[string]struct {
		Title   string `json:"title"`
		PubDate string `json:"pubdate"`
		Source  string `json:"source"`
	} `json:"result"`
}

// Search searches PubMed for academic evidence.
func (c *PubMedClient) Search(ctx context.Context, query string, maxResults int) ([]models.Evidence, error) {
	// Search for article IDs
	searchURL := fmt.Sprintf(
		"https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?db=pubmed&term=%s&retmax=%d&retmode=json",
		url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PubMed search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PubMed returned status %d", resp.StatusCode)
	}

	var searchData pubmedSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchData); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	if len(searchData.ESearchResult.IDList) == 0 {
		return nil, nil
	}

	// Get summaries for found articles
	ids := strings.Join(searchData.ESearchResult.IDList, ",")
	summaryURL := fmt.Sprintf(
		"https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?db=pubmed&id=%s&retmode=json",
		ids)

	req, err = http.NewRequestWithContext(ctx, "GET", summaryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create summary request: %w", err)
	}

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PubMed summary failed: %w", err)
	}
	defer resp.Body.Close()

	var summaryData pubmedSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summaryData); err != nil {
		return nil, fmt.Errorf("failed to decode summary response: %w", err)
	}

	now := time.Now()
	var evidences []models.Evidence

	for _, pmid := range searchData.ESearchResult.IDList {
		article, ok := summaryData.Result[pmid]
		if !ok || article.Title == "" {
			continue
		}

		snippet := article.Title
		if article.Source != "" {
			snippet += fmt.Sprintf(" (Published in %s, %s)", article.Source, article.PubDate)
		}

		evidences = append(evidences, models.Evidence{
			ID:          uuid.New().String(),
			SourceName:  "PubMed",
			SourceURL:   fmt.Sprintf("https://pubmed.ncbi.nlm.nih.gov/%s/", pmid),
			SourceType:  "academic",
			Snippet:     snippet,
			RetrievedAt: now,
		})
	}

	return evidences, nil
}
