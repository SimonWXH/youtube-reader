# Build / MyGo

YouTube Reader is a private macOS app. It is built with Go 1.27+ and MyGo v0.10.0 native UI (`github.com/egoist/mygo/ui`). There is no frontend, webview, HTML or JavaScript in the app.

## Requirements

- Go 1.27+ (the task toolchain is at `/Volumes/ZHITAI_1TB/Projects/firstmate/data/youtube-reader-mygo-spike-1/toolchain/go/bin/go`)
- macOS arm64 for the signed `.app` and DMG build
- A YouTube Data API v3 key entered in Settings (local only, never bundled or synced)

## Develop and build

```sh
go tool mygo dev
make test
make build
make dmg
make dmg-path
```

`make build` runs `Scripts/build-helper.sh` to build the standalone helper into `resources/darwin`, compiles the app, and signs `dist/darwin-arm64/YouTube Reader.app` ad hoc with `codesign --force --sign -`. `make dmg` packages that signed bundle and an `/Applications` symlink into a compressed UDZO disk image with `hdiutil`, and `make dmg-path` prints the image path. The build is ad hoc signed and not notarized; there is no Developer ID, Team ID, or stapler step.

If `go` is not on `PATH`, point `GO` at the toolchain when running Make:

```sh
make GO=/Volumes/ZHITAI_1TB/Projects/firstmate/data/youtube-reader-mygo-spike-1/toolchain/go/bin/go build
```

## Installation

1. Open the disk image and drag **YouTube Reader** into `/Applications`.
2. On first launch, macOS will block the unnotarized app. Allow it through **System Settings** -> **Privacy & Security** -> **Open Anyway**, or remove the quarantine attribute from the installed bundle with `xattr -d com.apple.quarantine "/Applications/YouTube Reader.app"`.

See [`.github/release-notes.md`](.github/release-notes.md) for the complete first-launch instructions.

## Continuous integration and releases

`.github/workflows/ci.yml` is the pull-request gate: it vets, tests, builds the app and DMG with `make dmg`, and verifies the image at `make dmg-path` on a macOS arm64 runner using the Go version from `go.mod`. Pull requests that change only Markdown files still report the required `verify` check, but skip the build and tests. There is no `push` trigger; after a direct push to `main` or a merge whose tree differs from the tested pull-request head, start CI manually with `workflow_dispatch`.

`.github/workflows/release.yml` runs on pushed `v*` tags. The tag must be exactly `v` plus the `version` in `mygo.json` (for example `v1.0.0` for version `1.0.0`), so prerelease-style tags are not supported while `mygo.json` holds a stable version. Releases follow copyqmac's deliberate prerelease-only policy: the workflow runs `make dmg`, attaches the DMG path reported by `make dmg-path`, and attaches release notes from `.github/release-notes.md` (falling back to GitHub-generated notes only when that file is absent) to a GitHub prerelease. There is no stable-release path. CI runs `make dmg` and verifies the image at `make dmg-path`, so pull requests and releases use the same ad-hoc signed UDZO artifact.

## Features

- OPML import preserving group hierarchy, duplicate channel memberships and channel merge by ID. Imports never delete existing channels. Multiple OPML files can be selected.
- YouTube Data API v3 only: `channels.list` (batches of 50), `playlistItems.list` (three pages maximum, stop at 30 days), and `videos.list` for missing metadata and Shorts detection. No RSS, no `search.list`, no server.
- Video grid that fills the center pane, with 16:9 remote thumbnails and avatars, two-line titles, unread dots, relative time and Shorts indicators. Channel names on video cards link to the channel page on YouTube. The column count defaults to 5 and can be set from 2 to 8 in Settings; the cards divide the pane's width after its padding and scrollbar gutter, so the grid fills the pane at every window width. Shorts are hidden by default and can be enabled in Settings. Sizes and styling are described in [Visual design](#visual-design).
- Right-side detail drawer that appears only when a video is selected, with Open on YouTube…, Copy link and Favorite/Unfavorite. It slides in from the right over 240 ms and gives the width back to the grid on close; Escape with the focus in the pane closes it and returns the focus to the grid. Opening a video in YouTube marks it as read; selecting a card, scrolling, filtering and searching do not.
- Resizable left navigation, center grid and right detail panes. Drag the dividers to change their widths; the chosen widths persist in local settings. The sidebar toggles from the toolbar or the View menu. The sidebar has Library (All videos, Favorites), the OPML group sections with their channels, and an Ungrouped section for channels in no group (未分组 when the system locale is Chinese); a channel in a group appears only there, and every channel row shows the channel's avatar. Group sections start collapsed and each group's fold state persists in local settings across restarts. Groups reorder by dragging a group row: the list takes the new order as the pointer crosses each block, giving the drag up with Escape puts the groups back, and the order persists in local settings. A fresh import starts the groups in the OPML file's order, a later import appends the groups it adds for the first time in its own file order, and groups that arrive without an import, as from another device sync, join the end alphabetically. All videos, Favorites, group and channel rows show their video counts; Up and Down move the selection, Return opens the selected row, and every row paints a focus ring when focused from the keyboard. Channel filter, keyword search and the Unwatched check box apply within the selected group.
- Native menu bar that mirrors the toolbar: the App menu carries Settings… at Command-Comma, Command-R refreshes, the View menu toggles the sidebar, and commands that cannot run are dimmed rather than hidden.
- Refresh intervals: Manual, 5, 15 (default), 30 or 60 minutes. Manual never refreshes on its own; the toolbar's refresh control and the Refresh command still do. Automatic refresh is owned by the optional background helper below. An interval stored by an older build stays visible and selectable. Retain at most 30 hours or 400 videos, protecting unread and favorited videos.
- Offline/failure UX: the last successful data stays visible. For a refresh with content on screen, refresh state lives in one menu bar item and its popover (see [Visual design](#visual-design)); the window repeats none of it, there is no top error banner, and a refresh posts no toast. Offline also disables the window's refresh control with a "You are offline" tooltip, and an open content pane with no cached video names the offline state instead of an empty library. Quota exhaustion pauses refreshes for 24 hours and raises one alert when the pause begins, then shows the Paused state with its resume time in the popover, including a pause already in force at launch (unless no API key is set, where setup owns the panel).
- Single-file Syncthing sync: one JSON file (`~/Library/Application Support/YouTubeReader/youtube-reader-sync.json` by default, configurable in Settings) holds groups/channel membership, read states (any device read wins) and favorites with optional metadata. Writes are read-before-write, idempotent by stable id, and use a dot-prefixed temp file, fsync, atomic rename and directory fsync. `*.sync-conflict-*` copies are merged on load and app-generated copies are removed. A local flock lives outside the Syncthing folder; Settings has "Force take over sync". SQLite, API key, notification preferences, local UI preferences (pane widths, group fold state, group order and the Settings pane last shown), locks, queues and logs are never synced.
- Settings is a separate 520 x 460 window with General, Backups and Notifications panes, opened from the App menu's Settings… (Command-Comma). The window title follows the active pane, the last pane reopens, edits save as they are made, and Escape closes it. The General pane holds the API key (masked, with a reveal toggle and validation on blur), the refresh interval, Shorts, the grid's column count, the background helper and OPML import; Backups holds the favorites backup path with export, restore and the last export time, the shared sync file, and Erase local favorites behind a system confirmation; Notifications holds per-channel consent and, when macOS refused notifications, a button that opens the relevant System Settings pane.
- Favorites backup: Settings has a configurable full backup path and writes a standalone favorites-only JSON (no API key, read history, video payload or synced state). Export writes the path atomically; Restore is an independent entry that reads a chosen favorites JSON and merges by stable video ID, with newer local favorites winning.
- Local SQLite store (`videos.sqlite`) uses WAL and a five-second busy timeout. Each refresh commits channels, videos and revision in one transaction; notifications post only after commit.
- Local system notifications: per-channel toggles default OFF; Settings' Notifications pane lists every subscription in the sync state and one Enable All turns them all on, whichever view the main window shows. One notification per opted-in channel per refresh with new-video count and newest title. A video counts as new only while the local store has never held it, no earlier refresh announced it and it has not been read; announced videos are remembered for longer than the 30-day uploads window, so the 30-hour/400-video retention limit cannot make the same video alert again an hour later. Clicking a notification opens that channel's filtered view. MyGo IDs route app clicks, including a click that launches the app. Notification preferences and announced-video records are local only.
- Background refresh: the standalone helper can be registered as a user LaunchAgent (RunAtLoad + StartInterval) from Settings, and its interval follows the refresh interval chosen there; Manual installs no helper, and choosing Manual removes an existing login item. A stale helper started under Manual performs no refresh. While the app is running, the helper hands its notifications to it over a local Unix socket in the data directory (`notifications.sock`), so macOS attributes them to YouTube Reader and a click activates the app on that channel's view; the app reads the refresh that hand-off committed, so the view already shows the announced videos. With the app closed, or when the app could not open its socket, the plans go out as plain AppleScript notifications, which never target the app bundle and so cannot pull the main window up. It performs the full YouTube Data API refresh, commits results in SQLite, posts notifications, and writes an atomic JSON handoff consumed by the app on next launch. The app holds the local flock while open; helper shared-JSON writes are queued in SQLite and drained on the app's next launch.

## Visual design

The main window frame is the app's own: a hidden-inset title bar leaves the traffic lights inside a 52 pt toolbar with a sidebar toggle, a centred 13px Semibold title, and borderless 28 pt refresh and overflow controls on the right; below it, a 44 pt filter strip holds the 160 pt channel menu, the 200 pt search field, the Unwatched check box and a right-aligned 11px tabular count, tightening at narrow widths. The standalone Settings window uses the same toolbar title style in its own 52 pt bar, with a 40 pt pane strip below it and a scrollable form body of a 140 pt label column beside the controls. The three main-window panes share one token vocabulary in `ui/theme.go` and `ui/typography.go`: warm neutral surfaces (`#f7f7f5` / `#ffffff` light, `#191a1c` / `#25272b` dark), a single blue accent, and re-derived dark colors with small elevation steps instead of inverted light values. Spacing follows a single 4 pt ladder (4/8/12/16/20/24/32, extended to 40 and 48). The sidebar is 260px wide by default and resizable with the other panes between 220px and 320px, remembering its width; the detail pane is 340px by default and resizes from 280 to 480px. Navigation and group fold rows are 28px with 2px gaps, channel rows are 34px with 24px avatars, sidebar glyphs use the accent, and video cards keep 18px channel avatars. Each scrolling pane keeps an extra 20 pt below its last row so it clears the window edge, and the grid reserves a 20 pt trailing gutter so its scrollbar sits beside the detail divider with an 8 pt gap from the last card. The type vocabulary is 13px/18px body text, 13px/18px Semibold card titles, 13px/18px Semibold Muted sidebar section headers, and 11px/14px Medium tabular counts; empty panes use the 15px/20px Semibold title, the 13px/18px body sentence, a 32 pt SF Symbol and a 28 pt primary action, all from those roles. Controls expose default, hover, focus, active, disabled, loading, error and success faces, with a visible focus outline and a reduced-motion fallback. Motion uses three timed transitions through `cubic-bezier(0.22, 1, 0.36, 1)`: 120ms for interaction colour, 180ms for popovers, and 240ms for the detail pane's width, which switches instantly when Reduce Motion is on; there is no `transition: all`, animated height/top/left, tracked caps, or gradient chrome. Video cards use 16:9 remote thumbnails that fall back to a real placeholder, with two-line titles and an 11 px Medium footer with tabular figures showing the unread dot, channel name and relative time; card height is the width times 9/16 plus 76 pt. Monogram avatars stand in when a channel has no avatar URL, while its picture loads, or if the picture fails. A selected card draws a 1 pt accent stroke with a 2 pt 24% accent halo and sets its title in the accent colour; keyboard focus adds a separate 2 pt focus ring. The detail drawer repeats the same tokens and actions so the center grid and right pane read as one surface.

Every content-pane state is part of the shipping view, not a mockup. First launch is a 32 pt SF Symbol over a 15 pt Semibold title, one 13 pt secondary sentence and one 28 pt primary action, with variants for a missing API key, no imported channels, an empty destination, an empty channel, no favorites and filters that match nothing; no variant draws an illustration or sample cards. With no channel imported, the sidebar says "No channels yet" beside a 28 pt "Add channel…" action, and the detail pane stays absent until a video is selected. A refresh with nothing cached draws a skeleton of 12 neutral cards in the real grid's card geometry, with no animation and no spinner of its own; the toolbar's refresh control holds the one unlabeled spinner and the strip above the grid carries Cancel. A refresh with content on screen keeps the content and reports itself in the toolbar control and the status popover, never by reloading the pane. A failed refresh with nothing cached draws the error inline: what failed, what to check, the recorded reason with the API key redacted, and a Retry button, as a live region rather than an alert dialog; only the quota pause raises an alert. Offline keeps cached content, disables the refresh control with the "You are offline" tooltip and draws the offline state in a pane with nothing cached, while the status popover keeps its Retry. A thumbnail that cannot load stays a neutral 16:9 frame with a play glyph and a tooltip, never a broken-image icon. Refresh failures never post notifications, and relative copy is rebuilt by the status surface's 30 second clock. Refresh status is a menu bar item and one popover, and with content on screen it is not part of any window. The item is an 18 pt template image - `checkmark.circle`, `arrow.triangle.2.circlepath`, `pause.circle`, `wifi.slash`, `exclamationmark.triangle`, `key.fill` or `arrow.clockwise` - so its shape says the state and macOS tints it for the menu bar and for both appearances; text joins the icon ("Set up", "Not refreshed", "Paused", "Offline", "Refresh failed") only while the reader needs attention, and the hover text repeats the popover's own copy. MyGo's `TrayOptions.Icon` takes PNG bytes plus `IconIsTemplate` and has no SF Symbol API, so the seven shapes are committed 36 px (18 pt at 2x) template PNGs in `assets/status`, drawn from SF Symbols by `Scripts/status-icons.swift` and embedded in the binary. The popover is 320 pt wide with automatic height, capped at 420 pt where its rows scroll; it is a focusable flyout with the system window shadow, a 1 pt `Border` hairline, a 12 pt radius drawn from `RadiusWindow`, and a 12 pt diamond arrow whose centre stays on the item as AppKit slides the panel back into the work area. Inside, the heading is 13 pt Semibold with a 12 pt body line and the rows are 12 pt with tabular values, all on the tokens and the type roles (`PopoverTitle`, `PopoverBody`). There is no close button: an outside click or Escape closes it, one panel exists at a time, and nothing opens over it. A quota pause raises a native alert when it begins while the app is running; a pause already in force at launch opens as the Paused state with its resume time instead of a second alert, except when no API key is set, where setup owns the panel. Copy is fixed per state: "Set up YouTube Reader" with Open Settings… while the API key is missing, "Not refreshed yet" with Refresh now, "Up to date" with "Last refresh 2 min ago", "Refreshing", "Refresh paused" with when refreshes resume, "Offline" with "Showing cached videos" and Retry, and a failure that quotes the recorded message with Retry or Open Settings…, except a rejected key, which says the repair instead. A quota pause disables every refresh action - the popover button, the window toolbar, the overflow item, View > Refresh and the empty grid's Refresh now - and shows when refreshes resume in the popover; a failed message is stored and displayed with the API key redacted. Relative times come from one function in `status.go`, and the popover is rebuilt every 30 seconds so they stay true.

## Known limitations

- Click routing depends on macOS delivering a notification click and on notification permission being granted. A notification the app posts itself, whether from its own refresh or from the helper's hand-off, carries its channel's ID and routes a click to that channel, including on a cold launch where macOS delivers it. A refresh that finds the app closed posts plain AppleScript notifications, which macOS attributes to Script Editor and which carry no click ID: clicking one opens Script Editor rather than the reader, because macOS cannot attribute a notification to an app it has not launched, and pulling the window up is exactly what a background refresh must not do. Physical cold-click verification for helper notifications remains manual.
- Residual hand-off race: if the main app exits in the millisecond gap after the helper hands a notification over and before the app shows it, that notification is not shown. The helper never waits on the display, so a background refresh is never blocked by the app.
- Notification permission must be granted in macOS System Settings the first time a notification is posted. When macOS refuses one, the Notifications pane says so and offers Open System Settings…; the refusal is remembered in local settings.
- The helper implementation has local vet/tests and app-level CI validation; LaunchAgent registration and live notification delivery still need manual verification on the captain's Mac. The LaunchAgent is installed per user and uses absolute paths resolved from the app bundle.

## Verification

```sh
make test
make build
make dmg
hdiutil verify "$(make --no-print-directory dmg-path)"
codesign -dv --verbose=4 "dist/darwin-arm64/YouTube Reader.app"
```

The menu bar item's template images are drawn from SF Symbols at 18 pt, and the seven popover states in both appearances are rendered from the shipping view into PNGs. Re-run either after touching the status surface:

```sh
swift Scripts/status-icons.swift
MYGO_STATUS_SCREENSHOTS=/tmp/status-shots go test -run TestStatusScreenshots .
```

To confirm the disk image contains the install shortcut, attach it and check that `Applications` is a symlink to `/Applications`:

```sh
DMG=$(make --no-print-directory dmg-path)
MNT=$(mktemp -d)
hdiutil attach -readonly -nobrowse -mountpoint "$MNT" "$DMG"
ls -l "$MNT/Applications"
hdiutil detach "$MNT"
rmdir "$MNT"
```

MyGo automation can drive the built app for UI verification:

```sh
go build -tags e2e -o /tmp/ytreader-e2e-app .
MYGO_AUTOMATION=/tmp/ytreader-e2e.sock /tmp/ytreader-e2e-app
mygo-automation open --socket /tmp/ytreader-e2e.sock
mygo-automation snapshot -i
mygo-automation screenshot /tmp/ytreader.png
```

The `e2e` build also honours `YTREADER_APPEARANCE=light` or `dark` so both appearances can be captured on a machine whose system appearance is one of them. The override lives only in the build-tagged `e2e.go`; release builds never compile it.
