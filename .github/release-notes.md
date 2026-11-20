# YouTube Reader Release

### Installation

1. Download the disk image (`.dmg`) attached below. It contains an Apple silicon (arm64) build.
2. Open the disk image and drag **YouTube Reader** into `/Applications`.
3. Complete the first-launch authorization steps below.

### Security and First-Launch Authorization (Ad-hoc Signed Build)

This build is ad-hoc signed and not notarized. YouTube Reader is a personal-use app built without a paid Apple Developer account. On modern macOS, Gatekeeper flags unnotarized downloaded applications and blocks them on first launch with an alert stating that the developer cannot be verified. This is normal system behavior for personal builds.

To authorize YouTube Reader to run (only do this for builds downloaded from a trusted source):

#### Method 1: System Settings (Standard macOS flow)
1. After dragging **YouTube Reader** into `/Applications`, double-click to open it. macOS will block launch and state that the developer cannot be verified. Click **Done**.
2. Open **System Settings** -> **Privacy & Security** and scroll down to the **Security** section.
3. Look for the notice stating "YouTube Reader was blocked" and click the **Open Anyway** button.
4. Authenticate with your Mac user password or Touch ID, then click **Open** in the confirmation dialog.
5. macOS records the application as a trusted exception; subsequent launches open directly with a normal double-click.

#### Method 2: Terminal Alternative
If you prefer the command line, strip the quarantine extended attribute from the installed application bundle:
```sh
xattr -d com.apple.quarantine "/Applications/YouTube Reader.app"
```

> **Security Note:** Both methods authorize this specific application once after you review and accept the risk. They do not disable system protection or Gatekeeper globally.
