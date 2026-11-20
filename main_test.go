package main

import (
	"strings"
	"testing"
	"time"
)

func TestRefreshedLabel(t *testing.T) {
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	tests := []struct {
		name  string
		stamp string
		want  string
	}{
		{"empty", "", ""},
		{"invalid", "not-a-time", "Refreshed not-a-time"},
		{"just now", time.Now().UTC().Format(time.RFC3339), "Refreshed just now"},
		{"minutes", time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339), "Refreshed 5 min ago"},
		{"hours", time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339), "Refreshed 3 hr ago"},
		{"older than a day", old.UTC().Format(time.RFC3339), "Refreshed on " + old.Local().Format("Jan 2, 2006 at 15:04")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refreshedLabel(tt.stamp)
			if got != tt.want {
				t.Fatalf("refreshedLabel(%q) = %q, want %q", tt.stamp, got, tt.want)
			}
			if tt.name == "older than a day" && !strings.Contains(got, old.Local().Format("2006")) {
				t.Fatalf("older refresh label omits the year: %q", got)
			}
		})
	}
}
