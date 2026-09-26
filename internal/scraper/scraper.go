package scraper

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type Embed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site,omitempty"`
	Image       string `json:"image,omitempty"`
	Video       string `json:"video,omitempty"`
}

type cachedEmbed struct {
	embed   Embed
	expires time.Time
}
type Scraper struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cachedEmbed
}

func New() *Scraper {
	return &Scraper{client: HTTPClient(3 * time.Second), cache: make(map[string]cachedEmbed)}
}

var linkPattern = regexp.MustCompile(`https://[^\s<>"']+`)

func Links(content string) []string {
	candidates := linkPattern.FindAllString(content, 5)
	links := make([]string, 0, len(candidates))
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		candidate = strings.TrimRight(candidate, ".,;:!?)\u005d}")
		if _, err := ValidateURL(candidate); err == nil && !seen[candidate] {
			links = append(links, candidate)
			seen[candidate] = true
		}
	}
	return links
}

func (s *Scraper) Fetch(ctx context.Context, raw string) (Embed, error) {
	parsed, err := ValidateURL(raw)
	if err != nil {
		return Embed{}, err
	}
	s.mu.Lock()
	cached, ok := s.cache[raw]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.embed, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Embed{}, err
	}
	request.Header.Set("User-Agent", "BrrpBrrp-LinkPreview/1.0")
	response, err := s.client.Do(request)
	if err != nil {
		return Embed{}, err
	}
	defer response.Body.Close()
	embed := Embed{URL: raw, Title: parsed.Hostname()}
	if response.StatusCode != http.StatusOK {
		return embed, nil
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(contentType, "image/gif"), strings.HasPrefix(contentType, "image/png"), strings.HasPrefix(contentType, "image/jpeg"), strings.HasPrefix(contentType, "image/webp"):
		embed.Image = raw
	case strings.HasPrefix(contentType, "text/html"):
		embed = Parse(io.LimitReader(response.Body, 2<<20), response.Request.URL)
		embed.URL = raw
		if embed.Image != "" && !PublicURL(ctx, embed.Image) {
			embed.Image = ""
		}
		if embed.Video != "" && !PublicURL(ctx, embed.Video) {
			embed.Video = ""
		}
	}
	s.mu.Lock()
	if len(s.cache) >= 256 {
		for key := range s.cache {
			delete(s.cache, key)
			break
		}
	}
	s.cache[raw] = cachedEmbed{embed: embed, expires: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
	return embed, nil
}

func Parse(reader io.Reader, base *url.URL) Embed {
	result := Embed{URL: base.String(), Title: base.Hostname()}
	tokenizer := html.NewTokenizer(reader)
	metadata := make(map[string]string)
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if token.Data == "title" && tokenizer.Next() == html.TextToken && metadata["og:title"] == "" {
			result.Title = string(tokenizer.Text())
		}
		if token.Data != "meta" {
			continue
		}
		var key, value string
		for _, attribute := range token.Attr {
			if attribute.Key == "property" || attribute.Key == "name" {
				key = strings.ToLower(attribute.Val)
			}
			if attribute.Key == "content" {
				value = attribute.Val
			}
		}
		if _, exists := metadata[key]; !exists {
			metadata[key] = value
		}
	}
	if metadata["og:title"] != "" {
		result.Title = metadata["og:title"]
	}
	result.Title = truncate(result.Title, 160)
	result.Description = truncate(metadata["og:description"], 300)
	if result.Description == "" {
		result.Description = truncate(metadata["description"], 300)
	}
	result.Site = truncate(metadata["og:site_name"], 80)
	result.Image = resolveMedia(base, metadata["og:image"])
	host := strings.ToLower(base.Hostname())
	if host == "tenor.com" || strings.HasSuffix(host, ".tenor.com") || host == "giphy.com" || strings.HasSuffix(host, ".giphy.com") {
		video := metadata["og:video:secure_url"]
		if video == "" {
			video = metadata["og:video"]
		}
		if strings.Contains(video, ".mp4") {
			result.Video = resolveMedia(base, video)
		}
	}
	return result
}

func resolveMedia(base *url.URL, raw string) string {
	if raw == "" {
		return ""
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(reference).String()
	if _, err := ValidateURL(resolved); err != nil {
		return ""
	}
	return resolved
}

func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
