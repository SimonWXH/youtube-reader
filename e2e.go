//go:build e2e

package main

import (
	"os"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"
)

// init forces an appearance for screenshot runs: an e2e build started with
// YTREADER_APPEARANCE=light or dark draws in it, so both appearances can be
// captured on a machine whose system appearance is one of them. Release
// builds never compile this file.
func init() {
	if source := os.Getenv("YTREADER_APPEARANCE"); source != "" {
		mygo.Theme.SetSource(mygo.ThemeSource(source))
	}
}
