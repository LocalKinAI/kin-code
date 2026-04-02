# Changelog

## [0.4.0] - 2026-04-02

### Session Persistence
- Auto-save conversation to `~/.kin-code/session.json` after each interaction
- Auto-restore previous session on startup (shows "[session restored: N messages]")
- Auto-save on Ctrl+C / SIGTERM (no lost conversations)
- `/clear` now also deletes session file for a clean start
- Session excludes system prompt (regenerated from soul file on load)

## [0.3.0] - 2026-04-02

### MCP Protocol Support
- MCP client: JSON-RPC 2.0 over stdio, full initialize handshake
- Auto-discover and register tools from MCP servers (`mcp_` prefix)
- Config file: `-mcp mcp.json` to define MCP server connections
- `/mcp` slash command to list connected servers and tools
- Graceful degradation: failed servers log warning, don't block startup
- Pure stdlib implementation, zero external MCP SDK

**kin-code is now the only Claude Code alternative with both MCP and Soul files.**

## [0.2.0] - 2026-04-02

### Web Tools, Memory, Sub-Agents & Context Compaction

**New tools (4):**
- `web_fetch` — Fetch URL content, strip HTML, return clean text
- `web_search` — DuckDuckGo search, zero API key required
- `memory` — Persistent key-value store across sessions (~/.kin-code/memory.json)
- `agent_spawn` — Spawn sub-agent for parallel task execution

**New features:**
- **Context compaction** — Auto-summarizes when messages exceed 80% of token limit. Keeps system prompt + last 5 messages, LLM summarizes the rest
- **Markdown terminal rendering** — Bold, inline code, code blocks, headers, bullets, tables rendered with ANSI codes
- **Diff visualization** — Colored unified diff output after file edits (green=added, red=removed)
- **10 new slash commands** — /model, /provider, /memory, /save, /load, /tokens, /diff, /soul, /version, /mcp

**Stats:** 10 built-in tools, 14 slash commands, 3,427 lines of Go, 9MB binary

## [0.1.0] - 2026-04-02

### Initial Release

A lightweight AI coding assistant for your terminal. Written in Go. Single binary. Zero dependencies.

**Core:**
- Multi-provider support: Anthropic (raw HTTP + SSE), OpenAI-compatible (OpenAI/Ollama/DeepSeek/Gemini/any endpoint)
- Agent loop with streaming, multi-round tool calling, max 25 rounds
- Interactive REPL with readline history, colored output
- Permission system with yolo mode and dangerous command blocklist

**Tools (6):**
- `bash` — Shell execution, 30s timeout, 128KB output cap, blocklist
- `file_read` — Read files with offset/limit, line numbers
- `file_write` — Write files with parent dir creation
- `file_edit` — Find and replace with uniqueness check
- `glob` — File pattern search, sorted by modification time
- `grep` — Regexp search with file type filtering

**Soul files:**
- Define custom AI personas via `.soul.md` (YAML frontmatter + Markdown body)
- Set name, temperature, rules — change how the AI thinks and codes
- Example: `examples/coder.soul.md` — senior engineer persona

**Stats:** 6 built-in tools, 4 slash commands, 2,086 lines of Go, 8.8MB binary
