// kincode is a lightweight AI coding assistant for the terminal.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LocalKinAI/kincode/internal/mcp"
	"github.com/LocalKinAI/kincode/pkg/agent"
	"github.com/LocalKinAI/kincode/pkg/permission"
	"github.com/LocalKinAI/kincode/pkg/provider"
	"github.com/LocalKinAI/kincode/pkg/repl"
	"github.com/LocalKinAI/kincode/pkg/server"
	"github.com/LocalKinAI/kincode/pkg/tools"
	"gopkg.in/yaml.v3"
)

const version = "0.6.0"

func main() {
	// Subprocess hygiene: when kincode runs as a child (typically of
	// KinClaw Mac), exit cleanly when the parent dies instead of
	// being reparented to launchd and leaking the bound port. No-op
	// when launched standalone from the CLI.
	startOrphanWatch()

	providerName := flag.String("provider", "anthropic", "LLM provider: anthropic, openai, ollama")
	model := flag.String("model", "claude-sonnet-4-6", "Model name")
	soulFile := flag.String("soul", "", "Path to .soul.md file for personality/rules")
	apiKey := flag.String("api-key", "", "API key (or use ANTHROPIC_API_KEY / OPENAI_API_KEY env)")
	endpoint := flag.String("endpoint", "", "Custom API endpoint (for ollama/compatible APIs)")
	mcpConfig := flag.String("mcp", "", "Path to MCP servers config JSON file")
	yolo := flag.Bool("yolo", false, "Auto-approve all tool calls without confirmation")
	showVersion := flag.Bool("version", false, "Show version and exit")
	login := flag.Bool("login", false, "Login via Claude OAuth (use your Claude account, no API key needed)")
	serve := flag.Bool("serve", false, "Run as HTTP+SSE server instead of REPL (for desktop shells)")
	port := flag.Int("port", 5002, "Port for -serve mode (default 5002, sits next to kinclaw on 5001)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("kincode v%s\n", version)
		os.Exit(0)
	}

	// Handle -login: run OAuth flow and exit.
	if *login {
		if _, err := provider.OAuthLogin(); err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %s\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Track which flags the user set explicitly. Used below to let
	// CLI flags override soul brain config — explicit always wins.
	explicitFlag := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicitFlag[f.Name] = true })

	// Load soul file early — its brain config can change the provider /
	// model / endpoint we pick below, so we need it before key resolution.
	systemPrompt := defaultSystemPrompt()
	var soulFM *soulFrontmatter
	if *soulFile != "" {
		sp, fm, err := loadSoulFile(*soulFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading soul file: %s\n", err)
			os.Exit(1)
		}
		systemPrompt = sp
		soulFM = fm
	}

	// Apply soul brain config — fills in any flag the CLI didn't set
	// explicitly. Lets a soul like Pilot (brain.provider=ollama,
	// brain.model=kimi-k2.5:cloud) drive kincode just by passing
	// `-soul pilot.soul.md` — same shape kinclaw uses.
	if soulFM != nil {
		if soulFM.Brain != nil {
			if !explicitFlag["provider"] && soulFM.Brain.Provider != "" {
				*providerName = soulFM.Brain.Provider
			}
			if !explicitFlag["model"] && soulFM.Brain.Model != "" {
				*model = soulFM.Brain.Model
			}
			if !explicitFlag["endpoint"] && soulFM.Brain.Endpoint != "" {
				*endpoint = soulFM.Brain.Endpoint
			}
		} else if soulFM.Model != "" && !explicitFlag["model"] {
			// Legacy top-level model: field — older kincode souls.
			*model = soulFM.Model
		}
	}

	// Resolve API key.
	key := *apiKey
	isOAuth := false
	if key == "" {
		switch *providerName {
		case "anthropic":
			key = os.Getenv("ANTHROPIC_API_KEY")
		case "openai":
			key = os.Getenv("OPENAI_API_KEY")
		case "ollama":
			// Ollama doesn't need a key.
		}
	}

	// Serve-mode auto-fallback to Ollama when the user explicitly
	// asked for Anthropic but has no creds available. The desktop
	// shell (KinClaw Mac) spawns kincode with `-provider anthropic`
	// as the default; if the user didn't set ANTHROPIC_API_KEY and
	// hasn't OAuth'd, switching to Ollama / kimi-k2.5:cloud is the
	// graceful fallback — matches what kinclaw kernel uses by default
	// (per pilot.soul.md), so kincode "just works" on the same Ollama
	// install the user already has running for kinclaw.
	//
	// Skipped when the user explicitly passed -provider on the CLI
	// (their choice wins) or already has Anthropic creds.
	if *serve && *providerName == "anthropic" && key == "" {
		if _, oauthErr := provider.GetValidToken(); oauthErr != nil {
			fmt.Fprintln(os.Stderr,
				"[serve] no Anthropic creds — falling back to ollama / kimi-k2.5:cloud (matches kinclaw)")
			*providerName = "ollama"
			if *model == "claude-sonnet-4-6" {
				*model = "kimi-k2.5:cloud"
			}
		}
	}

	// Set default endpoints and models per provider.
	ep := *endpoint
	mdl := *model
	switch *providerName {
	case "anthropic":
		if key == "" {
			// Try OAuth token as fallback.
			token, err := provider.GetValidToken()
			if err != nil {
				if *serve {
					// Server mode: don't hard-exit. The server is the
					// "always available" surface for desktop shells —
					// boot it, let the user resolve creds via env or
					// /api/login (future), and surface the missing-key
					// state through chat error events on first turn.
					fmt.Fprintf(os.Stderr, "[serve] no Anthropic key (env or OAuth); chat turns will fail until creds are added\n")
				} else {
					fmt.Fprintf(os.Stderr, "OAuth error: %v\n\n", err)
					fmt.Fprintln(os.Stderr, "No API key available. Either:")
					fmt.Fprintln(os.Stderr, "  1. Run 'kincode -login' to use your Claude account")
					fmt.Fprintln(os.Stderr, "  2. Set ANTHROPIC_API_KEY environment variable")
					fmt.Fprintln(os.Stderr, "  3. Use -api-key flag")
					os.Exit(1)
				}
			} else {
				key = token
				isOAuth = true
				// Default to Haiku 4.5 for OAuth users (included in all Claude plans).
				if mdl == "claude-sonnet-4-6" {
					mdl = "claude-haiku-4-5-20251001"
				}
				fmt.Println("Using Claude OAuth session (model: " + mdl + ")")
			}
		}
	case "openai":
		if key == "" {
			if *serve {
				fmt.Fprintln(os.Stderr, "[serve] no OpenAI key; chat turns will fail until creds are added")
			} else {
				fmt.Fprintln(os.Stderr, "Error: OPENAI_API_KEY not set. Use -api-key or set the environment variable.")
				os.Exit(1)
			}
		}
		if mdl == "claude-sonnet-4-6" {
			mdl = "gpt-4o" // default for OpenAI
		}
	case "ollama":
		if ep == "" {
			ep = "http://localhost:11434/v1/chat/completions"
		}
		if mdl == "claude-sonnet-4-6" {
			mdl = "qwen3:8b" // default for Ollama
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown provider %q (use anthropic, openai, or ollama)\n", *providerName)
		os.Exit(1)
	}

	// Create provider.
	var p provider.Provider
	switch *providerName {
	case "anthropic":
		ap := provider.NewAnthropic(key, mdl)
		if isOAuth {
			ap.SetOAuth(true)
		}
		p = ap
	case "openai", "ollama":
		p = provider.NewOpenAI(key, mdl, ep)
	}

	// (Soul was loaded earlier — soul.brain config influenced the
	//  provider/model/endpoint resolution above.)

	// Configure extended thinking if enabled in soul file.
	if soulFM != nil && soulFM.Thinking {
		if ap, ok := p.(*provider.AnthropicProvider); ok {
			ap.SetThinking(true, soulFM.ThinkingBudget)
			ap.SetOnThinking(func(text string) {
				fmt.Print(text)
			})
			budget := soulFM.ThinkingBudget
			if budget <= 0 {
				budget = 10000
			}
			fmt.Printf("Extended thinking enabled (budget: %d tokens)\n", budget)
		}
	}

	// Initialize tools.
	registry := tools.NewRegistry()
	// Register agent_spawn with a factory that creates sub-agents.
	tools.RegisterDefaultsWithAgent(registry, func() tools.SubAgentRunner {
		subRegistry := tools.NewRegistry()
		tools.RegisterDefaults(subRegistry)
		return agent.New(agent.Config{
			Provider:     p,
			Tools:        subRegistry,
			Permissions:  permission.New(true), // auto-approve for sub-agents
			SystemPrompt: systemPrompt,
			MaxRounds:    10,
		})
	})

	// Load MCP servers if config provided.
	var mcpClients []*mcp.Client
	if *mcpConfig != "" {
		mcpClients = loadMCPServers(*mcpConfig, registry)
	}
	defer func() {
		for _, c := range mcpClients {
			c.Close()
		}
	}()

	// Initialize permissions. Server mode forces yolo: there's no
	// user-facing prompt loop to gate tool calls through, and the
	// desktop shell's permission UI isn't wired in v1. Surface this
	// in the log so it's not surprising.
	yoloEffective := *yolo
	if *serve && !yoloEffective {
		fmt.Fprintln(os.Stderr, "[serve] forcing -yolo: server mode has no permission prompt loop")
		yoloEffective = true
	}
	perms := permission.New(yoloEffective)

	// Create agent.
	a := agent.New(agent.Config{
		Provider:     p,
		Tools:        registry,
		Permissions:  perms,
		SystemPrompt: systemPrompt,
	})

	ctx := context.Background()

	// Server mode: run as HTTP+SSE host instead of REPL. Spawned by
	// KinClaw Mac (or any other desktop shell) on a known port.
	if *serve {
		runServe(ctx, a, *port, *providerName, mdl)
		return
	}

	// Check if there's a message from command line args.
	if args := flag.Args(); len(args) > 0 {
		message := strings.Join(args, " ")
		r := repl.New(a, repl.WithMCPClients(mcpClients))
		if err := r.RunOnce(ctx, message); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
		return
	}

	// Start interactive REPL.
	r := repl.New(a, repl.WithMCPClients(mcpClients))
	if err := r.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

// runServe is the -serve mode entrypoint. Wires the agent to the
// HTTP server and translates Agent.Events callbacks into SSE events.
//
// Concurrency: only one turn runs at a time — handleChatPost holds
// turnCancel while the chatHandler goroutine is in flight, and a
// second POST while busy still echoes user_message + 202 but the
// agent.RunWithEvents call serializes on a.messages naturally
// (mutex would be cleaner but Stage 1 keeps it minimal — KinClaw Mac
// gates send button on turn_done anyway).
func runServe(ctx context.Context, a *agent.Agent, port int, providerName, model string) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	var (
		turnMu     sync.Mutex
		turnCancel context.CancelFunc
		srv        *server.Server // declared up-front so the chat handler closure can reference it
	)

	srv = server.New(addr, func(_ context.Context, message string) {
		// Cancel any prior turn that's somehow still in flight, then
		// install our own cancel for the new one.
		turnMu.Lock()
		if turnCancel != nil {
			turnCancel()
		}
		turnCtx, cancel := context.WithCancel(context.Background())
		turnCancel = cancel
		turnMu.Unlock()

		defer func() {
			turnMu.Lock()
			if turnCancel != nil {
				turnCancel = nil
			}
			turnMu.Unlock()
		}()

		_, usage, err := a.RunWithEvents(turnCtx, message, agent.Events{
			OnText: func(chunk string) {
				if chunk == "" {
					return
				}
				srv.Push(server.Event{Type: "text_delta", Text: chunk})
			},
			OnToolCall: func(id, name, summary string, args map[string]any) {
				srv.Push(server.Event{
					Type: "tool_call", ID: id, Name: name, Summary: summary,
					Params: stringifyArgs(args),
				})
			},
			OnToolResult: func(id, name, result string, err error) {
				ev := server.Event{Type: "tool_result", ID: id, Name: name, Output: result}
				if err != nil {
					ev.Message = err.Error()
				}
				srv.Push(ev)
			},
			OnAssistantDone: func(_ string) {
				// turn_done fires below after the whole loop, not per
				// round — UI cares about "agent finished, you can type"
				// not "model paused for tool call".
			},
		})
		if err != nil {
			srv.Push(server.Event{Type: "error", Message: err.Error()})
		}
		srv.Push(server.Event{
			Type:         "usage",
			InputTokens:  usage.Input,
			OutputTokens: usage.Output,
		})
		srv.Push(server.Event{Type: "turn_done"})
	})

	srv.SetInterruptHandler(func() {
		turnMu.Lock()
		defer turnMu.Unlock()
		if turnCancel != nil {
			turnCancel()
		}
	})

	srv.SetStateHandler(func() server.State {
		cwd, _ := os.Getwd()
		return server.State{
			Repo:         cwd,
			Provider:     providerName,
			Model:        model,
			MessageCount: len(a.Messages()),
		}
	})

	if err := srv.ListenAndServe(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "serve failed: %s\n", err)
		os.Exit(1)
	}
}

// startOrphanWatch fires a goroutine that exits the process when the
// original parent dies. macOS doesn't SIGTERM children automatically
// when their parent goes away — they get reparented to launchd (pid
// 1) and keep running, leaking subprocess + port until manually
// killed. Polling os.Getppid() every 2s catches the reparenting and
// triggers a clean exit.
//
// Skipped when the recorded parent is already pid <=1 — that means
// we were launched directly by launchd (or already orphaned), so
// there's nothing to watch for.
func startOrphanWatch() {
	origParent := os.Getppid()
	if origParent <= 1 {
		return
	}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			if os.Getppid() != origParent {
				fmt.Fprintln(os.Stderr,
					"[orphan-watch] parent died, exiting")
				os.Exit(0)
			}
		}
	}()
}

// stringifyArgs flattens the agent's structured tool arguments into a
// {string:string} map for SSE emission. Mirrors kinclaw's choice of
// stringifying values at emission — frontends render args as text
// labels anyway, and string-keyed-string maps decode trivially in
// every language without per-value type narrowing.
//
// Strings pass through; everything else goes through fmt.Sprint, which
// produces sensible defaults for numbers, bools, and nested types
// (best-effort for the latter; tools rarely emit deeply-nested args).
func stringifyArgs(args map[string]any) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		switch s := v.(type) {
		case string:
			out[k] = s
		case nil:
			out[k] = ""
		default:
			out[k] = fmt.Sprint(s)
		}
	}
	return out
}

func defaultSystemPrompt() string {
	return `You are kincode, an AI coding assistant running in the user's terminal.

You have access to tools for reading, writing, and editing files, running bash commands, and searching code.

Guidelines:
- Be concise and direct.
- When editing files, use the file_edit tool for surgical changes. Use file_write only for new files.
- Always use absolute file paths.
- Run bash commands to verify your changes work (e.g., compile, test).
- If you're unsure about the codebase structure, use glob and grep to explore first.
- Ask for clarification if the request is ambiguous.`
}

// soulFrontmatter represents the YAML frontmatter in a .soul.md file.
//
// Format compatibility: kincode souls now mirror the kinclaw kernel's
// soul shape, so the same .soul.md file works for both kernels (the
// SSE protocol unification + this brain unification together mean
// kincode is a drop-in alternate kernel for any kinclaw soul).
//
//	---
//	name: "KinClaw Coder"
//	rules:
//	  - "..."
//	brain:
//	  provider: "ollama"
//	  model: "kimi-k2.5:cloud"
//	  temperature: 0.3
//	  context_length: 131072
//	  endpoint: "http://localhost:11434/v1/chat/completions"
//	thinking: true
//	thinking_budget: 10000
//	---
//	You are a senior engineer...
//
// Resolution precedence (CLI flag > soul brain > soul legacy >
// hardcoded default):
//   - If the user passed -provider/-model/-endpoint on the CLI, those
//     win regardless of what the soul says.
//   - Otherwise, soul.brain takes effect.
//   - Otherwise, the legacy top-level `model:` field (kept for back-
//     compat with kincode <0.7 souls).
//   - Otherwise, the per-provider defaults baked into main.
type soulFrontmatter struct {
	Name           string     `yaml:"name"`
	Rules          []string   `yaml:"rules"`
	Brain          *soulBrain `yaml:"brain,omitempty"`
	Model          string     `yaml:"model"`       // legacy — top-level model string
	Temperature    float64    `yaml:"temperature"` // legacy — top-level temp (unused)
	Thinking       bool       `yaml:"thinking"`
	ThinkingBudget int        `yaml:"thinking_budget"`
}

// soulBrain mirrors kinclaw's nested brain config. Provider + model
// are the meaningful fields; Endpoint lets ollama souls point at a
// non-default Ollama install (e.g. remote LAN box). ContextLength
// is parsed for kinclaw-soul fidelity but kincode doesn't enforce
// it client-side — providers expose their own context limits.
type soulBrain struct {
	Provider      string  `yaml:"provider"`
	Model         string  `yaml:"model"`
	Temperature   float64 `yaml:"temperature"`
	ContextLength int     `yaml:"context_length"`
	Endpoint      string  `yaml:"endpoint"`
}

// mcpConfigFile is the JSON structure for MCP server configuration.
type mcpConfigFile struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env"`
}

// loadMCPServers reads the MCP config file, connects to each server, and
// registers their tools in the tool registry.
func loadMCPServers(path string, registry *tools.Registry) []*mcp.Client {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: cannot read MCP config %s: %v", path, err)
		return nil
	}

	var cfg mcpConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("Warning: invalid MCP config %s: %v", path, err)
		return nil
	}

	var clients []*mcp.Client
	for name, srv := range cfg.MCPServers {
		client, err := mcp.Connect(name, srv.Command, srv.Args, srv.Env)
		if err != nil {
			log.Printf("Warning: MCP server %q failed to connect: %v", name, err)
			continue
		}

		toolDefs, err := client.ListTools()
		if err != nil {
			log.Printf("Warning: MCP server %q failed to list tools: %v", name, err)
			client.Close()
			continue
		}

		// Register MCP tools in the tool registry.
		mcpTools := mcp.ToolsFromClient(client)
		for _, t := range mcpTools {
			registry.Register(t)
		}

		log.Printf("MCP server %q connected: %d tools", name, len(toolDefs))
		clients = append(clients, client)
	}

	return clients
}

func loadSoulFile(path string) (string, *soulFrontmatter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read soul file: %w", err)
	}

	content := string(data)

	// Parse YAML frontmatter if present.
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content[3:], "---", 2)
		if len(parts) == 2 {
			var fm soulFrontmatter
			if err := yaml.Unmarshal([]byte(parts[0]), &fm); err != nil {
				return "", nil, fmt.Errorf("parse frontmatter: %w", err)
			}

			body := strings.TrimSpace(parts[1])

			// Build system prompt from frontmatter + body.
			var sb strings.Builder
			if fm.Name != "" {
				sb.WriteString(fmt.Sprintf("You are %s.\n\n", fm.Name))
			}
			if len(fm.Rules) > 0 {
				sb.WriteString("Rules:\n")
				for _, rule := range fm.Rules {
					sb.WriteString(fmt.Sprintf("- %s\n", rule))
				}
				sb.WriteString("\n")
			}
			sb.WriteString(body)
			return sb.String(), &fm, nil
		}
	}

	// No frontmatter — use the whole file as the system prompt.
	return strings.TrimSpace(content), nil, nil
}
