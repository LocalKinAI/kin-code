# Changelog

## [0.7.0] - 2026-05-04

**Renamed `kin-code` → `kincode`** (matches the family pattern:
`kinclaw` / `kinrec` / `kinax` — all single-word, no hyphens), and
gained a **HTTP+SSE server mode** so desktop shells (KinClaw Mac
shipped today) can drive kincode the same way they drive the
kinclaw kernel.

### Renamed — kin-code → kincode

- Module: `github.com/LocalKinAI/kin-code` → `github.com/LocalKinAI/kincode`
- Binary: `kincode`
- Dotdir: `~/.kincode/` (oauth, memory, sessions, skills, history)
- `cmd/kincode/main.go` now tracked — old `.gitignore` `kin-code`
  pattern was matching the cmd subdir recursively, so `main.go` was
  silently never committed. Anchored to `/kincode` for binary-only
  exclusion.

### Added — `-serve` HTTP+SSE server mode

Mirrors the kinclaw kernel's transport so the same desktop client
code drives both kernels.

```
GET  /api/health                 — readiness probe
GET  /api/state                  — {repo, model, provider, message_count}
POST /api/repo {"path": "..."}   — chdir agent into a repo
POST /api/chat {"message": ...}  — kick a turn (202, output via SSE)
DELETE /api/chat                 — interrupt the in-flight turn
GET  /api/events                 — SSE stream of events
```

Event types: `user_message`, `text_delta`, `tool_call`,
`tool_result`, `turn_done`, `error`, `usage`. Field names aligned
with kinclaw's event shape (`params: map[string]string` not `args`)
so the same JSON struct decodes both kernels.

Agent loop refactored: `Run` is now a thin wrapper over
`RunWithEvents(ctx, msg, Events{...})` which routes streaming output
through caller-supplied callbacks. REPL keeps the stdout-printing
sink; server uses it to fan tokens into SSE. No behavior change for
existing CLI users.

Server mode forces `-yolo` (no permission prompt loop over HTTP).

### Added — Soul brain config (kinclaw-compatible souls)

The `soulFrontmatter` struct's `model:` and `temperature:` fields
were parsed but **never read** in code — soul files lying about the
brain and kincode silently ignoring it. Fixed by adopting the kinclaw
kernel's nested `brain:` shape:

```yaml
---
name: "kincode"
brain:
  provider: "ollama"
  model: "kimi-k2.6:cloud"
  temperature: 0.3
  context_length: 131072
rules: ["..."]
---
```

Resolution precedence: CLI flag (`-provider`/`-model`) > soul
`brain:` > soul legacy top-level `model:` > hardcoded default.
`flag.Visit` detects which flags the user set explicitly so the
soul fills in the rest.

This is the **second layer of unification** with the kinclaw kernel:
- Stage 4a aligned the SSE wire format
- Stage 7 (this) aligned the soul format

Same `pilot.soul.md` now drives either kernel:
```bash
kinclaw  serve  -soul souls/pilot.soul.md   # 5-claw computer-use
kincode  -serve -soul souls/pilot.soul.md   # repo-aware coding
```

### Added — `souls/coder.soul.md` canonical default soul

Promotes `examples/coder.soul.md` to a real `souls/` location with a
brain block (`ollama / kimi-k2.6:cloud`, no hardcoded endpoint —
inherits ollama's local default). Layout matches the kinclaw kernel:
`souls/<name>.soul.md` at repo root.

### Added — Auto-fallback to ollama when no Anthropic creds

In `-serve` mode, when `-provider` is the default (anthropic) and
neither `ANTHROPIC_API_KEY` nor `~/.kincode/oauth.json` is present,
auto-switch to `ollama / kimi-k2.6:cloud`. Lets KinClaw Mac launch
kincode cold without requiring API key setup; users with Ollama
already configured (which they likely are if they're running
kinclaw) get a working coding agent immediately.

### Added — Subprocess hygiene

`startOrphanWatch()` at boot polls `os.Getppid()` every 2s. If the
parent process dies, kincode self-exits instead of being reparented
to launchd. Eliminates the "ghost subprocess holding port :5002"
problem when the desktop shell gets `kill -9`'d.

### Changed — `-serve` doesn't hard-exit on missing API key

CLI/REPL behavior unchanged (still prints "no API key, run -login"
and exits). But in `-serve` mode the HTTP server boots regardless;
chat turns return an `error` SSE event when keys are still missing.
This lets the desktop shell render the failure state in the UI
instead of having the supervisor see exit code 1 and assume crash.

## [0.6.0] - 2026-04-02

### Skill Templates
- `/skill` — List available skill templates (reusable prompt patterns)
- `/skill <name>` — Load a skill template; prepended to your next message as context
- `/skill create <name>` — Create a new skill interactively (multi-line input)
- Skills stored as `.md` files in `~/.kincode/skills/`
- 5 example skills included: `code-review`, `write-tests`, `debug`, `refactor`, `explain`

### Extended Thinking
- New soul file fields: `thinking: true` and `thinking_budget: 10000`
- When enabled, the Anthropic provider sends `thinking` config in API requests
- Thinking content streamed in dim text: `[thinking] ...`
- Supports both streaming and non-streaming modes
- Budget defaults to 10,000 tokens; configurable per soul file

## [0.5.0] - 2026-04-02

### Claude OAuth Login
- `kincode -login` — Login via browser using your Claude account (Free/Pro/Max)
- Full OAuth PKCE flow: browser-based authorization, local callback server
- Auto-refreshes expired tokens — no manual re-login needed
- Defaults to Haiku 4.5 for OAuth sessions (included in all Claude plans)
- Tokens saved to `~/.kincode/oauth.json` with 0600 permissions
- Zero API key required — just login and code

**Use your Claude subscription to power kincode. No API key needed.**

## [0.4.0] - 2026-04-02

### Session Persistence
- Auto-save conversation to `~/.kincode/session.json` after each interaction
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

**kincode is now the only Claude Code alternative with both MCP and Soul files.**

## [0.2.0] - 2026-04-02

### Web Tools, Memory, Sub-Agents & Context Compaction

**New tools (4):**
- `web_fetch` — Fetch URL content, strip HTML, return clean text
- `web_search` — DuckDuckGo search, zero API key required
- `memory` — Persistent key-value store across sessions (~/.kincode/memory.json)
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
