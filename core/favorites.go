package core

func (s *SyncState) IsFavorite(videoID string) bool {
	_, ok := s.Favorites[videoID]
	return ok
}
