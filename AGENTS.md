# AGENTS.md

The rules below are settled decisions for this repository, not suggestions. Follow them; do not re-derive or contradict them.

## Overview and architecture

- YouTube Reader is an open-source, two-platform YouTube subscription reader for daily personal use (native macOS client in Go/MyGo + native Android client).
- Core model: Local-first, YouTube Data API v3 integration, channel avatars, standard long-form videos (filter out Shorts), with optional RSS aggregation on Android.
- All work stays inside the public repository `SimonWXH/youtube-reader`.

## Quality bar and design principles

- Commercial-grade professional quality bar: Treat as commercial-grade software despite personal use; UI/UX standards are higher than comparable utility apps.
- Pixel perfection and detail polish: Every interactive and layout detail matters - button styles, status popover, detail pane, settings screen, top-right refresh/settings controls, typography, borders, and all interactive states (loading, empty, error, active).
- Bilingual interfaces: English and 简体中文, with an in-app language switch in settings, defaulting to English.

## Build, test, and package (macOS)

- Built with MyGo toolchain + macOS Command Line Tools (no Xcode project).
- `make build`: Compiles the helper into `resources/darwin`, builds the application via `mygo build`, and ad-hoc signs the app bundle into `dist/darwin-arm64/YouTube Reader.app`.
- `make test`: Runs `go vet ./...` and `go test ./...`.
- `make dmg`: Packages the signed app bundle and an `/Applications` symlink into a compressed disk image.
- `make run`: Builds and launches the app bundle via `open`.
- `make clean`: Cleans `dist/` and staged helper binaries.

## Local app delivery

- A merged pull request is not delivery: This project ships as a locally built Mac app.
- Concrete delivery procedure after landing changes:
  1. Quit the running app cleanly (use `kill -TERM <pid>` if unresponsive).
  2. Run `make build` to produce a fresh bundle.
  3. Replace `/Applications/YouTube Reader.app` with `dist/darwin-arm64/YouTube Reader.app` via `ditto`.
  4. Verify the installed binary carries the expected change before reporting completion.
  5. Batch multiple landed changes into a single rebuild and install cycle.

## Delivery and PR rules

- Delivery posture: `no-mistakes +yolo`.
- Every pull request runs automated build and test checks via GitHub Actions CI (`.github/workflows/ci.yml`).
- Single-purpose PRs: One pull request does one thing; do not bundle independent features, fixes, or refactors.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
