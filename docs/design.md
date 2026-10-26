# Design notes

The decisions behind Scope's behaviour and look, and the reason for each. Herdr's source
(0.9.5) is the reference for what Herdr does; "seen on a real session" means the case was
watched on a real Herdr. The tests that pin each feature are listed in
[features.md](features.md).

## Identity and persistence

| Scope | Why |
| --- | --- |
| One record per agent keyed by **agent session** (`<agent>:<session id>`), falling back to terminal id, then pane id; markers, activity and last-seen persisted in `state.json` | Pane ids restart with every Herdr server, so a pane-keyed stamp can land on a different agent after a restart, and a pane moved to another workspace gets a new id. A session id names the same conversation across moves and restarts. |
| An unseen ✓ or unanswered ? survives a Herdr restart when Herdr resumes the same agent session | Tokens die with the server; the marker means "you have not looked yet", which a restart does not change. |
| A status remembered by a previous process is never used to infer a finished turn | The spec forbids treating absence of activity as completion. |
| With no record (a lost or corrupt state file), a held ✓ or ? is re-adopted from the pane's published `state_*` token | The token is the one other witness of the marker while the server lives. |
| When an agent leaves the list its markers are dropped; the activity stamp stays; records unseen for 30 days are pruned | A session that comes back (`--resume`) keeps its real last-activity time. |

## Observation

| Scope | Why |
| --- | --- |
| Wakes on Herdr's lifecycle events, `pane.moved`, `tab.created`, `tab.closed` and `tab.moved` among them (the full list: protocol.md, *Events*) | Moves and tab changes change membership and split order; each costs one read. |
| Labels (tabs, workspaces, worktrees) come from `session.snapshot`, re-read on any structural event and every `reconcile_interval` (30 s) | One authoritative read; the structural events keep it current, so no label cache with a lifetime of its own is needed. |
| The agent list is re-read at most once per `heartbeat_interval` (1 s) or on an event; animation frames (up to ~7×/s) reuse it | Every read is a request Herdr has to answer, and an animation frame brings no news. A ✓ can appear up to 1 s after the turn ends, inside the 2.5 s idle grace. |
| `ws_key` and `tab_key` are computed from this frame's activity stamps | The order follows a turn in the frame that records it, not one frame later. |
| Tier crossings are scheduled 1 ms past the fresh threshold | At the threshold itself the old tier still holds; 1 ms later the tier changes on that wake instead of the next heartbeat. |
| Worktree info comes from Herdr first; for a workspace Herdr reports none for, the git adapter reads `.git`/`gitdir`/`commondir` at the workspace's first pane's directory | The spec requires a `RepositoryResolver` port, and Herdr reports `worktree: null` for a workspace created with a plain `--cwd` inside a checkout — it fills the field only after its own worktree commands touched the repository (verified on Herdr 0.9.5). Herdr's info alone would show no tree for such worktrees. The key is Herdr's own (`repo_key` = git common dir), so both sources agree. |

## Presentation

| Scope | Why |
| --- | --- |
| An agent kind with no mark (Droid, Letta, anything new) draws the generic mark `❖` in ink colour in the logo cell; `harness_logo` and the Spaces logo stay empty | The spec asks for a generic mark. |
| With `group_gap`, no blank line anywhere inside a family: after the parent or after a sibling worktree | A blank line between siblings left the `├─` corner pointing at an empty row (seen on a real session). |
| Besides a git worktree under its repository, `nest_folders = true` (the default) hangs a workspace opened inside another workspace's folder under it — a checkout's folder is its checkout, anything else its first pane's. In Agents it is drawn as a worktree is, minus the worktree mark (`└─ billing`, a synthesised parent row when the parent runs no agent); in Spaces Scope draws the corner itself, in the columns of Herdr's own worktree rows, and only where Herdr's order puts the workspace right under its family. To keep it there Scope moves such a family together in Herdr's list, parent first, even with `reorder_workspaces = false`; families of worktrees alone are left where they are. One level deep: a folder nested twice hangs under the outermost workspace. Home and a filesystem root never adopt, a linked worktree stays its repository's | A project kept as a folder of repositories (`acme/services/billing`) read as unrelated workspaces scattered over the list. Herdr groups only a repository's main checkout with its linked worktrees (`client/shell/sidebar.rs workspace_entries`), so the Spaces corner has to be a token of Scope's; it lives in the mark's cell and wears its colour. `nest_folders = false` hangs only git worktrees and leaves Herdr's order alone. |

## Look

The look was settled on a real Herdr 0.9.5 session (iTerm2 with a dark profile, macOS in light
mode), where an earlier one read badly: every logo and state mark a box, light rows and a light
row fill on a dark background, ` · ` between a row's logo and its title. Each cause is below;
Herdr's source (0.9.5) is the reference for what it does. The ` · ` stays in the default
layout: it is the price of a logo that keeps its vendor's colour, and `layout = "compact"`
drops it.

| Scope | Why |
| --- | --- |
| `layout = "classic"` (the default) gives the logo a cell of its own, in a colour of its own (see `logo_color`): Herdr draws `§ · ✓ Cache search results`. `layout = "compact"` sends each line as one cell: `§ ✓ Cache search results`, and a workspace is one line, `? dashboard`, in its state's colour | Herdr puts ` · ` between every two visible cells of a line, except after its own `state_icon` (`ui/sidebar/tokens.rs separator`); nothing in the config changes that, so a line without the dot is a line of one cell. The cost: in a compact line the logo wears the line's colour — its vendor's while working, the state's otherwise — instead of a colour of its own. |
| The blocked mark pulses between `?` and the idle mark; in a compact line with plain-Unicode marks, between `?` and a blank, so the mark blinks in place and the title never moves. A mark the user chose for idle is kept | There the idle dot sits beside the logo and reads as Herdr's separator: `Λ · Which port?`. |
| `variant = "auto"` uses the icon font on Linux when it is installed or fontconfig finds it. On macOS and Windows only when the font is installed **and** a Ghostty or kitty config carries the codepoint map (`font install` writes it), or — macOS — when iTerm2's default profile draws non-ASCII text with a face that carries the icons (`font patch` makes one from any TrueType face; Scope reads the profile with `defaults export`, read-only) | On macOS and Windows nothing draws a private-use codepoint from a font that is merely installed: iTerm2, Terminal.app and Windows Terminal have no codepoint map, so every mark would be a box. The daemon cannot learn which terminal is in front: Herdr's server environment carries no `TERM_PROGRAM` and host colours reach only panes; iTerm2's preferences are the one terminal font a plugin can read, and a Herdr in another terminal on the same Mac would get icons it cannot draw (`variant = "text"` settles it). `variant = "font"` remains for any terminal whose font carries the icons. |
| Rows and row fill are written for the side of the theme Herdr runs: its `[theme] name` (every alias Herdr accepts; unset is catppuccin, dark). The desktop decides only with `follow_appearance` on and a `[theme]` table to drive, or stands in under `auto_switch` and the `terminal` theme (dark without a desktop answer) | Herdr leaves the sidebar's background to the terminal (`sidebar_bg` is `Reset` in every built-in) and colours its own text from its theme, so the theme in force is the witness to the side the rows sit on — the desktop is not. Rows written for a light desktop come out light on a dark theme. |
| `follow_appearance = false` by default | Driving Herdr's theme from the desktop is right only for a terminal that follows the desktop too; for any other it repaints Herdr light over a dark terminal. Opt in where it holds. |
| Under `auto_switch` the row fill goes in per side, `[theme.custom.light]` and `[theme.custom.dark]`, and Herdr picks | Herdr 0.9.5 applies per-mode overrides after the active base (`resolve_effective_theme`), so neither value is wrong half the time. |
| Classic layout (the default): a blank line between groups (`group_gap = true`). Compact: none by default, the bold headers separate the groups (`group_gap = true` brings the line back) | The blank line is a line of the group's last entry, so in a group of one agent the selection fill covers it too: a three-line block for one agent (seen on a real session). The compact layout is the dense one and does without it; in the classic layout `group_gap = false` drops it. |
| `space_mark = "logo"` (the default): a workspace is one line in Spaces, the mark the logo of the agent it speaks for, in the state's colour (`§ · dashboard`); a question blinks `?` with the logo in step with the rows' pulse; a result keeps `✓`, an unknown state `◌`, an empty workspace `◦`, and without logos (`variant = "none"`) the state mark stays. `space_mark = "state"` draws the state mark and name, and in the classic layout a second line naming the agents (`§ claude · ✦ gemini`) | The second line repeated logos the Agents panel already shows and doubled the column: 12 lines for 7 workspaces on a real session, pushing the Agents panel down. The cost: a workspace's other agents are named only in Agents. No cell is added. Chosen on the design canvas. |
| `logo_color = "active"` (the default): a logo keeps its vendor's colour while its agent works or waits on you; an idle one takes its title's colour, an unknown one the unknown violet, and a vendor in the Spaces line naming the agents (`space_mark = "state"`) greys the same way. `logo_color = "always"` keeps the vendor's colour whatever the agent does (a stale one fades) | With every logo in its vendor's colour an idle agent's logo is as orange as a working one's, and colour no longer says where to look. The cost: three more logo cells and one Spaces cell, inside Herdr's 16 a row. |
| `show_age = "waiting"` (the default) puts how long in front of a question (`? 4m`) and an unseen result (`✓ 12m`); `"all"` on every row (the turn, the idle time); `"off"` none. `true`/`false` read as `all`/`off` | How long someone has waited is what decides which to answer first; the length of a turn or of an idle spell is noise by default. |
| Both layouts: idle tiers are brightness alone, fresh to stale (`#c8cde0` → `#9499ad` → `#6b6f82` dark, `#2f3347` → `#6a6e82` → `#9a9da8` light); green means done and nothing else | Tinted green, a column of fresh idle rows read as a column of finished turns (seen on a real session). |
| Workspace names in the Spaces column are `#9a9eb3` on dark, `#5c5f77` on light | On a light panel (`#eff1f5`, catppuccin latte) `#9a9eb3` reads at 2.4:1; `#5c5f77` reads at about 5.6:1 (seen on a light stand). |
| The tab bar leads with who waits (`?1 ✓2`) before the focused pane's path and branch, and is read every 3 s | A question waiting should show without opening the sidebar, and within seconds; the command types out a file, so the short interval costs a `cat`. |
| The dark selected-row fill is `#232636` (`colors.active_row_bg_dark` sets any other; the light one is `#b9cdf2`) | The fill covers every line of the focused entry, the group's blank gap line included; on a black terminal a brighter fill such as `#414868` outshouted the rows it marks (chosen on a real session). |
| With `follow_appearance` off the daemon re-checks the managed blocks once a minute and rewrites them only if they drifted | A theme name edited by hand moves the rows within a minute, without an appearance switch or a `configure`. |

## Configuration and settings

| Scope | Why |
| --- | --- |
| `done_hold` takes `"until_seen"`, a number of seconds, or a duration string (`"30s"`, `"2m"`); an invalid value is reported by `config validate`, refused by the popup, and falls back to `until_seen` with a warning at load | The spec asks for `done_hold = <duration>` and validation before saving; a typo must not quietly become some other hold. |
| Keys at the top level, plus the spec's tables: `[display]` (wins over the top level), `[observer]` (`heartbeat_interval`, `reconcile_interval`), `log_level`; `display.sort` sets the order until one is chosen with the key | Both ways of writing the file load; saves keep each key where it already is. |
| `U+XXXX` (as in `worktree_mark = "U+F418"`) is read as the codepoint everywhere, in the file as in the popup | The README documents the U+ form. |
| `activity_stale_minutes` below `activity_fresh_minutes` is refused by validation | Such a pair can never show the middle tier. A file that already has it still loads, with a warning. |
| Saving tells the daemon to reload: it re-reads the config, repaints, and rewrites managed blocks whose content depends on it (variant, row colours). Hand edits are picked up within 2 s | Spec §22: no restart, and no window where nothing paints. |
| `[colors]` changes reach `[theme.custom]` on reload | Same reason. |
| The popup marks a value "(default)" whenever it equals its default; an empty number is rejected rather than read as 0; backspace removes a whole character | Scope's settings type has no "unset", and an empty field read as 0 would save a value nobody typed. |
| Managed blocks are fenced by `# >>> HERDR-SCOPE-BEGIN <block>` / `# <<< HERDR-SCOPE-END <block>`, and Scope edits nothing outside them: a table declared there keeps Scope's block out, and a `tab_bar_right` set by hand makes `configure` refuse | Spec §45. TOML allows a table once; a second declaration would break the file, and with it every plugin and keybinding. |
| The tab-bar block goes directly under `[ui]`; install, appearance switches and removal give back the user's file byte for byte | Spec §45/§46: removing Scope gives the file back as it was, blank lines included. |
| Of the config backups, the newest 20 are kept | Bounded disk use: every write makes one, and every appearance switch writes. |
| Removing the terminal font block removes only the block and the blank lines next to it; the font copy is written atomically; an empty `XDG_*`/`LOCALAPPDATA` falls back to the default | Leaves the user's terminal config alone; a half-written copy would otherwise be skipped as installed forever. |

## Lifecycle

| Scope | Why |
| --- | --- |
| `state-start`, `state-stop` and `unconfigure` take turns through one lock file in the runtime directory | Starts arrive at once — the startup hook, the watchdog per detection, an action — and each runs first-run setup against `config.toml`: observed on a real server, three starts ran in the same millisecond on first start. And `herdr plugin action invoke` returns before the command ends, so a start invoked right after a stop found the old daemon still answering, said "already running", and left nothing running once it exited. |
| The control socket is `control.sock` in the runtime directory, falling back to `$XDG_RUNTIME_DIR` or the per-user temp directory (hashed name) when the path would exceed the 104-byte Unix socket limit | A long home or state directory, or a named session, would otherwise leave the daemon unable to start. |
| `unconfigure` leaves an off switch (`<state>/off`). While it is on, the plugin's hooks (startup, the `pane.agent_detected` watchdog) log that Scope is unconfigured and do nothing, and the startup `view --reapply` leaves Herdr's order alone. The `state-start` or `configure` action, or a reinstall, turns it off again and puts the blocks back | Without it the hooks start the daemon again on the next agent, which paints tokens no block shows and re-sorts the Agents panel: seen on a real session, the watchdog brought the daemon back minutes after `unconfigure`. |
| A healthy daemon started from another build (its status carries a hash of its executable) is stopped and started again from this one; the watchdog does the same at the next agent | Seen three times in one session: after `make build` the start action said "already running" and the old build kept painting. |
| Setup re-runs when the version changes (idempotent: installed blocks are kept, a present font is not copied) | One identity for the build. |

## Tokens

Scope's tokens (`state_*`, `title_*`, `logo*`, `split_mark`, `group*`, `gap`, `sort_key`,
`ws_key`, `tab_key`, `space_*`, `harness_logo`) are written under its own source
`plugin:kriuchkov.herdr-scope`, so its clears cannot touch another plugin's token of the same
name. `harness_logo` is cleared from a pane whose agent left the list.

## Not verified

- Windows: the code builds and lints for Windows (named-pipe control endpoint, `bin/herdr-scope.exe`
  build step) but has not been run there; whether Herdr's Windows launcher resolves
  `bin/herdr-scope` to `bin/herdr-scope.exe` is unconfirmed.
- Linux: unit-tested by faking the OS; not run on a Linux host.
- The settings popup's terminal handling (raw mode, resize) has only been exercised by its
  state-machine tests, not on a real TTY.
- Kilo activity recovery needs the `sqlite3` binary on PATH; without it Kilo panes read as plain idle.
