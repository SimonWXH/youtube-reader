package core

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-shiori/go-readability"
)

const (
	// MinFullTextLength is the minimum number of effective text runes required
	// for an extracted article to be considered valid full text. Content with
	// fewer runes smoothly degrades to the feed's original summary.
	MinFullTextLength = 100

	// DefaultImageTimeout is the maximum duration allowed when checking an image resource.
	DefaultImageTimeout = 10 * time.Second
)

var (
	htmlTagRegex    = regexp.MustCompile(`<[^>]*>`)
	brRegex         = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</h[1-6]>|</li>`)
	whitespaceRegex = regexp.MustCompile(`\s+`)
	codeLangRegex   = regexp.MustCompile(`(?:language-|lang-)([a-zA-Z0-9_\-]+)`)
)

// CleanSnippet strips HTML tags, decodes HTML entities, collapses extra whitespace,
// and truncates the string to maxRunes for list previews.
func CleanSnippet(desc, content string, maxRunes int) string {
	raw := desc
	if strings.TrimSpace(raw) == "" {
		raw = content
	}
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	// Ensure line breaks and block closures translate into whitespace
	preprocessed := brRegex.ReplaceAllString(raw, " ")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(preprocessed))
	var text string
	if err == nil {
		text = doc.Text()
	} else {
		text = html.UnescapeString(htmlTagRegex.ReplaceAllString(preprocessed, " "))
	}
	text = strings.TrimSpace(whitespaceRegex.ReplaceAllString(text, " "))

	if maxRunes <= 0 {
		maxRunes = 200
	}
	if utf8.RuneCountInString(text) > maxRunes {
		runes := []rune(text)
		return string(runes[:maxRunes]) + "..."
	}
	return text
}

// ExtractHeroImage scans HTML for the first valid image source.
func ExtractHeroImage(htmlContent string) string {
	if htmlContent == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}
	firstImg := doc.Find("img").First()
	if firstImg.Length() > 0 {
		src, _ := firstImg.Attr("src")
		return strings.TrimSpace(src)
	}
	return ""
}

// ParseHTMLBlocks converts an HTML document or snippet into a slice of structured ArticleBlocks.
func ParseHTMLBlocks(htmlContent string) []ArticleBlock {
	trimmed := strings.TrimSpace(htmlContent)
	if trimmed == "" {
		return nil
	}

	// Plain text without HTML tags
	if !strings.Contains(trimmed, "<") || !strings.Contains(trimmed, ">") {
		paras := strings.Split(trimmed, "\n\n")
		var out []ArticleBlock
		for _, p := range paras {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, ArticleBlock{
					Type: BlockParagraph,
					Text: p,
					HTML: "<p>" + html.EscapeString(p) + "</p>",
					Hash: ParagraphHash(BlockParagraph, p, ""),
				})
			}
		}
		return out
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return []ArticleBlock{{
			Type: BlockParagraph,
			Text: trimmed,
			HTML: "<p>" + html.EscapeString(trimmed) + "</p>",
			Hash: ParagraphHash(BlockParagraph, trimmed, ""),
		}}
	}

	var blocks []ArticleBlock
	root := doc.Find("body")
	if root.Length() == 0 {
		root = doc.Selection
	}

	// Traverse top-level elements of body or article wrapper
	collectBlocks(root, &blocks)

	if len(blocks) == 0 {
		// Fallback if no block elements were parsed
		text := strings.TrimSpace(doc.Text())
		if text != "" {
			blocks = append(blocks, ArticleBlock{
				Type: BlockParagraph,
				Text: text,
				HTML: "<p>" + html.EscapeString(text) + "</p>",
				Hash: ParagraphHash(BlockParagraph, text, ""),
			})
		}
	}

	return blocks
}

func collectBlocks(sel *goquery.Selection, blocks *[]ArticleBlock) {
	sel.Children().Each(func(i int, child *goquery.Selection) {
		tag := strings.ToLower(goquery.NodeName(child))
		switch tag {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level, _ := strconv.Atoi(tag[1:])
			text := strings.TrimSpace(child.Text())
			inner, _ := child.Html()
			*blocks = append(*blocks, ArticleBlock{
				Type:  BlockHeading,
				Level: level,
				Text:  text,
				HTML:  inner,
				Hash:  ParagraphHash(BlockHeading, text, ""),
			})

		case "p":
			// If paragraph only wraps an image, treat it as an image block
			img := child.Find("img")
			if img.Length() == 1 && strings.TrimSpace(child.Text()) == "" {
				src, _ := img.Attr("src")
				alt, _ := img.Attr("alt")
				*blocks = append(*blocks, ArticleBlock{
					Type: BlockImage,
					URL:  src,
					Alt:  alt,
					Hash: ParagraphHash(BlockImage, alt, src),
				})
				return
			}
			text := strings.TrimSpace(child.Text())
			inner, _ := child.Html()
			if text != "" || child.Find("img, a").Length() > 0 {
				*blocks = append(*blocks, ArticleBlock{
					Type: BlockParagraph,
					Text: text,
					HTML: inner,
					Hash: ParagraphHash(BlockParagraph, text, ""),
				})
			}

		case "blockquote":
			text := strings.TrimSpace(child.Text())
			inner, _ := child.Html()
			*blocks = append(*blocks, ArticleBlock{
				Type: BlockBlockquote,
				Text: text,
				HTML: inner,
				Hash: ParagraphHash(BlockBlockquote, text, ""),
			})

		case "pre":
			code := child.Find("code")
			lang := ""
			class, _ := child.Attr("class")
			if match := codeLangRegex.FindStringSubmatch(class); len(match) > 1 {
				lang = match[1]
			} else if code.Length() > 0 {
				cClass, _ := code.Attr("class")
				if match := codeLangRegex.FindStringSubmatch(cClass); len(match) > 1 {
					lang = match[1]
				}
			}
			text := child.Text()
			*blocks = append(*blocks, ArticleBlock{
				Type:     BlockCode,
				Text:     text,
				Language: lang,
				Hash:     ParagraphHash(BlockCode, text, ""),
			})

		case "img":
			src, _ := child.Attr("src")
			alt, _ := child.Attr("alt")
			*blocks = append(*blocks, ArticleBlock{
				Type: BlockImage,
				URL:  src,
				Alt:  alt,
				Hash: ParagraphHash(BlockImage, alt, src),
			})

		case "figure":
			img := child.Find("img")
			src, _ := img.Attr("src")
			alt, _ := img.Attr("alt")
			caption := strings.TrimSpace(child.Find("figcaption").Text())
			if src != "" {
				*blocks = append(*blocks, ArticleBlock{
					Type:    BlockImage,
					URL:     src,
					Alt:     alt,
					Caption: caption,
					Hash:    ParagraphHash(BlockImage, alt+caption, src),
				})
			}

		case "ul", "ol":
			var items []string
			child.Find("li").Each(func(_ int, li *goquery.Selection) {
				t := strings.TrimSpace(li.Text())
				if t != "" {
					items = append(items, t)
				}
			})
			if len(items) > 0 {
				text := strings.Join(items, "\n• ")
				if tag == "ul" {
					text = "• " + text
				}
				inner, _ := child.Html()
				*blocks = append(*blocks, ArticleBlock{
					Type: BlockList,
					Text: text,
					HTML: inner,
					Hash: ParagraphHash(BlockList, text, ""),
				})
			}

		case "div", "section", "article", "main":
			// If container holds block elements, recurse into them
			if child.Children().Length() > 0 {
				collectBlocks(child, blocks)
			} else {
				text := strings.TrimSpace(child.Text())
				if text != "" {
					inner, _ := child.Html()
					*blocks = append(*blocks, ArticleBlock{
						Type: BlockParagraph,
						Text: text,
						HTML: inner,
						Hash: ParagraphHash(BlockParagraph, text, ""),
					})
				}
			}

		default:
			// For inline or unrecognized containers with meaningful text
			text := strings.TrimSpace(child.Text())
			if text != "" {
				inner, _ := child.Html()
				*blocks = append(*blocks, ArticleBlock{
					Type: BlockParagraph,
					Text: text,
					HTML: inner,
					Hash: ParagraphHash(BlockParagraph, text, ""),
				})
			}
		}
	})
}

// ExtractFullText attempts to fetch full readable content from pageURL using go-readability.
// When extraction fails, times out, or returns empty or insufficient text, it smoothly degrades
// to fallbackHTML or fallbackSnippet without returning an error to the caller or failing the pipeline.
func ExtractFullText(ctx context.Context, client *http.Client, pageURL string, fallbackHTML, fallbackSnippet string) (contentHTML string, blocks []ArticleBlock, isFullText bool, heroImage string, err error) {
	if client != nil && pageURL != "" && (strings.HasPrefix(pageURL, "http://") || strings.HasPrefix(pageURL, "https://")) {
		parsedURL, parseErr := url.Parse(pageURL)
		if parseErr == nil {
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
			if reqErr == nil {
				req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) YouTubeReader/1.0")
				req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
				resp, doErr := client.Do(req)
				if doErr == nil && resp != nil {
					defer resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						// Limit read to 10MB to avoid excessive memory usage
						limitReader := io.LimitReader(resp.Body, 10*1024*1024)
						art, readErr := readability.FromReader(limitReader, parsedURL)
						if readErr == nil {
							cleanText := strings.TrimSpace(art.TextContent)
							if utf8.RuneCountInString(cleanText) >= MinFullTextLength {
								parsed := ParseHTMLBlocks(art.Content)
								if len(parsed) > 0 {
									contentHTML = art.Content
									heroImage = art.Image
									if heroImage == "" {
										heroImage = ExtractHeroImage(contentHTML)
									}
									blocks = parsed
									isFullText = true
									return contentHTML, blocks, true, heroImage, nil
								}
							}
						}
					}
				}
			}
		}
	}

	// ==================================================================================
	// SMOOTH DEGRADATION (平滑降级): Fall back to Feed summary / description
	// ==================================================================================
	contentHTML = fallbackHTML
	if strings.TrimSpace(contentHTML) == "" {
		contentHTML = "<p>" + html.EscapeString(fallbackSnippet) + "</p>"
	}
	blocks = ParseHTMLBlocks(contentHTML)
	if len(blocks) == 0 && strings.TrimSpace(fallbackSnippet) != "" {
		blocks = []ArticleBlock{{
			Type: BlockParagraph,
			Text: fallbackSnippet,
			HTML: "<p>" + html.EscapeString(fallbackSnippet) + "</p>",
			Hash: ParagraphHash(BlockParagraph, fallbackSnippet, ""),
		}}
	}
	if heroImage == "" {
		heroImage = ExtractHeroImage(contentHTML)
	}
	isFullText = false
	return contentHTML, blocks, false, heroImage, nil
}

// CheckImageAccess checks whether an image URL is accessible over HTTP,
// returning true if the image responds with a 2xx status code.
// It verifies against anti-hotlinking (HTTP 403), missing resources (HTTP 404),
// and connection timeouts.
func CheckImageAccess(ctx context.Context, client *http.Client, imageURL string) (bool, int, error) {
	if strings.TrimSpace(imageURL) == "" {
		return false, 0, errors.New("empty image URL")
	}
	if client == nil {
		client = &http.Client{Timeout: DefaultImageTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) YouTubeReader/1.0")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, resp.StatusCode, nil
	}
	return false, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
}
