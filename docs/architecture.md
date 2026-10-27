# Architecture

Scope observes Herdr and paints the sidebar. It writes only its own display tokens, the
Agents panel's view, a waiting tab's mark, the workspace order (`reorder_workspaces`,
`nest_folders`), its managed blocks in Herdr's `config.toml`, and files in its own
directories. It changes focus and installs fonts only when an action asks for that.

## Layers

```text
 cmd/herdr-scope ─► internal/bootstrap         composition root, CLI
                          │ wires
              ┌───────────┴───────────┐
              ▼                       ▼
     internal/adapters/*     internal/application
     implement the ports     use cases, the daemon
              │                       │
              └──► internal/ports ◄───┘     interfaces, boundary DTOs
                          │
                          ▼
                   internal/domain          pure model and rules
```

`tests/unit/architecture_test.go` enforces the rule:

| Package | May import |
| --- | --- |
| `domain` | the standard library, without `os`, `net`, `os/exec`, `io/fs` or logging |
| `ports` | `domain`, `context`, `time` |
| `application` | `domain`, `ports`, the standard library without I/O |
| `adapters/*` | `domain`, `ports`, third-party libraries; `tui` → `config`, `application`; `scenario` → `config`, `herdr`, `sidebar` |
| `bootstrap` | everything |

The domain has two pure algorithms, both functions of their inputs and `now`:

- **`AgentSession.Observe`**: the status state machine (idle grace, done and blocked holds,
  freshness tiers). It returns the display state and the next moment that answer changes.
- **`BuildSidebar`**: ordering keys, the worktree and folder tree, groups, split corners,
  labels and Spaces marks.

An agent's identity is its agent session (`<agent>:<session id>`), which survives pane moves
and Herdr restarts. Without one it falls back to the terminal id, then the pane id.

## A frame

```text
Herdr event ─► ProcessAgentEvent ─► dirty flags, wake       (an event never changes state)
deadline / heartbeat / control command ─► frame:
  structure dirty or reconcile due ─► session.snapshot ─┐
  agents dirty or heartbeat due ────► agent.list ───────┴► model
  model × memory × settings × now ─► Observe ─► BuildSidebar ─► SidebarView
  SidebarView ─► SidebarWriter (diff, 16-token chunks, backoff) ─► Herdr
  memory ─► state.json (atomic, skipped when nothing changed)
```

One timer aims at the earliest deadline. While a row animates, a frame runs every 150 ms and
reuses the agent list, which is re-read at most once a second. With nothing animating, the
daemon sleeps until the next real deadline. The event stream reconnects with backoff (500 ms,
doubling to 30 s) and repaints everything after a reconnect. The daemon exits when Herdr's
socket disappears.

## Adapters

| Adapter | Role |
| --- | --- |
| `herdr` | socket client (herdr-api transport, own wire DTOs), events, CLI calls |
| `sidebar` | glyphs, palette, tokens, the write cache, tab marks, the managed sidebar block, golden renderer |
| `state` | `state.json`, view mode, folds: atomic JSON, corrupt files quarantined |
| `config` | the plugin's `config.toml` (line-preserving edits) and Herdr's managed blocks |
| `git` | reads `.git`, `gitdir:`, `commondir`; never runs `git` |
| `appearance` | the desktop's light/dark: `defaults`, `gsettings`, the registry |
| `filesystem` | transcript tails for activity (≤256 KiB, never logged), the tab-bar line, setup stamps |
| `control` | the daemon's socket (0600) or named pipe; binding it is the instance lock |
| `font` | the icon font in the user font dir, Ghostty/kitty codepoint maps, `font patch` |
| `tui` | the settings popup |
| `scenario` | replays recorded Herdr payloads through the pipeline for the golden tests and `render` |

`internal/bootstrap` builds every adapter once per process and injects functions where one
adapter needs another's behaviour.

## Errors and persistence

The domain's error kinds decide what the daemon does. On transport or decode errors it skips
the frame. If the protocol is too old it refuses to run. A refused write backs off, a
persistence error is logged, and a configuration error keeps the previous configuration. A
failed read or projection never touches memory. A corrupt `state.json` is renamed
`state.json.corrupt-<time>`, and the daemon starts with empty memory.

The look is pinned by `tests/golden`: every scenario in `tests/fixtures/scenarios` is drawn in
both layouts, and `make golden` rewrites the files after an intended change.
