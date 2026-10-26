# herdr-scope

Herdr Scope is a sidebar plugin for [Herdr](https://herdr.dev). It turns the sidebar's Agents
list into something you can read: who is working, who is waiting on you, who finished while you
were elsewhere, who has been parked for two hours. Agents are grouped by project, each in its
vendor's mark and colour; worktrees nest under their repository, rows order by activity, and
idle sessions fade. It is one Go binary with two layouts: `classic`, the default, drawn below,
and `compact`, which packs each line into one cell. It observes and paints; it never starts,
prompts or stops anything.

```
Agents
 dashboard
   § · ⣯ Add rate limits to the API         ← working: braille spinner, the title in the vendor's colour
   § · ✓ 12m Cache search results           ← done: green tick, held until you look, and for how long
   └─ feature/rate-limits                   ← a worktree under its repository
     Λ · ? 4m Which branch should I rebase? ← blocked: red, the ? pulses — it has asked you 4m ago

 billing
   § · Fix the rounding bug                 ← idle: just stopped; the logo goes grey
   ✦ · Rename the ledger columns            ← idle for two hours: the row dims
Spaces
 § · dashboard                              ← whose agent and its state: the most urgent one speaks
 ? · feature/rate-limits                    ← a question blinks ? with the logo
 § · billing
```

## Install

```sh
herdr plugin install kriuchkov/herdr-scope --ref release
```

The `release` branch carries prebuilt binaries for macOS, Linux and Windows (amd64 and arm64),
so no Go toolchain is needed. To pin a version, use `--ref v0.1.0-bin`. Without `--ref`, Herdr
checks out the default branch and builds it from source, which needs Go 1.26 or newer (the
modules are vendored, so the build needs no network). Each release also publishes archives and
`SHA256SUMS` on the GitHub Releases page. While the repository is private, Herdr fetches it
with your own git credentials.

The install's setup step writes Scope's managed
blocks into Herdr's `config.toml` (fenced by `HERDR-SCOPE-BEGIN`/`END` markers; nothing
outside them is touched), installs the icon font into your user font directory (no admin
rights), and maps its codepoints in Ghostty/kitty configs that exist.

> [!IMPORTANT]
> Herdr starts plugins when its server starts. If the sidebar has not changed after
> installing, start the daemon once:
> `herdr plugin action invoke kriuchkov.herdr-scope.state-start`.
> **Do not run `herdr server stop`** to "restart" anything: it ends every process in every pane.
> Nothing in Scope needs a server restart.

Requires Herdr 0.9.0+ (socket protocol 22). Scope refuses to start on an older Herdr:

```text
ERROR:
Herdr Scope requires Herdr protocol 22+.
Detected protocol 19.
```

From a checkout:

```sh
git clone https://github.com/kriuchkov/herdr-scope.git
cd herdr-scope && make build        # `plugin link` runs no build step
herdr plugin link .
herdr plugin action invoke kriuchkov.herdr-scope.state-start
```

### Keys

All of them go through Herdr's prefix (`ctrl+b` by default), so nothing inside a pane receives
them. Paste into `config.toml` (or print the snippet with `bin/herdr-scope configure --keys`):

```toml
[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "kriuchkov.herdr-scope.view-flip"     # order: active <-> recent

[[keys.command]]
key = "prefix+u"
type = "plugin_action"
command = "kriuchkov.herdr-scope.jump"          # the next agent waiting on you

[[keys.command]]
key = "prefix+f"
type = "plugin_action"
command = "kriuchkov.herdr-scope.view-here"     # only this workspace <-> all

[[keys.command]]
key = "prefix+m"
type = "plugin_action"
command = "kriuchkov.herdr-scope.fold"          # fold <-> unfold this folder

[[keys.command]]
key = "prefix+comma"
type = "plugin_action"
command = "kriuchkov.herdr-scope.settings"      # settings popup
```

Key bindings are yours: change the keys if they collide with anything you use.

`jump` focuses the next agent that waits on you, in panel order: every question first, then
every finished turn you have not seen; pressed again it moves on, round the list. Focusing a
finished agent clears its tick, as looking at it would.

`view-here` narrows the Agents panel to the workspace in front — with the folders nested in it
and its worktrees — and follows you as you switch; pressed again it shows everyone. The panel's
corner reads `here` meanwhile.

`fold` folds the folder in front — the workspace with the folders nested in it and its
worktrees — to one line in the Agents panel: `▸ acme · +3`, how many agents it hides, with the
spinner in front while one of them works. Agents waiting on you stay under that line, and so
does the workspace you switch to; pressed again it unfolds. A click on the line opens the folder
while you are in it — the line reads `▾ acme` — and it folds again once you leave; the key there
folds it at once. Folds are remembered by folder and survive a restart. The panel folds in the `active` and `here` orders; the Spaces column cannot
hide a workspace (Herdr gives each one a line), so it does not change.

The tab bar leads with who waits — `?1 ✓2` (a question, two unseen results) — before the path
and branch, so you see it with the sidebar collapsed too; nothing shows when nobody waits.

A tab whose agent waits on you ends its name with the wait's mark — `2 ?`, `api ✓` — until the
wait is over (`mark_tabs`). Herdr colours a tab only by focus, so the name is all a plugin can
mark. Herdr counts a tab renamed that way as named and stops renumbering it, so Scope keeps a
tab named with a bare number in step with its place. A stop takes the marks off.

### Update

```sh
herdr plugin update kriuchkov.herdr-scope
herdr plugin action invoke kriuchkov.herdr-scope.state-stop
herdr plugin action invoke kriuchkov.herdr-scope.state-start
```

The next start — the action, or Herdr's hook when an agent appears — sees that the daemon runs
an older build and restarts it on the new one; restarting the daemon does not touch your panes.

### Uninstall

In this order — `unconfigure` stops the daemon, clears every token it wrote, removes the
managed blocks (your `config.toml` comes back byte for byte) and restores `[theme]`. From then
on Scope stays off: Herdr's hooks no longer start it, until you run the `state-start` or
`configure` action (or install it again):

```sh
herdr plugin action invoke kriuchkov.herdr-scope.unconfigure
herdr plugin action invoke kriuchkov.herdr-scope.uninstall-font
herdr plugin uninstall kriuchkov.herdr-scope
```

The state directory with config backups stays (`$(herdr plugin config-dir …)` for config,
`~/.local/state/herdr/plugins/kriuchkov.herdr-scope` for state); delete it by hand if you want
nothing left.

## What the colours and marks mean

The **logo** says whose agent it is, in its vendor's colour or ink while the agent works or
waits on you, in its title's grey while it idles (an agent Scope has no mark for wears `❖`), and
Herdr draws ` · ` between it and the title; the **colour** of the title says what it is doing — a
working title wears its vendor's colour. (With `layout = "compact"` the line is one cell: no
` · `, and the logo wears the line's colour.) In **Spaces** a workspace's mark is the logo of
the agent its state speaks for, in that state's colour: a question blinks `?` with it, a result
shows `✓`, a workspace with no agent `◦`.

| State | Line | Before the title |
| --- | --- | --- |
| working | the vendor's colour, bold | a braille spinner |
| waiting on you | red | a question mark, blinking |
| done | green | a tick, held until you focus the pane |
| idle | the freshness scale: fresh → plain → stale (the whole row dims) | — |
| unknown | violet | a ring |

Idle is split by time since the last turn (`activity_fresh_minutes`, `activity_stale_minutes`).
For panes older than the daemon the last turn is recovered from the session's own record —
the tail of a Claude Code or Codex transcript, or Kilo's `kilo.db` row (read-only, via the
`sqlite3` binary when installed). Only timestamps are read; nothing is logged or kept.

## Settings

`prefix+,` opens the popup: `↑↓` select, `←→` change, `↵` edit, `r` default, `s` save and apply,
`q` close. Saving validates every value first, writes only the lines that changed (your
comments stay), and the daemon applies it immediately — no restart of anything.

| Option | Default | Does |
| --- | --- | --- |
| `agents_panel` | `plugin` | Scope's panel, or `herdr` for Herdr's own |
| `order` | `active` | `active` grouped by activity / `recent` flat / `here` active, only the focused workspace and its family / `off` Herdr's order |
| `variant` | `auto` | logos from the icon font (`font`), plain Unicode (`text`), or `none`; `auto` uses the font only where the terminal can draw it (fontconfig on Linux, a Ghostty/kitty codepoint map, or a patched face in iTerm2's default profile) |
| `layout` | `classic` | `classic`: the logo in a cell of its own, in its own colour (`§ · ✓ title`); `compact`: each line one cell, the logo in the line's colour (`§ ✓ title`) |
| `space_mark` | `logo` | `logo`: a workspace is one line in Spaces, its mark the logo of the agent its state speaks for (`? ↔ §` while asking, `✓` with a result); `state`: the workspace's state mark, with its agents named on a second line (in the `classic` layout) |
| `logo_color` | `active` | `active`: a logo wears its vendor's colour while its agent works or waits on you, an idle one its title's grey; `always`: the vendor's colour whatever the agent does |
| `done_hold` | `until_seen` | keep the tick until the pane is focused, or a number of seconds (or `"30s"`) |
| `blocked_hold` | `true` | keep the question mark until the agent works again |
| `idle_grace_seconds` | `2.5` | idle must persist this long to count as a finished turn |
| `activity_fresh_minutes` | `15` | how long after its last turn a pane reads as fresh |
| `activity_stale_minutes` | `120` | how long without a turn before the row dims |
| `group_indent` | `2` | spaces per tree level; `0` keeps headers and gaps, draws no tree lines |
| `group_gap` | `true` (`false` with `layout = "compact"`) | a blank row between groups |
| `split_corner` | `false` | hang the other panes of a split off the first with a corner |
| `reorder_workspaces` | `false` | make Herdr's workspace indices follow Scope's order |
| `show_age` | `waiting` | put how long a row has been in its state in front of its title: `waiting` on rows that wait on you (`? 4m`, `✓ 12m`), `all` on every row (the turn `⣯ 12m`, the idle time `3h`), or `off`; one cell's colour, so it costs width, not a ` · ` |
| `nest_folders` | `true` | hang a workspace opened inside another workspace's folder under it, in both columns, and keep it right below that workspace in Herdr's list (Scope moves it there) |
| `mark_tabs` | `true` | end a tab's name with the wait's mark (`2 ?`, `api ✓`) while an agent in it waits on you; off takes the marks off |
| `row_label` | `title` | `title`, `tab` (the tab's name) or `both` |
| `trim_group_prefix` | `true` | drop the workspace name from a title the header already shows |
| `worktree_mark` | `U+F418` | the mark on a worktree header (needs a Nerd Font); empty for none |
| `follow_appearance` | `false` | switch Herdr's theme with the desktop's light/dark — only for a terminal that follows the desktop too; needs a `[theme]` table |
| `colors.active_row_bg_light` | `#b9cdf2` | selected-row fill for a light theme; empty keeps the theme's own |
| `colors.active_row_bg_dark` | `#232636` | selected-row fill for a dark theme |

The file is `$(herdr plugin config-dir kriuchkov.herdr-scope)/config.toml`. A few keys live
only in the file: `log_level`, `led_glyph`, and the tables `[display]` (same keys, plus
`sort`), `[observer]` and `[glyphs]`:

```toml
log_level = "info"            # the daemon log: debug, info, warn or error
led_glyph = "●"               # one mark for the blocked and idle states at once

[observer]
heartbeat_interval = "1s"     # longest the agent list goes unread (250ms..1m)
reconcile_interval = "30s"    # longest the whole session goes unread (1s..1h)

[glyphs]                      # a state's mark, over the variant's own and led_glyph
done = "✔"                    # keys: working, done, blocked, idle_fresh, idle, idle_stale, unknown
```

Hand edits are picked up within two seconds. A number out of range takes its nearest limit, and
an unknown word or an invalid colour takes its default; `herdr-scope config validate` and the log
name each one.

## Diagnostics

```sh
bin/herdr-scope status     # daemon, Herdr version, protocol, connection, agents, last event/reconcile/sidebar
bin/herdr-scope doctor     # binary, protocol, socket, events, agent list, dirs, config, font, sidebar
herdr plugin log list --plugin kriuchkov.herdr-scope --limit 20
```

The daemon logs to `scope.log` in the state directory (rotated at 1 MiB). It logs ids, kinds,
statuses and errors — never prompts, titles, transcripts or environment.

**Colours.** Herdr draws the sidebar on your terminal's own background, so Scope writes its row
colours for the side of the theme Herdr runs (`[theme] name`; unset is catppuccin, a dark theme)
— not for the desktop. On a light terminal set a light theme (`name = "catppuccin-latte"`, or
`auto_switch = true`); the rows follow within a minute.

**Icons.** `variant = "auto"` draws plain-Unicode marks unless the terminal can draw the icon
font. Ghostty and kitty can once `font install` has written their codepoint map. iTerm2,
Terminal.app and Windows Terminal have no codepoint map, so the icons have to be in the
terminal's own font: patch a copy of it and pick the copy in the profile.

```sh
herdr-scope font patch ~/Library/Fonts/MesloLGLNerdFontMono-*.ttf   # any static TrueType face
```

Each copy is the original plus the icons, scaled to its grid, with ` Herdr` on its family name
(`MesloLGL Nerd Font Mono Herdr`), installed for this user beside the original; `--out DIR`
writes the files instead. In iTerm2, `variant = "auto"` then finds the icons in the default
profile's font (its non-ASCII font when that is on) and draws them; elsewhere set
`variant = "font"`. `herdr-scope doctor` says which face iTerm2 draws with and whether it carries
the icons. `dist/JetBrainsMonoHerdr-Regular.ttf` is the same thing ready-made for JetBrains Mono.
Boxes or CJK glyphs instead of logos mean the terminal draws `U+E1A0–U+E1BA`, `U+E1C0–U+E1C5`
from a face without them.

**Skipped blocks.** A skipped block means your `config.toml` already declares `[theme.custom]`
or `[ui.sidebar.*]` itself — TOML allows a table once, so Scope keeps its block out rather than
break the file. Likewise `configure` refuses while `[ui] tab_bar_right` is set by hand: merge
the two yourself.

## How it works

One resident daemon per Herdr session. Herdr's lifecycle events wake it; a one-second
heartbeat over the socket reads the agent list (Herdr has no cheap event for status or title
changes); structural events and a 30 s reconcile read the whole `session.snapshot`. Events are
hints only — every decision is made against the authoritative snapshot, so replayed,
duplicated or lost events cannot corrupt state. It writes only its own display tokens (under
the source `plugin:kriuchkov.herdr-scope`), its managed blocks, and files in its own state and
config directories. No network, no server. Like every Herdr plugin it runs as your user and is
not sandboxed — read `herdr-plugin.toml` and `cmd/`, `internal/` before installing if that
matters to you.

Architecture, protocol notes, every feature with the tests that pin it, and the design
decisions: [docs/architecture.md](docs/architecture.md), [docs/protocol.md](docs/protocol.md),
[docs/features.md](docs/features.md), [docs/design.md](docs/design.md).

## Development

```sh
make build        # bin/herdr-scope; the next state-start (or new agent) restarts the daemon on it
make race         # unit, contract and golden tests, with the race detector
make lint         # golangci-lint, darwin and windows targets
make golden       # rewrite tests/golden after an intended change to the look
HERDR_SCOPE_INTEGRATION=1 go test ./tests/integration -v   # against a real, isolated Herdr server
bin/herdr-scope render --input tests/fixtures/scenarios/scenario-01.json
```

The integration test starts its own headless Herdr with a private HOME, XDG directories and
socket; it never talks to your running session.

## License and credits

MIT, see [LICENSE](LICENSE). The glyph tables, colour palette, token names and managed-block
layout are ported from MIT-licensed code; the icon font is forked from
qintmb/herdr-icon-agent-ui (MIT); `dist/JetBrainsMonoHerdr-Regular.ttf` is JetBrains Mono with
the icons patched in, under the SIL OFL 1.1 (`dist/OFL.txt`). Vendor marks belong to their
owners. Details and license texts in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
