# Architecture

Herdr Scope observes Herdr and paints the sidebar. It never starts, stops or prompts anything.
What it writes: its own display tokens and the Agents panel's view; a tab's name while an
agent in it waits (`mark_tabs`; the mark comes off again, and on stop); the workspace order
(`reorder_workspaces`, `nest_folders`); its own managed blocks in Herdr's `config.toml`; its
own files under the plugin state/config directories; and, on explicit actions only, the focus
(the jump key) and the icon font in the user font directory.

## Layers

```text
                 ┌──────────────────────────────────────────────┐
  cmd/herdr-scope│ main → bootstrap (composition root, CLI)     │
                 └───────────────┬──────────────────────────────┘
                                 │ wires
        ┌────────────────────────┼──────────────────────────────┐
        ▼                        ▼                              ▼
 internal/adapters/*      internal/application            internal/ports
 herdr  sidebar  state    use cases, observer loop  ───►  interfaces + boundary DTOs
 config git appearance    (depends on ports+domain)              │
 filesystem control                                              ▼
        │  implement ports                                internal/domain
        └──────────────────────────────────────────────►  pure model and rules
```

Dependency rule (enforced by `tests/unit/architecture_test.go`):

| Package | May import |
| --- | --- |
| `internal/domain` | standard library only — no `os`, `os/exec`, `net`, `io/fs`, `log`, `log/slog`, TOML, Herdr client |
| `internal/ports` | `domain`, standard library (`context`, `time`) |
| `internal/application` | `domain`, `ports`, standard library (no `os`, `net`, `os/exec`) |
| `internal/adapters/...` | `domain`, `ports`, third-party libraries; another adapter or `application` only where noted: `tui` → `config`, `application` (the inbound settings UI); `scenario` → `config`, `herdr`, `sidebar` (test/tool replay of the whole pipeline) |
| `internal/bootstrap` | everything |

`path/filepath` is allowed in the domain for pure path *string* operations (base name, `~`
abbreviation of a cwd); it never touches the filesystem.

## Domain (`internal/domain`)

| Type | Meaning |
| --- | --- |
| `AgentID`, `PaneID`, `TabID`, `WorkspaceID`, `TerminalID`, `RepositoryID`, `WorktreeID` | distinct string types so identifiers cannot be mixed |
| `AgentStatus` | Herdr's verdict: `working`, `blocked`, `done`, `idle`, `unknown` |
| `DisplayState` | what a row shows: `working`, `done`, `blocked`, `idle_fresh`, `idle`, `idle_stale`, `unknown` |
| `Agent` | one observed agent pane (identity, placement, kind, title, status, focus, authority) |
| `Workspace`, `Tab`, `RepositoryInfo` | placement and git identity |
| `Activity` | time since the last turn → freshness tier |
| `CompletionMarker`, `BlockedMarker` | the held ✓ and held ? |
| `AgentSession` | per-agent memory: last effective status, last activity, markers, last seen |
| `DisplayPolicy` | every behaviour setting (holds, grace, thresholds, row label, indent, gap, split corner) |
| `SortMode` | `active` (grouped), `recent` (flat), `off` (Herdr's order) and its transitions |
| `SidebarView` | the projection: groups → rows with display state, text, depth, corners, sort keys; Spaces marks; tab marks |
| `TabMark` | a tab's own name and the most urgent wait among its agents (`mark_tabs`) |

The two pure algorithms:

1. **`AgentSession.Observe`** — the status state machine (idle grace, done hold, blocked hold
   with authoritative release, re-adoption, freshness tiers). It returns the display state and
   the next moments at which that answer changes (tier crossings, timed hold expiry).
2. **`BuildSidebar`** — ordering keys (minute-grained activity per pane, per tab/split, per
   worktree family), predicted display order, worktree tree (parent/child/orphan/detached),
   group boundaries, stale groups, split corners, row labels and prefix trimming, Spaces
   aggregation.

Both are deterministic functions of their inputs and `now`; tests drive them with fixed times.

### Identity

`AgentID` is derived by the Herdr adapter, most stable source first:

1. `agent_session` (`<agent>:<session id>`) — survives pane moves **and** Herdr restarts;
2. `terminal_id` (`term:<id>`) — survives pane moves within one server;
3. `pane_id` (`pane:<id>`).

Duplicates inside one snapshot fall back to the next source. When an agent's id changes
(its session id is reported after detection, or a pane moved) and its terminal is known under
an id that has left the snapshot, the observer re-keys the memory (`Memory.Rekey`) instead of
starting over. A pane id alone would not do: a pane moved to another workspace gets a new one,
and every Herdr server numbers its panes afresh.

## Application (`internal/application`)

| Use case | Role |
| --- | --- |
| `ReconcileAgents` | full `session.snapshot` → model (workspaces, tabs, repositories, agents) |
| `ObserveAgents` | cheap `agent.list` refresh of the agents in the model |
| `ProcessAgentEvent` | a normalised event marks the model dirty (agents / structure) and wakes the loop — it never changes domain state |
| `MarkPaneSeen` | focusing a pane acknowledges its ✓ |
| `BuildSidebar` | model + memory + policy + now → `SidebarView` and deadlines |
| `ToggleSortMode` | flip/cycle/native/set; via the daemon when one answers, standalone otherwise |
| `UpdateConfiguration` | validate, save, tell the daemon to reload |
| `StartObserver` / `StopObserver` | the resident daemon: scheduler, reconnect, control commands, shutdown |

### Frame pipeline

```text
Herdr event ──► EventSource (adapter) ──► AgentEvent ──► ProcessAgentEvent ──► dirty flags, wake
                                                                                     │
scheduler deadline / heartbeat / control command ─────────────────────────────────── ▼
                                                                                   frame:
  structure dirty or reconcile due ─► SessionSource.Snapshot ─┐
  agents dirty or heartbeat due ────► AgentSource.ListAgents ─┴► model
  model × memory × policy × now ─► AgentSession.Observe (per agent) ─► BuildSidebar ─► SidebarView
  SidebarView ─► SidebarWriter.Apply (diff, chunk, backoff) ─► Herdr tokens / agent view / tab marks
  memory ─► StateRepository.Save (debounced, atomic, skipped when nothing changed)
  next = min(domain deadlines, animation deadline, heartbeat, reconcile, catch-all 30 s)
```

A frame with nothing new for the domain — the same agents and session, the same settings,
mode, folds and focus, no domain deadline reached and the same minute — skips `Observe` and
`BuildSidebar` and only repaints: the previous view with the new `At`, so the spinner and the
ages move. The worktree tree is resolved on reconcile and on a settings change, not per frame.

Startup follows the order the spec requires: protocol check (`ping`) → `agent.list` →
`session.snapshot` → `events.subscribe` → reconcile → live processing.

### Scheduling

One timer, aimed at the earliest deadline, measured on the monotonic clock. Wakes are
debounced (50 ms) and floored (120 ms between frames). While nothing animates and nothing
changes the daemon sleeps until the next real deadline (tier crossing, timed ✓ expiry,
reconcile interval) — no busy loop. While a row animates the frame cadence is the spinner step
(150 ms) but the agent list is re-read at most once per `heartbeat` (1 s).

### Reconnect

The event stream reconnects forever with exponential backoff (500 ms × 2ⁿ, capped at 30 s).
After any reconnect the sidebar writer forgets what it sent (a live handoff gives a new server
with none of our tokens) and the next frame repaints everything and re-installs the sort
override. When the socket file disappears the server is gone and the daemon exits; the next
Herdr start runs the startup hook.

## Ports (`internal/ports`)

`AgentSource`, `SessionSource`, `EventSource`, `ActivitySource`, `RepositoryResolver`,
`StateRepository`, `SortModeStore`, `FoldStore`, `SidebarWriter`, `ConfigRepository`,
`AppearanceSource`, `TabBarPublisher`, `PaneFocus`, `WorkspaceMover`, `Clock`, `Logger`, plus
`HostConfig` and `ManagedConfig` (managed blocks in Herdr's config), `HostReloader` and
`Notifier` (Herdr's CLI), `DaemonControl` and `ControlServer` (thin client → daemon),
`DaemonProcess`, `ServerProbe` (ping/liveness), `FontInstaller`, `SetupStamp` and `OffSwitch`.
Each has a production adapter and a test fake.

A deliberate departure from the spec's sketch: there is no per-pane `pane.agent_status_changed`
subscription. Herdr runs a `pane_get` every 100 ms for each such subscription server-side,
and one malformed entry rejects the whole stream; a 1 s agent-list heartbeat over the socket is
cheaper for Herdr (protocol.md, *Events*).

`ports.Settings` is the boundary type for the plugin configuration: a `domain.DisplayPolicy`,
the sort default, observer intervals, and `PresentationSettings` (glyph variant, row layout,
worktree mark, row colours). Hex colours live there, never in the domain.

## Adapters (`internal/adapters`)

| Adapter | Implements | Notes |
| --- | --- | --- |
| `herdr` | `AgentSource`, `SessionSource`, `EventSource`, `ServerProbe`, `PaneFocus`, `WorkspaceMover`, `HostReloader`, `Notifier`, sidebar `Host` | socket transport via herdr-api; own wire DTOs; event normalisation; CLI cold path |
| `sidebar` | `SidebarWriter` | presentation policy: glyphs and vendor logos, palette, token projection, write cache/delta/chunking/backoff, tab marks, managed sidebar block text, golden renderer |
| `state` | `StateRepository`, `SortModeStore`, `FoldStore` | atomic JSON (`tmp → fsync → rename → fsync dir`), corrupt-file quarantine |
| `config` | `ConfigRepository`, `HostConfig`, `ManagedConfig` | plugin `config.toml` (line-preserving edits); Herdr `config.toml` managed blocks |
| `git` | `RepositoryResolver` | reads `.git`, `gitdir:`, `commondir` — never runs `git` |
| `appearance` | `AppearanceSource` | `defaults` (macOS), `gsettings` (Linux), registry (Windows) |
| `filesystem` | `ActivitySource`, `TabBarPublisher`, `SetupStamp`, `OffSwitch`, paths | transcript tail only (≤256 KiB), never logged; Herdr's plugin environment (`InvokedByHook`) and its detector manifests (`KnownAgents`) |
| `control` | `DaemonControl`, `ControlServer` | Unix socket (0600) / named pipe; ping, view, reload, stop; its own wire DTOs (`wire.go`); binding it is the instance lock; a socket path over 104 bytes moves to `$XDG_RUNTIME_DIR` or the per-user temp dir |
| `font` | `FontInstaller` | user font dir, hash-named copy, Windows registry / fontconfig, Ghostty and kitty codepoint maps |
| `tui` | — (inbound UI) | the settings popup: a pure editor state machine plus raw-terminal I/O |
| `scenario` | — (test/tool input) | replays Herdr wire payloads through the whole pipeline into a simulated token store; backs the golden tests and `render` |

The composition root is `internal/bootstrap`: `runtime.go` builds every adapter once per
process from the environment, `cli.go` maps each command to a use case, `daemon.go` is
`herdr-scope run`, `process*.go` spawns the detached daemon, `lock.go` serialises concurrent
`state-start`s, `diagnostics.go` is `status` and `doctor`. Apart from `tui` and `scenario`
(see the dependency rule), adapters do not import one another; where one needs another's behaviour (the Herdr adapter reading back the sidebar's tokens, the
managed config rendering the sidebar block) the root injects a function.

## Persistence

`<HERDR_PLUGIN_STATE_DIR>/state.json` (schema version 1) holds one record per agent:
identity (agent, pane, terminal, workspace), kind, last status, last activity, done marker,
blocked marker, last seen, updated-at. Records unseen for 30 days are dropped. A save that
changes nothing that matters — display times at minute grain, `last_seen` and `updated_at` at
hour grain — is skipped, so an idle daemon does not rewrite the file.
The sort-mode choice lives beside it in `view-mode` (written by the daemon or, when no daemon
answers, by the thin client). Config backups go to `backups/`.

A corrupt `state.json` is renamed to `state.json.corrupt-<timestamp>`, a warning is logged,
and the daemon continues with empty memory. A failed projection never touches persisted state.

## Error kinds

`domain` defines `ErrTransport`, `ErrProtocol`, `ErrRefused`, `ErrDecode`, `ErrPersistence`,
`ErrProjection`, `ErrConfiguration` (sentinels wrapped with `%w`). The observer reacts by kind:
transport → skip the frame and retry soon; protocol (a server too old) → refuse to run; refused
(Herdr turned down one request) → that write backs off, the daemon keeps running; decode → skip
the frame and log;
persistence → log and keep running; projection → back off that target only; configuration →
keep the previous configuration.
