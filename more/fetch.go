package more

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"bytes"
	"path/filepath"
	"time"
)

const (
	cacheDir      = "/var/cache/alps/more"
	cacheFile     = "/var/cache/alps/more/main.txt"
	lastSyncFile  = "/var/cache/alps/more/last_sync"
	expireDays    = 90

	primaryURL  = "https://moreland.codeberg.page/alps-more/main.txt"
	fallbackURL = "https://adrianpriza-ai.github.io/alps-more/main.txt"
)

// CacheStatus returns whether cache exists, and whether it is expired.
func CacheStatus() (exists bool, expired bool) {
	info, err := os.Stat(cacheFile)
	if err != nil || info.Size() == 0 {
		return false, true
	}

	data, err := os.ReadFile(lastSyncFile)
	if err != nil {
		return true, true
	}

	t, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		return true, true
	}

	expired = time.Since(t) > expireDays*24*time.Hour
	return true, expired
}

// FetchAndCache downloads main.txt and writes to cache.
// Tries primary (Codeberg) first, falls back to GitHub Pages.
// Requires sudo (caller must ensure privilege).
func FetchAndCache() error {
	content, err := download(primaryURL)
	if err != nil {
		fmt.Printf("  Primary failed (%v), trying fallback...\n", err)
		content, err = download(fallbackURL)
		if err != nil {
			return fmt.Errorf("both sources failed: %w", err)
		}
		fmt.Println("  Using fallback (GitHub Pages).")
	}

	cmd := exec.Command("sudo", "mkdir", "-p", cacheDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	write := exec.Command("sudo", "tee", cacheFile)
	write.Stdin = bytes.NewReader(content)
	write.Stdout = io.Discard
	write.Stderr = os.Stderr
	if err := write.Run(); err != nil {
		return fmt.Errorf("failed to write cache: %w", err)
	}

	ts := time.Now().Format(time.RFC3339)
	writeTs := exec.Command("sudo", "tee", lastSyncFile)
	writeTs.Stdin = strings.NewReader(ts)
	writeTs.Stdout = io.Discard
	writeTs.Stderr = os.Stderr
	if err := writeTs.Run(); err != nil {
		return fmt.Errorf("failed to write sync timestamp: %w", err)
	}

	return nil
}

// ReadCache reads the cached main.txt content.
func ReadCache() ([]byte, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("cache not found, run: alps repo update")
	}
	return data, nil
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
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

// CachePath returns the cache file path (for display purposes).
func CachePath() string {
	return filepath.Clean(cacheFile)
}
