// Package search provides DuckDuckGo search implementation.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/factchecker/verity/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"
)

// DuckDuckGoClient searches using DuckDuckGo and fetches page content.
type DuckDuckGoClient struct {
	httpClient *http.Client
}

// NewDuckDuckGoClient creates a new DuckDuckGo client.
func NewDuckDuckGoClient() *DuckDuckGoClient {
	return &DuckDuckGoClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the source name.
func (c *DuckDuckGoClient) Name() string {
	return "DuckDuckGo"
}

// Available returns true as DuckDuckGo requires no API key.
func (c *DuckDuckGoClient) Available() bool {
	return true
}

type ddgResponse struct {
	Abstract      string `json:"Abstract"`
	AbstractURL   string `json:"AbstractURL"`
	Heading       string `json:"Heading"`
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"RelatedTopics"`
}

// extractKeywords extracts key terms from a claim for better search results
func extractKeywords(claim string) string {
	// Remove common stop words and keep meaningful terms
	stopWords := map[string]bool{
		// Portuguese
		"a": true, "o": true, "e": true, "de": true, "da": true, "do": true,
		"em": true, "para": true, "com": true, "por": true, "que": true,
		"um": true, "uma": true, "os": true, "as": true, "no": true, "na": true,
		"é": true, "são": true, "foi": true, "está": true, "tem": true,
		"sobre": true, "fez": true, "fazer": true, "como": true, "mais": true,
		"menos": true, "muito": true, "pouco": true, "seu": true, "sua": true,
		"ele": true, "ela": true, "eles": true, "elas": true, "nos": true,
		"das": true, "dos": true, "nas": true, "aos": true, "pela": true,
		"pelo": true, "entre": true, "após": true, "até": true, "desde": true,
		// English
		"the": true, "is": true, "are": true, "was": true, "were": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"of": true, "and": true, "an": true, "has": true, "have": true,
		"it": true, "its": true, "this": true, "that": true, "with": true,
		"be": true, "been": true, "being": true, "by": true, "from": true,
		"or": true, "but": true, "not": true, "also": true,
	}

	// Split and filter - preserve case for proper nouns
	words := strings.Fields(claim)
	var keywords []string
	var priorityKeywords []string // For capitalized words (likely proper nouns)

	for _, word := range words {
		// Check if word starts with uppercase (potential proper noun/entity)
		isProperNoun := len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z'

		// Clean and lowercase for stop word check
		cleanWord := strings.Trim(word, ".,!?;:\"'()[]«»")
		lowerWord := strings.ToLower(cleanWord)

		if len(lowerWord) > 2 && !stopWords[lowerWord] {
			if isProperNoun && len(cleanWord) > 2 {
				// Keep original case for proper nouns
				priorityKeywords = append(priorityKeywords, cleanWord)
			} else {
				keywords = append(keywords, lowerWord)
			}
		}
	}

	// Prioritize proper nouns, then regular keywords
	allKeywords := append(priorityKeywords, keywords...)

	// Keep more keywords for better context (increased from 8 to 12)
	if len(allKeywords) > 12 {
		allKeywords = allKeywords[:12]
	}

	return strings.Join(allKeywords, " ")
}

// Search searches DuckDuckGo for evidence and fetches page content with retry logic.
func (c *DuckDuckGoClient) Search(ctx context.Context, query string, maxResults int) ([]models.Evidence, error) {
	keywords := extractKeywords(query)
	log.Debug().Str("original", query).Str("keywords", keywords).Msg("DuckDuckGo: Searching")

	var evidences []models.Evidence
	var lastErr error

	// Retry logic for instant answer
	for attempt := 0; attempt < 2; attempt++ {
		instantEvidences, err := c.searchInstantAnswer(ctx, keywords, maxResults)
		if err == nil {
			evidences = append(evidences, instantEvidences...)
			break
		}
		lastErr = err
		if attempt < 1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Retry logic for HTML search
	for attempt := 0; attempt < 2; attempt++ {
		htmlEvidences, err := c.searchHTMLWithContent(ctx, keywords, maxResults)
		if err == nil {
			evidences = append(evidences, htmlEvidences...)
			break
		}
		lastErr = err
		log.Warn().Err(err).Int("attempt", attempt+1).Msg("DuckDuckGo HTML search failed")
		if attempt < 1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Deduplicate by URL
	seen := make(map[string]bool)
	var unique []models.Evidence
	for _, e := range evidences {
		if !seen[e.SourceURL] && e.Snippet != "" {
			seen[e.SourceURL] = true
			unique = append(unique, e)
			if len(unique) >= maxResults {
				break
			}
		}
	}

	log.Debug().Int("count", len(unique)).Msg("DuckDuckGo: Search completed")
	
	if len(unique) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return unique, nil
}

// searchInstantAnswer uses the Instant Answer API
func (c *DuckDuckGoClient) searchInstantAnswer(ctx context.Context, query string, maxResults int) ([]models.Evidence, error) {
	u := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var data ddgResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var evidences []models.Evidence
	now := time.Now()

	if data.Abstract != "" {
		evidences = append(evidences, models.Evidence{
			ID:          uuid.New().String(),
			SourceName:  "DuckDuckGo",
			SourceURL:   data.AbstractURL,
			SourceType:  "search_engine",
			Snippet:     data.Abstract,
			RetrievedAt: now,
		})
	}

	for _, topic := range data.RelatedTopics {
		if len(evidences) >= maxResults {
			break
		}
		if topic.Text != "" {
			evidences = append(evidences, models.Evidence{
				ID:          uuid.New().String(),
				SourceName:  "DuckDuckGo",
				SourceURL:   topic.FirstURL,
				SourceType:  "search_engine",
				Snippet:     topic.Text,
				RetrievedAt: now,
			})
		}
	}

	return evidences, nil
}

// searchResult holds parsed search result data
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searchHTMLWithContent searches and fetches actual page content
func (c *DuckDuckGoClient) searchHTMLWithContent(ctx context.Context, query string, maxResults int) ([]models.Evidence, error) {
	// Get search results
	results, err := c.getSearchResults(ctx, query, maxResults+2)
	if err != nil {
		return nil, err
	}

	log.Debug().Int("results", len(results)).Msg("DuckDuckGo: Found search results")

	// Fetch content from top results concurrently
	var evidences []models.Evidence
	var mu sync.Mutex
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, 3) // Limit concurrent fetches

	for _, result := range results {
		if len(evidences) >= maxResults {
			break
		}

		wg.Add(1)
		go func(r searchResult) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Try to fetch page content
			content, err := c.fetchPageContent(ctx, r.URL)
			if err != nil {
				log.Debug().Str("url", r.URL).Err(err).Msg("Failed to fetch page")
				// Use snippet from search results as fallback
				content = r.Snippet
			}

			if content == "" {
				content = r.Snippet
			}

			// Truncate very long content
			if len(content) > 1000 {
				content = content[:1000] + "..."
			}

			if content != "" {
				mu.Lock()
				evidences = append(evidences, models.Evidence{
					ID:          uuid.New().String(),
					SourceName:  extractDomain(r.URL),
					SourceURL:   r.URL,
					SourceType:  "web_page",
					Snippet:     content,
					RetrievedAt: time.Now(),
				})
				mu.Unlock()
			}
		}(result)
	}

	wg.Wait()
	return evidences, nil
}

// getSearchResults parses DuckDuckGo HTML search results
func (c *DuckDuckGoClient) getSearchResults(ctx context.Context, query string, maxResults int) ([]searchResult, error) {
	u := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pt-PT,pt;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	htmlContent := string(body)
	var results []searchResult

	// Pattern for result links with title
	linkPattern := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>([^<]+)</a>`)
	linkMatches := linkPattern.FindAllStringSubmatch(htmlContent, -1)

	// Pattern for snippets
	snippetPattern := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>([^<]+)</a>`)
	snippetMatches := snippetPattern.FindAllStringSubmatch(htmlContent, -1)

	for i, match := range linkMatches {
		if len(results) >= maxResults {
			break
		}
		if len(match) >= 3 {
			rawURL := match[1]
			title := strings.TrimSpace(html.UnescapeString(match[2]))

			// Decode DuckDuckGo redirect URL
			actualURL := decodeRedirectURL(rawURL)
			if actualURL == "" || strings.HasPrefix(actualURL, "//duckduckgo.com") {
				continue
			}

			snippet := ""
			if i < len(snippetMatches) && len(snippetMatches[i]) >= 2 {
				snippet = strings.TrimSpace(html.UnescapeString(snippetMatches[i][1]))
			}

			results = append(results, searchResult{
				Title:   title,
				URL:     actualURL,
				Snippet: snippet,
			})
		}
	}

	return results, nil
}

// decodeRedirectURL extracts actual URL from DuckDuckGo redirect
func decodeRedirectURL(rawURL string) string {
	if strings.Contains(rawURL, "uddg=") {
		decoded, err := url.QueryUnescape(rawURL)
		if err != nil {
			return rawURL
		}
		if idx := strings.Index(decoded, "uddg="); idx >= 0 {
			actualURL := decoded[idx+5:]
			if ampIdx := strings.Index(actualURL, "&"); ampIdx >= 0 {
				actualURL = actualURL[:ampIdx]
			}
			// Decode again if needed
			if decodedURL, err := url.QueryUnescape(actualURL); err == nil {
				return decodedURL
			}
			return actualURL
		}
	}
	return rawURL
}

// fetchPageContent fetches and extracts text content from a web page
func (c *DuckDuckGoClient) fetchPageContent(ctx context.Context, pageURL string) (string, error) {
	// Skip certain domains that block scraping
	skipDomains := []string{"facebook.com", "instagram.com", "twitter.com", "x.com", "linkedin.com"}
	for _, domain := range skipDomains {
		if strings.Contains(pageURL, domain) {
			return "", fmt.Errorf("skipped domain")
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "pt-PT,pt;q=0.9,en;q=0.8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	// Limit body size
	body, err := io.ReadAll(io.LimitReader(resp.Body, 500*1024))
	if err != nil {
		return "", err
	}

	return extractTextFromHTML(string(body)), nil
}

// extractTextFromHTML extracts readable text from HTML content
func extractTextFromHTML(htmlContent string) string {
	// Remove script and style tags
	scriptPattern := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	stylePattern := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	htmlContent = scriptPattern.ReplaceAllString(htmlContent, "")
	htmlContent = stylePattern.ReplaceAllString(htmlContent, "")

	// Remove HTML comments
	commentPattern := regexp.MustCompile(`<!--[\s\S]*?-->`)
	htmlContent = commentPattern.ReplaceAllString(htmlContent, "")

	// Try to extract main content areas
	mainPatterns := []string{
		`(?is)<article[^>]*>(.*?)</article>`,
		`(?is)<main[^>]*>(.*?)</main>`,
		`(?is)<div[^>]*class="[^"]*content[^"]*"[^>]*>(.*?)</div>`,
		`(?is)<div[^>]*class="[^"]*post[^"]*"[^>]*>(.*?)</div>`,
		`(?is)<div[^>]*class="[^"]*article[^"]*"[^>]*>(.*?)</div>`,
	}

	var mainContent string
	for _, pattern := range mainPatterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(htmlContent); len(matches) > 1 {
			mainContent = matches[1]
			break
		}
	}

	if mainContent == "" {
		// Fallback: try to get body content
		bodyPattern := regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
		if matches := bodyPattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
			mainContent = matches[1]
		} else {
			mainContent = htmlContent
		}
	}

	// Remove remaining HTML tags
	tagPattern := regexp.MustCompile(`<[^>]+>`)
	text := tagPattern.ReplaceAllString(mainContent, " ")

	// Decode HTML entities
	text = html.UnescapeString(text)

	// Clean up whitespace
	spacePattern := regexp.MustCompile(`\s+`)
	text = spacePattern.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	// Remove very short results
	if len(text) < 50 {
		return ""
	}

	return text
}

// extractDomain extracts domain name from URL for source attribution
func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "Web"
	}
	host := parsed.Hostname()
	// Remove www prefix
	host = strings.TrimPrefix(host, "www.")
	return host
}
