# Herdr protocol notes

These notes were established against **Herdr 0.9.5, socket protocol 22**. The sources, most
trusted first:

1. the live server;
2. Herdr's source at `v0.9.5`;
3. `herdr api schema --json`.

When Herdr changes, re-run `herdr api schema --json` and `herdr-scope doctor`.

## Transport

The transport is newline-delimited JSON over `$HERDR_SOCKET_PATH`: a Unix socket, or the
named pipe `\\.\pipe\<path>` on Windows.

- A request is `{id, method, params}`. The answer is `{id, result: {type, …}}` or
  `{id, error: {code, message}}`. An event is `{event, data}`, with no id.
- The server answers one request per connection, then closes it. `events.subscribe` is the
  only long-lived connection, and the client must not write to it after the request.
- Scope uses `github.com/cyperx84/herdr-api` for transport only. Its `Agent` type lacks
  `tokens`, `terminal_id`, `agent_session`, `display_agent` and `title`, so Scope decodes its
  own DTOs (`internal/adapters/herdr/wire.go`).

## Methods

| Method | Used for |
| --- | --- |
| `ping` | protocol gate, version, liveness |
| `session.snapshot` | the authoritative state: workspaces with worktree info, tabs, panes, agents, focus |
| `agent.list` | the per-frame refresh of status, title and focus |
| `events.subscribe` | wake hints |
| `pane.report_metadata`, `workspace.report_metadata` | write and clear tokens |
| `agent.view.set` / `clear` | the Agents panel's sort and filter |
| `workspace.move_block` | `reorder_workspaces`, `nest_folders` |
| `pane.list`, `workspace.list`, `tab.list` | orphan sweeps, a stop's clear, the tab-bar line |
| `pane.focus` | the jump action |
| `pane.process_info`, `pane.layout` | the tab-bar line: cwd and width |
| `tab.rename` | `mark_tabs`; `tab_not_found` counts as done |

The CLI (`$HERDR_BIN_PATH`) handles the cold paths: `server reload-config`, `config check`,
`notification show`, `plugin pane open` and `--version`. Nothing polls it.

## Observed facts

- Agent statuses are `idle`, `working`, `blocked`, `done` and `unknown`. A hook status
  (`screen_detection_skipped = true`) is authoritative. A screen-pattern status can flap to
  `idle` mid-turn, and `done` collapses into `idle` within seconds.
- Panes and agents come in layout order. A tab's first pane is the one the others were split
  from.
- Pane and tab ids restart with every server. A pane moved to another workspace gets a new
  id but keeps its `terminal_id`, and Herdr still resolves the old id to it.
- `agent_session.value` is the agent's own session id. It survives a Herdr restart when
  Herdr resumes the agent.
- **Worktree info does not come from the cwd.** A workspace created with `--cwd <checkout>`
  reports `worktree: null` until Herdr's own worktree commands touch the repository. Scope's
  git adapter fills the gap, keyed the same way (`repo_key` = the git common dir).
- Title fields are `string|null`, and an empty title arrives as `""`.

## Events

Scope subscribes to lifecycle kinds only:

```text
pane.created pane.closed pane.exited pane.agent_detected pane.focused pane.moved
workspace.created workspace.closed workspace.renamed workspace.focused
tab.created tab.closed tab.renamed tab.moved
(+ workspace.moved workspace.reordered when Scope moves workspaces)
```

- **One bad entry rejects the whole subscription.**
- Per-pane subscriptions need a live `pane_id`, and each one costs the server a `pane_get`
  every 100 ms. Scope reads `agent.list` once a second instead.
- `pane.updated` is mostly the echo of Scope's own writes. Subscribing to it was measured to
  slow every token write from 1–2 ms to about 110 ms.
- A burst arrives grouped by kind, not in time order.
- 0.9.5 replays no history. 0.8.x replayed up to 512 events. A subscriber that falls behind
  gets `events_lost`.

None of this matters for correctness, because **an event never changes state**. It marks the
model dirty, and the next frame reads the authoritative snapshot.

## Tokens

| Limit | Value |
| --- | --- |
| tokens per request | 16; a larger patch is rejected whole |
| tokens per pane or workspace | 32 |
| key | `^[A-Za-z0-9_-]{1,32}$` |
| value | trimmed, control characters removed, 80 chars max; empty clears |
| source | `^[A-Za-z0-9:._-]{1,80}$` |

`null` clears a token. Indentation rides on a leading U+200B, which `trim()` keeps. A report
to a target that is gone (`pane_not_found`, `workspace_not_found`) counts as landed. Every
write carries `source = "plugin:kriuchkov.herdr-scope"`, so Scope's clears cannot touch
another plugin's token.

## Sidebar rows

From `[ui.sidebar.agents]` in `config.toml`, as of 0.9.5 (`ui/sidebar/tokens.rs`):

- A row allows **16 tokens; one more fails the whole config file**, and every plugin and
  keybinding goes down with it. A layout allows 16 rows.
- Visible cells are joined with ` · ` (one space after `state_icon`). Nothing changes that,
  so a line without the dot is a line of one cell.
- `fg` takes only `#RGB` or `#RRGGBB`.
- `rows_by_agent.<id>` must name an id Herdr knows, or the file fails to parse.
- Spaces indents only worktree families. Any other nesting is the plugin's to draw.
- The focused agent's whole entry, header included, gets `active_row_bg`.
- The sidebar's background is the terminal's (`sidebar_bg` is `Reset`), so rows must be
  written for the theme's side, not the desktop's. A plugin cannot learn the host terminal:
  no API returns its colours, and the server has no `TERM_PROGRAM`.

## Agent view

`agent.view.set {source, label, sort, filter}` reorders and filters the Agents panel.

- `{"context": "current_workspace_id"}` follows the focus, but only against the built-in
  `workspace_id`.
- Folds filter on a token: `not exists folded`.
- Herdr reports no sidebar clicks. A click on an entry focuses its pane.
- The override lives in server memory and is lost on a restart or live handoff. While it is
  set, Herdr disables the panel's own toggle.

## Plugin host

- Commands run with the plugin directory as cwd. `bin/herdr-scope` resolves against the plugin
  root.
- Herdr injects `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH`, `HERDR_PLUGIN_ID`, `HERDR_PLUGIN_ROOT`,
  `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`, `HERDR_PLUGIN_EVENT(_JSON)` and others.
- `[[build]]` runs on `plugin install`, not on `plugin link`. `[[startup]]` runs after the
  session is restored and after a live handoff.
- Action ids may not contain dots.

`herdr-scope run` refuses protocols below 22, with `Herdr Scope requires Herdr protocol 22+`.
It accepts newer ones, and `doctor` warns when the live protocol differs from the tested one.
