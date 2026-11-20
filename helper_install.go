package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const helperLabel = "com.simon.youtubereader.helper"

type helperPaths struct {
	Helper   string
	App      string
	DataDir  string
	Interval int
	Home     string
}

func helperPathsFor() helperPaths {
	home, _ := os.UserHomeDir()
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
	return helperPaths{Helper: helper, App: exe, DataDir: dir, Interval: 3600, Home: home}
}

const launchdPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>{{.Label}}</string>
  <key>ProgramArguments</key><array>
    <string>{{.Helper}}</string>
    <string>-data-dir</string><string>{{.DataDir}}</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>StartInterval</key><integer>{{.Interval}}</integer>
  <key>StandardOutPath</key><string>{{.DataDir}}/helper.log</string>
  <key>StandardErrorPath</key><string>{{.DataDir}}/helper.err.log</string>
</dict></plist>
`

func installHelper() error {
	p := helperPathsFor()
	path := filepath.Join(p.Home, "Library", "LaunchAgents", helperLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmpl, err := template.New("plist").Parse(launchdPlist)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tmpl.Execute(f, struct {
		helperPaths
		Label string
	}{helperPaths: p, Label: helperLabel}); err != nil {
		return err
	}
	return exec.Command("launchctl", "bootout", "gui/"+uid(), path).Run()
}

func uninstallHelper() error {
	p := helperPathsFor()
	path := filepath.Join(p.Home, "Library", "LaunchAgents", helperLabel+".plist")
	_ = exec.Command("launchctl", "bootout", "gui/"+uid(), path).Run()
	return os.Remove(path)
}

func uid() string {
	out, _ := exec.Command("id", "-u").Output()
	return string(out[:len(out)-1])
}
