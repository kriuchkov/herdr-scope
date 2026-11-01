# Configuration

The file is `$(herdr plugin config-dir kriuchkov.herdr-scope)/config.toml`. The settings popup
(`prefix+,`) edits it: `↑↓` select, `←→` change, `↵` edit, `r` default, `s` save, `q` close. A
save validates every value, rewrites only the lines that changed (your comments stay), and the
daemon applies it at once. Hand edits are picked up within two seconds.

| Option | Default | Does |
| --- | --- | --- |
| `agents_panel` | `plugin` | Scope's Agents panel, or `herdr` for Herdr's own |
| `order` | `active` | `active` grouped by activity, `recent` flat, `here` only the focused workspace and its family, `off` Herdr's order |
| `layout` | `classic` | `classic`: the logo in a cell of its own colour (`§ · ✓ title`); `compact`: one cell per line (`§ ✓ title`) |
| `variant` | `auto` | `font`, `text` (plain Unicode) or `none`; `auto` uses the font only where the terminal can draw it |
| `space_mark` | `logo` | `logo`: a workspace's mark in Spaces is the logo of the agent its state speaks for; `state`: the state mark, plus a line naming its agents in `classic` |
| `logo_color` | `active` | `active`: the vendor's colour while the agent works or waits, grey when idle; `always`: the vendor's colour |
| `done_hold` | `until_seen` | keep the `✓` until the pane is focused, or a number of seconds (or `"30s"`) |
| `blocked_hold` | `true` | keep the `?` until the agent works again |
| `idle_grace_seconds` | `2.5` | how long idle must last to count as a finished turn |
| `activity_fresh_minutes` | `15` | how long after its last turn a row reads as fresh |
| `activity_stale_minutes` | `120` | how long without a turn before a row dims |
| `show_age` | `waiting` | time in the state before the title: `waiting` on rows waiting on you (`? 4m`), `all`, or `off` |
| `group_indent` | `2` | spaces per tree level; `0` draws no tree lines |
| `group_gap` | `true` (`false` in `compact`) | a blank row between groups |
| `split_corner` | `false` | hang a split's other panes off the first with a corner |
| `nest_folders` | `true` | hang a workspace opened inside another one's folder under it, and keep it right below it in Herdr's list |
| `reorder_workspaces` | `false` | make Herdr's workspace order follow Scope's |
| `mark_tabs` | `true` | end a waiting tab's name with its mark (`2 ?`, `api ✓`) |
| `row_label` | `title` | `title`, `tab` (the tab's name) or `both` |
| `trim_group_prefix` | `true` | drop the workspace name from a title the header already shows |
| `worktree_mark` | `U+F418` | the mark on a worktree header (needs a Nerd Font); empty for none |
| `follow_appearance` | `false` | switch Herdr's theme with the desktop's light/dark; needs a `[theme]` table |
| `colors.active_row_bg_light` | `#b9cdf2` | selected-row fill on a light theme; empty keeps the theme's |
| `colors.active_row_bg_dark` | `#232636` | selected-row fill on a dark theme |

A few keys live only in the file:

```toml
log_level = "info"            # debug, info, warn or error
led_glyph = "●"               # one mark for the blocked and idle states

[observer]
heartbeat_interval = "1s"     # longest the agent list goes unread (250ms..1m)
reconcile_interval = "30s"    # longest the whole session goes unread (1s..1h)

[glyphs]                      # a state's mark: working, done, blocked, idle_fresh, idle, idle_stale, unknown
done = "✔"
```

The keys of the table above may also sit under `[display]`, which also takes `sort`. A number
out of range takes its nearest limit; an unknown word or a bad colour takes the default.
`herdr-scope config validate` and the log name each one.

## Idle tiers

Idle is split by the time since the last turn. For panes older than the daemon, that time is
read from the session's own record: the tail of a Claude Code or Codex transcript, or Kilo's
`kilo.db` (through the `sqlite3` binary when it is installed). Only timestamps are read, and
nothing is kept.
