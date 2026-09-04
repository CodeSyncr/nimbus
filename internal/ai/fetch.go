package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

/*
Fetching a page.

The agent could already reach the network through bash, but only as raw curl:
the model got a wall of markup and had to build sed or python pipelines to make
it readable, which is fragile and wastes the context it needs for the work.

fetch_url does the job properly — one request, HTML reduced to readable text,
bounded in size. Two formats, because the two real uses want different things:

	text  reading — documentation, an article, an API reference
	html  studying construction — how a page you are asked to imitate is
	      actually built, its markup and the stylesheets it pulls in
*/

const (
	// fetchTimeout bounds a single request.
	fetchTimeout = 30 * time.Second
	// maxFetchBytes caps the download, so a huge asset cannot exhaust memory.
	maxFetchBytes = 5 << 20
	// maxFetchChars caps what is handed to the model.
	maxFetchChars = 40000
	// maxRedirects stops a redirect loop.
	maxRedirects = 5
)

var fetchHTTPClient = &http.Client{
	Timeout: fetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	},
}

// FetchURL retrieves a page and returns it as readable text, or as raw markup
// when format is "html".
func (t *ToolExecutor) FetchURL(ctx context.Context, rawURL, format string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("that is not a valid URL: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("only http and https are supported, got %q", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	// Some sites serve a different page, or none, without a normal UA.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; nimbus-ai/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	resp, err := fetchHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not fetch %s: %w", parsed.Host, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", parsed.Host, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s returned %d %s", parsed.Host, resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	contentType := resp.Header.Get("Content-Type")
	if isBinaryContentType(contentType) {
		return fmt.Sprintf("%s is %s (%d bytes) — binary content is not shown.", parsed, contentType, len(body)), nil
	}

	content := string(body)
	if strings.EqualFold(strings.TrimSpace(format), "html") {
		return header(parsed.String(), contentType, len(body)) + truncateOutput(content, maxFetchChars), nil
	}

	text := htmlToText(content)
	if strings.TrimSpace(text) == "" {
		text = "(the page returned no readable text; fetch it with format \"html\" to see the markup)"
	}
	out := header(parsed.String(), contentType, len(body))
	if title := pageTitle(content); title != "" {
		out += "Title: " + title + "\n"
	}
	return out + "\n" + truncateOutput(text, maxFetchChars), nil
}

func header(url, contentType string, size int) string {
	return fmt.Sprintf("URL: %s\nType: %s (%d bytes)\n", url, strings.TrimSpace(contentType), size)
}

var (
	// Go's regexp has no backreferences, so each element is spelled out
	// rather than matched against a captured tag name.
	reScriptStyle = regexp.MustCompile(`(?is)<script[^>]*>.*?</script\s*>|<style[^>]*>.*?</style\s*>|<noscript[^>]*>.*?</noscript\s*>|<svg[^>]*>.*?</svg\s*>|<template[^>]*>.*?</template\s*>`)
	reComment     = regexp.MustCompile(`(?s)<!--.*?-->`)
	reBlockClose  = regexp.MustCompile(`(?i)</(p|div|section|article|header|footer|li|tr|h[1-6]|blockquote|pre)>`)
	reBreak       = regexp.MustCompile(`(?i)<(br|hr)\s*/?>`)
	reTag         = regexp.MustCompile(`<[^>]+>`)
	reBlankLines  = regexp.MustCompile(`\n{3,}`)
	reSpaces      = regexp.MustCompile(`[ \t]{2,}`)
	reTitle       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// htmlToText reduces markup to the words a reader would see, keeping the line
// breaks that carry structure.
func htmlToText(html string) string {
	s := reScriptStyle.ReplaceAllString(html, " ")
	s = reComment.ReplaceAllString(s, " ")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reBlockClose.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = unescapeEntities(s)

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(reSpaces.ReplaceAllString(line, " "))
		out = append(out, line)
	}
	return strings.TrimSpace(reBlankLines.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

// pageTitle pulls the document title, which orients the model quickly.
func pageTitle(html string) string {
	if m := reTitle.FindStringSubmatch(html); len(m) == 2 {
		return strings.TrimSpace(unescapeEntities(reTag.ReplaceAllString(m[1], "")))
	}
	return ""
}

// unescapeEntities decodes the handful of entities that matter for reading.
func unescapeEntities(s string) string {
	return strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&apos;", "'", "&mdash;", "—", "&ndash;", "–",
	).Replace(s)
}

// isBinaryContentType reports whether a response is not worth rendering.
func isBinaryContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "text/"),
		strings.Contains(ct, "json"),
		strings.Contains(ct, "xml"),
		strings.Contains(ct, "javascript"),
		strings.Contains(ct, "x-www-form-urlencoded"):
		return false
	case ct == "":
		return false
	}
	return true
}
