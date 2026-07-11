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

// defaultBranches are tried when no branch is specified.
var defaultBranches = []string{"HEAD", "main", "master"}

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

// getBuildCacheRoot returns the root of the per-package build cache (~/.cache/alps/more).
func getBuildCacheRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/root", ".cache", "alps", "more")
	}
	return filepath.Join(home, ".cache", "alps", "more")
}

// CleanCache removes the build cache directory (~/.cache/alps/more).
// The index cache (/var/cache/alps/more) is NOT touched.
func CleanCache() error {
	return os.RemoveAll(getBuildCacheRoot())
}

// CleanPackageCache removes a specific package's build cache directory.
// Pattern: ~/.cache/alps/more/<package-name>/
func CleanPackageCache(pkgName string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".cache", "alps", "more", pkgName)
	return os.RemoveAll(dir)
}

// CacheDir returns the path of the build cache directory.
func CacheDir() string {
	return getBuildCacheRoot()
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

func remoteRawURL(ref RemoteRef, branch string) string {
	switch ref.Provider {
	case "github":
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/ALPSMORE", ref.RepoPath, branch)
	case "codeberg":
		return fmt.Sprintf("https://%s/%s/raw/branch/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	default:
		return fmt.Sprintf("https://%s/%s/-/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	}
}

func branchesForRef(ref RemoteRef) []string {
	if ref.Branch != "" {
		return []string{ref.Branch}
	}
	return defaultBranches
}

func fetchRemoteRef(ref RemoteRef) (*Entry, RemoteRef, error) {
	attempts := []RemoteRef{ref}

	if ref.Branch == "" {
		segments := strings.Split(ref.RepoPath, "/")
		if len(segments) >= 3 {
			branchGuess := RemoteRef{
				Provider: ref.Provider,
				Host:     ref.Host,
				RepoPath: strings.Join(segments[:len(segments)-1], "/"),
				Branch:   segments[len(segments)-1],
			}
			attempts = append(attempts, branchGuess)
		}
	}

	resolved := ref
	var lastErr error
	for _, attempt := range attempts {
		for _, branch := range branchesForRef(attempt) {
			url := remoteRawURL(attempt, branch)
			data, err := downloadOnce(url)
			if err != nil {
				lastErr = err
				continue
			}
			e, err := parseALPSMORE(data, attempt.RepoPath)
			if err != nil {
				return nil, resolved, err
			}
			resolved = attempt
			return e, resolved, nil
		}
	}
	return nil, resolved, fmt.Errorf("could not fetch ALPSMORE from %s: %w", ref.DisplayURL(), lastErr)
}

// FetchALPSMORE fetches and parses an ALPSMORE file from a GitHub repository.
// repoPath must be in the form "user/repo".
func FetchALPSMORE(repoPath string) (*Entry, error) {
	ref := RemoteRef{Provider: "github", Host: "github.com", RepoPath: repoPath}
	e, _, err := fetchRemoteRef(ref)
	return e, err
}

// FetchALPSMOREGitLab fetches and parses an ALPSMORE file from gitlab.com.
// repoPath must be in the form "user/repo".
func FetchALPSMOREGitLab(repoPath string) (*Entry, error) {
	ref := RemoteRef{Provider: "gitlab", Host: "gitlab.com", RepoPath: repoPath}
	e, _, err := fetchRemoteRef(ref)
	return e, err
}

// FetchALPSMORERemote fetches and parses an ALPSMORE file from a remote ref.
func FetchALPSMORERemote(ref RemoteRef) (*Entry, RemoteRef, error) {
	return fetchRemoteRef(ref)
}

// FetchALPSMOREFromSource fetches an ALPSMORE file using a stored source string.
func FetchALPSMOREFromSource(source string) (*Entry, error) {
	ref, err := ParseSource(source)
	if err != nil {
		return nil, err
	}
	e, _, err := fetchRemoteRef(*ref)
	return e, err
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
