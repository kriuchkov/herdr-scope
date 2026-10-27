# Design decisions

Each decision comes with its reason. "Seen" means the case was watched on a real Herdr 0.9.5
session.

## Memory

- **An agent is keyed by its agent session, not its pane.** Pane ids restart with every
  server and change when a pane moves; a session id names the same conversation across both.
- **An unseen `✓` or unanswered `?` survives a Herdr restart.** The marker means "you have not
  looked yet", and a restart does not change that.
- **A status remembered by an earlier process never infers a finished turn.** No activity
  does not mean done.
- **Records unseen for 30 days are pruned.** An agent that leaves the list loses its markers
  but keeps its activity time, so a `--resume` shows the real last turn.

## Observation

- **Events only wake the loop.** Herdr's events arrive grouped by kind, can replay or be
  lost, so every decision is made against `session.snapshot` and `agent.list`.
- **The agent list is read at most once a second, and animation frames reuse it.** Every read
  costs Herdr a request, and a spinner step brings no news. A `✓` can land up to 1 s late,
  inside the 2.5 s idle grace.
- **Worktrees come from Herdr first, then from `.git`.** Herdr reports no worktree for a
  workspace created with a plain `--cwd` (protocol.md).
- **Folders nest (`nest_folders`).** A project kept as a folder of repositories otherwise
  reads as scattered workspaces. Herdr groups only worktree families, so Scope draws the
  Spaces corner itself and moves the family together in Herdr's list. It nests one level
  deep, and home or `/` never adopt.

## Look

- **`classic` gives the logo a cell of its own; `compact` sends one cell per line.** Herdr puts
  ` · ` between cells and nothing removes it. The dot is the price of a logo in its own
  colour, and `compact` drops both.
- **Idle tiers are brightness alone, and green means done only.** A green-tinted fresh idle
  row read as a second tick (seen).
- **A logo wears its vendor's colour only while its agent works or waits** (`logo_color =
  "active"`). With every logo in colour, colour stops saying where to look.
- **Spaces is one line per workspace** (`space_mark = "logo"`). The second line of agent names
  doubled the column, 12 lines for 7 workspaces (seen), and repeated the Agents panel.
- **`show_age = "waiting"`.** How long someone has waited decides whom to answer first; the
  length of a turn is noise.
- **The blocked mark pulses in place.** In a compact text line it blinks against a blank, since
  `Λ · Which port?` would read as Herdr's separator.
- **Rows are coloured for Herdr's theme, not the desktop.** The sidebar sits on the terminal's
  background and Herdr colours its own text from the theme, so the theme is the witness.
  Rows written for a light desktop came out light on a dark theme (seen).
- **`follow_appearance` is off by default.** Driving Herdr's theme from the desktop suits only
  a terminal that follows the desktop too.
- **`variant = "auto"` uses the icon font only where the terminal can draw it**: fontconfig on
  Linux, a Ghostty/kitty codepoint map, or a patched face in iTerm2's default profile. No
  macOS or Windows terminal draws a private-use codepoint from a font that is merely
  installed, and the daemon cannot tell which terminal is in front.
- **The dark selected-row fill is `#232636`.** On a black terminal a brighter fill outshone the
  rows it marks (seen).
- **No blank line inside a family.** One between siblings left `├─` pointing at an empty row
  (seen). `compact` drops the gap between groups too.
- **An unknown agent kind wears `❖`.** The row keeps its shape and nobody mistakes it for
  another vendor.

## Configuration

- **Scope edits nothing outside its `HERDR-SCOPE` markers.** TOML allows a table once, so a
  table you declared keeps Scope's block out instead of breaking the file, and every plugin
  with it. Removing Scope gives the file back byte for byte.
- **A save is validated, then applied by the daemon without a restart.** A typo must not
  quietly become another value, and nothing should stop painting meanwhile.
- **`activity_stale_minutes` below `activity_fresh_minutes` is refused.** Such a pair never
  shows the middle tier.
- **The newest 20 config backups are kept.** Every write makes one.

## Lifecycle

- **Starts, stops and `unconfigure` take turns through one lock.** Three starts ran in the
  same millisecond on a first start (seen), and `plugin action invoke` returns before its
  command ends.
- **`unconfigure` leaves an off switch.** Without it the `pane.agent_detected` watchdog brought
  the daemon back minutes later (seen). `state-start`, `configure` or a reinstall turns it on
  again.
- **A daemon of another build is replaced on the next start.** After `make build` the old
  build kept painting (seen three times).
- **The control socket moves to `$XDG_RUNTIME_DIR` or the temp dir past 104 bytes,** the Unix
  socket path limit.

## Not verified

- Windows builds and lints but has not been run. Whether Herdr's launcher resolves
  `bin/herdr-scope` to `.exe` is unconfirmed.
- Linux is unit-tested by faking the OS, not run on a host.
- The settings popup's raw-terminal handling is covered by its state-machine tests only.
- Kilo activity needs `sqlite3` on PATH; without it, Kilo panes read as plain idle.
