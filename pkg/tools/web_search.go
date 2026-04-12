package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/LocalKinAI/kin-code/pkg/provider"
)

const (
	webSearchTimeout   = 15 * time.Second
	webSearchUserAgent = "kin-code/1.0"
	webSearchMaxCount  = 10
)

// WebSearchTool performs web searches using Tavily (when TAVILY_API_KEY is set)
// or DuckDuckGo HTML scraping as the default fallback.
type WebSearchTool struct{}

func (w *WebSearchTool) Name() string { return "web_search" }

func (w *WebSearchTool) Description() string {
	return "Search the web. Uses Tavily when TAVILY_API_KEY is set, otherwise DuckDuckGo. Returns titles, URLs, and snippets."
}

func (w *WebSearchTool) Def() provider.ToolDef {
	return provider.NewToolDef("web_search", w.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of results to return (default 5, max 10)",
			},
		},
		"required": []string{"query"},
	})
}

type searchResult struct {
	title   string
	url     string
	snippet string
}

// parseDDGResults extracts results from DuckDuckGo HTML.
func parseDDGResults(html string, count int) []searchResult {
	var results []searchResult

	// Find result links with class "result__a".
	linkRe := regexp.MustCompile(`(?is)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	snippetRe := regexp.MustCompile(`(?is)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)

	links := linkRe.FindAllStringSubmatch(html, count*2)
	snippets := snippetRe.FindAllStringSubmatch(html, count*2)

	for i, link := range links {
		if i >= count {
			break
		}
		if len(link) < 3 {
			continue
		}

		resultURL := link[1]
		// DuckDuckGo wraps URLs in a redirect; try to extract the actual URL.
		if strings.Contains(resultURL, "uddg=") {
			if u, err := url.Parse(resultURL); err == nil {
				if actual := u.Query().Get("uddg"); actual != "" {
					resultURL = actual
				}
			}
		}

		title := htmlTagRe.ReplaceAllString(link[2], "")
		title = strings.TrimSpace(title)

		snippet := ""
		if i < len(snippets) && len(snippets[i]) >= 2 {
			snippet = htmlTagRe.ReplaceAllString(snippets[i][1], "")
			snippet = strings.TrimSpace(snippet)
		}

		results = append(results, searchResult{
			title:   title,
			url:     resultURL,
			snippet: snippet,
		})
	}

	return results
}

// tavilySearchRequest is the request body for the Tavily Search API.
type tavilySearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	APIKey     string `json:"api_key"`
}

// tavilySearchResponse is the response from the Tavily Search API.
type tavilySearchResponse struct {
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// searchTavily calls the Tavily Search API and returns results.
func searchTavily(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	reqBody := tavilySearchRequest{
		Query:      query,
		MaxResults: count,
		APIKey:     apiKey,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal tavily request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("tavily API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tavilyResp tavilySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&tavilyResp); err != nil {
		return nil, fmt.Errorf("decode tavily response: %w", err)
	}

	results := make([]searchResult, 0, len(tavilyResp.Results))
	for _, r := range tavilyResp.Results {
		results = append(results, searchResult{
			title:   r.Title,
			url:     r.URL,
			snippet: r.Content,
		})
	}
	return results, nil
}

func (w *WebSearchTool) Execute(args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}

	count := 5
	if c, ok := args["count"].(float64); ok && c > 0 {
		count = int(c)
		if count > webSearchMaxCount {
			count = webSearchMaxCount
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), webSearchTimeout)
	defer cancel()

	// Use Tavily if API key is configured, otherwise fall back to DuckDuckGo.
	if apiKey := os.Getenv("TAVILY_API_KEY"); apiKey != "" {
		results, err := searchTavily(ctx, apiKey, query, count)
		if err != nil {
			return "", err
		}
		return formatSearchResults(results), nil
	}

	return w.searchDDG(ctx, query, count)
}

// searchDDG performs a web search using DuckDuckGo HTML scraping.
func (w *WebSearchTool) searchDDG(ctx context.Context, query string, count int) (string, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", webSearchUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	html := string(body)
	results := parseDDGResults(html, count)

	if len(results) == 0 {
		// Fallback: return stripped raw text.
		text := stripHTMLTags(html)
		if len(text) > 2000 {
			text = text[:2000] + "\n... (truncated)"
		}
		return fmt.Sprintf("No structured results found. Raw text:\n%s", text), nil
	}

	return formatSearchResults(results), nil
}

// formatSearchResults formats a slice of searchResult into a numbered text list.
func formatSearchResults(results []searchResult) string {
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n", i+1, r.title, r.url)
		if r.snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", r.snippet)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
