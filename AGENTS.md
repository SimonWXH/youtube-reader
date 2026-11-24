# AGENTS.md

The rules below are settled decisions for this repository, not suggestions. Follow them; do not re-derive or contradict them.

## Overview and architecture

- YouTube Reader is an open-source, two-platform YouTube subscription reader for daily personal use: a native macOS client in Go/MyGo plus a native Android client.
- Core model: local-first, YouTube Data API v3 integration, channel avatars, standard long-form videos (Shorts filtered out), with optional RSS aggregation.
- All work stays inside the public repository `SimonWXH/youtube-reader`.
- Dual-platform by default: every feature is expected on both macOS and Android, with synced data (read state, favorites, and later notes and annotations). Shipping on one platform first is fine when that is the stated plan, but its data model and interactions must not block the other platform or the sync model later.

## Quality bar and design principles

- Commercial-grade bar: treat this as commercial-grade software despite personal use, holding UI/UX standards above comparable utility apps, and make the owner's near-obsessive standard the acceptance bar - every detail must be right, not merely functional.
- Pixel perfection across the shipping UI: spacing, alignment, borders, typography, dark mode, window resizing, button styles, the status popover, the detail pane, the settings screen, the top-right refresh and settings controls, and every control state - hover, pressed, focused, active, disabled, loading, empty, error.
- Bilingual interfaces: English and 简体中文, with an in-app language switch in settings, defaulting to English.

## Build, test, and package (macOS)

- Built with the MyGo toolchain and macOS Command Line Tools; there is no Xcode project.
- `make build`: compiles the helper into `resources/darwin`, builds the app with `mygo build`, and ad-hoc signs `dist/darwin-arm64/YouTube Reader.app`.
- `make test`: `go vet ./...` and `go test ./...`.
- `make dmg`: packages the signed bundle and an `/Applications` symlink into a compressed disk image.
- `make run`: builds the app bundle and launches it via `open`.
- `make clean`: removes `dist/` and the staged helper binary.

## Local app delivery

- A merged pull request is not delivery: this project ships as a locally built Mac app. After landing changes:
  1. Quit the running app cleanly (`kill -TERM <pid>` if unresponsive).
  2. Run `make build` for a fresh bundle.
  3. Replace `/Applications/YouTube Reader.app` with `dist/darwin-arm64/YouTube Reader.app` via `ditto`.
  4. Verify the installed binary carries the expected change before reporting completion.
  5. Batch multiple landed changes into one rebuild and install cycle.

## Delivery and PR rules

- Delivery posture: `no-mistakes +yolo`.
- Every pull request runs automated build and test checks via GitHub Actions CI (`.github/workflows/ci.yml`).
- Single-purpose PRs: one pull request does one thing; never bundle independent features, fixes or refactors.

## Maintaining this file

- Keep this file to knowledge useful to almost every future agent session; do not restate what the codebase already shows - point to the authoritative file or command.
- Prefer rewriting or pruning existing entries over appending new ones, and keep entries concise while preserving this bar for all agents.
