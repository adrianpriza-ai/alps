package more

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrianpriza-ai/alps/config"
)

const (
	defaultCacheDir = "/var/cache/alps/more"
	expireDays      = 90

	primaryURL  = "https://adrianpriza-ai.github.io/alps-more/main.txt"
	fallbackURL = "https://moreland.codeberg.page/alps-more/main.txt"

	downloadTimeout = 15 * time.Second
	serverTimeout   = 5 * time.Second
	maxRetries      = 3
	retryDelay      = 2 * time.Second
)

// defaultServers are the two official alps-more mirrors.
// Used as fallback when a package entry has no servers= field.
var defaultServers = []string{
	"https://adrianpriza-ai.github.io/alps-more/",
	"https://moreland.codeberg.page/alps-more/",
}

// getCacheDir returns the cache directory, respecting Termux PREFIX.
func getCacheDir() string {
	if isTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/cache/alps/more")
	}
	return defaultCacheDir
}

func getCacheFile() string      { return filepath.Join(getCacheDir(), "main.txt") }
func getLastSyncFile() string   { return filepath.Join(getCacheDir(), "last_sync") }
func getInstalledFile() string  { return filepath.Join(getCacheDir(), "installed.json") }

// ensureCacheDir creates the cache directory using sudo on Linux, directly on Termux.
func ensureCacheDir() error {
	dir := getCacheDir()
	if isTermux() {
		return os.MkdirAll(dir, 0755)
	}
	cmd := exec.Command("sudo", "mkdir", "-p", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeCacheFile writes data to a cache path using sudo on Linux, directly on Termux.
func writeCacheFile(path string, data []byte) error {
	if isTermux() {
		return os.WriteFile(path, data, 0644)
	}
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CacheStatus returns whether cache exists and whether it is expired.
func CacheStatus() (exists bool, expired bool) {
	info, err := os.Stat(getCacheFile())
	if err != nil || info.Size() == 0 {
		return false, true
	}

	if !isCacheValid() {
		return false, true
	}

	data, err := os.ReadFile(getLastSyncFile())
	if err != nil {
		return true, true
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true, true
	}

	expired = time.Since(t) > expireDays*24*time.Hour
	return true, expired
}

// isCacheValid checks that main.txt contains at least one valid [package] entry.
func isCacheValid() bool {
	data, err := os.ReadFile(getCacheFile())
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) > 2 {
			return true
		}
	}
	return false
}

// fetchResult holds the outcome of a single download attempt.
type fetchResult struct {
	data []byte
	src  string
	err  error
}

// fetchRace fires both mirrors simultaneously and returns the first
// response that contains valid content. Whichever mirror is faster wins.
func fetchRace() (data []byte, src string, err error) {
	sources := []struct{ url, name string }{
		{primaryURL, "GitHub Pages"},
		{fallbackURL, "Codeberg Pages"},
	}

	ch := make(chan fetchResult, len(sources))
	for _, s := range sources {
		go func(url, name string) {
			d, e := downloadOnce(url)
			ch <- fetchResult{d, name, e}
		}(s.url, s.name)
	}

	var lastErr error
	for range sources {
		r := <-ch
		if r.err == nil && hasValidEntries(r.data) {
			return r.data, r.src, nil
		}
		if r.err != nil {
			lastErr = r.err
		} else {
			lastErr = fmt.Errorf("invalid content from %s", r.src)
		}
	}
	return nil, "", fmt.Errorf("all sources failed: %w", lastErr)
}

// resolveServer returns the first reachable server from the list.
// If servers is empty, falls back to defaultServers.
// Fires all requests simultaneously and returns the fastest responding one.
func resolveServer(servers []string) (string, error) {
	if len(servers) == 0 {
		servers = defaultServers
	}

	type result struct {
		url string
		ok  bool
	}

	ch := make(chan result, len(servers))
	client := &http.Client{Timeout: serverTimeout}

	for _, s := range servers {
		go func(url string) {
			resp, err := client.Head(url)
			if err != nil || resp.StatusCode >= 400 {
				ch <- result{url, false}
				return
			}
			ch <- result{url, true}
		}(s)
	}

	for range servers {
		r := <-ch
		if r.ok {
			return r.url, nil
		}
	}
	return "", fmt.Errorf("no reachable server found")
}

// FetchAndCache downloads main.txt from both mirrors simultaneously,
// takes the fastest valid response, and writes it to cache.
func FetchAndCache(cfg *config.Config) error {
	content, src, err := fetchRace()
	if err != nil {
		return fmt.Errorf("failed to fetch repo: %w", err)
	}

	if !hasValidEntries(content) {
		return fmt.Errorf("downloaded content is empty or invalid — cache not updated")
	}

	fmt.Printf("  fetched from %s\n", src)

	if err := ensureCacheDir(); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}
	if err := writeCacheFile(getCacheFile(), content); err != nil {
		return fmt.Errorf("failed to write cache: %w", err)
	}

	ts := []byte(time.Now().Format(time.RFC3339))
	if err := writeCacheFile(getLastSyncFile(), ts); err != nil {
		return fmt.Errorf("failed to write sync timestamp: %w", err)
	}

	return nil
}

// ReadCache reads and validates the cached main.txt content.
func ReadCache() ([]byte, error) {
	data, err := os.ReadFile(getCacheFile())
	if err != nil {
		return nil, fmt.Errorf("cache not found, run: alps repo update")
	}
	if !hasValidEntries(data) {
		return nil, fmt.Errorf("cache is corrupt or empty, run: alps repo update")
	}
	return data, nil
}

// downloadWithRetry tries a URL up to maxRetries times with backoff.
func downloadWithRetry(url string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		data, err := downloadOnce(url)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if attempt < maxRetries {
			fmt.Printf("  attempt %d/%d failed: %v — retrying...\n", attempt, maxRetries, err)
			time.Sleep(retryDelay * time.Duration(attempt))
		}
	}
	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func downloadOnce(url string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from %s", url)
	}
	return body, nil
}

// hasValidEntries checks that content has at least one [package] header.
func hasValidEntries(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) > 2 {
			return true
		}
	}
	return false
}

// CachePath returns the cache file path (for display purposes).
func CachePath() string {
	return filepath.Clean(getCacheFile())
}
