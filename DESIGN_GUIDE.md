# pom dock design guide

Version 1.0 · 25 October 2026

## 1. Product direction

Build a polished desktop workspace for local pentesting: macOS-inspired visual restraint, clear operational context, reliable terminal interaction, and efficient handling of dense technical information.

The primary user works for hours with Docker containers, terminals, recorded command output, and an engagement activity tracker. The interface must make it easy to identify the current context, locate evidence, and act without losing place.

Use Apple Human Interface Guidelines for desktop layout and visual hierarchy, PatternFly for list/detail interactions, and Carbon for table behavior. Implement a single coherent component system in the existing stack. These references do not require installing their component libraries.

This guide separates **product decisions** (colors, dimensions, visual treatment) from **usability and accessibility requirements**. Numerical design defaults below are starting values for pom dock, not universal UX standards. Preserve existing functional contracts; inspect the implementation before changing terms, scope, or data handling.

## 2. Principles

1. **Content first.** Commands, output, targets, and current context receive the strongest hierarchy. Repeated page headings and explanatory boilerplate should not displace working content.
2. **Recognition first.** Identify recordings and containers with meaningful labels and useful secondary metadata. Keep full technical identifiers accessible.
3. **Fast frequent actions.** Keep commonly used actions visible. Put occasional configuration in menus or inspectors. Measure frequency before hiding controls.
4. **Clear scope.** Distinguish workspace, engagement, container, terminal, recording, and selected-row actions. Show which object an action affects.
5. **Honest state.** Separate known, unknown, stale, running, completed, and failed states. Never infer connectivity or success from unrelated signals.
6. **Continuity.** Preserve selected items, panel sizes, filters, and reading position where appropriate. Engagement changes must not reuse unrelated selections or results.
7. **Readable density.** Support comfortable and compact spacing. Do not achieve density by shrinking all text or click targets.

## 3. Visual foundation

### Surface and color tokens

Use semantic tokens everywhere. Light mode defaults to the operating system preference; users can explicitly choose light, dark, or system. Theme and density preferences persist locally.

| Token | Dark | Light | Purpose |
| --- | --- | --- | --- |
| `--pd-bg` | `#17191D` | `#F5F6F8` | Workspace background |
| `--pd-sidebar` | `#1D2025` | `#ECEEF2` | Navigation surface |
| `--pd-surface` | `#23272E` | `#FFFFFF` | Controls and grouped surfaces |
| `--pd-elevated` | `#2B3038` | `#FFFFFF` | Popovers and dialogs |
| `--pd-terminal` | `#111318` | `#FFFFFF` | Terminal and recorded output |
| `--pd-text` | `#EEF1F5` | `#202631` | Primary text |
| `--pd-text-muted` | `#AAB3C2` | `#566173` | Metadata, helper text, placeholders |
| `--pd-divider` | `#373E49` | `#D9DEE7` | Nonessential surface separators |
| `--pd-control-border` | `#727D8E` | `#778397` | Essential input boundaries |
| `--pd-hover` | `#2B3038` | `#E3E7EE` | Hover background |
| `--pd-selected` | `#183B46` | `#DCEFF5` | Selected row background |
| `--pd-accent` | `#80CDDD` | `#08687D` | Focus, links, primary actions |
| `--pd-on-accent` | `#102029` | `#FFFFFF` | Text on accent-filled buttons |
| `--pd-success` | `#8BD5AD` | `#22633F` | Confirmed successful state |
| `--pd-warning` | `#E6BD75` | `#805209` | Attention or stale state |
| `--pd-danger` | `#F49B9B` | `#B12D3B` | Failed or destructive state |

Use primary text on selected backgrounds. Use a checkmark, icon, or label as well as color for selection and status. Decorative divider tokens are not adequate substitutes for required input boundaries or focus indicators.

Keep the working canvas opaque. An optional restrained translucent navigation surface must have an opaque fallback and must not change legibility over moving output. Use depth sparingly: popovers and dialogs may cast a subtle shadow; ordinary table rows and command blocks do not float.

Do not add decorative traffic-light window controls in a browser app. Use real window controls only if a native wrapper provides their functionality. Avoid neon glow, scanlines, ornamental terminal text, large gradients, and unnecessary nesting of cards.

### Typography

| Role | Starting size | Weight and treatment |
| --- | --- | --- |
| Main page or selected-item title | 18–20px | 600 |
| Section heading | 14px | 600 |
| Navigation, controls, table content | 14px | 400–500 |
| Secondary metadata | 12–13px | 400, readable contrast |
| Commands and output | 13–14px | Monospace, line height 1.45–1.55 |

Express type in `rem`. Use `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif` for UI text and `ui-monospace, "SFMono-Regular", Menlo, Consolas, "Liberation Mono", monospace` for technical content. Font names are fallbacks, not requests to download Apple fonts. Keep assets local. Reuse an existing locally bundled font when appropriate.

Use tabular numerals for timestamps, durations, counts, and changing measurements. Avoid letter-spacing and all-caps labels for everyday controls. Terminal font size is independent of UI density. Preserve code characters and provide selectable text.

### Spacing, shape, and density

Use a 4px spacing base: `4, 8, 12, 16, 24, 32`. Use 6px control radii, 8px selection/popover radii, and 12px dialog radii. Pills are appropriate for compact status labels, not every control.

| Element | Comfortable | Compact |
| --- | --- | --- |
| Standard control height | 36px | 32px |
| Single-line table row | 40px | 32px |
| Two-line recording row | About 64px | About 52px |
| Toolbar internal vertical padding | 8px | 4px |
| Section gap | 24px | 16px |

These are minimum starting layouts, not fixed heights that clip wrapped text. Keep UI font sizes stable when density changes. Default to comfortable; remember the user's explicit preference. Target areas meet the accessibility requirements below in both modes.

Use brief 120–180ms transitions for disclosure, menus, and panel changes. Respect reduced-motion preferences. Do not animate streaming text or postpone status feedback for visual effects.

## 4. Application shell

Keep labeled navigation visible initially. Start with a 216px sidebar and a 288px recordings pane, then adjust to content and zoom. Both may be resized or collapsed with clear controls. Do not force these dimensions at narrow widths.

Place the engagement selector consistently near navigation and make its scope explicit. Inspect whether Containers and Terminals are global or engagement-specific before grouping them. Preserve existing scope semantics.

Use one context header per view. The header identifies the current page or selected object; its toolbar holds frequent actions. Navigation highlighting, page location, and an object title can coexist when they communicate different levels of context. Remove only redundant headings.

Persist pane sizes and visibility. At narrow effective widths, switch list/detail to one pane at a time with an explicit return action. Let users restore any hidden pane. Never leave a no-selection screen instructing users to choose from an invisible list without a Browse recordings action.

An optional inspector can show infrequent metadata. Do not introduce a permanent fourth column. Its closure restores focus to the invoking control.

## 5. Sessions and recorded output

### Recording list

The primary line identifies the recording; the secondary line adds context. Prefer an existing user name, then a known container or shell label plus start time. Add a short ID when otherwise identical items cannot be distinguished. Never fabricate an inferred session purpose.

Synthetic example:

> Proxy checks
> lab-kali · 25 Oct, 09:14 · Ended

Without a custom name:

> lab-kali · 09:14:32
> 25 Oct · 18 min · ID …a7f2

Row activation opens a recording. Checkboxes select items for batch actions; checking an item must not unexpectedly navigate. The opened row and checked rows need distinct indicators and state. Archive controls show the selected count and make their scope explicit.

### Viewer

Show selected recording name, container, live/ended state, and relevant timing in a compact header. Make full IDs, absolute timestamps, and timezone available in details and copy actions. Recorded history and a live interactive terminal must be visibly distinguishable.

Each execution block contains:

1. Time and working directory as secondary metadata.
2. The full command, preserving multiline structure.
3. Output beneath it on a continuous opaque surface.
4. Exit code and duration when genuinely captured.
5. A small action area for copying command or output and expanding/collapsing long output.

Use space and a subtle divider between executions. Avoid separate heavy cards around every command and every output fragment. Keep copy actions visible or readily discoverable on hover and keyboard focus, with an always-available path on touch devices.

Do not guess command/output boundaries from ambiguous imported transcripts. If parsing is uncertain, expose a raw transcript view. Distinguish “No output” from “Output unavailable” and unknown exit status. Preserve original content independently of display transformations.

Offer wrap versus horizontal scroll for long output. Long content may collapse with an explicit hidden-line count or expansion affordance. Copying output should copy the documented full scope, not silently only the rendered or visible portion. Search must reach collapsed and virtualized content, or clearly disclose its scope.

### Search and live following

Keep search scope visible: this recording versus this engagement's recordings. Clearly disclose archive exclusions. Show result counts, next/previous matches, and a no-results state. Results spanning recordings must identify their recording and command context.

Follow live output only while the user is at the bottom or has explicitly enabled following. Scrolling back pauses following. Provide a visible Resume following control and, where practical, new-output indication. Copying and selecting text must remain stable while data arrives.

Distinguish loading, failure, no recordings, no selection, and no matching results. An error must not look like an empty history.

## 6. Tracker

Keep Tracker as a table. Determine default column order from the actual workflow. A candidate is action/measure, target, outcome, time, source, tester; validate this against existing data and use. Preserve access to all fields and export semantics.

Use a compact table toolbar for search, filters, display preferences, and export. Keep the most frequent action directly available. If Copy TSV is used repeatedly, it may remain a first-level action with download and formatting options in a neighboring menu.

Show a contextual selection bar when rows are checked. Include selected count, clear selection, and relevant batch actions. If bulk editing is frequent, a stable inline editor may be preferable to a popover; do not hide it purely to reduce visual density. Make changes to multiple rows explicit.

If selection and export inclusion are different concepts, label them separately. State export scope explicitly, including whether hidden columns, filters, or selected rows affect the export. Preserve exact timestamp data in export even if the on-screen format is shortened.

Support sticky headers, resizable columns, readable row hover, and horizontal scrolling confined to the table. Consider freezing identifying columns if useful; avoid frozen columns consuming the entire narrow viewport. Show full values through expansion, details, or copy actions rather than inaccessible truncation.

Undo reversible edits where supported. Keep delete separate from routine editing and identify its effect. Do not claim undo for backend operations that cannot be reversed.

## 7. Terminals, containers, and operational context

Live terminals show the active engagement, container, working directory when available, and connection state. Switching UI themes must preserve readable ANSI colors, cursor visibility, selection, and terminal focus. A terminal-specific palette is acceptable if explicit and consistent.

Container rows expose useful identity, image, running/stopped state, and a frequent action such as Open terminal. Put mounts, detailed networking, and less-used configuration in details. Keep stop, restart, and remove distinct; do not make the entire row an operational trigger.

Operational status must be scoped to its source:

| Indicator | Meaning |
| --- | --- |
| Container running | Runtime reports the container is running |
| Terminal connected | Frontend has the relevant terminal connection |
| VPN connected | An available, scoped VPN signal reports connected |
| Target reachable | A defined check reached that target at a displayed time |
| Unknown / stale | No reliable current signal is available |

A running container does not imply VPN connectivity. A VPN does not imply target reachability. Display freshness and source details where they affect interpretation. Do not add automatic network probes solely to decorate the UI; use existing telemetry and explicit checks.

Copying a recorded command never executes it. If an existing rerun feature is present, it requires a distinct action and clear destination context. Render captured output as untrusted text or through a safe terminal renderer; never inject it as unrestricted HTML.

## 8. Keyboard and accessibility requirements

- All actionable controls support keyboard use, accessible names, and visible focus. Keep focus visible and unobscured by sticky toolbars or overlays.
- Use a 2px focus ring with a 2px offset as the initial design. Verify contrast against adjacent surfaces.
- Normal text meets WCAG AA contrast of at least 4.5:1; large text meets 3:1. Essential non-text control indicators meet applicable 3:1 contrast requirements.
- Pointer targets meet 24×24 CSS pixels or WCAG 2.2's permitted spacing/equivalent-control exceptions. Prefer the specified 32–36px controls where practical.
- Do not rely only on hue, hover, or placeholder text to communicate essential meaning.
- Preserve text resizing and reflow. Test at 200% zoom and at an effective 320 CSS-pixel width; genuinely two-dimensional tables and terminal content may retain contained horizontal scrolling.
- Use native semantic tables for ordinary table content. Only introduce an interactive ARIA grid if its complete keyboard model is implemented.
- Dialogs and menus manage focus and restore it after dismissal. Use shortcuts appropriate to the actual OS; the visual style must not force Mac keybindings on Linux.
- A command palette accelerates navigation and existing actions. It supplements visible controls. Choose a shortcut after auditing browser, OS, and terminal conflicts.
- Do not capture app shortcuts while the terminal owns them. Escape closes the relevant app overlay first; otherwise preserve terminal behavior.
- Persist theme, density, terminal font size, and panel preferences locally. Store only layout preferences there, not credentials or captured command contents.

## 9. Implementation sequence

1. Inspect existing routes, components, styles, terminal integration, data contracts, and tests. Inventory current actions and their scopes.
2. Define shared semantic tokens and reusable control styles in the current styling system.
3. Finish a realistic Sessions slice first: list, opened recording, search match, long output, empty/error states, and an open menu. Use synthetic data for screenshots and fixtures.
4. Evaluate it at comfortable/compact density, light/dark themes, and multiple effective widths. Correct interaction problems before propagating styles.
5. Apply the same foundations to Tracker, Containers, and Terminals. Preserve distinctive workflows.
6. Run relevant checks, verify representative tasks, and document remaining constraints.

Do not introduce a framework migration, external assets, telemetry, cloud dependencies, backend redesign, or replacement of the terminal engine for cosmetic reasons. Extend functionality only when required for this scope and supported by the existing application. If a desired feature needs unavailable data, report that limitation instead of simulating it.

## 10. Acceptance checklist

### Visual and responsive

- [ ] A populated Sessions screen has clear context, recognizable list items, readable commands, and quiet metadata.
- [ ] Light and dark themes use the same semantic hierarchy; all interaction states have been checked.
- [ ] Comfortable and compact modes preserve text readability and usable targets.
- [ ] At 1280px and 1440px CSS viewport widths, controls remain accessible and no application-wide horizontal overflow appears.
- [ ] At 150% and 200% zoom, navigation and actions remain usable; reflow is checked at 320 CSS pixels.
- [ ] Long paths, multiline commands, narrow panes, and large output do not overlap controls or lose access to information.

### Workflow and correctness

- [ ] Find a previous recording, locate a command, and copy its complete output without losing position.
- [ ] The collapsed-list empty state offers a direct route to recordings.
- [ ] Viewing, checking, and export inclusion behave independently where the data model requires it.
- [ ] Selecting rows and applying a bulk change affects only the intended rows.
- [ ] Export scope and output values remain correct after sorting, filtering, and column customization.
- [ ] Opening a terminal reaches the intended container; terminal shortcuts, selection, resizing, and focus still work.
- [ ] Scrollback is not pulled away while reading; live following resumes explicitly.
- [ ] Missing or stale telemetry never appears as a confirmed connection or successful result.
- [ ] Theme and density changes do not interrupt sessions or execute commands.

### Engineering verification

- [ ] Existing build, type, lint, and relevant tests pass where available.
- [ ] Add targeted regression tests for changed behavior, particularly selection, copying, scope, and focus.
- [ ] Measure long-history performance using a representative fixture; disclose the tested size and any limit.
- [ ] Verify keyboard navigation and computed contrast, including hover, selected, focus, and error states.
- [ ] Compare representative task completion, mis-selections, and backtracking with the previous UI where a baseline is available.
- [ ] Report checks actually performed and explicitly identify unverified browser behavior.

## 11. Reference basis

The exact palette, spacing, dimensions, and component composition in this guide are pom dock product choices. The following sources support the broader patterns and accessibility requirements. Links reviewed on 25 October 2026.

- [Apple — Designing for macOS](https://developer.apple.com/design/human-interface-guidelines/designing-for-macos): comfortable density, keyboard support, customization, and desktop workflows.
- [Apple — Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars): discoverable navigation, labels, and show/hide behavior.
- [Apple — Toolbars](https://developer.apple.com/design/human-interface-guidelines/toolbars): frequent actions, logical grouping, and customization.
- [Apple — Get to know the new design system](https://developer.apple.com/videos/play/wwdc2025/356/): separation and legibility when using materials.
- [PatternFly — Primary-detail](https://www.patternfly.org/patterns/primary-detail/design-guidelines/): list/detail layouts, selection distinctions, and adaptable panes.
- [PatternFly — Foundations](https://www.patternfly.org/foundations-and-styles/overview/): consistent tokens, typography, spacing, and theming.
- [IBM Carbon — Data table](https://www.carbondesignsystem.com/building-blocks/core/components/data-table/guidelines): table utilities, selection, and batch actions.
- [Cloudscape — Content density](https://cloudscape.design/foundation/visual-foundation/content-density/): comfortable and compact modes with consistent spacing.
- [Cloudscape — Table view](https://cloudscape.design/patterns/resource-management/view/table-view/): table structure and user-relevant column priorities.
- [Nielsen Norman Group — Recognition and recall](https://www.nngroup.com/articles/recognition-and-recall/): recognizable context and visible interaction cues.
- [Nielsen Norman Group — Progressive disclosure](https://www.nngroup.com/articles/progressive-disclosure/): keeping important options accessible while deferring occasional complexity.
- [WCAG 2.2 — Contrast minimum](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html), [non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html), [target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html), and [reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html): measurable accessibility checks.
