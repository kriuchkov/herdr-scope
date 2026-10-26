# Features

Every feature of Herdr Scope and the tests or references that pin it. The decisions behind
them, and their reasons, are in [design.md](design.md).

## How the look is pinned

`TestGoldenSidebar` (tests/golden) replays every scenario in `tests/fixtures/scenarios`
through Scope's whole pipeline and draws it as Herdr draws rows, in both layouts:
`scenario-NN.classic.txt` (the default) and `scenario-NN.txt` (compact), with
`*.tokens.txt` naming the token behind every cell. `TestGoldenAges` draws scenario 01 once
more with every age shown (`*.ages.txt`). A change to the look must change these files
explicitly: `go test ./tests/golden -update` (or `make golden`).

Two more kinds of test stand behind the rows below. `TestScopeAgainstARealHerdr`
(tests/integration, opt-in; *integration* below) starts an isolated Herdr 0.9.5 server, links
the plugin and drives agents through Herdr's hook API. tests/contract decodes `agent.list`,
`session.snapshot` and event frames recorded from Herdr 0.9.5.

## Sidebar behaviour

| Feature | Test / reference |
| --- | --- |
| working indicator (braille spinner, vendor-coloured bold title) | `TestWorkingRowSpinner`, `TestWorkingTitleWearsItsVendorsColour`, scenario-01 |
| blocked indicator (pulsing `?`) | `TestBlockedPulse`, scenario-07 |
| done persistence (`until_seen`) | `TestDoneHeldUntilSeen`, `TestTurnEndShowsDoneUntilFocused`, integration |
| done persistence (timed `done_hold`) | `TestDoneTimedHoldExpires`, scenario-06 |
| no tick for a turn that ends in front of you | `TestDoneNotSetWhileFocused` |
| blocked persistence (`blocked_hold`) | `TestBlockedHeldUntilWorkResumes`, scenario-07 |
| blocked released by an authoritative (hook) idle | `TestBlockedReleasedByAuthoritativeIdle`, `TestAuthoritativeIdleDoesNotReadoptPublishedBlocked`, scenario-06 |
| startup prompt (question before any work) not pinned | `TestBlockedStartupPromptReleasedOnIdle`, scenario-01 |
| idle never becomes blocked | `TestIdleNeverBecomesBlocked` |
| idle grace (`idle_grace_seconds`) | `TestGraceAbsorbsDetectorFlap`, `TestGraceDoesNotDelayBlocked`, scenario-06, integration |
| idle tiers (fresh / idle / stale), configurable | `TestFreshnessTiers`, `TestFreshnessFollowsConfiguredThresholds` |
| stale dimming (row, logo, header) | `TestStaleRowFadesItsLogoAndHeader`, `TestStaleHeaderOnlyWhenEveryAgentIsStale`, scenario-05 |
| unknown status is its own state | `TestUnknownStaysUnknown`, `TestParseAgentStatusTreatsTheUnrecognisedAsUnknown` |
| activity recovery for panes older than the daemon (Claude Code and Codex transcripts; Kilo via the `sqlite3` binary) | `TestClaudeActivityFromTheTranscriptTail`, `TestCodexActivity`, `TestKiloActivity` |
| `show_age`: how long a row has been in its state, by default only on rows waiting on you (question, result), on every row with `"all"`; repainted when a shown age changes; a turn's start survives a restart | `TestEachStateKnowsWhenItBegan`, `TestAgeIsShortAndCoarse`, `TestShowAgePutsTheAgeBeforeTheTitle`, `TestShowAgeWaitingIsForRowsThatWaitOnYou`, `TestShowAgeIsForWaitingRowsUnlessChosen`, `TestRoundTrip` |
| workspace grouping (headers, gaps) | `TestActiveOrderPutsTheBusiestGroupFirst`, `TestGapsFollowTheSetting`, scenarios |
| worktree hierarchy (tee/corner, gap rules) | `TestWorktreeFamilyStaysTogetherParentFirst`, `TestWorktreeBranches`, scenario-01 |
| orphan worktree with a synthesised repository row | `TestOrphanWorktreeGetsARepositoryRow`, scenario-05 |
| stray worktree detached in Herdr's order | `TestStrayWorktreeIsDetachedInHerdrOrder`, scenario-03 |
| worktrees Herdr reports no worktree info for (read from `.git`) | `TestRepositoryFallbackForWorkspacesWithoutHerdrInfo`, `TestResolveMainCheckoutAndWorktree` |
| folder nesting (`nest_folders`): a workspace inside another's folder hangs under it in Agents and Spaces, and is moved right below it in Herdr's list | `TestResolveTreeNestsFoldersUnderTheEnclosingWorkspace`, `TestResolveTreeStaysOneLevelDeep`, `TestResolveTreeLeavesPeersHomeAndRootsAlone`, `TestResolveTreeKeepsWorktreesWithTheirFamily`, `TestNestOrderMovesOnlyFamiliesThatTookInAFolder`, `TestSpaceCornersHangNestedFoldersOffTheirParent`, `TestSpaceCornersSeeHerdrsWorktreeBlocks`, `TestNestedFolderHangsUnderItsParentInAgents`, `TestNestedSpaceHangsOffItsParent`, `TestNestedFolderHeaderWearsNoWorktreeMark`, `TestNestedFolderIsMovedUnderItsParentWithoutReorder`; seen on a real Herdr 0.9.5 |
| split grouping (`split_corner`) | `TestSplitCorners`, `TestSplitCornerCarriesTheIndent`, scenario-04 |
| `group_indent` (0 = no tree lines) | `TestIndentZeroKeepsHeadersGapsAndMarks`, scenario-05 |
| `group_gap`; none by default in the compact layout | `TestGapsFollowTheSetting`, `TestGroupGapDefaultFollowsTheLayout`, `tests/golden/scenario-NN.txt` |
| sorting `active` | `TestActiveOrderPutsTheBusiestGroupFirst`, `TestSortKeysEncodeFamilyAndSplit`, `TestWriterOrders` |
| sorting `recent` | `TestRecentOrderIsFlatAndUnstampedLast`, scenario-02 |
| sorting `off` (Herdr's order, grouped or priority) | `TestOffOrderKeepsHerdrOrderAndGroupsOnlyWhenHerdrDoes`, `TestPanelGrouped`, scenario-03 |
| `here` order: the focused workspace and its family only, following the focus (Herdr's `current_workspace_id` filter, the family named by Scope) | `TestHereNarrowsActiveAndComesBack`, `TestFamilyIsTheRootAndItsMembers`, `TestHereKeepsToTheFocusAndItsFamily`, `TestSetAgentViewFilters`, `TestHereFollowsTheFocusToAnotherFamily`; seen on a real Herdr |
| flip / cycle / native transitions | `TestSortTransitions`, `TestNativeToggleSwitchesRowsBeforeTheOrder`, `TestFlipNeverTouchesTheRows` |
| `fold`: a folder (with its nested folders and worktrees) folds to one line in Agents, `▸ name · +N`, the spinner of a hidden agent at work; who waits on you and the focus stay; a click on the line (focus landing on the hidden agent that carries it) opens the family until the focus leaves it (`▾`); remembered by folder; only in `active`/`here` | `TestFoldKeepsWhatWaitsOnYou`, `TestAFoldThatKeepsNothingIsOneLine`, `TestFoldNeverHidesTheFocus`, `TestALineBorrowsTheKeysOfItsFamilysFirstRow`, `TestFoldsLeaveOnlyScopesGroupedOrders`, `TestFoldLineIsARowOfItsOwn`, `TestALineThatHidesNothingRidesOnTheFirstRow`, `TestAloneFoldShowsTheLineAlone`, `TestAFamilyOpenForAVisitShowsWhole`, `TestGoingToAFoldLineOpensItForTheVisit`, `TestFoldCountColours`, `TestWriterHidesFoldedRowsAndAStopShowsThemAgain`, `TestFoldHidesTheFamilyAndIsRemembered`, `TestDaemonFoldsAFamily`, `TestFoldFamilyNamesTheFocusedWorkspace`, `TestFolds`; seen on a real Herdr 0.9.5 |
| `jump`: focus the next agent waiting on you (questions, then unseen results), round the list | `TestWaitingAsksBeforeFinished`, `TestJumpToWaitingGoesRoundTheList`, integration |
| `row_label` (`title` / `tab` / `both`, the older `show_tab`) | `TestRowTextFor`, `TestTopLevelKeysFile`, scenarios 04/05 |
| `trim_group_prefix` | `TestTrimGroupPrefix`, `TestRowLabelsAndPrefixTrim`, scenario-01 |
| title normalisation (vendor pulse strip, location-only → vendor name) | `TestResolveTitle`, `TestLivePaneShapes` |
| `display_agent` override (known vendors only) | `TestLivePaneShapes`, scenario-08 |
| vendor icons (27 vendors, font + text) | `TestGlyphVariants`, scenario-07 |
| unknown agent kind: the generic mark `❖` | `TestUnknownAgentGetsTheGenericMark` |
| Spaces column (aggregated state, vendor logos, label) | `TestSpacesAggregateByUrgency`, `TestSpaceTokens`, every scenario |
| `space_mark = "logo"`: a workspace is one line in Spaces, its mark the logo of the agent it speaks for; a question blinks `?` with the logo | `TestSpaceMarkIsTheAgentsLogo`, `TestCompactSpaceIsOneCell`, `TestSpaceMarkAndLogoColour`, `tests/golden/*.classic.txt` |
| pane moves keep memory, no clear to the old id | `TestTrackFollowsAPaneMoveByTerminal`, `TestPaneMoveKeepsTheTick`, `TestWriterDoesNotClearAMovedPane`, scenario-08 |
| closed panes cleared; orphan token sweep | `TestWriterClearsAClosedPane`, `TestWriterSweepsOrphans` |
| write delta, 16-token chunks, per-target backoff | `TestWriterSendsOnlyWhatChanged`, `TestChunksNeverExceedTheLimit`, `TestWriterBacksOffAFailingPane` |
| `reorder_workspaces` | `TestWorkspaceOrderMovesFamiliesAndIsIdempotent` |

## Memory and events

| Feature | Test / reference |
| --- | --- |
| markers and activity keyed by agent session, persisted across daemon and Herdr restarts | `TestRestartRestoresMarkersAndActivity`, `TestRekeyMergesIntoAPersistedRecord`, integration |
| no done inferred from a status another process remembered | `TestNoDoneInferredFromAStatusRememberedByAnotherProcess` |
| events never mutate state; replayed or duplicated events are harmless | `TestEventsOnlyMarkWhatToReread`, `TestReplayedEventsAreHarmless` |
| a failed read or projection never touches memory | `TestFailedReadSkipsTheFrameAndKeepsState`, `TestProjectionFailureKeepsMemory` |
| atomic state, corrupt-file quarantine, schema version | `TestRoundTrip`, `TestCorruptFileIsQuarantined`, `TestUnknownSchemaVersionIsQuarantined` |

## Presentation

| Feature | Test / reference |
| --- | --- |
| classic layout (the default): `logo · title`, the logo in a cell of its own; two-line Spaces with `space_mark = "state"` | `TestGoldenSidebar`, `tests/golden/*.classic.txt` |
| compact layout (`layout = "compact"`): one cell per line, no ` · ` inside a row, one-line Spaces | `TestCompactLineIsOneCell`, `TestCompactSpaceIsOneCell`, `TestCompactLayoutKeepsEveryLineInOneCell`, `tests/golden/scenario-NN.txt` |
| blocked mark blinks in a compact line | `TestCompactBlockedPulseBlinks` |
| palette (brand, state, per-appearance freshness); idle tiers by brightness alone, in both layouts; idle logos in their title's colour unless `logo_color = "always"` | `TestLayoutColoursFollowTheAppearance`, `TestIdleLogosTakeTheirTitlesColour` |
| rows and row fill on the side of Herdr's theme | `TestRowsFollowHerdrsThemeNotTheDesktop`, `TestNoThemeBlockForAHostFollowingTheme` |
| row fill per side under `auto_switch` (`[theme.custom.light]`, `[theme.custom.dark]`) | `TestApplyInstallsBlocksAndKeepsTheRest` |
| `follow_appearance` drives `[theme] name` (off by default; needs a `[theme]` table) | `TestApplyAppearanceDrivesTheThemeAndRemoveRestoresIt`, `TestAppearanceFollowerActsOncePerFlip`, `TestFollowAppearanceLetsTheDesktopDecide`, `TestDarwin` |
| active row colours (`[colors]`, empty = theme's own) | `TestEmptyColourOverrideDropsTheThemeBlock`, `TestColorOverrides` |
| managed sidebar block (rows, rules, `rows_by_agent`, Spaces, 16-token guard) | `TestLayoutParsesAsTOMLAndRespectsHerdrLimits`, integration (`herdr config check`) |
| font variant (`auto` / `font` / `text` / `none`); `auto` only where the terminal can draw the font | `TestGlyphVariants`, `TestRenderable`, `TestNoneVariantHasNoLogoRules`, `TestRefreshRewritesADriftedBlock` |
| `variant = "auto"` and `doctor` read iTerm2's default profile font | `TestTerminalFontReadsITermsDefaultProfile`; seen on a real iTerm2 |
| `font patch`: any static TrueType face with the icons grafted in, renamed and installed beside it | `TestPatchScalesIconsOntoTheFacesCell`, `TestPatchKeepsEveryGlyphOfARealFace`, `TestPatchRefusesWhatItCannotGraft`, `TestSFNTRoundTrip`, `TestGlyphCodecRoundTrip`, `TestPatchFontsRegistersOnWindows`; Meslo Nerd Font checked with fontTools |
| `worktree_mark` (`U+F418`, empty = none) | `TestWorktreeBranches`, `TestSpecStyleTables` |
| custom state glyphs (`[glyphs]`, `led_glyph`) | `TestGlyphOverrides`, `TestTopLevelKeysFile` |
| tab-bar path line | `TestComposeFitsTheBudget`, `TestTabBarPublishesOnlyChanges` |
| tab bar badge `?1 ✓2` ahead of the path | `TestBadgeCountsWhoWaits`, `TestTabBarLeadsWithWhoWaits`, `TestWaitingCounts`; seen on a real Herdr |
| `mark_tabs`: a tab whose agent waits ends its name with the wait's mark (`2 ?`); a bare number follows the tab's place; a stop, or the setting off, takes the marks off | `TestTabMarksCarryTheMostUrgentWait`, `TestTabMarksTakeScopesMarkOff`, `TestABareNumberFollowsTheTabsPlace`, `TestMarksOffTouchOnlyMarkedTabs`, `TestSidebarRowsReadTheTabsNameWithoutTheMark`, `TestWriterMarksATabWhileItsAgentWaits`, `TestWriterRenamesATabTheUserRenamedMeanwhile`, `TestWriterBacksOffAFailingRename`, `TestWriterClearTakesTheMarksOff`, `TestTabsAndRenameTabShape` |

## Settings and keys

| Feature | Test / reference |
| --- | --- |
| settings popup (every key, validated, applied without a restart) | `internal/adapters/tui` tests (25), `TestDaemonReloadAppliesNewSettings`, `TestUpdateConfigurationValidatesBeforeSaving` |
| configuration hot reload (no daemon restart) | `TestDaemonReloadAppliesNewSettings`; verified on a real server |
| `prefix+a` → `view-flip`, `prefix+u` → `jump`, `prefix+f` → `view-here`, `prefix+m` → `fold`, `prefix+comma` → `settings` | `herdr-plugin.toml`, `TestManifestCommandsExist` |
| keybindings stay user-owned (`configure --keys` prints them) | `keybindings` in internal/bootstrap/cli.go |

## Lifecycle

| Feature | Test / reference |
| --- | --- |
| plugin manifest (actions, build, startup, events, popup) | `TestManifestIdentity`, `TestManifestActionsAreHerdrConventionsAndSpecComplete`, `TestManifestCommandsExist`; accepted by `herdr plugin link` |
| install: build, first-run setup, daemon start, no server restart | `TestSetupRunsOncePerBuild`, integration |
| prebuilt binaries: `--ref release` installs without Go | `.github/workflows/release.yml`, `scripts/build.sh`, `TestManifestCommandsExist` |
| protocol gate with the spec's message | `TestProtocolGateRefusesAnOldHerdr` |
| configure / unconfigure (markers, backups, foreign tables, `config check` rollback) | `TestClaimsTable`, `TestForeignTablesKeepTheirBlockOut`, `TestUnparsableWriteIsRestored`, `TestApplyRefusals` |
| `[theme]` restored, user file restored byte for byte | `TestApplyAppearanceDrivesTheThemeAndRemoveRestoresIt`, integration |
| `unconfigure` stays off: hooks do not restart the daemon or re-sort the panel; the start action brings it back | `TestEnsureObserverLeavesAnUnconfiguredScopeToTheUser`, `TestReapplyDoesNothingWhileUnconfigured`, `TestInvokedByHook`, integration |
| font install / uninstall (user dir, hash-named, no admin) | `internal/adapters/font` tests (19) |
| terminal codepoint maps (Ghostty, kitty) | `TestConfigureGhosttyAppendsTheMap`, `TestConfigureKittyAppendsTheMap`, `TestConfigureKeepsASymlinkedConfigASymlink` |
| `state-start` / `state-stop`, single instance, stall replacement | `TestEnsureObserver`, `TestDaemonHealthThresholds`, `TestASecondDaemonIsRefused`, `TestAStaleSocketIsReclaimed` |
| concurrent `state-start`s serialised | `TestWithLockSerialises`, integration |
| a start after a rebuild restarts the daemon on the new build | `TestEnsureObserverRestartsADaemonOfAnotherBuild`, integration |
| watchdog on `pane.agent_detected` | manifest `[[events]]`; observed in the integration log |
| per-session daemons (named Herdr sessions) | `TestNamedSessionsGetTheirOwnRuntime`, `TestALongRuntimeDirGetsAShortEndpoint` |
| reconnect with backoff, live-handoff resync | `TestDaemonWakesOnEventsAndResyncsOnReconnect`, `TestBackoffIsExponentialAndCapped` |
| daemon exits when the server is gone; startup hook restarts it | `TestDaemonExitsWhenTheServerIsGone`; verified on a real server restart |
| view re-asserted after the server loses it | `periodicTasks` (15 s) in internal/application/daemon.go |
| uninstall order: unconfigure → uninstall-font → plugin uninstall | README, `TestUnconfigureStopsThenRemoves` |
| `doctor`, `status`, `config validate`, `version` | run against a real server (see README) |
| dependency rule enforced | `TestInnerLayersKeepTheDependencyRule`, `TestAdaptersDoNotLeakIntoInnerLayers` |
