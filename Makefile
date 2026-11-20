# Build, test, and package YouTube Reader.
#
# MyGo compiles and signs the app bundle. This Makefile then applies the same
# simple ad-hoc signing scheme as CopyQmac and packages the disk image with
# hdiutil. There is no Developer ID, Team ID, notarization, stapler, or Xcode
# project involved.

APP_NAME     := YouTube Reader
SLUG         := youtube-reader
VERSION      := $(shell sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' mygo.json)
ARCH         ?= arm64
PLATFORM     := darwin/$(ARCH)
OUT_DIR      := dist/darwin-$(ARCH)
BUNDLE       := $(OUT_DIR)/$(APP_NAME).app
DMG          := $(OUT_DIR)/$(SLUG)-$(VERSION)-$(ARCH).dmg

# Prefer Go from PATH. The fallback is the task toolchain documented in the
# README, which keeps this Makefile usable from a bare agent environment.
GO           ?= $(shell command -v go 2>/dev/null || printf '%s' '/Volumes/ZHITAI_1TB/Projects/firstmate/data/youtube-reader-mygo-spike-1/toolchain/go/bin/go')
CODESIGN     ?= codesign

.PHONY: all build test dmg dmg-path run clean

all: build

## Build the helper into resources/darwin, compile the app, then re-sign the
## bundle ad hoc with the minimal command CopyQmac uses. -skip-dmg leaves DMG
## packaging to the dmg target so the image is built with staging and a UDZO
## image.
build:
	@GO="$(GO)" Scripts/build-helper.sh
	$(GO) tool mygo build -platform $(PLATFORM) -skip-dmg -skip-notarize -sign -
	@$(CODESIGN) --force --sign - "$(BUNDLE)"
	@echo "Built $(BUNDLE)"

test:
	$(GO) vet ./...
	$(GO) test ./...

## Package the signed app and an /Applications symlink into a compressed
## read-only disk image.
dmg: build
	@Scripts/package-dmg.sh "$(BUNDLE)" "$(DMG)" "$(APP_NAME)"

## Print the path of the disk image `make dmg` produces.
dmg-path:
	@echo "$(DMG)"

run: build
	@open "$(BUNDLE)"

clean:
	@rm -rf dist resources/darwin/ytreader-helper
	@echo "Cleaned dist and the staged helper"
