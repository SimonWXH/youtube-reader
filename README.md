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
- Adaptive video grid with 16:9 remote thumbnails and avatars, two-line titles, unread dots, relative time and Shorts indicators. Channel names on video cards link to the channel page on YouTube. The maximum number of columns per row defaults to 5 and can be set from 2 to 8 in Settings; the effective count also adapts to pane width and no card is wider than 280 pt. Shorts are hidden by default and can be enabled in Settings. Sizes and styling are described in [Visual design](#visual-design).
- Right-side detail drawer that appears only when a video is selected, with Open in YouTube and Favorite/Unfavorite. Opening a video in YouTube marks it as read; selecting a card, scrolling, filtering and searching do not.
- Resizable left navigation, center grid and right detail panes. Drag the dividers to change their widths; the chosen widths persist in local settings. The sidebar toggles from the toolbar or the View menu. The sidebar has Library (All videos, Favorites), OPML group sections with channels, an all-channels section, and a pinned collapsible status item at the bottom. Group sections start collapsed and each group's fold state persists in local settings across restarts. All videos, Favorites, group and channel rows show their video counts; Up and Down move the selection, Return opens the selected row, and every row paints a focus ring when focused from the keyboard. Channel filter, keyword search and the Unwatched filter apply within the selected group.
- Native menu bar that mirrors the toolbar: the App menu carries Settings… at Command-Comma, Command-R refreshes, the View menu toggles the sidebar, and commands that cannot run are dimmed rather than hidden.
- Refresh intervals: 15m/30m/1h (default)/2h/4h. Startup refresh when the last successful refresh is older than the interval. Retain at most 30 hours or 400 videos, protecting unread and favorited videos.
- Offline/failure UX: the last successful data stays visible, with a two-second toast on failure transitions, a "Refresh issue" state in the overflow menu and details in the pinned status item; no top error banner. Quota exhaustion pauses refreshes for 24 hours.
- Single-file Syncthing sync: one JSON file (`~/Library/Application Support/YouTubeReader/youtube-reader-sync.json` by default, configurable in Settings) holds groups/channel membership, read states (any device read wins) and favorites with optional metadata. Writes are read-before-write, idempotent by stable id, and use a dot-prefixed temp file, fsync, atomic rename and directory fsync. `*.sync-conflict-*` copies are merged on load and app-generated copies are removed. A local flock lives outside the Syncthing folder; Settings has "Force take over sync". SQLite, API key, notification preferences, local UI preferences (pane widths and group fold state), locks, queues and logs are never synced.
- Favorites backup: Settings has a configurable full backup path and writes a standalone favorites-only JSON (no API key, read history, video payload or synced state). Export writes the path atomically; Restore is an independent entry that reads a chosen favorites JSON and merges by stable video ID, with newer local favorites winning.
- Local SQLite store (`videos.sqlite`) uses WAL and a five-second busy timeout. Each refresh commits channels, videos and revision in one transaction; notifications post only after commit.
- Local system notifications: per-channel toggles default OFF; one-click enable-all for the selected group (All enables all channels). One notification per opted-in channel per refresh with new-video count and newest title. Clicking a notification opens that channel's filtered view. MyGo IDs route app clicks, including a click that launches the app; a private local queue records only helper notifications that were actually posted and is consumed only on an actual notification-click callback. Notification preferences and click records are local only.
- Background refresh: the standalone helper can be registered as a user LaunchAgent (RunAtLoad + StartInterval) from Settings. It performs the full YouTube Data API refresh, commits results in SQLite, posts notifications, and writes an atomic JSON handoff consumed by the app on next launch. The app holds the local flock while open; helper shared-JSON writes are queued in SQLite and drained when the app exits.

## Visual design

The window frame is the app's own: a hidden-inset title bar leaves the traffic lights inside a 52 pt toolbar with a sidebar toggle, a centred 13px Semibold title, and borderless 28 pt refresh and overflow controls on the right; below it, a 44 pt filter strip holds the 160 pt channel menu, the 200 pt search field, the Unwatched check box and a right-aligned 11px tabular count, tightening at narrow widths. The three panes share one token vocabulary in `ui/theme.go` and `ui/typography.go`: warm neutral surfaces (`#f7f7f5` / `#ffffff` light, `#191a1c` / `#25272b` dark), a single blue accent, and re-derived dark colors with small elevation steps instead of inverted light values. Spacing follows a single 4 pt ladder (4/8/12/16/20/24/32, extended to 40 and 48). The sidebar is 260px wide by default and resizable with the other panes between 220px and 320px, remembering its width; navigation rows are 28px with 2px gaps, sidebar glyphs use the accent, and avatars are 18px in both the sidebar and video cards. The type vocabulary is 13px/18px body text, 13px/18px Semibold card titles, 13px/18px Semibold Muted sidebar section headers, and 11px/14px Medium tabular counts; call sites still on pre-migration sizes, such as the 20px drawer title, move to these roles with their zone. Controls expose default, hover, focus, active, disabled, loading, error and success faces, with a visible focus outline and a reduced-motion fallback. Motion uses three timed transitions through `cubic-bezier(0.22, 1, 0.36, 1)`: 120ms for interaction colour, 180ms for popovers, and 240ms for the detail pane's width; there is no `transition: all`, animated height/top/left, tracked caps, or gradient chrome. Video cards use 16:9 remote thumbnails that fall back to a real placeholder, with two-line titles and an 11 px Medium footer with tabular figures showing the unread dot, channel name and relative time; card height is the width times 9/16 plus 76 pt. Monogram avatars stand in when a channel has no avatar URL. A selected card draws a 1 pt accent stroke with a 2 pt 24% accent halo and sets its title in the accent colour; keyboard focus adds a separate 2 pt focus ring. The detail drawer repeats the same tokens and actions so the center grid and right pane read as one surface.

## Known limitations

- Click routing depends on macOS delivering a notification click and on notification permission being granted. A helper's AppleScript notification may not deliver a cold-launch click ID; the local queue records only actual posts, is consumed only on a real click callback, and an ordinary dock or Finder launch never routes to it. Stable MyGo IDs route app clicks, including a cold launch where delivered. Physical cold-click verification for helper notifications remains manual.
- Notification permission must be granted in macOS System Settings the first time a notification is posted.
- The helper implementation has local vet/tests and app-level CI validation; LaunchAgent registration and live notification delivery still need manual verification on the captain's Mac. The LaunchAgent is installed per user and uses absolute paths resolved from the app bundle.

## Verification

```sh
make test
make build
make dmg
hdiutil verify "$(make --no-print-directory dmg-path)"
codesign -dv --verbose=4 "dist/darwin-arm64/YouTube Reader.app"
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
