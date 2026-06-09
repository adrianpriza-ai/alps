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
	defaultLibDir   = "/var/lib/alps"
	expireDays      = 90

	primaryURL  = "https://adrianpriza-ai.github.io/alps-more/main.txt"
	fallbackURL = "https://moreland.codeberg.page/alps-more/main.txt"

	downloadTimeout = 15 * time.Second
	serverTimeout   = 5 * time.Second
	maxRetries      = 3
	retryDelay      = 2 * time.Second
)

// defaultServers are the official alps-more mirrors.
var defaultServers = []string{
	"https://adrianpriza-ai.github.io/alps-more/",
	"https://moreland.codeberg.page/alps-more/",
}

// githubRawBase is the base URL for raw GitHub content.
const githubRawBase = "https://raw.githubusercontent.com"

// gitlabRawBase is the base URL for raw GitLab content.
const gitlabRawBase = "https://gitlab.com"

// getCacheDir returns the cache directory (expendable — main.txt, last_sync).
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

// getLibDir returns the state directory (persistent — installed.json).
// /var/lib/ is the FHS standard for application state that must survive cache cleans.
// On Termux: $PREFIX/var/lib/alps (same rationale, different root).
func getLibDir() string {
	if isTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/lib/alps")
	}
	return defaultLibDir
}

func getCacheFile() string     { return filepath.Join(getCacheDir(), "main.txt") }
func getLastSyncFile() string  { return filepath.Join(getCacheDir(), "last_sync") }
func getInstalledFile() string { return filepath.Join(getLibDir(), "installed.json") }

// ensureCacheDir creates the cache directory.
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

// ensureLibDir creates the state directory.
func ensureLibDir() error {
	dir := getLibDir()
	if isTermux() {
		return os.MkdirAll(dir, 0755)
	}
	cmd := exec.Command("sudo", "mkdir", "-p", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeCacheFile writes data to a cache path.
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

// CacheStatus returns cache existence and expiration.
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

// isCacheValid checks for valid entries in main.txt.
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

// fetchResult holds download outcome.
type fetchResult struct {
	data []byte
	src  string
	err  error
}

// fetchRace gets data from fastest mirror.
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

// resolveServer returns the first reachable server.
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

// FetchAndCache downloads and caches main.txt.
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

// ReadCache reads and validates the cache.
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

// downloadWithRetry downloads with retries.
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

// hasValidEntries checks for package entries.
func hasValidEntries(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) > 2 {
			return true
		}
	}
	return false
}

// CachePath returns the cache file path.
func CachePath() string {
	return filepath.Clean(getCacheFile())
}

// FetchALPSMORE fetches and parses an ALPSMORE file from a GitHub repository.
// repoPath must be in the form "user/repo".
// Tries HEAD, then main, then master branches in order.
// If the file has no [name] header, the repo name is used as fallback.
func FetchALPSMORE(repoPath string) (*Entry, error) {
	branches := []string{"HEAD", "main", "master"}
	var lastErr error
	for _, branch := range branches {
		url := fmt.Sprintf("%s/%s/%s/ALPSMORE", githubRawBase, repoPath, branch)
		data, err := downloadOnce(url)
		if err != nil {
			lastErr = err
			continue
		}
		e, err := parseALPSMORE(data, repoPath)
		if err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, fmt.Errorf("could not fetch ALPSMORE from github.com/%s: %w", repoPath, lastErr)
}

// FetchALPSMOREGitLab fetches and parses an ALPSMORE file from a GitLab repository.
// repoPath must be in the form "user/repo".
// Tries HEAD, then main, then master branches in order.
// If the file has no [name] header, the repo name is used as fallback.
func FetchALPSMOREGitLab(repoPath string) (*Entry, error) {
	branches := []string{"HEAD", "main", "master"}
	var lastErr error
	for _, branch := range branches {
		url := fmt.Sprintf("%s/%s/-/raw/%s/ALPSMORE", gitlabRawBase, repoPath, branch)
		data, err := downloadOnce(url)
		if err != nil {
			lastErr = err
			continue
		}
		e, err := parseALPSMORE(data, repoPath)
		if err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, fmt.Errorf("could not fetch ALPSMORE from gitlab.com/%s: %w", repoPath, lastErr)
}

// FetchALPSMOREFromSource fetches an ALPSMORE file using a source string.
// source must be in the form "github:user/repo" or "gitlab:user/repo".
func FetchALPSMOREFromSource(source string) (*Entry, error) {
	switch {
	case strings.HasPrefix(source, "github:"):
		return FetchALPSMORE(strings.TrimPrefix(source, "github:"))
	case strings.HasPrefix(source, "gitlab:"):
		return FetchALPSMOREGitLab(strings.TrimPrefix(source, "gitlab:"))
	default:
		return nil, fmt.Errorf("unknown source provider in %q", source)
	}
}

// parseALPSMORE parses raw ALPSMORE content as a single entry.
// If no [name] header is present, falls back to the repo name.
func parseALPSMORE(data []byte, repoPath string) (*Entry, error) {
	entries, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ALPSMORE from %s: %w", repoPath, err)
	}

	// Entry has a [name] header — return it directly.
	if len(entries) > 0 {
		for _, e := range entries {
			return e, nil
		}
	}

	// No [name] header — inject repo name as fallback.
	repoName := repoPath
	if idx := strings.LastIndex(repoPath, "/"); idx >= 0 {
		repoName = repoPath[idx+1:]
	}
	injected := append([]byte("["+repoName+"]\n"), data...)
	entries, err = Parse(injected)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ALPSMORE from %s: %w", repoPath, err)
	}
	for _, e := range entries {
		return e, nil
	}

	return nil, fmt.Errorf("ALPSMORE file from github.com/%s is empty or has no valid content", repoPath)
}
