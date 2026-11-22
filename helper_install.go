package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

const helperLabel = "com.simon.youtubereader.helper"

var helperHomeDir = os.UserHomeDir

var helperLaunchctl = func(args ...string) error {
	return exec.Command("launchctl", args...).Run()
}

type helperPaths struct {
	Helper  string
	DataDir string
}

var helperPathsFor = func() helperPaths {
	home, _ := helperHomeDir()
	exe, err := os.Executable()
	if err != nil {
		exe = filepath.Join(home, "Applications", "YouTube Reader.app", "Contents", "MacOS", "YouTube Reader")
	}
	helper := filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources", "ytreader-helper")
	if _, err := os.Stat(helper); err != nil {
		helper = filepath.Join(appDir(), "ytreader-helper")
	}
	dir := appDir()
	_ = os.MkdirAll(dir, 0o700)
	return helperPaths{Helper: helper, DataDir: dir}
}

// helperLaunchAgentPath is where the background helper's login item lives.
func helperLaunchAgentPath() string {
	home, _ := helperHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", helperLabel+".plist")
}

const launchdPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>{{.Label}}</string>
  <key>ProgramArguments</key><array>
    <string>{{.Helper}}</string>
    <string>-data-dir</string><string>{{.DataDir}}</string>
  </array>
{{- if .Interval}}
  <key>RunAtLoad</key><true/>
  <key>StartInterval</key><integer>{{.Interval}}</integer>
{{- end}}
  <key>StandardOutPath</key><string>{{.DataDir}}/helper.log</string>
  <key>StandardErrorPath</key><string>{{.DataDir}}/helper.err.log</string>
</dict></plist>
`

// installHelper registers the helper as a user LaunchAgent. The Settings
// window's refresh interval schedules it: an interval in minutes becomes the
// LaunchAgent's StartInterval and runs it at login; Manual (zero) writes no
// schedule or login run, and the Settings pane removes the login item instead.
func installHelper(refreshMinutes int) error {
	return writeHelperItem(helperPathsFor(), helperLaunchAgentPath(), refreshMinutes)
}

// writeHelperItem writes the login item at path and reloads launchd, so the
// schedule launchd holds is the one the property list names. A failed reload
// puts the login item installed before back on disk and, once its job has
// been unloaded, asks launchd to load it again, so a refused rewrite never
// stops a schedule that worked; a fresh install leaves nothing behind.
func writeHelperItem(p helperPaths, path string, refreshMinutes int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	plist, err := renderLaunchAgent(p, refreshMinutes)
	if err != nil {
		return err
	}
	prior, readErr := os.ReadFile(path)
	hadPrior := readErr == nil
	if err := os.WriteFile(path, []byte(plist), 0o600); err != nil {
		if hadPrior {
			_ = os.WriteFile(path, prior, 0o600)
		} else {
			_ = os.Remove(path)
		}
		return err
	}
	_ = helperLaunchctl("bootout", "gui/"+uid(), path)
	if refreshMinutes <= 0 {
		return nil
	}
	if err := helperLaunchctl("bootstrap", "gui/"+uid(), path); err != nil {
		_ = helperLaunchctl("bootout", "gui/"+uid(), path)
		if hadPrior {
			_ = os.WriteFile(path, prior, 0o600)
			_ = helperLaunchctl("bootstrap", "gui/"+uid(), path)
		} else {
			_ = os.Remove(path)
		}
		return err
	}
	return nil
}

// reconcileHelperInterval rewrites a login item that differs from the one this
// build would write, including one an older build wrote with a fixed interval.
// Manual removes the login item it no longer schedules. It runs only when this
// build can run the helper it would name: a dev run outside the app bundle
// reconciles nothing, so launch never replaces a working login item with a
// schedule whose program is missing.
func reconcileHelperInterval(refreshMinutes int) error {
	if !runningFromAppBundle() {
		return nil
	}
	p := helperPathsFor()
	if _, err := os.Stat(p.Helper); err != nil {
		return nil
	}
	return reconcileHelperItem(p, helperLaunchAgentPath(), refreshMinutes)
}

// runningFromAppBundle reports whether this process executes from inside a
// .app bundle, the only packaging that carries the helper binary beside it.
var runningFromAppBundle = func() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if filepath.Base(filepath.Dir(exe)) != "MacOS" || filepath.Base(filepath.Dir(filepath.Dir(exe))) != "Contents" {
		return false
	}
	return strings.HasSuffix(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(exe)))), ".app")
}

func reconcileHelperItem(p helperPaths, path string, refreshMinutes int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if refreshMinutes <= 0 {
		return removeHelperItem(path)
	}
	current, err := renderLaunchAgent(p, refreshMinutes)
	if err != nil {
		return err
	}
	if string(data) == current {
		return nil
	}
	return writeHelperItem(p, path, refreshMinutes)
}

func uninstallHelper() error {
	return removeHelperItem(helperLaunchAgentPath())
}

func removeHelperItem(path string) error {
	_ = helperLaunchctl("bootout", "gui/"+uid(), path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// helperIntervalSeconds is the LaunchAgent's StartInterval for a refresh
// interval in minutes; zero installs no schedule.
func helperIntervalSeconds(refreshMinutes int) int {
	return refreshMinutes * 60
}

// renderLaunchAgent is the LaunchAgent's property list, so the interval the
// Settings window chose can be read back in a test.
func renderLaunchAgent(p helperPaths, refreshMinutes int) (string, error) {
	tmpl, err := template.New("plist").Parse(launchdPlist)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, struct {
		helperPaths
		Label    string
		Interval int
	}{helperPaths: p, Label: helperLabel, Interval: helperIntervalSeconds(refreshMinutes)}); err != nil {
		return "", err
	}
	return out.String(), nil
}

func uid() string {
	return strconv.Itoa(os.Getuid())
}
