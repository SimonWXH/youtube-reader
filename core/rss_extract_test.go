package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestCleanSnippet(t *testing.T) {
	raw := `<p>Hello <b>World</b>! &amp; welcome to &#39;Go&#39; programming.   New lines<br>and spaces.</p>`
	got := CleanSnippet(raw, "", 100)
	want := "Hello World! & welcome to 'Go' programming. New lines and spaces."
	if got != want {
		t.Fatalf("CleanSnippet got %q; want %q", got, want)
	}

	// Truncation check
	long := strings.Repeat("A", 300)
	truncated := CleanSnippet(long, "", 50)
	if len([]rune(truncated)) != 53 || !strings.HasSuffix(truncated, "...") {
		t.Fatalf("CleanSnippet should truncate to 50 runes + '...', got length %d: %q", len([]rune(truncated)), truncated)
	}

	// Entities must be decoded exactly once: a post quoting markup shows the
	// literal "&amp;" text, not the "&" it decodes to.
	quoted := CleanSnippet(`<p>A &amp;amp; B</p>`, "", 100)
	if quoted != "A &amp; B" {
		t.Fatalf("CleanSnippet must decode entities once, got %q; want %q", quoted, "A &amp; B")
	}
}

func TestParseHTMLBlocks(t *testing.T) {
	htmlContent := `
	<h1>Main Article Title</h1>
	<h2>Section Heading</h2>
	<p>This is the first introductory paragraph with a <a href="https://example.com">link</a> inside.</p>
	<blockquote>This is a memorable quote from an expert.</blockquote>
	<pre><code class="language-go">func main() {
    println("Hello, RSS!")
}</code></pre>
	<figure>
		<img src="https://example.com/figure.png" alt="Architecture diagram">
		<figcaption>Figure 1: High level architecture</figcaption>
	</figure>
	<ul>
		<li>Point number one</li>
		<li>Point number two</li>
	</ul>
	`

	blocks := ParseHTMLBlocks(htmlContent)
	if len(blocks) < 6 {
		t.Fatalf("expected at least 6 blocks, got %d", len(blocks))
	}

	// Heading 1
	if blocks[0].Type != BlockHeading || blocks[0].Level != 1 || blocks[0].Text != "Main Article Title" {
		t.Errorf("block 0: expected H1, got %+v", blocks[0])
	}
	// Heading 2
	if blocks[1].Type != BlockHeading || blocks[1].Level != 2 || blocks[1].Text != "Section Heading" {
		t.Errorf("block 1: expected H2, got %+v", blocks[1])
	}
	// Paragraph
	if blocks[2].Type != BlockParagraph || !strings.Contains(blocks[2].Text, "introductory paragraph") {
		t.Errorf("block 2: expected paragraph, got %+v", blocks[2])
	}
	// Blockquote
	if blocks[3].Type != BlockBlockquote || !strings.Contains(blocks[3].Text, "memorable quote") {
		t.Errorf("block 3: expected blockquote, got %+v", blocks[3])
	}
	// Code
	if blocks[4].Type != BlockCode || blocks[4].Language != "go" || !strings.Contains(blocks[4].Text, "println") {
		t.Errorf("block 4: expected code with lang 'go', got %+v", blocks[4])
	}
	// Figure image with caption
	if blocks[5].Type != BlockImage || blocks[5].URL != "https://example.com/figure.png" || blocks[5].Caption != "Figure 1: High level architecture" {
		t.Errorf("block 5: expected image with caption, got %+v", blocks[5])
	}

	// Verify all blocks have stable hashes
	for i, b := range blocks {
		if b.Hash == "" {
			t.Errorf("block %d hash is empty", i)
		}
	}
}

func TestParseHTMLBlocksPlainTextFallback(t *testing.T) {
	plain := "First paragraph text.\n\nSecond paragraph text."
	blocks := ParseHTMLBlocks(plain)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 paragraph blocks, got %d", len(blocks))
	}
	if blocks[0].Text != "First paragraph text." || blocks[1].Text != "Second paragraph text." {
		t.Fatalf("unexpected blocks text: %+v", blocks)
	}
}

func TestExtractHeroImage(t *testing.T) {
	html := `<p>Intro text</p><img src="https://example.com/lead.jpg" alt="lead"><p>More text</p>`
	got := ExtractHeroImage(html)
	if got != "https://example.com/lead.jpg" {
		t.Fatalf("ExtractHeroImage got %q; want https://example.com/lead.jpg", got)
	}
}

func TestExtractFullTextSuccess(t *testing.T) {
	richHTML := `<!DOCTYPE html>
<html>
<head><title>Full Tech Article</title></head>
<body>
<article>
	<h1>In-depth Deep Dive into Modern Systems</h1>
	<p>This is a comprehensive article containing thorough technical analysis and detailed descriptions designed to satisfy the readability threshold cleanly and reliably.</p>
	<p>Second paragraph expanding on architecture, scalability patterns, decoupled synchronization models, and zero-downtime database upgrades.</p>
	<img src="https://example.com/diagram.png" alt="Architecture">
</article>
</body>
</html>`

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(richHTML)),
				}, nil
			},
		},
	}

	contentHTML, blocks, isFullText, heroImage, err := ExtractFullText(
		context.Background(),
		client,
		"https://example.com/article/1",
		"<p>Fallback summary</p>",
		"Fallback summary",
	)

	if err != nil {
		t.Fatalf("ExtractFullText returned unexpected error: %v", err)
	}
	if !isFullText {
		t.Fatalf("isFullText should be true on successful extraction")
	}
	if len(blocks) == 0 {
		t.Fatalf("blocks should not be empty")
	}
	if !strings.Contains(contentHTML, "comprehensive article") {
		t.Fatalf("contentHTML missing expected extracted text: %s", contentHTML)
	}
	_ = heroImage
}

func TestExtractFullTextFailureDegradation(t *testing.T) {
	// Mock HTTP client returning 404 Not Found
	client404 := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Body:       io.NopCloser(strings.NewReader("Not Found")),
				}, nil
			},
		},
	}

	fallbackHTML := "<p>This is the original feed summary description.</p>"
	fallbackSnippet := "This is the original feed summary description."

	contentHTML, blocks, isFullText, _, err := ExtractFullText(
		context.Background(),
		client404,
		"https://example.com/article/missing",
		fallbackHTML,
		fallbackSnippet,
	)

	// Invariant: Extraction failure MUST NOT return fatal error (smooth degradation)
	if err != nil {
		t.Fatalf("expected nil error on degradation, got: %v", err)
	}
	if isFullText {
		t.Fatalf("isFullText must be false on degradation")
	}
	if contentHTML != fallbackHTML {
		t.Fatalf("contentHTML should be fallbackHTML, got: %s", contentHTML)
	}
	if len(blocks) != 1 || blocks[0].Text != fallbackSnippet {
		t.Fatalf("blocks should degrade to fallback summary block, got: %+v", blocks)
	}

	// Also verify network error degradation
	clientNetErr := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: connection refused")
			},
		},
	}

	_, _, isFullTextNet, _, errNet := ExtractFullText(
		context.Background(),
		clientNetErr,
		"https://example.com/article/down",
		fallbackHTML,
		fallbackSnippet,
	)
	if errNet != nil {
		t.Fatalf("expected nil error on network failure degradation, got: %v", errNet)
	}
	if isFullTextNet {
		t.Fatalf("isFullText must be false on network failure")
	}
}
