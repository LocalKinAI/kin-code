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
- Skill templates: reusable prompt patterns (/skill)
- Extended thinking: deep reasoning mode for complex problems
- Session persistence: auto-save/restore conversations across restarts
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

## Claude Login (No API Key Needed)

Use your Claude account directly — works with Free, Pro, and Max:

```bash
# First time: login via browser
kin-code -login

# Then just use it (defaults to Haiku 4.5)
kin-code

# Or specify a different model
kin-code -model claude-sonnet-4-6
```

Your session auto-refreshes. No API key needed.

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

### Extended Thinking

Enable deep reasoning for complex problems:

```yaml
---
name: "Architect"
thinking: true
thinking_budget: 15000
---
```

When enabled, the model will show its reasoning process in dim text before the final response. Useful for complex debugging, architecture decisions, and multi-step analysis.

## Slash Commands

| Command | Description |
|---|---|
| `/help` | Show all commands |
| `/clear` | Clear conversation history |
| `/compact` | Manually compress context |
| `/model <name>` | Switch model mid-session |
| `/provider <name>` | Switch provider |
| `/soul <file>` | Load a soul file |
| `/memory` | Show persistent memory |
| `/save <file>` | Save conversation to file |
| `/load <file>` | Load conversation from file |
| `/tokens` | Show estimated token usage |
| `/diff` | Show last file edit as colored diff |
| `/mcp` | List connected MCP servers and tools |
| `/skill` | List available skill templates |
| `/skill <name>` | Load a skill template for next message |
| `/skill create <name>` | Create a new skill interactively |
| `/version` | Show version |
| `/quit` | Exit |

## Architecture

```
kin-code (9MB single binary)
├── cmd/kin-code/       # CLI entry point, flag parsing, soul loading
├── pkg/
│   ├── agent/          # Core loop: message → LLM → tool calls → loop
│   │                   # Context compaction (auto-summarize at 80%)
│   ├── provider/       # LLM providers (raw HTTP, no SDKs)
│   │   ├── anthropic   # Anthropic Messages API + SSE streaming
│   │   └── openai      # OpenAI-compatible (OpenAI/Ollama/DeepSeek/Gemini/...)
│   ├── tools/          # 10 built-in tools
│   │   ├── bash        # Shell execution (30s timeout, blocklist)
│   │   ├── file_*      # Read, write, edit (with diff visualization)
│   │   ├── glob/grep   # File and content search
│   │   ├── web_*       # Fetch URLs, DuckDuckGo search
│   │   ├── memory      # Persistent key-value store
│   │   └── agent_spawn # Sub-agent for parallel tasks
│   ├── repl/           # Interactive terminal
│   │                   # Readline, markdown rendering, 14 slash commands
│   └── permission/     # Tool call approval (yolo / confirm)
└── internal/
    └── mcp/            # MCP protocol client (JSON-RPC 2.0 over stdio)
```

## Comparison

| | Claude Code | claw-code (Rust) | nano-claude-code (Python) | **kin-code (Go)** |
|---|---|---|---|---|
| Binary size | ~100MB | ~15MB | N/A (needs Python) | **9MB** |
| Memory usage | ~150MB | ~30MB | ~80MB | **~20MB** |
| Dependencies | Node.js + npm | Rust toolchain | Python 3.10+ | **zero** |
| Install | npm install | cargo build | pip install | **download & run** |
| Providers | Anthropic | Multi | 10+ | **any OpenAI-compatible** |
| Built-in tools | 40+ | ~20 | 13 | **10 + MCP** |
| Sub-agents | ✅ | ❌ | ✅ | **✅** |
| Memory | ✅ | ❌ | ✅ | **✅** |
| Context compaction | ✅ | ✅ | ✅ | **✅** |
| MCP protocol | ✅ | ✅ | ❌ | **✅** |
| Markdown rendering | ✅ | ✅ | ❌ | **✅** |
| Diff visualization | ❌ | ❌ | ✅ | **✅** |
| Web search | ❌ | ❌ | ✅ | **✅** |
| Session persistence | ✅ | ✅ | ✅ | **✅** |
| Soul files | ❌ | ❌ | ❌ | **✅ unique** |
| Open source | ❌ | ✅ | ✅ | **✅** |

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
