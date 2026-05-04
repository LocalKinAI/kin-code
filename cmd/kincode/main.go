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

	"github.com/LocalKinAI/kincode/internal/mcp"
	"github.com/LocalKinAI/kincode/pkg/agent"
	"github.com/LocalKinAI/kincode/pkg/permission"
	"github.com/LocalKinAI/kincode/pkg/provider"
	"github.com/LocalKinAI/kincode/pkg/repl"
	"github.com/LocalKinAI/kincode/pkg/tools"
	"gopkg.in/yaml.v3"
)

const version = "0.6.0"

func main() {
	providerName := flag.String("provider", "anthropic", "LLM provider: anthropic, openai, ollama")
	model := flag.String("model", "claude-sonnet-4-6", "Model name")
	soulFile := flag.String("soul", "", "Path to .soul.md file for personality/rules")
	apiKey := flag.String("api-key", "", "API key (or use ANTHROPIC_API_KEY / OPENAI_API_KEY env)")
	endpoint := flag.String("endpoint", "", "Custom API endpoint (for ollama/compatible APIs)")
	mcpConfig := flag.String("mcp", "", "Path to MCP servers config JSON file")
	yolo := flag.Bool("yolo", false, "Auto-approve all tool calls without confirmation")
	showVersion := flag.Bool("version", false, "Show version and exit")
	login := flag.Bool("login", false, "Login via Claude OAuth (use your Claude account, no API key needed)")
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

	// Set default endpoints and models per provider.
	ep := *endpoint
	mdl := *model
	switch *providerName {
	case "anthropic":
		if key == "" {
			// Try OAuth token as fallback.
			token, err := provider.GetValidToken()
			if err != nil {
				fmt.Fprintf(os.Stderr, "OAuth error: %v\n\n", err)
				fmt.Fprintln(os.Stderr, "No API key available. Either:")
				fmt.Fprintln(os.Stderr, "  1. Run 'kincode -login' to use your Claude account")
				fmt.Fprintln(os.Stderr, "  2. Set ANTHROPIC_API_KEY environment variable")
				fmt.Fprintln(os.Stderr, "  3. Use -api-key flag")
				os.Exit(1)
			}
			key = token
			isOAuth = true
			// Default to Haiku 4.5 for OAuth users (included in all Claude plans).
			if mdl == "claude-sonnet-4-6" {
				mdl = "claude-haiku-4-5-20251001"
			}
			fmt.Println("Using Claude OAuth session (model: " + mdl + ")")
		}
	case "openai":
		if key == "" {
			fmt.Fprintln(os.Stderr, "Error: OPENAI_API_KEY not set. Use -api-key or set the environment variable.")
			os.Exit(1)
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

	// Load system prompt from soul file.
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

	// Initialize permissions.
	perms := permission.New(*yolo)

	// Create agent.
	a := agent.New(agent.Config{
		Provider:     p,
		Tools:        registry,
		Permissions:  perms,
		SystemPrompt: systemPrompt,
	})

	ctx := context.Background()

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
type soulFrontmatter struct {
	Name           string   `yaml:"name"`
	Temperature    float64  `yaml:"temperature"`
	Rules          []string `yaml:"rules"`
	Model          string   `yaml:"model"`
	Thinking       bool     `yaml:"thinking"`
	ThinkingBudget int      `yaml:"thinking_budget"`
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
