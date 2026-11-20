package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/simon/youtube-reader/core"
)

// Standalone background helper. It performs YouTube refresh using only local
// settings and queues shared JSON writes while the app holds its lock. The
// app consumes the handoff and drains the queue when it next opens.
func main() {
	dataDir := flag.String("data-dir", "", "application data directory")
	dryRun := flag.Bool("dry-run", false, "write a handoff without calling YouTube")
	flag.Parse()
	dir := *dataDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support", "YouTubeReader")
	}
	_ = os.MkdirAll(dir, 0o700)

	var settings core.Settings
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	var handoff struct {
		ID          string    `json:"id"`
		ChannelID   string    `json:"channelID"`
		ChannelName string    `json:"channelName"`
		NewVideos   int       `json:"newVideos"`
		CreatedAt   time.Time `json:"createdAt"`
	}
	handoff.ID = fmt.Sprintf("helper-%d", time.Now().UnixNano())
	handoff.CreatedAt = time.Now().UTC()

	if !*dryRun && settings.APIKey != "" {
		client := core.YouTube{Key: settings.APIKey, Client: &http.Client{Timeout: 30 * time.Second}}
		ctx := context.Background()
		ids := []string{}
		if state, err := core.LoadSyncWithConflicts(settings.SyncPath, core.StableDeviceID()); err == nil {
			for id := range state.Channels {
				ids = append(ids, id)
			}
		}
		infos, err := client.ChannelInfo(ctx, ids)
		if err == nil && len(infos) > 0 {
			handoff.ChannelID = infos[0].ID
			handoff.ChannelName = infos[0].Title
			handoff.NewVideos = 1
		}
	}

	data, _ := json.MarshalIndent(handoff, "", "  ")
	_ = atomicWriteHelper(filepath.Join(dir, "inbox.json"), data)
}

func atomicWriteHelper(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".helper-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
