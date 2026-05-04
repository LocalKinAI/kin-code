package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

const (
	webFetchTimeout    = 15 * time.Second
	webFetchUserAgent  = "kincode/1.0"
	webFetchMaxDefault = 10000
)

// WebFetchTool fetches a URL and returns content as plain text.
type WebFetchTool struct{}

func (w *WebFetchTool) Name() string { return "web_fetch" }

func (w *WebFetchTool) Description() string {
	return "Fetch a URL and return its content as plain text. HTML tags are stripped. Timeout: 15s."
}

func (w *WebFetchTool) Def() provider.ToolDef {
	return provider.NewToolDef("web_fetch", w.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch",
			},
			"max_length": map[string]any{
				"type":        "integer",
				"description": "Maximum content length in characters (default 10000)",
			},
		},
		"required": []string{"url"},
	})
}

// stripHTMLTags removes HTML tags and decodes common entities.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var htmlSpaceRe = regexp.MustCompile(`\s{3,}`)

func stripHTMLTags(s string) string {
	// Remove script and style blocks.
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	s = scriptRe.ReplaceAllString(s, "")
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	s = styleRe.ReplaceAllString(s, "")

	// Remove tags.
	s = htmlTagRe.ReplaceAllString(s, " ")

	// Decode common HTML entities.
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
	)
	s = replacer.Replace(s)

	// Collapse whitespace.
	s = htmlSpaceRe.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)

	return s
}

func (w *WebFetchTool) Execute(args map[string]any) (string, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return "", fmt.Errorf("url is required")
	}

	maxLength := webFetchMaxDefault
	if ml, ok := args["max_length"].(float64); ok && ml > 0 {
		maxLength = int(ml)
	}

	ctx, cancel := context.WithTimeout(context.Background(), webFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", webFetchUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read body with a cap to avoid huge downloads.
	limited := io.LimitReader(resp.Body, int64(maxLength*4)) // allow extra for HTML tags
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	content := stripHTMLTags(string(body))

	// Truncate to max length.
	if len(content) > maxLength {
		content = content[:maxLength] + "\n... (truncated)"
	}

	return fmt.Sprintf("---BEGIN WEB CONTENT---\n%s\n---END WEB CONTENT---", content), nil
}
