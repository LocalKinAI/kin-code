package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

const (
	webSearchTimeout   = 15 * time.Second
	webSearchUserAgent = "kincode/1.0"
	webSearchMaxCount  = 10
)

// WebSearchTool performs web searches using DuckDuckGo HTML.
type WebSearchTool struct{}

func (w *WebSearchTool) Name() string { return "web_search" }

func (w *WebSearchTool) Description() string {
	return "Search the web using DuckDuckGo. Returns titles, URLs, and snippets. No API key needed."
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

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	ctx, cancel := context.WithTimeout(context.Background(), webSearchTimeout)
	defer cancel()

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

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n", i+1, r.title, r.url)
		if r.snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", r.snippet)
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}
