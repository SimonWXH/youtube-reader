package core

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultImageTimeout bounds one picture download: a server that accepts the
// connection and then stalls must end in the reader's failed-image placeholder
// rather than hold the frame forever.
const DefaultImageTimeout = 15 * time.Second

// imageReferers is the per-source header table a picture's host can require. A
// CDN that guards against hotlinking refuses a request that arrives without a
// Referer and serves it to one that names a site: sspai's picture CDN answers
// 403 "deny by referer access rule" to an empty Referer and 200 to any other, so
// its pictures need a reader to claim sspai's own address. A host that is
// missing from the table is sent no Referer, as before.
var imageReferers = map[string]string{
	"cdnfile.sspai.com": "https://sspai.com/",
}

// NewImageClient is the HTTP client the app downloads pictures with: it sends
// the Referer a picture's host requires, and gives one transient failure a
// second chance. Pictures then stay in netimage's memory and on its disk, as
// their servers' caching headers say, so a second open of the same article
// shows them without asking again. timeout bounds one download, whole.
func NewImageClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultImageTimeout
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &imageTransport{base: http.DefaultTransport},
	}
}

// imageTransport adds the Referer a picture's host requires before the request
// leaves, and asks once more after a transient failure. Everything else passes
// through untouched, so a request that already carries a Referer keeps it.
type imageTransport struct {
	base http.RoundTripper
	// rules is the table to look a host up in; nil is imageReferers, and a test
	// gives its own so it can serve pictures from its own host.
	rules map[string]string
}

func (t *imageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.send(req)
	if err == nil || !replayable(req) {
		return res, err
	}
	return t.send(req)
}

// send makes one attempt: the request with its Referer rule applied, or the
// request as it stands when its host has no rule or it already names a site.
func (t *imageTransport) send(req *http.Request) (*http.Response, error) {
	next := req
	if req.Header.Get("Referer") == "" {
		if referer := t.refererFor(req.URL); referer != "" {
			next = req.Clone(req.Context())
			next.Header.Set("Referer", referer)
		}
	}
	return t.base.RoundTrip(next)
}

// refererFor returns the Referer a picture's address must claim, or "" when its
// host requires none. A rule is keyed by host alone - lowercased and without
// user info, port, path, query or fragment - so one rule covers every picture a
// source publishes, whatever address the article around it has.
func (t *imageTransport) refererFor(picture *url.URL) string {
	if picture == nil {
		return ""
	}
	rules := t.rules
	if rules == nil {
		rules = imageReferers
	}
	return rules[strings.ToLower(picture.Hostname())]
}

// replayable reports whether asking again is safe and could still help. A
// picture has no body to replay and no side effect to repeat, so a download that
// failed on the wire is worth one more request; one the caller gave up on, or
// whose deadline has already passed, is not.
func replayable(req *http.Request) bool {
	return req.Body == nil && req.GetBody == nil && req.Context().Err() == nil
}
