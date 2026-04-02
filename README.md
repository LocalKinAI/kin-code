# kin-code

A lightweight AI coding assistant for your terminal. Written in Go. Single binary. Zero dependencies.

Like Claude Code, but open-source and 10x lighter.

## Features

- Single binary (~10MB), zero runtime dependencies
- Multi-provider: Anthropic, OpenAI, Ollama (local models)
- 10 built-in tools: bash, file read/write/edit, glob, grep, web_fetch, web_search, memory, agent_spawn
- Permission system with tool call confirmation
- Soul files: define custom personas and rules (.soul.md)
- Streaming responses with markdown rendering
- Context compaction: auto-summarizes when context gets large
- Sub-agents: spawn parallel tasks with agent_spawn
- MCP support: connect any MCP-compatible tool server
- Persistent memory across sessions
- Web tools: fetch URLs and search the web (DuckDuckGo, no API key)
- Fast: Go concurrency, minimal memory footprint

## Quick Start

```bash
# Install
go install github.com/LocalKinAI/kin-code/cmd/kin-code@latest

# Or download binary
curl -fsSL https://github.com/LocalKinAI/kin-code/releases/latest/download/kin-code-$(uname -s)-$(uname -m) -o kin-code
chmod +x kin-code

# Run with Anthropic
export ANTHROPIC_API_KEY=sk-ant-...
kin-code

# Run with Ollama (free, local)
kin-code -provider ollama -model qwen3:8b

# Run with OpenAI
OPENAI_API_KEY=sk-... kin-code -provider openai -model gpt-4o

# Run with a soul file
kin-code -soul coder.soul.md

# One-shot mode (non-interactive)
kin-code "explain this codebase"

# YOLO mode (auto-approve all tool calls)
kin-code -yolo "fix the failing tests"
```

## Soul Files

Define custom personas with `.soul.md` files:

```yaml
---
name: "Senior Go Developer"
temperature: 0.3
rules:
  - "Always write idiomatic Go"
  - "Prefer stdlib over external packages"
  - "Write tests for every function"
---

You are a senior Go developer. You write clean, efficient, well-tested code.
Focus on simplicity and readability.
```

## Architecture

```
kin-code
├── cmd/kin-code/     # CLI entry point
├── pkg/
│   ├── agent/        # Core agent loop (message → tool → response)
│   ├── provider/     # LLM providers (Anthropic, OpenAI, Ollama)
│   ├── tools/        # Built-in tools (bash, file ops, search)
│   ├── repl/         # Interactive terminal (readline, colors)
│   └── permission/   # Tool call approval system
└── internal/
    └── mcp/          # MCP protocol support (planned)
```

## Comparison

| | Claude Code | claw-code | kin-code |
|---|---|---|---|
| Language | TypeScript | Rust | **Go** |
| Binary size | ~100MB | ~15MB | **~10MB** |
| Memory | ~150MB | ~30MB | **~20MB** |
| Dependencies | npm ecosystem | cargo | **zero** |
| Multi-provider | Anthropic only | Multi | **Multi** |
| Soul files | No | No | **Yes** |
| Build time | N/A | Minutes | **Seconds** |

## MCP Support

Connect to any [MCP](https://modelcontextprotocol.io/)-compatible tool server:

```bash
# Create mcp.json
cat > mcp.json << 'EOF'
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": ["GITHUB_TOKEN=ghp_xxx"]
    }
  }
}
EOF

# Run with MCP servers
kin-code -mcp mcp.json

# List connected servers and tools
> /mcp
```

MCP tools are automatically registered with a `mcp_` prefix (e.g., `mcp_read_file`, `mcp_search_repositories`). The LLM can call them like any built-in tool.

## Build from Source

```bash
git clone https://github.com/LocalKinAI/kin-code.git
cd kin-code
go build -o kin-code ./cmd/kin-code/
```

## License

MIT

---

Built by the team behind [LocalKin](https://localkin.dev) -- a self-evolving AI agent swarm with 78 specialized agents.
