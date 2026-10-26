# Herdr protocol notes

Everything here was established against **Herdr 0.9.5, socket protocol 22**, from three
sources, in this order of trust:

1. the live server (`ping`, `session.snapshot`, `events.subscribe` probes);
2. Herdr's own source at tag `v0.9.5` (`src/api/subscriptions.rs`, `src/api/event_hub.rs`,
   `src/app/api_helpers.rs`, `src/plugin_command.rs`, `src/ui/sidebar/tokens.rs`,
   `src/agent_view_eval.rs`);
3. `herdr api schema --json` (schema version 1) and the plugin/configuration docs shipped
   with that tag.

When Herdr changes, re-run `herdr api schema --json` and `herdr-scope doctor`; the
compatibility gate is described at the end.

## Transport

| Item | Value |
| --- | --- |
| Endpoint | `$HERDR_SOCKET_PATH` (Unix socket on macOS/Linux, named pipe `\\.\pipe\<path>` on Windows) |
| Framing | newline-delimited JSON |
| Request | `{"id": "...", "method": "...", "params": {...}}` |
| Success | `{"id": "...", "result": {"type": "<result kind>", ...}}` |
| Error | `{"id": "...", "error": {"code": "...", "message": "..."}}` |
| Event | `{"event": "<kind>", "data": {...}}` — no `id` |

Ordinary requests are **one per connection**: the server answers and closes. The only
long-lived connection is `events.subscribe`, which must stay **write-silent** after the
request (client bytes on a streaming connection read as a disconnect on Unix).

The transport is provided by `github.com/cyperx84/herdr-api` v0.1.1 (`Client.Call`,
`Client.Subscribe`, `Client.Ping`). Its typed models are not used: its `Agent` lacks
`tokens`, `terminal_id`, `agent_session`, `display_agent` and `title`, all of which Scope
needs. Scope decodes its own wire DTOs, and shapes its `agent.view` sort and filter, in
`internal/adapters/herdr/wire.go`; nothing from that package is visible outside
`internal/adapters/herdr`.

## Methods Scope uses

| Method | Path | Purpose |
| --- | --- | --- |
| `ping` | hot | protocol gate, server version, liveness (`result.type = "pong"`) |
| `session.snapshot` | hot | the authoritative state: workspaces (+ worktree info), tabs, panes (+ tokens), agents, layouts, focused ids, `version`, `protocol` |
| `agent.list` | hot | cheap per-frame refresh of agent status/title/focus |
| `events.subscribe` | hot | wake hints (see Events) |
| `pane.report_metadata` | hot | publish/clear pane tokens (`tokens: {name: value\|null}`) |
| `workspace.report_metadata` | hot | publish/clear workspace tokens (Spaces column) |
| `agent.view.set` / `agent.view.clear` | hot | install/remove the Agents panel sort override |
| `workspace.move_block` | hot | `reorder_workspaces`, `nest_folders` |
| `pane.list` | hot | token names on every pane, for the orphan sweep when the set of agents changes and for a stop's clear; the focused pane, for the tab-bar line (every 2 s) and the jump and fold actions |
| `workspace.list` | hot | a stop clears Scope's Spaces tokens from every workspace |
| `pane.focus` | hot | the jump action: focus the next pane that waits (a write) |
| `pane.process_info`, `pane.layout` | hot | tab-bar line (focused pane's foreground cwd, row width) |
| `tab.rename` | hot | `mark_tabs`: a waiting tab's mark, sent only when it changes; a stop takes the marks off. A tab that closed meanwhile (`tab_not_found`) counts as renamed |
| `tab.list` | hot | tab-bar line (how many tabs the focused workspace has); a stop reads which tabs still carry a mark |
| `herdr server reload-config` | cold (CLI) | after a managed-block write |
| `herdr config check` | cold (CLI) | verify Herdr still parses config.toml after a write |
| `herdr notification show` | cold (CLI) | user-visible notes from configure/setup |
| `herdr plugin pane open` | cold (CLI) | open the settings popup |
| `herdr --version` | cold (CLI) | `doctor`: the version of the binary Herdr runs |

Cold-path calls go through `$HERDR_BIN_PATH`. Nothing polls the CLI.

### `session.snapshot` shape (subset Scope reads)

```text
snapshot.version, snapshot.protocol
snapshot.focused_pane_id | focused_tab_id | focused_workspace_id : string|null
snapshot.workspaces[]: workspace_id, number, label, focused, tokens{},
                       worktree?: {repo_key, repo_name, repo_root, checkout_path, is_linked_worktree}
snapshot.tabs[]:       tab_id, workspace_id, number, label, pane_count
snapshot.panes[]:      pane_id, terminal_id, workspace_id, tab_id, tokens{}, agent_status, cwd, foreground_cwd
snapshot.agents[]:     pane_id, terminal_id, workspace_id, tab_id, agent, display_agent,
                       agent_status, screen_detection_skipped, focused,
                       terminal_title, terminal_title_stripped, title,
                       agent_session?: {source, agent, kind, value},
                       cwd, foreground_cwd, tokens{}, state_change_seq, completion_seq, revision
```

Observed facts:

- `repo_key` is the absolute git common directory (`<repo>/.git`); `repo_name` is its
  repository directory's name. The Git adapter computes the same key so both sources agree.
- `screen_detection_skipped` is omitted when false.
- Title fields are `string|null` and an empty title arrives as `""`, not null.
- Panes and agents are listed in **layout order**; the first pane of a tab is the one the
  others were split from.
- Pane ids (`w4:p2`) and tab ids (`w4:t5`) restart with each server; a pane moved to another
  workspace gets a **new pane id** while its `terminal_id` stays, and Herdr keeps resolving the
  old id to the moved pane (so a clear sent to the old id lands on the live pane).
- `agent_session.value` is the agent's own session id (Claude Code's session UUID, Codex's
  rollout id) and survives a Herdr restart when Herdr resumes the agent.

Observed on a live 0.9.5 server (recorded in `tests/fixtures/herdr/`):

- **Worktree info is not derived from the cwd.** A workspace created with
  `herdr workspace create --cwd <checkout>` reports `worktree: null`, even inside a git
  repository. Herdr fills the field once its own worktree commands (`worktree open|create`)
  have touched the repository — then for every workspace of it. Scope's git adapter covers
  the gap with the same key (design.md, *Observation*).
- `pane report-agent` (hook authority) turns a reported `working → idle` into `done`, keeps
  `done` until the pane is focused, then reports `idle`. `--agent-session-id` is not reflected
  in `agent_session` for a test hook source; Herdr's own integrations (`source: herdr:claude`)
  do fill it.

### Statuses

`agent_status ∈ {idle, working, blocked, done, unknown}`. Herdr derives them either from an
integration hook (`screen_detection_skipped = true`, authoritative) or from screen patterns
(heuristic: can flap to `idle` for a beat mid-turn; treats anything unrecognised as `idle`).
`done` collapses into `idle` within seconds.

## Events

Subscription request: `{"subscriptions": [{"type": "pane.created"}, ...]}`. The first line
back is `{"id": ..., "result": {"type": "subscription_started"}}` or an error line followed by
a close. **One bad entry rejects the whole request.**

Two naming styles on the wire:

| Subscription | Wire `event` | `data` |
| --- | --- | --- |
| lifecycle (`pane.created`, `workspace.renamed`, …) | snake_case (`pane_created`) | has `type` |
| per-pane (`pane.agent_status_changed`, `pane.output_matched`, `pane.scroll_changed`) | dotted, as subscribed | no `type` |

`internal/adapters/herdr/events.go` normalises both to `domain.EventKind`.

Per-pane subscriptions **require `pane_id`** (no wildcard), and the server probes the pane at
setup — a pane that closed between snapshot and subscribe rejects the whole stream. Server
side, each per-pane status subscription runs a `pane_get` every 100 ms when no hub event
arrived. Scope therefore does not subscribe per pane; it subscribes to lifecycle kinds only:

```text
pane.created pane.closed pane.exited pane.agent_detected pane.focused pane.moved
workspace.created workspace.closed workspace.renamed workspace.focused
tab.created tab.closed tab.renamed tab.moved
(+ workspace.moved workspace.reordered when reorder_workspaces or nest_folders is on)
```

`pane.updated` and `workspace.metadata_updated` are deliberately not subscribed: they are
mostly the echo of Scope's own token writes, and a subscriber to `pane.updated` has been
measured to make every token write slow (~110 ms vs 1–2 ms).

### Delivery order

The server polls each subscription in turn and writes that subscription's backlog before the
next one's: a burst arrives grouped by kind (three `pane_created`, then two `pane_focused`,
…), **not in chronological order across kinds** (observed creating three workspaces). Nothing
may be inferred from the order of events of different kinds.

### Replay and history

- Herdr 0.9.5 starts a subscription at the hub's **current sequence**: no history is replayed.
- Herdr 0.8.x replayed up to 512 historical events on every connect (documented by herdr-api,
  including `pane_exited` for long-dead panes).
- A subscriber that falls behind the 512-event ring receives
  `{"error": {"code": "events_lost", ...}}` and must resubscribe and resync.

Scope is correct under all three: **an event never mutates domain state.** It only marks the
model dirty and wakes the scheduler; the next frame reads `agent.list`/`session.snapshot` and
the domain transitions run against that authoritative state. Replayed, duplicated, throttled
or lost events can only cost an extra read.

## Tokens (`report_metadata`)

From `src/app/api_helpers.rs`:

| Limit | Value |
| --- | --- |
| tokens per request | 16 (an oversized patch is rejected whole) |
| tokens per pane/workspace | 32 |
| key | `^[A-Za-z0-9_-]{1,32}$` |
| value | `trim()`ed, control characters removed, truncated to 80 chars; empty ⇒ clear |
| source | `^[A-Za-z0-9:._-]{1,80}$` |

`null` clears a token. Leading whitespace is trimmed, so indentation is carried by a leading
**U+200B ZERO WIDTH SPACE**, which Rust's `trim()` does not strip.

A report to a pane or workspace that no longer exists answers `pane_not_found` /
`workspace_not_found`; Scope treats that as *landed* (the target and its tokens are gone), and
a tab mark sent to a tab that closed (`tab_not_found`) likewise.

### Token ownership

Every write is tagged `source = "plugin:<plugin id>"` (`plugin:kriuchkov.herdr-scope`). A
token's identity is therefore `(source, target, name)` — written in Scope's docs and logs as
`herdr-scope/<pane-id>/<token-name>` or `herdr-scope/<workspace-id>/<token-name>`. Token
names cannot contain `/`, so the triple is not a single token name. Cleanup only ever sends
clears under Scope's source; clearing a same-named token another plugin set is a no-op.

## Sidebar rows (`config.toml`)

`[ui.sidebar.agents] rows = [[...], ...]` renders each inner array as one line; `$name` is a
custom token. A cell may be `{ token, fg, bold, dim, rules = [...] }` with up to 16 rules
(`equals|contains|starts_with|gt|lt`, first match wins). Limits: 16 rows per layout, **16
tokens per row — one more and the whole config file fails to parse**, taking every plugin and
keybinding down with it. Adjacent visible cells are joined with ` · ` (a single space after
`state_icon`); a row with no visible cell disappears. `rows_by_agent.<id>` replaces `rows` for
one canonical agent id; an id Herdr does not know is a parse error, so Scope only names ids
present in `<herdr state dir>/agent-detection/remote/*.toml`.

How a line is drawn (0.9.5 source, `ui/sidebar/tokens.rs`, `client/shell/agent_sidebar.rs`):

- The separator is fixed: `separator(previous, current)` is `" "` after the built-in
  `state_icon` (or before `git_status`) and `" · "` everywhere else. No key changes it, so a
  line without the dot has one visible cell.
- `fg` takes only `#RGB`/`#RRGGBB` — no theme colour names. A cell without `fg` inherits the
  panel's own style: `overlay0` of the active theme in Agents, `overlay1` in Spaces.
- Spaces indents nothing but worktree families: a repository with a linked worktree and its
  main checkout open is drawn as one block where its first member stands, main checkouts then
  worktrees behind `   ├─ ` / `   └─ ` (`client/shell/sidebar.rs workspace_entries`). Any other
  nesting is the plugin's to draw, inside its own tokens.
- The focused agent's whole entry (every line, the group header included when it rides on that
  entry) is filled with the theme's `active_row_bg`.
- The sidebar's background is the terminal's: `sidebar_bg` is `Color::Reset` in every built-in
  theme, while Herdr's own text colours come from the theme. Rows have to be written for the
  theme's side, not the desktop's.

Host appearance: the attached client queries the terminal (`OSC 10/11`, `CSI ?996n`) and the
server passes the answer to panes (a process in a pane gets the host's colours back), but not
to plugin commands, and no API method returns it. The server's environment carries no
`TERM_PROGRAM` either, so a plugin daemon cannot tell which terminal is drawing the sidebar.
`[theme] auto_switch` switches between `dark_name`/`light_name` and applies
`[theme.custom.dark|light]` on top of the active base.

## Agent view

`agent.view.set {source, label, sort: [{field: {token: "ws_key"}, order: "desc"}, ...]}`
reorders the Agents panel. Missing values sort last. A `filter` (`all`/`any`/`not`/`eq`/`in`/`exists` over
`status`, `workspace_id`, `tab_id`, `agent`, `seen` or a token) narrows it; the value
`{"context": "current_workspace_id"}` follows the focus on the server side, but only against the
built-in `workspace_id` — against a token it is refused (`invalid_agent_view`: "agent view
context type does not match the selected field"). Scope's folds use a token as a filter field:
`{"op": "not", "filter": {"op": "exists", "field": {"token": "folded"}}}` drops every row it marks
with `folded`, and a stop that clears the token brings the rows back. Herdr tells a plugin
nothing about clicks in the sidebar; a click on an Agents entry focuses that entry's pane, which is
how the fold line (a hidden agent's row) is clicked open for the visit. The override lives only in server memory:
it is gone after a server restart or live handoff, and while it is active Herdr disables the
panel's own grouped/priority toggle. `agent_panel_sort` (`"spaces"` | `"priority"`) persists
in `config.toml` only — no query, no event.

## Plugin host surface

- Runtime commands run with the plugin directory as cwd. A relative program containing `/` is
  resolved against the plugin root (`program_for_cwd`), so `["bin/herdr-scope", ...]` works.
- Injected env: `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH`, `HERDR_ENV=1`, `HERDR_PLUGIN_ID`,
  `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`,
  `HERDR_PLUGIN_CONTEXT_JSON`, `HERDR_PLUGIN_ACTION_ID`, `HERDR_PLUGIN_EVENT(_JSON)`,
  `HERDR_PLUGIN_ENTRYPOINT_ID`.
- `[[build]]` runs during `plugin install` (not `plugin link`) without socket env.
- `[[startup]]` runs after the server restores the session and again after a live handoff.
- Action ids may not contain dots and must be unique even across platforms.

## Compatibility gate

`herdr-scope run` pings first and refuses to start below protocol **22**:

```text
ERROR:
Herdr Scope requires Herdr protocol 22+.
Detected protocol 19.
```

Newer protocols are accepted (Herdr tolerates unknown fields); a real break surfaces as a
decode error naming the method. `herdr-scope doctor` compares the live protocol with the
tested one and warns when they differ.

## Not used (investigated)

- `completion_seq` / `state_change_seq`: present on agents, unused — the done badge follows
  the working→idle transition, past the idle grace. Candidate signal for a future improvement.
- `pane.agent_status_changed` per-pane subscriptions: see Events.
- `events.wait`: single-shot; not useful for a resident observer.
