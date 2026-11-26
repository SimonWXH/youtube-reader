package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sspaiPicture is a picture the reader kept from a full-text extraction of an
// sspai article: the address is absolute, and its CDN refuses it without a
// Referer.
const sspaiPicture = "https://cdnfile.sspai.com/2026/11/25/article/059192f3eea37fdaf3a0519800f48d8d.jpeg?imageView2/2/w/1120/q/90/interlace/1/ignore-error/1"

// A picture's Referer rule is keyed by the host of its address alone.
func TestImageTransportResolvesRefererByHost(t *testing.T) {
	transport := &imageTransport{base: http.DefaultTransport}
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"the article's own picture", sspaiPicture, "https://sspai.com/"},
		{"host casing", "https://CDNFILE.SSPAI.COM/a.jpeg", "https://sspai.com/"},
		{"an explicit port", "https://cdnfile.sspai.com:443/a.jpeg", "https://sspai.com/"},
		{"plain http", "http://cdnfile.sspai.com/a.jpeg", "https://sspai.com/"},
		{"user info", "https://someone@cdnfile.sspai.com/a.jpeg", "https://sspai.com/"},
		{"a neighbour host", "https://sspai.com/a.jpeg", ""},
		{"a subdomain of a guarded host", "https://img.cdnfile.sspai.com/a.jpeg", ""},
		{"a host the table does not name", "https://images.unsplash.com/photo-1.jpeg", ""},
		{"a relative address", "/2026/11/25/article/a.jpeg", ""},
		{"nothing", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := url.Parse(tc.url)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", tc.url, err)
			}
			if got := transport.refererFor(parsed); got != tc.want {
				t.Fatalf("refererFor(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
	if got := transport.refererFor(nil); got != "" {
		t.Fatalf("refererFor(nil) = %q, want none", got)
	}
}

// pictureServer serves one JPEG-ish body and records what each request carried.
func pictureServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request) bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if handler != nil && handler(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "image/jpeg")
		io.WriteString(w, "picture")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// clientFor builds the transport under test with a rule for the server's host,
// the way the app keys a rule by the picture's host.
func clientFor(t *testing.T, srv *httptest.Server, rules map[string]string) *http.Client {
	t.Helper()
	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if rules == nil {
		rules = map[string]string{parsed.Hostname(): "https://sspai.com/"}
	}
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &imageTransport{base: http.DefaultTransport, rules: rules},
	}
}

func getWith(t *testing.T, client *http.Client, url, referer string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

// A picture on a guarded host is asked for with the Referer that host demands,
// and the answer keeps the caching headers netimage stores it by.
func TestImageClientSendsHostReferer(t *testing.T) {
	var seen atomic.Value
	srv, _ := pictureServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		seen.Store(r.Header.Get("Referer"))
		return false
	})

	res, body := getWith(t, clientFor(t, srv, nil), srv.URL+"/a.jpeg", "")
	if body != "picture" {
		t.Fatalf("body = %q, want %q", body, "picture")
	}
	if got := seen.Load(); got != "https://sspai.com/" {
		t.Fatalf("Referer = %v, want %q", got, "https://sspai.com/")
	}
	// The CDN's freshness is what keeps the picture on disk for the next open.
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "max-age=31536000") {
		t.Fatalf("Cache-Control = %q, want the server's max-age", got)
	}
	if got := res.Header.Get("ETag"); got != `"v1"` {
		t.Fatalf("ETag = %q, want %q", got, `"v1"`)
	}
}

// A host the table does not name is asked for exactly as before: no Referer.
func TestImageClientAddsNoRefererWithoutARule(t *testing.T) {
	var seen atomic.Value
	srv, _ := pictureServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		seen.Store(r.Header.Get("Referer"))
		return false
	})

	getWith(t, clientFor(t, srv, map[string]string{"media.example.com": "https://example.com/"}), srv.URL+"/a.jpeg", "")
	if got := seen.Load(); got != "" {
		t.Fatalf("Referer = %v, want none", got)
	}
}

// A Reader that already names a site keeps its own Referer: the rule fills a
// gap, it does not overrule the caller.
func TestImageClientKeepsAnExplicitReferer(t *testing.T) {
	var seen atomic.Value
	srv, _ := pictureServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		seen.Store(r.Header.Get("Referer"))
		return false
	})

	getWith(t, clientFor(t, srv, nil), srv.URL+"/a.jpeg", "https://sspai.com/post/119563")
	if got := seen.Load(); got != "https://sspai.com/post/119563" {
		t.Fatalf("Referer = %v, want the caller's own", got)
	}
}

// dropConnection severs the wire without answering, as a CDN reset does.
func dropConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	conn.Close()
}

// One transient failure is asked for again, so a reset picture still shows.
func TestImageClientRetriesOnceAfterAWireFailure(t *testing.T) {
	var first atomic.Bool
	srv, hits := pictureServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/flaky.jpeg" && first.CompareAndSwap(false, true) {
			dropConnection(w)
			return true
		}
		return false
	})

	_, body := getWith(t, clientFor(t, srv, nil), srv.URL+"/flaky.jpeg", "")
	if body != "picture" {
		t.Fatalf("body = %q, want the picture after a retry", body)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("asked %d times, want 2: one retry", got)
	}
}

// A server's own answer is not asked again: a 403 stays a failure the reader
// names, rather than a retry loop against the CDN.
func TestImageClientDoesNotRetryAnAnswer(t *testing.T) {
	srv, hits := pictureServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		http.Error(w, "deny by referer access rule", http.StatusForbidden)
		return true
	})

	res, _ := getWith(t, clientFor(t, srv, nil), srv.URL+"/guarded.jpeg", "")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.StatusCode)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("asked %d times, want 1", got)
	}
}

// A caller who has already given up is not asked for again.
func TestImageClientDoesNotRetryAnAbandonedRequest(t *testing.T) {
	srv, hits := pictureServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		dropConnection(w)
		return true
	})

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/gone.jpeg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !replayable(req) {
		t.Fatal("a plain picture request is replayable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if replayable(req.WithContext(ctx)) {
		t.Fatal("a request the caller cancelled must not be asked for again")
	}
	if _, err := clientFor(t, srv, nil).Do(req.WithContext(ctx)); err == nil {
		t.Fatal("a cancelled request must fail")
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("asked %d times, want 0", got)
	}
}
