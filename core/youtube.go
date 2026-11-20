package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://www.googleapis.com/youtube/v3"

type YouTube struct {
	Key    string
	Client *http.Client
}

type ytChannelsResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title string `json:"title"`
		} `json:"snippet"`
		ContentDetails struct {
			RelatedPlaylists struct {
				Uploads string `json:"uploads"`
			} `json:"relatedPlaylists"`
		} `json:"contentDetails"`
	} `json:"items"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type ytPlaylistResponse struct {
	Items []struct {
		Snippet struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			PublishedAt string `json:"publishedAt"`
			ResourceID  struct {
				VideoID string `json:"videoId"`
			} `json:"resourceId"`
		} `json:"snippet"`
		ContentDetails struct {
			VideoID string `json:"videoId"`
		} `json:"contentDetails"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
	Error         *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type ytVideosResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title        string `json:"title"`
			Description  string `json:"description"`
			PublishedAt  string `json:"publishedAt"`
			ChannelID    string `json:"channelId"`
			ChannelTitle string `json:"channelTitle"`
			Thumbnails   struct {
				Medium struct {
					URL string `json:"url"`
				} `json:"medium"`
				High struct {
					URL string `json:"url"`
				} `json:"high"`
			} `json:"thumbnails"`
		} `json:"snippet"`
		ContentDetails struct {
			Duration  string `json:"duration"`
			Dimension string `json:"dimension"`
		} `json:"contentDetails"`
	} `json:"items"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var ErrQuota = errors.New("quota exceeded")

func apiError(message string, status int) error {
	if status == 403 || status == 429 || strings.Contains(strings.ToLower(message), "quota") {
		return ErrQuota
	}
	return errors.New(message)
}

func (y YouTube) ChannelInfo(ctx context.Context, ids []string) ([]Channel, error) {
	var out []Channel
	for start := 0; start < len(ids); start += 50 {
		end := minInt(start+50, len(ids))
		var resp ytChannelsResponse
		q := url.Values{"part": {"snippet,contentDetails"}, "id": {strings.Join(ids[start:end], ",")}, "maxResults": {"50"}}
		q.Set("key", y.Key)
		client := y.Client
		if client == nil {
			client = http.DefaultClient
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/channels?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		err = json.NewDecoder(httpResp.Body).Decode(&resp)
		httpResp.Body.Close()
		if err != nil {
			return nil, err
		}
		if httpResp.StatusCode >= 400 {
			message := ""
			if resp.Error != nil {
				message = resp.Error.Message
			}
			return nil, apiError(message, httpResp.StatusCode)
		}
		for _, item := range resp.Items {
			ch := Channel{ID: item.ID, Title: item.Snippet.Title, UploadedAt: item.ContentDetails.RelatedPlaylists.Uploads, UpdatedAt: time.Now().UnixMilli()}
			out = append(out, ch)
		}
	}
	return out, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (y YouTube) PlaylistVideos(ctx context.Context, uploads string, maxPages int) ([]Video, error) {
	var out []Video
	token := ""
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for page := 0; page < maxPages; page++ {
		var resp ytPlaylistResponse
		q := url.Values{"part": {"snippet,contentDetails"}, "playlistId": {uploads}, "maxResults": {"50"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		q.Set("key", y.Key)
		client := y.Client
		if client == nil {
			client = http.DefaultClient
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/playlistItems?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		err = json.NewDecoder(httpResp.Body).Decode(&resp)
		httpResp.Body.Close()
		if err != nil {
			return nil, err
		}
		if httpResp.StatusCode >= 400 {
			message := ""
			if resp.Error != nil {
				message = resp.Error.Message
			}
			return nil, apiError(message, httpResp.StatusCode)
		}
		stop := false
		for _, item := range resp.Items {
			videoID := item.ContentDetails.VideoID
			if videoID == "" {
				videoID = item.Snippet.ResourceID.VideoID
			}
			if videoID == "" {
				continue
			}
			published := item.Snippet.PublishedAt
			if t, err := time.Parse(time.RFC3339, published); err == nil && t.Before(cutoff) {
				stop = true
				continue
			}
			v := Video{ID: videoID, Title: item.Snippet.Title, Description: item.Snippet.Description, PublishedAt: published, ThumbnailURL: "https://i.ytimg.com/vi/" + videoID + "/hqdefault.jpg"}
			out = append(out, v)
		}
		if stop || resp.NextPageToken == "" {
			break
		}
		token = resp.NextPageToken
	}
	return out, nil
}

func (y YouTube) Hydrate(ctx context.Context, videos []Video) ([]Video, error) {
	if len(videos) == 0 {
		return nil, nil
	}
	var out []Video
	for start := 0; start < len(videos); start += 50 {
		end := minInt(start+50, len(videos))
		ids := make([]string, 0, end-start)
		for _, v := range videos[start:end] {
			ids = append(ids, v.ID)
		}
		var resp ytVideosResponse
		q := url.Values{"part": {"snippet,contentDetails"}, "id": {strings.Join(ids, ",")}, "maxResults": {"50"}}
		q.Set("key", y.Key)
		client := y.Client
		if client == nil {
			client = http.DefaultClient
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/videos?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		err = json.NewDecoder(httpResp.Body).Decode(&resp)
		httpResp.Body.Close()
		if err != nil {
			return nil, err
		}
		if httpResp.StatusCode >= 400 {
			message := ""
			if resp.Error != nil {
				message = resp.Error.Message
			}
			return nil, apiError(message, httpResp.StatusCode)
		}
		for _, item := range resp.Items {
			v := Video{ID: item.ID, Title: item.Snippet.Title, Description: item.Snippet.Description, PublishedAt: item.Snippet.PublishedAt, ChannelID: item.Snippet.ChannelID, ChannelTitle: item.Snippet.ChannelTitle, Duration: item.ContentDetails.Duration}
			if item.Snippet.Thumbnails.High.URL != "" {
				v.ThumbnailURL = item.Snippet.Thumbnails.High.URL
			} else if item.Snippet.Thumbnails.Medium.URL != "" {
				v.ThumbnailURL = item.Snippet.Thumbnails.Medium.URL
			}
			v.IsShort = IsShort(v.Duration, 0, 0)
			out = append(out, v)
		}
	}
	return out, nil
}

func ParseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func FormatRFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func ParseRefreshMinutes(s string) int {
	n, _ := strconv.Atoi(s)
	if n <= 0 {
		return 60
	}
	return n
}
