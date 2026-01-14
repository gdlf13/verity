// Package search provides evidence search functionality from multiple sources.
package search

import (
	"context"
	"time"

	"github.com/factchecker/verity/internal/models"
)

// SearchClient defines the interface for search providers.
type SearchClient interface {
	// Search searches for evidence related to the query.
	Search(ctx context.Context, query string, maxResults int) ([]models.Evidence, error)

	// Name returns the source name.
	Name() string

	// Available returns whether this client is properly configured.
	Available() bool
}

// AggregatedSearchClient searches across multiple sources.
type AggregatedSearchClient struct {
	clients []SearchClient
}

// NewAggregatedSearchClient creates a new aggregated search client.
func NewAggregatedSearchClient(clients ...SearchClient) *AggregatedSearchClient {
	// Filter to only available clients
	available := make([]SearchClient, 0, len(clients))
	for _, c := range clients {
		if c.Available() {
			available = append(available, c)
		}
	}
	return &AggregatedSearchClient{clients: available}
}

// SearchResult contains results from a single source.
type SearchResult struct {
	Source    string
	Evidences []models.Evidence
	Error     error
}

// Search searches all configured sources concurrently.
func (a *AggregatedSearchClient) Search(ctx context.Context, query string, maxResultsPerSource int) ([]models.Evidence, []models.Warning) {
	if len(a.clients) == 0 {
		return nil, []models.Warning{{Source: "search", Message: "No search sources configured"}}
	}

	results := make(chan SearchResult, len(a.clients))

	// Search all sources concurrently
	for _, client := range a.clients {
		go func(c SearchClient) {
			evidences, err := c.Search(ctx, query, maxResultsPerSource)
			results <- SearchResult{
				Source:    c.Name(),
				Evidences: evidences,
				Error:     err,
			}
		}(client)
	}

	// Collect results with timeout
	var allEvidences []models.Evidence
	var warnings []models.Warning

	timeout := time.After(15 * time.Second)
	for i := 0; i < len(a.clients); i++ {
		select {
		case result := <-results:
			if result.Error != nil {
				warnings = append(warnings, models.Warning{
					Source:  result.Source,
					Message: result.Error.Error(),
				})
			} else {
				allEvidences = append(allEvidences, result.Evidences...)
			}
		case <-timeout:
			warnings = append(warnings, models.Warning{
				Source:  "search",
				Message: "Some sources timed out",
			})
			break
		case <-ctx.Done():
			warnings = append(warnings, models.Warning{
				Source:  "search",
				Message: "Search cancelled",
			})
			break
		}
	}

	return allEvidences, warnings
}

// HasClients returns whether any search clients are available.
func (a *AggregatedSearchClient) HasClients() bool {
	return len(a.clients) > 0
}
