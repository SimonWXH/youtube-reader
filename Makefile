# Build, test, and package YouTube Reader.
#
# MyGo compiles and initially packages the app bundle with ad-hoc signing.
# Scripts/sign-app.sh then signs nested binaries first and re-signs the bundle
# with SIGN_IDENTITY when present in the keychain, falling back to ad-hoc (-).
# There is no Developer ID, Team ID, notarization, stapler, or Xcode project.

APP_NAME     := YouTube Reader
SLUG         := youtube-reader
VERSION      := $(shell sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' mygo.json)
ARCH         ?= arm64
PLATFORM     := darwin/$(ARCH)
OUT_DIR      := dist/darwin-$(ARCH)
BUNDLE       := $(OUT_DIR)/$(APP_NAME).app
DMG          := $(OUT_DIR)/$(SLUG)-$(VERSION)-$(ARCH).dmg

GO           ?= go
CODESIGN     ?= codesign
SECURITY     ?= security
SIGN_IDENTITY ?= YouTube Reader Local Signing

.PHONY: all build test dmg dmg-path run clean

all: build

## Build the helper into resources/darwin, compile the app, then sign nested
## binaries and the bundle via Scripts/sign-app.sh (using SIGN_IDENTITY when
## available in the keychain, or falling back to ad-hoc). -skip-dmg leaves DMG
## packaging to the dmg target so the image is built with staging and a UDZO
## image.
build:
	@GO="$(GO)" Scripts/build-helper.sh
	$(GO) tool mygo build -platform $(PLATFORM) -skip-dmg -skip-notarize -sign -
	@CODESIGN="$(CODESIGN)" SECURITY="$(SECURITY)" SIGN_IDENTITY="$(SIGN_IDENTITY)" Scripts/sign-app.sh "$(BUNDLE)"
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
