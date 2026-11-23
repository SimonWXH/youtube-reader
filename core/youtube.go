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
	Key     string
	Client  *http.Client
	BaseURL string
}

func (y YouTube) baseURL() string {
	if y.BaseURL != "" {
		return strings.TrimRight(y.BaseURL, "/")
	}
	return apiBase
}

type ytChannelsResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title      string `json:"title"`
			Thumbnails struct {
				Default struct {
					URL string `json:"url"`
				} `json:"default"`
			} `json:"thumbnails"`
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

// ErrChannelNotFound is a lookup that reached YouTube and found no channel. The
// add-subscription dialog tells it apart from a failed request, so the reader
// is told to check the handle rather than the network.
var ErrChannelNotFound = errors.New("channel not found")

// ErrChannelRefUnresolvable is a reference the Data API has no lookup for, such
// as a custom /c/ link. The app rejects those locally, so this only answers a
// caller that resolved one by mistake.
var ErrChannelRefUnresolvable = errors.New("channel reference cannot be looked up")

func apiError(message string, status int) error {
	if status == 403 || status == 429 || strings.Contains(strings.ToLower(message), "quota") {
		return ErrQuota
	}
	return errors.New(message)
}

// channelsByQuery performs one channels.list request for query and decodes the
// channel records it returns. Every lookup of a channel ID, handle, or legacy
// username goes through it, so they share one request, one decode, and one
// reading of the API's errors.
func (y YouTube) channelsByQuery(ctx context.Context, query url.Values) ([]Channel, error) {
	query.Set("part", "snippet,contentDetails")
	query.Set("maxResults", "50")
	query.Set("key", y.Key)
	var resp ytChannelsResponse
	client := y.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.baseURL()+"/channels?"+query.Encode(), nil)
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
	out := make([]Channel, 0, len(resp.Items))
	for _, item := range resp.Items {
		out = append(out, Channel{
			ID:         item.ID,
			Title:      item.Snippet.Title,
			AvatarURL:  item.Snippet.Thumbnails.Default.URL,
			UploadedAt: item.ContentDetails.RelatedPlaylists.Uploads,
			UpdatedAt:  time.Now().UnixMilli(),
		})
	}
	return out, nil
}

func (y YouTube) ChannelInfo(ctx context.Context, ids []string) ([]Channel, error) {
	var out []Channel
	for start := 0; start < len(ids); start += 50 {
		end := minInt(start+50, len(ids))
		channels, err := y.channelsByQuery(ctx, url.Values{"id": {strings.Join(ids[start:end], ",")}})
		if err != nil {
			return nil, err
		}
		out = append(out, channels...)
	}
	return out, nil
}

// ResolveChannelRef looks up the channel a parsed reference names. A bare name
// is tried as a handle first and as a legacy username second, which is the one
// fallback the API offers; a video link resolves to the channel that uploaded
// the video. A lookup that finds nothing, or a channel without a title, reports
// ErrChannelNotFound so no subscription is created without a name.
func (y YouTube) ResolveChannelRef(ctx context.Context, ref ChannelRef) (Channel, error) {
	switch ref.Kind {
	case ChannelRefID:
		return y.firstChannel(ctx, url.Values{"id": {ref.Value}})
	case ChannelRefHandle:
		return y.firstChannel(ctx, url.Values{"forHandle": {ref.Value}})
	case ChannelRefUsername:
		return y.firstChannel(ctx, url.Values{"forUsername": {ref.Value}})
	case ChannelRefName:
		channel, err := y.firstChannel(ctx, url.Values{"forHandle": {ref.Value}})
		if err == nil || !errors.Is(err, ErrChannelNotFound) {
			return channel, err
		}
		return y.firstChannel(ctx, url.Values{"forUsername": {ref.Value}})
	case ChannelRefVideo:
		return y.channelOfVideo(ctx, ref.Value)
	default:
		return Channel{}, ErrChannelRefUnresolvable
	}
}

// firstChannel is one lookup that must name exactly one channel.
func (y YouTube) firstChannel(ctx context.Context, query url.Values) (Channel, error) {
	channels, err := y.channelsByQuery(ctx, query)
	if err != nil {
		return Channel{}, err
	}
	for _, channel := range channels {
		if strings.TrimSpace(channel.Title) != "" {
			return channel, nil
		}
	}
	return Channel{}, ErrChannelNotFound
}

// channelOfVideo resolves a video link to the channel that uploaded the video.
func (y YouTube) channelOfVideo(ctx context.Context, videoID string) (Channel, error) {
	var resp ytVideosResponse
	q := url.Values{"part": {"snippet"}, "id": {videoID}, "maxResults": {"1"}}
	q.Set("key", y.Key)
	client := y.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.baseURL()+"/videos?"+q.Encode(), nil)
	if err != nil {
		return Channel{}, err
	}
	httpResp, err := client.Do(req)
	if err != nil {
		return Channel{}, err
	}
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	httpResp.Body.Close()
	if err != nil {
		return Channel{}, err
	}
	if httpResp.StatusCode >= 400 {
		message := ""
		if resp.Error != nil {
			message = resp.Error.Message
		}
		return Channel{}, apiError(message, httpResp.StatusCode)
	}
	for _, item := range resp.Items {
		if item.Snippet.ChannelID != "" {
			return y.firstChannel(ctx, url.Values{"id": {item.Snippet.ChannelID}})
		}
	}
	return Channel{}, ErrChannelNotFound
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.baseURL()+"/playlistItems?"+q.Encode(), nil)
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.baseURL()+"/videos?"+q.Encode(), nil)
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
