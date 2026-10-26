package sidebar

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kriuchkov/herdr-scope/internal/domain"
	"github.com/kriuchkov/herdr-scope/internal/ports"
)

// Token names, published under Scope's own source.
const (
	tokenHarnessLogo    = "harness_logo"
	tokenLogo           = "logo"
	tokenLogoWorking    = "logo_working"
	tokenLogoStale      = "logo_stale"
	tokenLogoFresh      = "logo_fresh"
	tokenLogoIdle       = "logo_idle"
	tokenLogoUnknown    = "logo_unknown"
	tokenSplit          = "split_mark"
	tokenGroup          = "group"
	tokenGroupStale     = "group_stale"
	tokenGroupParent    = "group_parent"
	tokenGap            = "gap"
	tokenSortKey        = "sort_key"
	tokenWsKey          = "ws_key"
	tokenTabKey         = "tab_key"
	tokenSpaceNone      = "space_none"
	tokenSpaceLabel     = "space_label"
	tokenSpaceOther     = "space_logo_other"
	tokenSpaceIdleLogos = "space_logo_idle" //nolint:gosec // a sidebar token's name, not a credential
	tokenFold           = "fold"
	tokenFoldCount      = "fold_count"
	// tokenFolded marks a row a fold hides; the panel's filter drops it.
	tokenFolded = "folded"
	statePrefix = "state_"
	titlePrefix = "title_"
)

// foldMark leads a folded family's line, as Herdr's own folded worktree
// group; openMark leads it while the family is open for a visit.
const (
	foldMark = "▸"
	openMark = "▾"
)

// zwsp carries indentation: Herdr trims leading whitespace off a token
// value, and a zero-width space is a format character, not whitespace.
const zwsp = "\u200B"

// Animation cadence: one spinner step, and half a beat of the blocked pulse.
const (
	spinStep  = 150 * time.Millisecond
	pulseHalf = spinStep * pulseSteps
)

// paneTokenNames is every pane token the projection owns.
func paneTokenNames() []string {
	names := []string{
		tokenHarnessLogo, tokenLogo, tokenLogoWorking, tokenLogoStale, tokenLogoFresh, tokenLogoIdle, tokenLogoUnknown, tokenSplit,
		tokenGroup, tokenGroupStale, tokenGroupParent, tokenGap,
		tokenSortKey, tokenWsKey, tokenTabKey,
		tokenFold, tokenFoldCount, tokenFolded,
	}
	for _, state := range domain.DisplayStates() {
		names = append(names, statePrefix+string(state), titlePrefix+string(state))
	}
	return names
}

// spaceTokenNames is every workspace token the projection owns.
func spaceTokenNames() []string {
	names := []string{"space_" + string(domain.DisplayBlocked)}
	for _, vendor := range SpaceWorkingVendors() {
		names = append(names, "space_working_"+string(vendor))
	}
	names = append(names, "space_working_other", "space_done", "space_idle", "space_unknown", tokenSpaceNone)
	names = append(names, spaceLogoTokens()...)
	return append(names, tokenSpaceOther, tokenSpaceIdleLogos, tokenSpaceLabel)
}

// tokenTables are the names and lookups every frame projects with. They
// follow from Scope's own tables alone, so a presentation builds them once.
type tokenTables struct {
	// paneBlank and spaceBlank clear every owned name; each target's tokens
	// for a frame start as a copy.
	paneBlank, spaceBlank Tokens
	// brand is the hue of each vendor that has one.
	brand map[domain.AgentKind]string
	// spaceWorking is the working mark's token of each vendor with its own.
	spaceWorking map[domain.AgentKind]string
	// logoCells are the Spaces agents line's cells in the order the row
	// draws them.
	logoCells []string
}

func newTokenTables() *tokenTables {
	t := &tokenTables{
		paneBlank:    clearAll(paneTokenNames()),
		spaceBlank:   clearAll(spaceTokenNames()),
		brand:        map[domain.AgentKind]string{},
		spaceWorking: map[domain.AgentKind]string{},
		logoCells:    append(spaceLogoTokens(), tokenSpaceOther, tokenSpaceIdleLogos),
	}
	for _, b := range brandColors() {
		t.brand[b.Kind] = b.Color
	}
	for _, vendor := range SpaceWorkingVendors() {
		t.spaceWorking[vendor] = "space_working_" + string(vendor)
	}
	return t
}

// Presentation is everything the projection needs besides the view.
type Presentation struct {
	Glyphs       Glyphs
	Layout       ports.Layout
	IndentWidth  int
	WorktreeMark string
	// SpaceMark is what marks a workspace in the Spaces column.
	SpaceMark ports.SpaceMark
	// LogoColor is when a logo wears its vendor's colour.
	LogoColor ports.LogoColor
	// ShowAge is which rows put their age in front of their title.
	ShowAge ports.AgeMode
	// tables is shared by every copy and never written after NewPresentation.
	tables *tokenTables
}

// NewPresentation derives the presentation from settings.
func NewPresentation(settings ports.Settings, fontAvailable func() bool) Presentation {
	p := Presentation{
		Glyphs:       NewGlyphs(settings.Presentation, fontAvailable),
		Layout:       cmp.Or(settings.Presentation.Layout, ports.LayoutClassic),
		IndentWidth:  settings.Policy.GroupIndent,
		WorktreeMark: settings.Presentation.WorktreeMark,
		SpaceMark:    settings.Presentation.SpaceMark,
		LogoColor:    settings.Presentation.LogoColor,
		ShowAge:      settings.Presentation.ShowAge,
		tables:       newTokenTables(),
	}
	p.Glyphs.blankRest = p.compact()
	return p
}

// lookup is the tables NewPresentation built; a presentation put together by
// hand builds them afresh.
func (p Presentation) lookup() *tokenTables {
	if p.tables == nil {
		return newTokenTables()
	}
	return p.tables
}

// compact is whether each line goes out as one cell.
func (p Presentation) compact() bool { return p.Layout == ports.LayoutCompact }

// indent is the leading run for a depth: a zero-width space protecting the
// spaces behind it from Herdr's trim. Zero indent width draws no indent.
func (p Presentation) indent(depth int) string {
	if p.IndentWidth <= 0 || depth <= 0 {
		return ""
	}
	return zwsp + strings.Repeat(" ", p.IndentWidth*min(depth, 3))
}

// Tokens is a token patch: a value sets, nil clears.
type Tokens map[string]*string

func (t Tokens) set(name, value string) {
	if value == "" {
		t[name] = nil
		return
	}
	v := value
	t[name] = &v
}

// PaneTokens is what one row's pane carries this frame — every owned name,
// set or cleared.
func (p Presentation) PaneTokens(view domain.SidebarView, group domain.SidebarGroup, index int) Tokens {
	row := group.Rows[index]
	tokens := p.rowTokens(row, stepAt(view.At), view.At)
	if row.Fold != nil {
		if row.Fold.Alone {
			blankLine(tokens)
		}
		p.foldTokens(tokens, *row.Fold, stepAt(view.At))
	}
	if view.Grouped {
		p.groupTokens(tokens, group, index)
	}
	return tokens
}

// FoldedTokens is what a row a fold hides carries: the row as it would read,
// so a stopped daemon leaves it readable, still at the first spinner step so
// a working agent nobody sees costs no writes, and the mark the filter drops.
func (p Presentation) FoldedTokens(view domain.SidebarView, row domain.SidebarRow) Tokens {
	tokens := p.rowTokens(row, 0, view.At)
	tokens.set(tokenFolded, "1")
	return tokens
}

// rowTokens is a row's own line and sort keys, every other owned name
// cleared.
func (p Presentation) rowTokens(row domain.SidebarRow, step int64, now time.Time) Tokens {
	tokens := maps.Clone(p.lookup().paneBlank)
	p.lineTokens(tokens, row, step, now)
	if logo, ok := p.Glyphs.Logo(row.Kind); ok {
		tokens.set(tokenHarnessLogo, logo)
	}
	tokens.set(tokenSortKey, row.Keys.Pane)
	tokens.set(tokenWsKey, row.Keys.Workspace)
	tokens.set(tokenTabKey, row.Keys.Tab)
	return tokens
}

// blankLine takes a row's drawn line off it, leaving the unrendered state
// mark: the row of a fold that hides it too shows the fold line alone.
func blankLine(tokens Tokens) {
	for name := range tokens {
		if strings.HasPrefix(name, titlePrefix) || strings.HasPrefix(name, tokenLogo) || name == tokenSplit {
			tokens[name] = nil
		}
	}
}

// foldTokens is a folded family's line: the mark and the root's name, then
// how many agents it hides, behind the logo and spinner of one that works.
// The value is what colours the count cell (see foldLine): a pane holds too
// few tokens for a cell per vendor.
func (p Presentation) foldTokens(tokens Tokens, f domain.Fold, step int64) {
	head := foldMark + " " + f.Label
	if f.Open {
		head = openMark + " " + f.Label
	}
	tail := ""
	if f.Hidden > 0 {
		tail = "+" + strconv.Itoa(f.Hidden)
	}
	if f.Working {
		lead := p.Glyphs.Lead(domain.DisplayWorking, step)
		if logo, ok := p.Glyphs.Logo(f.WorkingKind); ok {
			lead = logo + " " + lead
		}
		tail = strings.TrimSpace(lead + " " + tail)
	}
	if p.compact() {
		tokens.set(tokenFold, strings.TrimSpace(head+" "+tail))
		return
	}
	tokens.set(tokenFold, head)
	tokens.set(tokenFoldCount, tail)
}

// lineTokens is the row itself, under a state-named token so the state picks
// its colour. The compact layout sends `logo mark title` as that one cell;
// the classic one sends the logo in a cell of its own, in its vendor's colour, which
// Herdr draws as `logo · mark title`. The state mark is published too,
// unrendered, so a restarted daemon can read back what a pane was showing.
func (p Presentation) lineTokens(tokens Tokens, row domain.SidebarRow, step int64, now time.Time) {
	g := p.Glyphs
	indent := p.indent(row.Depth)
	corner := ""
	switch row.Corner {
	case domain.CornerTee:
		corner = "├─ "
	case domain.CornerLast:
		corner = "└─ "
	case domain.CornerNone:
	}
	logo := g.Resolve(domain.Agent{Kind: row.Kind}).Glyph

	tab, mark := "", g.State(row.Display)
	if row.TabLabel != "" {
		tab, mark = row.TabLabel+" ", row.TabLabel+" "+mark
	}
	lead := g.Lead(row.Display, step)
	if lead != "" {
		lead += " "
	}
	if age := Age(now, row.Since); p.showsAge(row.Display) && age != "" {
		lead += age + " "
	}
	tokens.set(statePrefix+string(row.Display), mark)
	if p.compact() {
		head, body := corner+logo, tab+lead+row.Title
		if logo != "" && body != "" {
			head += " "
		}
		// The indent alone is a zero-width space Herdr keeps: a blank line.
		if line := strings.TrimRight(head+body, " "); line != "" {
			tokens.set(titlePrefix+string(row.Display), indent+line)
		}
		return
	}
	// The indent belongs to whichever cell comes first: Herdr only hangs its
	// own indent on continuation rows, and a member's row is its first.
	var split, logoValue, prefix string
	switch {
	case corner != "":
		split = indent + corner
		logoValue = logo
		prefix = tab + lead
	case logo != "":
		logoValue = indent + logo
		prefix = tab + lead
	default:
		prefix = indent + tab + lead
	}
	tokens.set(tokenSplit, split)
	if logoValue != "" {
		tokens.set(p.logoToken(row.Display), logoValue)
	}
	if row.Title != "" {
		tokens.set(titlePrefix+string(row.Display), prefix+row.Title)
	}
}

// logoToken is the cell a logo goes out under, which picks its colour: the
// vendor's, or with logo_color active an idle logo's title colour. A stale
// logo fades in either.
func (p Presentation) logoToken(state domain.DisplayState) string {
	quiet := p.LogoColor == ports.LogoColorActive
	switch state {
	case domain.DisplayWorking:
		return tokenLogoWorking
	case domain.DisplayIdleStale:
		return tokenLogoStale
	case domain.DisplayIdleFresh:
		if quiet {
			return tokenLogoFresh
		}
	case domain.DisplayIdle:
		if quiet {
			return tokenLogoIdle
		}
	case domain.DisplayUnknown:
		if quiet {
			return tokenLogoUnknown
		}
	case domain.DisplayDone, domain.DisplayBlocked:
	}
	return tokenLogo
}

// showsAge is whether a row in this state wears its age.
func (p Presentation) showsAge(state domain.DisplayState) bool {
	switch p.ShowAge {
	case ports.AgeAll:
		return true
	case ports.AgeWaiting:
		return state.Waiting()
	case ports.AgeOff:
	}
	return false
}

// groupTokens is the furniture: the header on the run's first row (faded with
// its workspace when every agent there is stale), a synthesised repository
// row above an orphan worktree, and a spacer on the run's last row. A fold
// line stands in for the repository row and puts the header on a
// continuation line.
func (p Presentation) groupTokens(tokens Tokens, group domain.SidebarGroup, index int) {
	if index == 0 && group.Header {
		folded := group.Rows[0].Fold != nil
		label := p.branch(group, folded) + group.Label
		if group.Stale {
			tokens.set(tokenGroupStale, label)
		} else {
			tokens.set(tokenGroup, label)
		}
		if group.Tree == domain.TreeOrphan && !folded {
			tokens.set(tokenGroupParent, group.OrphanRepo)
		}
	}
	if index == len(group.Rows)-1 && group.Gap {
		tokens.set(tokenGap, zwsp)
	}
}

// branch is the tree prefix of a nested header. At zero indent the row keeps
// the worktree mark and draws no corner; a nested folder is no worktree and
// wears no mark. A continued header sits on a line Herdr indents already.
func (p Presentation) branch(group domain.SidebarGroup, continued bool) string {
	mark := ""
	if p.WorktreeMark != "" && !group.Folder {
		mark = p.WorktreeMark + " "
	}
	hasIndent := p.IndentWidth > 0
	switch group.Tree {
	case domain.TreeChild, domain.TreeLastChild:
		corner := ""
		if hasIndent {
			corner = "├─ "
			if group.Tree == domain.TreeLastChild {
				corner = "└─ "
			}
		}
		if continued {
			return corner + mark
		}
		return p.indent(1) + corner + mark
	case domain.TreeOrphan:
		// No indent: under the repository row this is Herdr's own
		// continuation row, which arrives indented already.
		if hasIndent {
			return "└─ " + mark
		}
		return mark
	case domain.TreeDetached:
		return mark
	case domain.TreeRoot:
	}
	return ""
}

// spaceToken is the token a workspace's mark is published under. The three
// idle tiers share one cell; working splits by vendor so the mark keeps its
// brand colour.
func (t *tokenTables) spaceToken(state domain.DisplayState, vendor domain.AgentKind) string {
	switch state {
	case "":
		return tokenSpaceNone
	case domain.DisplayWorking:
		if name, ok := t.spaceWorking[vendor]; ok {
			return name
		}
		return "space_working_other"
	case domain.DisplayIdleFresh, domain.DisplayIdle, domain.DisplayIdleStale:
		return "space_idle"
	case domain.DisplayDone, domain.DisplayBlocked, domain.DisplayUnknown:
	}
	return "space_" + string(state)
}

// SpaceTokens is what one workspace carries this frame; at picks the frame of
// a blinking mark.
func (p Presentation) SpaceTokens(space domain.SpaceView, at time.Time) Tokens {
	t := p.lookup()
	tokens := maps.Clone(t.spaceBlank)
	mark := p.spaceMark(space, stepAt(at))
	head, under := spaceCorner(space.Corner)
	if p.compact() {
		// One line, one cell: the name takes the mark's colour.
		tokens.set(t.spaceToken(space.Mark, space.MarkKind), head+strings.TrimSpace(mark+" "+space.Label))
		return tokens
	}
	tokens.set(t.spaceToken(space.Mark, space.MarkKind), head+mark)
	tokens.set(tokenSpaceLabel, space.Label)
	if p.SpaceMark == ports.SpaceMarkLogo {
		// The mark already says whose: no line naming the agents.
		return tokens
	}
	// One token per branded vendor, so each keeps its colour; everything
	// else shares one neutral cell, and with logo_color active a vendor
	// with nothing going on shares a grey one.
	var others, quiet []string
	for _, kind := range space.Kinds {
		label := string(kind)
		if logo, ok := p.Glyphs.Logo(kind); ok {
			label = logo + " " + label
		}
		_, branded := t.brand[kind]
		switch {
		case p.LogoColor == ports.LogoColorActive && !slices.Contains(space.Active, kind):
			quiet = append(quiet, label)
		case branded:
			tokens.set("space_logo_"+string(kind), label)
		default:
			others = append(others, label)
		}
	}
	tokens.set(tokenSpaceOther, strings.Join(others, " "))
	tokens.set(tokenSpaceIdleLogos, strings.Join(quiet, " "))
	if under != "" {
		// The agents line starts under the nested mark: whichever logo cell
		// the row draws first carries the indent.
		for _, name := range t.logoCells {
			if v := tokens[name]; v != nil {
				tokens.set(name, under+*v)
				break
			}
		}
	}
	return tokens
}

// spaceMark is a workspace's mark. With logo marks a working or idle
// workspace shows its agent's logo, and a question blinks the question mark
// with it, in step with the rows' pulse; a result, an unknown state and an
// empty workspace keep their state marks, as does variant none.
func (p Presentation) spaceMark(space domain.SpaceView, step int64) string {
	mark := p.Glyphs.SpaceMark(space.Mark)
	if p.SpaceMark != ports.SpaceMarkLogo || space.Mark == "" {
		return mark
	}
	logo := p.Glyphs.Resolve(domain.Agent{Kind: space.MarkKind}).Glyph
	if logo == "" {
		return mark
	}
	switch space.Mark {
	case domain.DisplayWorking, domain.DisplayIdleFresh, domain.DisplayIdle, domain.DisplayIdleStale:
		return logo
	case domain.DisplayBlocked:
		if resting(step) {
			return logo
		}
	case domain.DisplayDone, domain.DisplayUnknown:
	}
	return mark
}

// spaceCorner is a nested folder's lead in the Spaces column, drawn where
// Herdr draws its own worktree rows: the corner at the third column for the
// mark's line, and the indent that puts the agents line under the mark.
func spaceCorner(corner domain.Corner) (string, string) {
	switch corner {
	case domain.CornerTee:
		return zwsp + "  ├─ ", zwsp + "     "
	case domain.CornerLast:
		return zwsp + "  └─ ", zwsp + "     "
	case domain.CornerNone:
	}
	return "", ""
}

// spaceLogoTokens are the branded logo cells in the order the row draws them.
func spaceLogoTokens() []string {
	names := make([]string, 0, len(BrandVendors()))
	for _, vendor := range BrandVendors() {
		names = append(names, "space_logo_"+string(vendor))
	}
	return names
}

// ReadBack is which display state a pane's published tokens say, for a
// daemon that lost its state file. Empty when none of ours is set.
func ReadBack(tokens map[string]string) domain.DisplayState {
	for _, state := range domain.DisplayStates() {
		if _, ok := tokens[statePrefix+string(state)]; ok {
			return state
		}
	}
	return ""
}

// NextAnimation is when the view next needs repainting for its own motion:
// the next spinner step while anything works (behind a fold too: its line
// spins), the next half of the pulse
// while anything is blocked (a row, or a workspace's mark); zero when nothing
// moves.
func NextAnimation(view domain.SidebarView) time.Time {
	var next time.Time
	ms := view.At.UnixMilli()
	consider := func(period time.Duration) {
		p := period.Milliseconds()
		at := time.UnixMilli((ms/p + 1) * p)
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	for _, g := range view.Groups {
		for _, row := range g.Rows {
			if row.Fold != nil && row.Fold.Working {
				consider(spinStep)
			}
			if row.Display.Animated() {
				consider(cadence(row.Display))
			}
		}
	}
	// A workspace that asks blinks its mark.
	for _, space := range view.Spaces {
		if space.Mark == domain.DisplayBlocked {
			consider(pulseHalf)
		}
	}
	return next
}

// cadence is how often an animated row's lead changes: the blocked pulse every
// half beat, the spinner every step.
func cadence(state domain.DisplayState) time.Duration {
	if state == domain.DisplayBlocked {
		return pulseHalf
	}
	return spinStep
}

// stepAt is the animation step a moment falls in.
func stepAt(at time.Time) int64 { return at.UnixMilli() / spinStep.Milliseconds() }

// The steps of an age: minutes under an hour, hours under two days, then days.
const (
	ageInHoursFrom = time.Hour
	ageInDaysFrom  = 48 * time.Hour
	day            = 24 * time.Hour
)

// Age is how long since since, as short as a title can afford: "12m",
// "5h", "3d". Empty under a minute, and when since is unknown.
func Age(now, since time.Time) string {
	if since.IsZero() {
		return ""
	}
	d := now.Sub(since)
	switch {
	case d < time.Minute:
		return ""
	case d < ageInHoursFrom:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < ageInDaysFrom:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/day)) + "d"
}

// ageChange is the next moment after now at which Age(·, since) reads
// differently.
func ageChange(now, since time.Time) time.Time {
	d := now.Sub(since)
	unit := time.Minute
	switch {
	case d >= ageInDaysFrom:
		unit = day
	case d >= ageInHoursFrom:
		unit = time.Hour
	case d < time.Minute:
		return since.Add(time.Minute)
	}
	steps := int64(d/unit) + 1
	return since.Add(time.Duration(steps) * unit)
}

// NextRepaint is NextAnimation and the next moment a shown age changes.
func (p Presentation) NextRepaint(view domain.SidebarView) time.Time {
	next := NextAnimation(view)
	for _, g := range view.Groups {
		for _, row := range g.Rows {
			if row.Since.IsZero() || !p.showsAge(row.Display) {
				continue
			}
			if at := ageChange(view.At, row.Since); next.IsZero() || at.Before(next) {
				next = at
			}
		}
	}
	return next
}
