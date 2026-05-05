# Changelog

## [0.10.0] - 2026-05-05

**Image input + plan mode.** Two capability additions on top of
v0.9.0's named subagents — closes the bulk of the Claude Code
parity gap for day-to-day coding. Vision-capable models (Anthropic
claude-3+, OpenAI gpt-4o) can now look at screenshots / diagrams /
mockups; plan mode lets you ask the agent to investigate + propose
without modifying anything until you approve.

### Added — Image input (vision blocks)

Multimodal user turns. Provider layer translates attached images
into Anthropic content blocks (`type:image` with base64 source) and
OpenAI content parts (`image_url` with data URL). Layered shape:

- `provider.Message` gains optional `Blocks []ContentBlock` alongside
  `Content string`. Empty Blocks = pure-text path (every existing
  call site unchanged, no breaking change for non-vision models).
- `provider.ContentBlock {Type, Text, ImageBase64, ImageMediaType}`
  + helpers `TextBlock(...)` / `ImageBlock(media, base64)`.
- `agent.Agent.RunWithImagesAndEvents` accepts `[]agent.Attachment`
  and builds the multimodal user message. `RunWithEvents` stays as
  a thin pass-through.
- `server.ChatHandler` signature gains `attachments []ChatAttachment`.
  `POST /api/chat` now accepts `{message, images:[{media_type,
  data}]}`. Empty-text turns are valid when images are present.
- `server.Event` gains `ImageCount` so UIs can render "📎 N images"
  on user bubbles without re-echoing base64 over SSE.

Tests: `TestMultimodalMessageBlocks` + `TextBlock` / `ImageBlock`
helper invariants locking the JSON shape.

### Added — Plan mode (read-only gate)

Toggle via `POST /api/plan_mode {enabled}`. While on:

1. `permission.Manager.CheckPlanMode` denies any tool outside the
   read-only allowlist (`file_read`, `glob`, `grep`, `web_fetch`,
   `web_search`) with a deny-message shaped to teach the model:
   "you may only use read-only tools, end with a markdown plan."
   Hard enforcement at `agent.executeTool` entry.
2. Agent prepends a directive to every user message while plan mode
   is on, so the model sees the rules up-front rather than learning
   by repeated denials. Directive ages out naturally with
   conversation compaction.

Server surface:
- `POST /api/plan_mode {enabled} → {enabled}` (server may refuse
  e.g. mid-turn; UI confirms via the round-tripped bool)
- SSE `plan_mode` event broadcasts on toggle so multi-window UIs sync
- `GET /api/state` grows a `plan_mode` field
- Toggling cancels any in-flight turn (same hygiene as `/api/clear`)

Tests: `TestPlanModeOff_AllowsEverything` +
`TestPlanModeOn_AllowsReadOnlyDeniesWrite`.

### Why this matters

Image input unlocks the workflows that were getting awkward without
it: dropping a screenshot to ask "what's this UI bug", pasting a
design mockup into Code mode, etc. Plan mode is the safety valve —
you can hand kincode a non-trivial change and have it research +
draft a plan first, instead of Yolo-modifying files and hoping.

Bumps kincode to feature-parity with Claude Code on the parts that
matter for desktop-shell users. Remaining gaps (PreToolUse /
PostToolUse hooks, REPL custom slash commands, full settings.json
hierarchy) deferred — low value vs. existing surface.

---

## [0.9.0] - 2026-05-05

**Named subagents.** Mirrors Claude Code's named-subagent convention:
drop a `.md` file with YAML frontmatter at `~/.kincode/agents/<name>.md`
or `~/.localkin/agents/<name>.md` (family-shared) and the parent
model gets a dispatchable specialist persona it can call as
`agent_spawn(agent="code-reviewer", task="...")`.

### Added — Persona dispatch via `agent_spawn(agent, task)`

```yaml
---
name: code-reviewer
description: Senior code reviewer — flags bugs, security, style.
tools: ["bash", "file_read", "glob", "grep"]   # optional restrict
model: "claude-sonnet-4-6"                      # optional override
---
You are a senior code reviewer...
```

- `pkg/agent/persona.go`: `SubAgentPersona` type + `LoadSubAgentPersona`
  + `ListSubAgentPersonas`. Walks both directories, dedups by name
  (user-level wins over family-shared), parses YAML frontmatter +
  `.md` body as the subagent's system prompt.
- `pkg/tools/agent_spawn.go`: `SubAgentFactory` signature changed
  to `func(personaName string) SubAgentRunner`. New `Personas
  []PersonaInfo` field surfaces names + descriptions in the tool's
  `Description()` so the parent model knows what's available without
  filesystem queries. `Def()` exposes optional `agent` param as enum.
- `pkg/tools/tools.go`: `RegisterDefaultsWithAgent(r, factory,
  personas...)` variadic personas.
- `cmd/kincode/main.go`: discovers personas at boot, factory closure
  loads persona's body as subagent system prompt. Falls back to
  parent's prompt if the named persona doesn't exist (better to run
  the task than refuse outright).

`PersonaInfo` struct prevents `pkg/tools → pkg/agent` circular
import — `pkg/tools` advertises persona metadata; `main.go` is the
bridge layer that loads persona bodies via `agent.LoadSubAgentPersona`.

### Why named subagents matter

Sub-agents in v0.8.0 were generic — you could spawn a parallel
worker but it inherited the parent's system prompt. Named subagents
let you dispatch specialized tasks to specialized prompts:
"have the code-reviewer look at this diff, have the test-writer
generate XCTest cases for this Swift file." The parent agent stays
cleaner because expertise lives in the persona file, not the prompt.

---

## [0.8.0] - 2026-05-04

**Skill forge + project memory + MultiEdit + TodoWrite.** Five
Tier-S/A capability additions in one push, closing the most-cited
Claude Code gaps:

### Added — External skill forge (`pkg/tools/external.go`)

kincode's external skill loader matches kinclaw kernel's `SKILL.md`
format, so the LocalKin family shares one skill marketplace at
`~/.localkin/skills/`. `LoadAllExternalSkills()` searches
`~/.kincode/skills/` (kincode-specific) then `~/.localkin/skills/`
(family-shared), dedups by name. Each skill becomes a regular tool
the agent can invoke alongside the 10 builtins — kept binary at
17MB while supporting Playwright / browser-use / image-gen via
external dirs with their own deps.

### Added — Project memory (`pkg/agent/memory.go`)

`LoadProjectMemory(startDir)` walks up at most 10 levels looking for
`KINCODE.md` (preferred) or `CLAUDE.md` (cross-tool portability
fallback). Stops at `$HOME` or root. Auto-prepended to system prompt
at boot so the agent has project conventions / contributor notes /
"don't touch X" rules without you re-typing them every session.

### Added — `multi_edit` tool (`pkg/tools/multi_edit.go`)

Atomic multi-place find-replace. Sequential edits in one call (each
sees prior edits' state); failure mode names the failing edit
(1-indexed) with no partial writes. Matches Claude Code's MultiEdit
shape so cross-tool memory references work.

### Added — `todo_write` tool (`pkg/tools/todo_write.go`)

In-process per-agent `TodoItem` list across rounds. Status enum:
`pending` / `in_progress` / `completed`. Full-list-overwrite
semantics (matches Claude Code). Validates non-empty `content` +
`activeForm`. Soft-warns when more than one item is `in_progress`.

### Added — `scripts/install.sh`

`go build` → `codesign --force --sign - --identifier
dev.localkin.kincode --options=runtime` → install to
`~/.localkin/bin/kincode`. Stable bundle ID across rebuilds means
macOS TCC remembers grants. Re-signs at install path.

### Added — JSON-encoded SSE event params

`stringifyArgs` in `cmd/kincode/main.go` JSON-encodes complex args
(arrays / maps) instead of `fmt.Sprint`. Lets desktop shells parse
structured tool args (e.g. `todos` array for inline checklist UI)
without re-running the tool just to get its args back.

---

## [0.7.1] - 2026-05-04

**First external contribution.** Tavily Search API support landed
via [PR #1](https://github.com/LocalKinAI/kincode/pull/1) from the
Tavily team — adds Tavily as an optional `web_search` provider
alongside the existing DuckDuckGo path.

### Added — Tavily as optional web search provider

- `web_search` tool: when `TAVILY_API_KEY` env var is set, searches
  go through `https://api.tavily.com/search` instead of DuckDuckGo
  HTML scraping
- DuckDuckGo path stays the default — no env var = unchanged behavior
- Pure stdlib (`net/http` + `encoding/json`), no new dependencies
- Refactor: DDG path extracted into `searchDDG()` method; result
  formatting extracted into shared `formatSearchResults()` helper

### Why this matters

DDG HTML scraping is fragile — rate limits, occasional HTML format
changes, no structured ranking. Tavily is purpose-built for LLM
agents: clean text snippets, ranking scores, fewer flakes. Users
who already pay for Tavily can just `export TAVILY_API_KEY=...` and
get better web search; everyone else keeps the zero-config DDG
fallback.

This also marks kincode's first external contributor — within 12
hours of v0.7.0's open-source push, Tavily's integration team
landed a clean PR. Worth noting as a sign the rename + serve mode
+ kinclaw-mac integration is being noticed.

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
