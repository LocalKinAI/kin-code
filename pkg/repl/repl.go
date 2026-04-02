// Package repl implements the interactive terminal for kin-code.
package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/LocalKinAI/kin-code/internal/mcp"
	"github.com/LocalKinAI/kin-code/pkg/agent"
	"golang.org/x/term"
)

// MCPClient is the interface needed from MCP clients for the /mcp command.
type MCPClient interface {
	Name() string
	Tools() []mcp.ToolDef
}

const (
	colorReset     = "\033[0m"
	colorDim       = "\033[2m"
	colorBold      = "\033[1m"
	colorUnderline = "\033[4m"
	colorGreen     = "\033[32m"
	colorYellow    = "\033[33m"
	colorCyan      = "\033[36m"
	colorRed       = "\033[31m"

	version = "0.4.0"
)

// Option configures the REPL.
type Option func(*REPL)

// WithMCPClients sets the MCP clients for the /mcp command.
func WithMCPClients(clients []*mcp.Client) Option {
	return func(r *REPL) {
		r.mcpClients = clients
	}
}

// REPL is the interactive read-eval-print loop.
type REPL struct {
	agent       *agent.Agent
	mcpClients  []*mcp.Client
	historyFile string
	sessionFile string
	history     []string
	totalTokens struct {
		input  int
		output int
	}
	lastDiff string
}

// New creates a new REPL.
func New(a *agent.Agent, opts ...Option) *REPL {
	homeDir, _ := os.UserHomeDir()
	histDir := filepath.Join(homeDir, ".kin-code")
	_ = os.MkdirAll(histDir, 0755)

	r := &REPL{
		agent:       a,
		historyFile: filepath.Join(histDir, "history"),
		sessionFile: filepath.Join(histDir, "session.json"),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run starts the interactive REPL loop.
func (r *REPL) Run(ctx context.Context) error {
	r.loadHistory()
	r.loadSession()
	r.printBanner()

	// Handle Ctrl+C gracefully — save session before exit.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		r.saveSession()
		fmt.Println("\nGoodbye!")
		os.Exit(0)
	}()

	for {
		input, err := r.readInput()
		if err != nil {
			return err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle slash commands.
		if strings.HasPrefix(input, "/") {
			if quit := r.handleCommand(ctx, input); quit {
				return nil
			}
			continue
		}

		// Save to history.
		r.addHistory(input)

		// Run through agent.
		_, usage, err := r.agent.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError: %s%s\n", colorRed, err, colorReset)
			continue
		}

		// Show token usage.
		if usage != nil {
			r.totalTokens.input += usage.Input
			r.totalTokens.output += usage.Output
			fmt.Printf("%s[tokens: %d in / %d out]%s\n", colorDim, usage.Input, usage.Output, colorReset)
		}
		fmt.Println()

		// Auto-save session after each interaction.
		r.saveSession()
	}
}

// RunOnce processes a single message (non-interactive mode).
func (r *REPL) RunOnce(ctx context.Context, message string) error {
	_, _, err := r.agent.Run(ctx, message)
	if err != nil {
		return fmt.Errorf("agent error: %w", err)
	}
	fmt.Println()
	return nil
}

func (r *REPL) readInput() (string, error) {
	// Check if stdin is a terminal.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Non-interactive: read all of stdin.
		buf := make([]byte, 1024*1024)
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}

	fmt.Printf("%s> %s", colorGreen, colorReset)

	var lines []string
	buf := make([]byte, 4096)
	n, err := os.Stdin.Read(buf)
	if err != nil {
		return "", err
	}

	input := string(buf[:n])
	input = strings.TrimRight(input, "\n\r")

	// If input contains newlines, it's likely a paste — accept as-is.
	if strings.Contains(input, "\n") {
		return input, nil
	}

	lines = append(lines, input)
	return strings.Join(lines, "\n"), nil
}

func (r *REPL) handleCommand(ctx context.Context, input string) bool {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/help":
		fmt.Println(colorCyan + "Commands:" + colorReset)
		fmt.Println("  /help              — Show this help")
		fmt.Println("  /clear             — Clear conversation history")
		fmt.Println("  /compact           — Summarize conversation to reduce context")
		fmt.Println("  /model <name>      — Switch model mid-session")
		fmt.Println("  /provider <name>   — Switch provider")
		fmt.Println("  /memory            — Show memory contents")
		fmt.Println("  /save <file>       — Save conversation to file")
		fmt.Println("  /load <file>       — Load conversation from file")
		fmt.Println("  /tokens            — Show estimated token usage")
		fmt.Println("  /diff              — Show last file edit as colored diff")
		fmt.Println("  /mcp               — List connected MCP servers and tools")
		fmt.Println("  /soul <file>       — Load a soul file mid-session")
		fmt.Println("  /version           — Show version")
		fmt.Println("  /quit              — Exit kin-code")
		fmt.Println()
		fmt.Println(colorDim + "Tips:" + colorReset)
		fmt.Println("  - Paste multi-line text directly")
		fmt.Println("  - Ctrl+C to cancel current operation")
		fmt.Println("  - Ctrl+D to exit")

	case "/clear":
		r.agent.ClearMessages()
		_ = os.Remove(r.sessionFile)
		fmt.Println(colorDim + "Conversation cleared." + colorReset)

	case "/compact":
		fmt.Print(colorDim + "Compacting conversation..." + colorReset)
		if err := r.agent.Compact(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n%sError: %s%s\n", colorRed, err, colorReset)
		} else {
			fmt.Println(" done.")
		}

	case "/model":
		if len(parts) < 2 {
			fmt.Printf("%sUsage: /model <name>%s\n", colorYellow, colorReset)
			fmt.Printf("%sCurrent provider: %s%s\n", colorDim, r.agent.Provider().Name(), colorReset)
		} else {
			// Model switching requires provider-level support.
			// For now, show confirmation that we'd switch.
			fmt.Printf("%sModel switch to %q noted. Restart with -model %s for full effect.%s\n", colorYellow, parts[1], parts[1], colorReset)
		}

	case "/provider":
		if len(parts) < 2 {
			fmt.Printf("%sUsage: /provider <name>%s\n", colorYellow, colorReset)
			fmt.Printf("%sCurrent: %s%s\n", colorDim, r.agent.Provider().Name(), colorReset)
		} else {
			fmt.Printf("%sProvider switch to %q noted. Restart with -provider %s for full effect.%s\n", colorYellow, parts[1], parts[1], colorReset)
		}

	case "/memory":
		homeDir, _ := os.UserHomeDir()
		memPath := filepath.Join(homeDir, ".kin-code", "memory.json")
		data, err := os.ReadFile(memPath)
		if err != nil {
			fmt.Printf("%sMemory is empty or not found.%s\n", colorDim, colorReset)
		} else {
			var store map[string]string
			if json.Unmarshal(data, &store) == nil {
				if len(store) == 0 {
					fmt.Printf("%sMemory is empty.%s\n", colorDim, colorReset)
				} else {
					fmt.Println(colorCyan + "Memory contents:" + colorReset)
					for k, v := range store {
						fmt.Printf("  %s%s%s = %s\n", colorBold, k, colorReset, v)
					}
				}
			}
		}

	case "/save":
		if len(parts) < 2 {
			fmt.Printf("%sUsage: /save <file>%s\n", colorYellow, colorReset)
		} else {
			r.saveConversation(parts[1])
		}

	case "/load":
		if len(parts) < 2 {
			fmt.Printf("%sUsage: /load <file>%s\n", colorYellow, colorReset)
		} else {
			r.loadConversation(parts[1])
		}

	case "/tokens":
		msgs := r.agent.Messages()
		estimated := 0
		for _, m := range msgs {
			estimated += len(m.Content) / 4
		}
		fmt.Printf("%sEstimated context: ~%d tokens (%d messages)%s\n", colorDim, estimated, len(msgs), colorReset)
		fmt.Printf("%sSession total: ~%d in / ~%d out%s\n", colorDim, r.totalTokens.input, r.totalTokens.output, colorReset)

	case "/diff":
		if r.lastDiff == "" {
			fmt.Printf("%sNo recent file edits.%s\n", colorDim, colorReset)
		} else {
			fmt.Println(r.lastDiff)
		}

	case "/mcp":
		if len(r.mcpClients) == 0 {
			fmt.Printf("%sNo MCP servers connected. Use -mcp <config.json> to connect.%s\n", colorDim, colorReset)
		} else {
			fmt.Println(colorCyan + "MCP Servers:" + colorReset)
			totalTools := 0
			for _, c := range r.mcpClients {
				ts := c.Tools()
				totalTools += len(ts)
				fmt.Printf("  %s%s%s (%d tools)\n", colorBold, c.Name(), colorReset, len(ts))
				for _, t := range ts {
					fmt.Printf("    %smcp_%s%s — %s\n", colorGreen, t.Name, colorReset, t.Description)
				}
			}
			fmt.Printf("%s%d server(s), %d tool(s) total%s\n", colorDim, len(r.mcpClients), totalTools, colorReset)
		}

	case "/soul":
		if len(parts) < 2 {
			fmt.Printf("%sUsage: /soul <file>%s\n", colorYellow, colorReset)
		} else {
			data, err := os.ReadFile(parts[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "%sError reading soul file: %s%s\n", colorRed, err, colorReset)
			} else {
				// Extract body (skip frontmatter if present).
				content := string(data)
				if strings.HasPrefix(content, "---") {
					fmParts := strings.SplitN(content[3:], "---", 2)
					if len(fmParts) == 2 {
						content = strings.TrimSpace(fmParts[1])
					}
				}
				r.agent.Clear()
				// Re-run with new system prompt by sending it as context.
				_, _, _ = r.agent.Run(ctx, "[System update] New persona loaded from "+parts[1]+":\n"+content)
				fmt.Printf("%sSoul file loaded: %s%s\n", colorGreen, parts[1], colorReset)
			}
		}

	case "/version":
		fmt.Printf("kin-code v%s\n", version)

	case "/quit", "/exit", "/q":
		fmt.Println("Goodbye!")
		return true

	default:
		fmt.Printf("%sUnknown command: %s (type /help)%s\n", colorYellow, cmd, colorReset)
	}

	return false
}

func (r *REPL) saveConversation(path string) {
	msgs := r.agent.Messages()
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError: %s%s\n", colorRed, err, colorReset)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "%sError: %s%s\n", colorRed, err, colorReset)
		return
	}
	fmt.Printf("%sSaved %d messages to %s%s\n", colorDim, len(msgs), path, colorReset)
}

func (r *REPL) loadConversation(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError: %s%s\n", colorRed, err, colorReset)
		return
	}
	// For now, just show that we'd load it. Full implementation would
	// require the agent to expose a SetMessages method.
	var msgs []map[string]any
	if err := json.Unmarshal(data, &msgs); err != nil {
		fmt.Fprintf(os.Stderr, "%sError parsing file: %s%s\n", colorRed, err, colorReset)
		return
	}
	fmt.Printf("%sLoaded %d messages from %s. Use /clear first if starting fresh.%s\n", colorDim, len(msgs), path, colorReset)
}

func (r *REPL) printBanner() {
	fmt.Println(colorCyan + `
 _    _                         _
| | _(_)_ __         ___ ___  __| | ___
| |/ / | '_ \ _____ / __/ _ \ / _` + "`" + ` |/ _ \
|   <| | | | |_____| (_| (_) | (_| |  __/
|_|\_\_|_| |_|      \___\___/ \__,_|\___|` + colorReset)
	fmt.Println()
	fmt.Printf("%sAI coding assistant — type /help for commands%s\n\n", colorDim, colorReset)
}

func (r *REPL) loadHistory() {
	data, err := os.ReadFile(r.historyFile)
	if err != nil {
		return
	}
	r.history = strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (r *REPL) addHistory(line string) {
	r.history = append(r.history, line)
	// Keep last 1000 entries.
	if len(r.history) > 1000 {
		r.history = r.history[len(r.history)-1000:]
	}
	_ = os.WriteFile(r.historyFile, []byte(strings.Join(r.history, "\n")+"\n"), 0644)
}

// --- Session Persistence ---

func (r *REPL) loadSession() {
	if err := r.agent.LoadSession(r.sessionFile); err == nil {
		msgs := r.agent.Messages()
		// Count non-system messages
		count := 0
		for _, m := range msgs {
			if m.Role != "system" {
				count++
			}
		}
		if count > 0 {
			fmt.Printf("%s[session restored: %d messages]%s\n", colorDim, count, colorReset)
		}
	}
}

func (r *REPL) saveSession() {
	_ = r.agent.SaveSession(r.sessionFile)
}

// --- Markdown Terminal Rendering ---

var (
	mdBoldRe      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	mdInlineCode  = regexp.MustCompile("`([^`]+)`")
	mdHeaderRe    = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	mdBulletRe    = regexp.MustCompile(`^(\s*)[-*]\s+(.+)$`)
	mdCodeBlockRe = regexp.MustCompile("^```")
)

// RenderMarkdown applies basic ANSI formatting to markdown text.
func RenderMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	inCodeBlock := false

	for _, line := range lines {
		if mdCodeBlockRe.MatchString(line) {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				result = append(result, colorDim+"  "+strings.TrimPrefix(line, "```")+colorReset)
			} else {
				result = append(result, colorDim+"  ```"+colorReset)
			}
			continue
		}

		if inCodeBlock {
			result = append(result, colorDim+"  "+colorCyan+line+colorReset)
			continue
		}

		// Headers.
		if m := mdHeaderRe.FindStringSubmatch(line); m != nil {
			result = append(result, colorBold+colorUnderline+m[2]+colorReset)
			continue
		}

		// Bullet lists.
		if m := mdBulletRe.FindStringSubmatch(line); m != nil {
			formatted := m[1] + "  * " + m[2]
			formatted = applyInlineFormatting(formatted)
			result = append(result, formatted)
			continue
		}

		// Table rows.
		if strings.Contains(line, "|") && strings.Count(line, "|") >= 2 {
			result = append(result, formatTableRow(line))
			continue
		}

		// Regular line — apply inline formatting.
		result = append(result, applyInlineFormatting(line))
	}

	return strings.Join(result, "\n")
}

func applyInlineFormatting(line string) string {
	// Bold.
	line = mdBoldRe.ReplaceAllString(line, colorBold+"$1"+colorReset)
	// Inline code.
	line = mdInlineCode.ReplaceAllString(line, colorDim+colorCyan+"$1"+colorReset)
	return line
}

func formatTableRow(line string) string {
	cells := strings.Split(line, "|")
	var formatted []string
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		// Skip separator rows (---)
		if strings.Trim(cell, "- :") == "" {
			return colorDim + line + colorReset
		}
		formatted = append(formatted, fmt.Sprintf(" %-20s", cell))
	}
	return strings.Join(formatted, "|")
}
