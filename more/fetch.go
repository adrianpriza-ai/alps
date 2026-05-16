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
)

const (
	cacheDir     = "/var/cache/alps/more"
	cacheFile    = "/var/cache/alps/more/main.txt"
	lastSyncFile = "/var/cache/alps/more/last_sync"
	expireDays   = 90

	primaryURL  = "https://adrianpriza-ai.github.io/alps-more/main.txt"
	fallbackURL = "https://moreland.codeberg.page/alps-more/main.txt"

	downloadTimeout = 15 * time.Second
	maxRetries      = 3
	retryDelay      = 2 * time.Second
)

// CacheStatus returns whether cache exists and whether it is expired.
func CacheStatus() (exists bool, expired bool) {
	info, err := os.Stat(cacheFile)
	if err != nil || info.Size() == 0 {
		return false, true
	}

	// Validate cache content — must have at least one [package] header
	if !isCacheValid() {
		return false, true
	}

	data, err := os.ReadFile(lastSyncFile)
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
	data, err := os.ReadFile(cacheFile)
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

// FetchAndCache downloads main.txt and writes to cache.
// Tries primary (GitHub) first with retries, falls back to Codeberg.
// Validates content before overwriting existing cache.
// Requires sudo (caller must ensure privilege).
func FetchAndCache() error {
	content, err := downloadWithRetry(primaryURL)
	if err != nil {
		fmt.Printf("  %s  Primary failed (%v), trying fallback...\n", symWarn(), err)
		content, err = downloadWithRetry(fallbackURL)
		if err != nil {
			return fmt.Errorf("both sources failed: %w", err)
		}
		fmt.Println("  Using fallback (Codeberg Pages).")
	}

	// Validate before writing — never overwrite good cache with garbage
	if !hasValidEntries(content) {
		return fmt.Errorf("downloaded content is empty or invalid — cache not updated")
	}

	if err := sudoMkdir(cacheDir); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}
	if err := sudoWrite(cacheFile, content); err != nil {
		return fmt.Errorf("failed to write cache: %w", err)
	}

	ts := []byte(time.Now().Format(time.RFC3339))
	if err := sudoWrite(lastSyncFile, ts); err != nil {
		return fmt.Errorf("failed to write sync timestamp: %w", err)
	}

	return nil
}

// ReadCache reads and validates the cached main.txt content.
func ReadCache() ([]byte, error) {
	data, err := os.ReadFile(cacheFile)
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

func sudoMkdir(dir string) error {
	cmd := exec.Command("sudo", "mkdir", "-p", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sudoWrite(path string, data []byte) error {
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CachePath returns the cache file path (for display purposes).
func CachePath() string {
	return filepath.Clean(cacheFile)
}
