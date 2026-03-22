package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxImageBytes  = 5 * 1024 * 1024 // 5 MB hard cap
	minImageBytes  = 1024             // 1 KB minimum — reject empty/stub files
	maxVQDBody     = 256 * 1024
	maxAPIBody     = 1 * 1024 * 1024
)

// allowedMIMEToExt maps permitted image MIME types to file extensions.
var allowedMIMEToExt = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// magicSignatures holds the leading byte sequences that identify each image format.
var magicSignatures = []struct {
	ext    string
	header []byte
}{
	{".jpg", []byte{0xFF, 0xD8, 0xFF}},
	{".png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}},
	{".gif", []byte{0x47, 0x49, 0x46, 0x38}}, // GIF8
	{".webp", []byte{0x52, 0x49, 0x46, 0x46}}, // RIFF (WebP also checks bytes 8-11)
}

// privateIPNets lists CIDR ranges that must never be contacted (SSRF prevention).
var privateIPNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // link-local
		"100.64.0.0/10",  // shared address space (RFC 6598)
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 ULA
		"fe80::/10",      // IPv6 link-local
	} {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("bad private CIDR: " + cidr)
		}
		privateIPNets = append(privateIPNets, ipnet)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, block := range privateIPNets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// safeDialContext resolves the hostname and refuses connections to private IPs.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed for %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %q", host)
	}

	for _, a := range addrs {
		if isPrivateIP(a.IP) {
			return nil, fmt.Errorf("refusing to connect: %q resolves to private/reserved address %s", host, a.IP)
		}
	}

	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].IP.String(), port))
}

// validateURLScheme ensures only http/https URLs are followed.
func validateURLScheme(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("unsupported URL scheme %q (only http/https permitted)", u.Scheme)
	}
}

// checkMagicBytes verifies that data begins with the expected image signature.
func checkMagicBytes(data []byte, ext string) bool {
	for _, sig := range magicSignatures {
		if sig.ext != ext {
			continue
		}
		if len(data) < len(sig.header) {
			return false
		}
		if !bytes.Equal(data[:len(sig.header)], sig.header) {
			return false
		}
		// WebP: RIFF????WEBP — also verify bytes 8-11
		if ext == ".webp" {
			return len(data) >= 12 && bytes.Equal(data[8:12], []byte("WEBP"))
		}
		return true
	}
	return false
}

type ImageSearcher struct {
	client    *http.Client
	imagesDir string
}

func NewImageSearcher(timeout time.Duration, imagesDir string) *ImageSearcher {
	jar, _ := cookiejar.New(nil) // allows DDG session cookies to flow from VQD fetch to image API
	transport := &http.Transport{
		DialContext:         safeDialContext,
		DisableKeepAlives:   true,
		MaxIdleConns:        10,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		Jar:       jar,
		// Validate redirect targets so an open redirect can't bypass SSRF checks.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateURLScheme(req.URL)
		},
	}
	return &ImageSearcher{client: client, imagesDir: imagesDir}
}

// SearchAndDownloadRecipeImage searches for images, tries each candidate URL in order,
// and returns a local path once one downloads successfully. Falls back to the first
// remote URL if every download attempt fails.
func (s *ImageSearcher) SearchAndDownloadRecipeImage(ctx context.Context, recipeTitle, filename string) (string, error) {
	candidates, err := s.searchRecipeImageCandidates(ctx, recipeTitle)
	if err != nil {
		return "", err
	}

	if s.imagesDir == "" {
		return candidates[0], nil
	}

	if err := os.MkdirAll(s.imagesDir, 0755); err != nil {
		log.Printf("Image download: could not create images dir: %v", err)
		return candidates[0], nil
	}

	for i, remoteURL := range candidates {
		localURL, err := s.tryDownload(ctx, remoteURL, filename)
		if err != nil {
			log.Printf("Image download: candidate %d/%d skipped (%s): %v", i+1, len(candidates), remoteURL, err)
			continue
		}
		return localURL, nil
	}

	log.Printf("Image download: all %d candidate(s) failed for %q, using remote URL", len(candidates), recipeTitle)
	return candidates[0], nil
}

// tryDownload fetches remoteURL and saves it to disk. Returns the local /images/ path on
// success, or an error if validation or the transfer fails.
func (s *ImageSearcher) tryDownload(ctx context.Context, remoteURL, filename string) (string, error) {
	u, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if err := validateURLScheme(u); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", remoteURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Validate Content-Type before reading the body.
	ct := strings.ToLower(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	ct = strings.TrimSpace(ct)
	ext, ok := allowedMIMEToExt[ct]
	if !ok {
		// Fall back to URL-derived extension only when Content-Type is absent/generic.
		if ct != "" && ct != "application/octet-stream" && ct != "binary/octet-stream" {
			return "", fmt.Errorf("rejected Content-Type %q (not an allowed image type)", ct)
		}
		ext = extFromURL(remoteURL)
		if ext == "" {
			return "", fmt.Errorf("cannot determine image type: no usable Content-Type or extension")
		}
	}

	// Reject oversized responses before downloading using Content-Length.
	if cl := resp.ContentLength; cl > maxImageBytes {
		return "", fmt.Errorf("Content-Length %d exceeds maximum allowed size %d", cl, maxImageBytes)
	}

	// Read at most maxImageBytes+1 so we can detect files that exceed the limit.
	limited := io.LimitReader(resp.Body, maxImageBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > maxImageBytes {
		return "", fmt.Errorf("image exceeds maximum allowed size of %d bytes", maxImageBytes)
	}
	if int64(len(data)) < minImageBytes {
		return "", fmt.Errorf("image too small (%d bytes), likely invalid", len(data))
	}

	// Verify the actual bytes match the declared image format.
	if !checkMagicBytes(data, ext) {
		return "", fmt.Errorf("file content does not match expected image format %q", ext)
	}

	localPath := filepath.Join(s.imagesDir, filename+ext)
	absImagesDir, err := filepath.Abs(s.imagesDir)
	if err != nil {
		return "", fmt.Errorf("resolving images dir: %w", err)
	}
	absLocalPath, err := filepath.Abs(localPath)
	if err != nil {
		return "", fmt.Errorf("resolving local path: %w", err)
	}
	if !strings.HasPrefix(absLocalPath, absImagesDir+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid filename: path escapes images directory")
	}
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return "/images/" + filename + ext, nil
}

func extFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	lower := strings.ToLower(u.Path)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif"} {
		if strings.HasSuffix(lower, ext) {
			if ext == ".jpeg" {
				return ".jpg"
			}
			return ext
		}
	}
	return ""
}

// SearchRecipeImage returns the first usable image URL for the given recipe title.
func (s *ImageSearcher) SearchRecipeImage(ctx context.Context, recipeTitle string) (string, error) {
	candidates, err := s.searchRecipeImageCandidates(ctx, recipeTitle)
	if err != nil {
		return "", err
	}
	return candidates[0], nil
}

// searchRecipeImageCandidates returns all usable image URLs found for the recipe title.
func (s *ImageSearcher) searchRecipeImageCandidates(ctx context.Context, recipeTitle string) ([]string, error) {
	query := recipeTitle + " food recipe"

	vqd, err := s.fetchVQD(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fetching vqd token: %w", err)
	}

	return s.fetchImageCandidates(ctx, query, vqd)
}

// fetchVQD retrieves the vqd token DuckDuckGo requires for image searches.
func (s *ImageSearcher) fetchVQD(ctx context.Context, query string) (string, error) {
	u := "https://duckduckgo.com/?q=" + url.QueryEscape(query) + "&iax=images&ia=images"

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "none")
	req.Header.Set("sec-fetch-user", "?1")
	req.Header.Set("upgrade-insecure-requests", "1")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVQDBody))
	if err != nil {
		return "", err
	}

	// vqd is embedded in the page in several possible forms.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`vqd="([^"]+)"`),
		regexp.MustCompile(`vqd=([0-9a-zA-Z%-]+)[&"'\s]`),
		regexp.MustCompile(`"vqd"\s*:\s*"([^"]+)"`),
	}
	for _, re := range patterns {
		if m := re.FindSubmatch(body); len(m) > 1 {
			return string(m[1]), nil
		}
	}

	return "", fmt.Errorf("vqd token not found in DuckDuckGo response")
}

// fetchImageCandidates calls DDG's image JSON endpoint and returns all suitable image URLs.
func (s *ImageSearcher) fetchImageCandidates(ctx context.Context, query, vqd string) ([]string, error) {
	u := fmt.Sprintf(
		"https://duckduckgo.com/i.js?q=%s&o=json&vqd=%s&f=,,,,,&p=1",
		url.QueryEscape(query), url.QueryEscape(vqd),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://duckduckgo.com/")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("x-requested-with", "XMLHttpRequest")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("image search returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Results []struct {
			Image     string `json:"image"`
			Thumbnail string `json:"thumbnail"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBody)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding image results: %w", err)
	}

	var candidates []string
	for _, r := range result.Results {
		img := r.Image
		if img == "" {
			img = r.Thumbnail
		}
		if img == "" {
			continue
		}

		// Validate scheme before adding to candidates.
		parsed, err := url.Parse(img)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			continue
		}

		lower := strings.ToLower(img)
		if strings.HasSuffix(lower, ".svg") || strings.Contains(lower, "favicon") {
			continue
		}
		if r.Width > 0 && r.Width < 100 {
			continue
		}
		candidates = append(candidates, img)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no image found for %q", query)
	}
	return candidates, nil
}
