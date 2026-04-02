// Package repl implements the interactive terminal for kin-code.
package repl

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/LocalKinAI/kin-code/pkg/agent"
	"golang.org/x/term"
)

const (
	colorReset  = "\033[0m"
	colorDim    = "\033[2m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorRed    = "\033[31m"
)

// REPL is the interactive read-eval-print loop.
type REPL struct {
	agent       *agent.Agent
	historyFile string
	history     []string
}

// New creates a new REPL.
func New(a *agent.Agent) *REPL {
	homeDir, _ := os.UserHomeDir()
	histDir := filepath.Join(homeDir, ".kin-code")
	_ = os.MkdirAll(histDir, 0755)

	return &REPL{
		agent:       a,
		historyFile: filepath.Join(histDir, "history"),
	}
}

// Run starts the interactive REPL loop.
func (r *REPL) Run(ctx context.Context) error {
	r.loadHistory()
	r.printBanner()

	// Handle Ctrl+C gracefully.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
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
			fmt.Printf("%s[tokens: %d in / %d out]%s\n", colorDim, usage.Input, usage.Output, colorReset)
		}
		fmt.Println()
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
		fmt.Println("  /help     — Show this help")
		fmt.Println("  /clear    — Clear conversation history")
		fmt.Println("  /compact  — Summarize conversation to reduce context size")
		fmt.Println("  /quit     — Exit kin-code")
		fmt.Println()
		fmt.Println(colorDim + "Tips:" + colorReset)
		fmt.Println("  - Paste multi-line text directly")
		fmt.Println("  - Ctrl+C to cancel current operation")
		fmt.Println("  - Ctrl+D to exit")

	case "/clear":
		r.agent.Clear()
		fmt.Println(colorDim + "Conversation cleared." + colorReset)

	case "/compact":
		fmt.Print(colorDim + "Compacting conversation..." + colorReset)
		if err := r.agent.Compact(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n%sError: %s%s\n", colorRed, err, colorReset)
		} else {
			fmt.Println(" done.")
		}

	case "/quit", "/exit", "/q":
		fmt.Println("Goodbye!")
		return true

	default:
		fmt.Printf("%sUnknown command: %s%s\n", colorYellow, cmd, colorReset)
	}

	return false
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
