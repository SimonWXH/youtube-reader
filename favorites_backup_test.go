package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func favoriteVideo(id, channel, title string, favoritedAt, updatedAt int64) core.Video {
	return core.Video{
		ID:              id,
		ChannelID:       channel,
		ChannelTitle:    "Channel " + channel,
		Title:           title,
		Description:     "description must not be exported",
		ThumbnailURL:    "https://example.invalid/thumb-" + id,
		AvatarURL:       "https://example.invalid/avatar-" + channel,
		PublishedAt:     "2024-01-02T03:04:05Z",
		FavoritedAt:     favoritedAt,
		FavoriteChannel: "Favorites",
	}
}

func TestExportFavoritesWritesStandaloneBackup(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	backupPath := filepath.Join(dir, "exports", "favorites.json")
	a.Settings.BackupPath = backupPath
	a.Settings.APIKey = "secret-api-key"
	first := favoriteVideo("video-1", "UC_1", "First", 1000, 2000)
	second := favoriteVideo("video-2", "UC_2", "Second", 2000, 3000)
	a.Sync.Favorites[first.ID] = core.FavoriteRecord{VideoID: first.ID, FavoritedAt: first.FavoritedAt, Video: &first, UpdatedAt: 2000}
	a.Sync.Favorites[second.ID] = core.FavoriteRecord{VideoID: second.ID, FavoritedAt: second.FavoritedAt, Video: &second, UpdatedAt: 3000}
	a.Sync.ReadStates["video-1"] = map[string]core.ReadRecord{"device-1": {Read: true, UpdatedAt: 4000}}
	a.Sync.DeviceID = "device-1"

	if err := a.ExportFavorites(); err != nil {
		t.Fatalf("ExportFavorites: %v", err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var payload favoritesBackup
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("parse backup: %v", err)
	}
	if payload.SchemaVersion != 1 || len(payload.Favorites) != 2 {
		t.Fatalf("backup payload: %+v", payload)
	}
	if payload.Favorites[0].VideoID != "video-2" || payload.Favorites[1].VideoID != "video-1" {
		t.Fatalf("backup order: %+v", payload.Favorites)
	}
	if payload.Favorites[0].ChannelID != "UC_2" || payload.Favorites[0].Title != "Second" {
		t.Fatalf("backup entry: %+v", payload.Favorites[0])
	}
	text := string(data)
	for _, forbidden := range []string{"secret-api-key", "readStates", "deviceID", "description", "https://example.invalid", "avatarURL", "videos.sqlite"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("backup leaked %q: %s", forbidden, text)
		}
	}
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions: %v", info.Mode().Perm())
	}
}

func TestExportFavoritesHandlesMissingMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	state := core.SyncState{
		Favorites: map[string]core.FavoriteRecord{
			"video-1": {VideoID: "video-1", FavoritedAt: 1000, UpdatedAt: 2000},
		},
	}
	if err := ExportFavorites(state, path, time.UnixMilli(3000)); err != nil {
		t.Fatalf("ExportFavorites: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload favoritesBackup
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Favorites) != 1 || payload.Favorites[0].VideoID != "video-1" || payload.Favorites[0].FavoritedAt != 1000 {
		t.Fatalf("backup payload: %+v", payload)
	}
}

func TestImportFavoritesMergesAndPreservesNewerLocal(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	path := filepath.Join(dir, "restore.json")
	local := favoriteVideo("video-local", "UC_LOCAL", "Local", 5000, 9000)
	localNewer := favoriteVideo("video-shared", "UC_SHARED", "Local newer", 6000, 9000)
	a.Sync.Favorites[local.ID] = core.FavoriteRecord{VideoID: local.ID, FavoritedAt: local.FavoritedAt, Video: &local, UpdatedAt: 9000}
	a.Sync.Favorites[localNewer.ID] = core.FavoriteRecord{VideoID: localNewer.ID, FavoritedAt: localNewer.FavoritedAt, Video: &localNewer, UpdatedAt: 9000}

	sharedOlder := favoritesBackupVideo{VideoID: "video-shared", ChannelID: "UC_SHARED", Title: "Older remote", FavoritedAt: 1000, UpdatedAt: 1000}
	remoteNewer := favoritesBackupVideo{VideoID: "video-remote", ChannelID: "UC_REMOTE", Title: "Newer remote", FavoritedAt: 2000, UpdatedAt: 9500}
	data, err := json.Marshal(favoritesBackup{SchemaVersion: 1, Favorites: []favoritesBackupVideo{sharedOlder, remoteNewer}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := a.ImportFavorites(path)
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed count: %d", changed)
	}
	if len(a.Sync.Favorites) != 3 {
		t.Fatalf("favorites after restore: %+v", a.Sync.Favorites)
	}
	if got := a.Sync.Favorites["video-shared"]; got.Video.Title != "Local newer" || got.UpdatedAt != 9000 || got.FavoritedAt != 6000 {
		t.Fatalf("newer local favorite overwritten: %+v", got)
	}
	if got := a.Sync.Favorites["video-remote"]; got.VideoID != "video-remote" || got.Video.ChannelID != "UC_REMOTE" {
		t.Fatalf("remote favorite missing: %+v", got)
	}
	saved, err := core.LoadSyncWithConflicts(filepath.Join(dir, "sync.json"), a.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Favorites) != 3 {
		t.Fatalf("saved favorites: %+v", saved.Favorites)
	}
}

func TestImportFavoritesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	video := favoriteVideo("video-1", "UC_1", "Round trip", 1234, 2345)
	a.Sync.Favorites[video.ID] = core.FavoriteRecord{VideoID: video.ID, FavoritedAt: video.FavoritedAt, Video: &video, UpdatedAt: 2000}
	backupPath := filepath.Join(dir, "backup.json")
	if err := ExportFavorites(a.Sync, backupPath, time.UnixMilli(9999)); err != nil {
		t.Fatal(err)
	}
	a2 := newTestApp(t, filepath.Join(dir, "second"))
	defer a2.Close()
	changed, err := a2.ImportFavorites(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("changed count: %d", changed)
	}
	got := a2.Sync.Favorites["video-1"]
	if got.VideoID != "video-1" || got.FavoritedAt != 1234 || got.UpdatedAt != 2000 || got.Video.ChannelID != "UC_1" {
		t.Fatalf("round trip record: %+v", got)
	}
}

func TestImportFavoritesKeepsDataOnMissingOrCorruptFile(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	video := favoriteVideo("video-1", "UC_1", "Existing", 1000, 2000)
	a.Sync.Favorites[video.ID] = core.FavoriteRecord{VideoID: video.ID, FavoritedAt: video.FavoritedAt, Video: &video, UpdatedAt: 2000}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrongVersion := filepath.Join(dir, "wrong-version.json")
	if err := os.WriteFile(wrongVersion, []byte(`{"schemaVersion":99,"favorites":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.json"), corrupt, wrongVersion} {
		changed, err := a.ImportFavorites(path)
		if err == nil {
			t.Fatalf("ImportFavorites(%s) unexpectedly succeeded", path)
		}
		if changed != 0 {
			t.Fatalf("ImportFavorites(%s) changed %d favorites", path, changed)
		}
		if len(a.Sync.Favorites) != 1 {
			t.Fatalf("ImportFavorites(%s) cleared favorites: %+v", path, a.Sync.Favorites)
		}
	}
}

func TestApplyFavoritesBackupRejectsMalformedEntries(t *testing.T) {
	state := core.SyncState{Favorites: map[string]core.FavoriteRecord{}}
	for _, data := range []string{
		`{"schemaVersion":1}`,
		`{"schemaVersion":1,"favorites":[{"channelID":"UC_1"}]}`,
		`{"schemaVersion":1,"favorites":[{"videoID":"v","channelID":"a"},{"videoID":"v","channelID":"b"}]}`,
	} {
		changed, err := ApplyFavoritesBackup(&state, []byte(data), 1000)
		if err == nil || changed != 0 || len(state.Favorites) != 0 {
			t.Fatalf("malformed backup accepted: data=%s err=%v changed=%d favorites=%+v", data, err, changed, state.Favorites)
		}
	}
}
