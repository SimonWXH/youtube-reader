# Build / MyGo

YouTube Reader is a private macOS app. It is built with Go 1.27+ and MyGo v0.10.0 native UI (`github.com/egoist/mygo/ui`). There is no frontend, webview, HTML or JavaScript in the app.

## Requirements

- Go 1.27+ (the task toolchain is at `/Volumes/ZHITAI_1TB/Projects/firstmate/data/youtube-reader-mygo-spike-1/toolchain/go/bin/go`)
- macOS arm64 for the signed `.app` and DMG build
- A YouTube Data API v3 key entered in Settings (local only, never bundled or synced)

## Develop and build

```sh
go tool mygo dev
go tool mygo build -platform darwin/arm64 -skip-notarize
```

The build produces `dist/darwin-arm64/YouTube Reader.app` and `dist/darwin-arm64/youtube-reader-1.0.0-arm64.dmg`. It is ad-hoc signed when no Developer ID is configured; notarization is skipped in this MVP.

The standalone background helper is built into `Contents/Resources/ytreader-helper` by `Scripts/build-helper.sh`. Run that script after changing `helper/`, then build the app.

## Continuous integration and releases

`.github/workflows/ci.yml` is the pull-request gate: it builds, vets, tests and builds the app and DMG on a macOS arm64 runner using the Go version from `go.mod`. Pull requests that change only Markdown files still report the required `verify` check, but skip the build and tests. There is no `push` trigger; after a direct push to `main` or a merge whose tree differs from the tested pull-request head, start CI manually with `workflow_dispatch`.

`.github/workflows/release.yml` runs on pushed `v*` tags. The tag must be exactly `v` plus the `version` in `mygo.json` (for example `v1.0.0` for version `1.0.0`), so prerelease-style tags are not supported while `mygo.json` holds a stable version. Releases follow copyqmac's deliberate prerelease-only policy: the workflow attaches the arm64 DMG and release notes from `.github/release-notes.md` (falling back to GitHub-generated notes only when that file is absent) to a GitHub prerelease. There is no stable-release path. Release builds use the same `go tool mygo build` command described above.

## Features

- OPML import preserving group hierarchy, duplicate channel memberships and channel merge by ID. Imports never delete existing channels. Multiple OPML files can be selected.
- YouTube Data API v3 only: `channels.list` (batches of 50), `playlistItems.list` (three pages maximum, stop at 30 days), and `videos.list` for missing metadata and Shorts detection. No RSS, no `search.list`, no server.
- Adaptive one-to-five column video grid with remote thumbnails/avatars, relative time, two-line descriptions, unread badges and Shorts badges. Shorts are hidden by default and can be enabled in Settings.
- Right-side detail drawer with Open in YouTube and Favorite/Unfavorite. Opening a detail and opening externally mark a video as read; scrolling, filtering and searching do not.
- Fixed left navigation: All, Favorites, OPML groups with channels, and a pinned collapsible status item at the bottom. Channel filter, keyword search and unread-only toggle filter within the selected group.
- Refresh intervals: 15m/30m/1h (default)/2h/4h. Startup refresh when the last successful refresh is older than the interval. Retain at most 30 hours or 400 videos, protecting unread and favorited videos.
- Offline/failure UX: the last successful data stays visible, with a two-second toast on failure transitions and a corner status icon; no top error banner. Quota exhaustion pauses refreshes for 24 hours.
- Single-file Syncthing sync: one JSON file (`~/Library/Application Support/YouTubeReader/youtube-reader-sync.json` by default, configurable in Settings) holds groups/channel membership, read states (any device read wins) and favorites with optional metadata. Writes are read-before-write, idempotent by stable id, and use a dot-prefixed temp file, fsync, atomic rename and directory fsync. `*.sync-conflict-*` copies are merged on load and app-generated copies are removed. A local flock lives outside the Syncthing folder; Settings has "Force take over sync". SQLite, API key, notification preferences, locks, queues and logs are never synced.
- Favorites can be exported/imported as a separate JSON file with no API key or read history.
- Local SQLite store (`videos.sqlite`) uses WAL and a five-second busy timeout. Each refresh commits channels, videos and revision in one transaction; notifications post only after commit.
- Local system notifications: per-channel toggles default OFF; one-click enable-all for the selected group (All enables all channels). One notification per opted-in channel per refresh with new-video count and newest title. Notification preferences are local only.
- Background refresh: the standalone helper can be registered as a user LaunchAgent (RunAtLoad + StartInterval) from Settings. It writes an atomic JSON handoff consumed by the app on next launch. The app holds the local flock while open; helper shared-JSON writes are queued in SQLite and drained when the app exits.

## Visual design

The three panes share one token set in `ui/theme.go`: warm neutral surfaces (`#f7f7f5` / `#ffffff` light, `#191a1c` / `#25272b` dark), a single blue accent, and re-derived dark colors with small elevation steps instead of inverted light values. Spacing uses the 4/8/12/16/20/24/32 scale. The sidebar is 256px wide with 34px navigation rows and 2px gaps; avatars are 22px in the sidebar and 18px in video cards. Body text is 14-15px with fixed 20-22px line heights, pane titles are 28px, and card titles are 14px/20px. Controls expose default, hover, focus, active, disabled, loading, error and success faces, with a visible focus outline and a reduced-motion fallback. Motion is limited to a 180ms color transition through `cubic-bezier(0.22, 1, 0.36, 1)`; there is no `transition: all`, animated width/height/top/left, tracked caps, or gradient chrome. Video cards use remote thumbnails, two-line descriptions, unread dots, relative time and monogram avatars when a channel image is unavailable. The detail drawer repeats the same tokens and actions so the center grid and right pane read as one surface.

## Known limitations

- Clicking a notification to launch a fully quit app and deep-link into a channel is not supported in this MVP. Notifications work while the app is running; cold-launch click routing will be handled later.
- Notification permission must be granted in macOS System Settings the first time a notification is posted.
- The helper performs metadata checks and handoff; full background refresh body can be refined in the next iteration. The LaunchAgent is installed per user and uses absolute paths resolved from the app bundle.

## Verification

```sh
go vet ./...
go test ./...
go tool mygo build -platform darwin/arm64 -skip-notarize
hdiutil verify dist/darwin-arm64/youtube-reader-1.0.0-arm64.dmg
codesign -dv "dist/darwin-arm64/YouTube Reader.app"
```

MyGo automation can drive the built app for UI verification:

```sh
go build -tags e2e -o /tmp/ytreader-e2e-app .
MYGO_AUTOMATION=/tmp/ytreader-e2e.sock /tmp/ytreader-e2e-app
mygo-automation open --socket /tmp/ytreader-e2e.sock
mygo-automation snapshot -i
mygo-automation screenshot /tmp/ytreader.png
```
