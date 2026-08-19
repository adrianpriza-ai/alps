// Package aur implements AUR (Arch User Repository) integration: RPC search
// and info lookups, dependency-resolved installs via the built-in makepkg
// pipeline or an installed AUR helper (paru/yay), local PKGBUILD builds, and
// ABS fetching.
package aur

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adrianpriza-ai/alps/config"
)

// Types and constants

const (
	aurRPCBase    = "https://aur.archlinux.org/rpc/v5/"
	absGitLab     = "https://gitlab.archlinux.org/archlinux/packaging/packages/"
	aurMaxRetries = 5 // maximum number of AUR RPC fetch attempts
)

// aurHTTPClient is a shared HTTP client for AUR requests.
var aurHTTPClient = &http.Client{Timeout: 15 * time.Second}

// Package represents an AUR package.
type Package struct {
	Name        string   `json:"Name"`
	Version     string   `json:"Version"`
	Description string   `json:"Description"`
	URL         string   `json:"URL"`
	Votes       int      `json:"NumVotes"`
	Popularity  float64  `json:"Popularity"`
	Maintainer  string   `json:"Maintainer"`
	URLPath     string   `json:"URLPath"`
	OutOfDate   int64    `json:"OutOfDate"`
	Depends     []string `json:"Depends"`
	MakeDepends []string `json:"MakeDepends"`
	License     []string `json:"License"`
}

type rpcResponse struct {
	Results []Package `json:"results"`
	Error   string    `json:"error"`
}

// RPC — fetch, search, info

// fetchRPC performs an AUR RPC request with up to aurMaxRetries attempts.
// Network errors and HTTP 429/5xx responses are retried with exponential
// backoff (base 1 s, doubled each attempt) plus ±25 % jitter.
// Bad request errors (4xx other than 429) and AUR-level errors are returned
// immediately without retrying.
func fetchRPC(rawURL string) (*rpcResponse, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("AUR request build failed (%s): %w", rawURL, err)
	}

	var lastErr error
	backoff := time.Second // initial wait before first retry

	for attempt := 1; attempt <= aurMaxRetries; attempt++ {
		attemptReq := req.Clone(req.Context())

		resp, err := aurHTTPClient.Do(attemptReq)
		if err != nil {
			lastErr = fmt.Errorf("AUR request failed (%s): %w", rawURL, err)
		} else {
			switch {
			case resp.StatusCode == http.StatusOK:
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					lastErr = fmt.Errorf("failed to read AUR response: %w", readErr)
					break
				}
				var result rpcResponse
				if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
					return nil, fmt.Errorf("failed to parse AUR response: %w", jsonErr)
				}
				if result.Error != "" {
					return nil, fmt.Errorf("AUR error: %s", result.Error)
				}
				return &result, nil

			case resp.StatusCode == http.StatusTooManyRequests ||
				resp.StatusCode >= http.StatusInternalServerError:
				resp.Body.Close()
				lastErr = fmt.Errorf("AUR returned HTTP %d for %s", resp.StatusCode, rawURL)

			default:
				resp.Body.Close()
				return nil, fmt.Errorf("AUR returned HTTP %d for %s", resp.StatusCode, rawURL)
			}
		}

		if attempt < aurMaxRetries {
			jitter := time.Duration(float64(backoff) * (0.75 + 0.5*rand.Float64()))
			time.Sleep(jitter)
			backoff *= 2
		}
	}

	return nil, fmt.Errorf("AUR request failed after %d attempts (%s): %w", aurMaxRetries, rawURL, lastErr)
}

// Search searches AUR sorted by votes.
func Search(query string) ([]Package, error) {
	if err := validatePkgName(strings.TrimSpace(query)); err != nil {
		return nil, fmt.Errorf("invalid search query: %w", err)
	}
	u, err := url.JoinPath(aurRPCBase, "search", query)
	if err != nil {
		return nil, fmt.Errorf("failed to build search URL: %w", err)
	}
	result, err := fetchRPC(u)
	if err != nil {
		return nil, err
	}
	sort.Slice(result.Results, func(i, j int) bool {
		return result.Results[i].Votes > result.Results[j].Votes
	})
	return result.Results, nil
}

// SearchNarrow searches AUR using all words in query.
func SearchNarrow(query string) ([]Package, error) {
	words := strings.Fields(query)
	if len(words) == 0 {
		return nil, fmt.Errorf("empty search query")
	}
	results, err := Search(words[0])
	if err != nil {
		return nil, err
	}
	for _, word := range words[1:] {
		w := strings.ToLower(word)
		var filtered []Package
		for _, p := range results {
			if strings.Contains(strings.ToLower(p.Name), w) ||
				strings.Contains(strings.ToLower(p.Description), w) {
				filtered = append(filtered, p)
			}
		}
		results = filtered
	}
	return results, nil
}

// Info fetches package info by name.
func Info(name string) (*Package, error) {
	if err := validatePkgName(name); err != nil {
		return nil, err
	}
	u, err := url.JoinPath(aurRPCBase, "info", name)
	if err != nil {
		return nil, fmt.Errorf("failed to build info URL: %w", err)
	}
	result, err := fetchRPC(u)
	if err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("package %q not found in AUR", name)
	}
	return &result.Results[0], nil
}

// InfoBatch fetches info for multiple packages in parallel.
func InfoBatch(names []string) (map[string]*Package, error) {
	if err := validatePkgNames(names); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	results := make(map[string]*Package)
	var wg sync.WaitGroup
	var firstErr error

	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			pkg, err := Info(n)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if !strings.Contains(err.Error(), "not found in AUR") && firstErr == nil {
					firstErr = err
				}
				return
			}
			results[n] = pkg
		}(name)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, fmt.Errorf("batch info fetch failed: %w", firstErr)
	}
	return results, nil
}

// Exists checks if a package exists in AUR.
func Exists(name string) bool {
	_, err := Info(name)
	return err == nil
}

// PrintSearchResult prints a search result.
func PrintSearchResult(idx int, p Package, source string) {
	ood := ""
	if p.OutOfDate != 0 {
		ood = " [out-of-date]"
	}
	orphan := ""
	if p.Maintainer == "" {
		orphan = " (orphaned)"
	}
	fmt.Printf("%s/%s %s%s%s\n    %s\n",
		source, p.Name, p.Version, ood, orphan, p.Description)
}

// PrintPackageInfo prints package details.
func PrintPackageInfo(p *Package) {
	ood := ""
	if p.OutOfDate != 0 {
		ood = " [out-of-date]"
	}
	fmt.Printf("\naur/%s %s%s\n", p.Name, p.Version, ood)
	if p.Description != "" {
		fmt.Printf("    %s\n", p.Description)
	}
	if len(p.License) > 0 {
		fmt.Printf("    License     : %s\n", strings.Join(p.License, ", "))
	}
	if p.Maintainer != "" {
		fmt.Printf("    Maintainer  : %s\n", p.Maintainer)
	} else {
		fmt.Printf("    Maintainer  : (orphaned)\n")
	}
	fmt.Printf("    Votes       : %d\n", p.Votes)
	if p.URL != "" {
		fmt.Printf("    URL         : %s\n", p.URL)
	}
	if len(p.Depends) > 0 {
		fmt.Printf("    Depends     : %s\n", strings.Join(p.Depends, "  "))
	}
	if len(p.MakeDepends) > 0 {
		fmt.Printf("    MakeDepends : %s\n", strings.Join(p.MakeDepends, "  "))
	}
	fmt.Println()
}

// Utilities — validation, I/O, caching helpers

// validPkgName matches allowed Arch package name characters.
// See: https://wiki.archlinux.org/title/Package_naming_guidelines
var validPkgName = regexp.MustCompile(`^[a-zA-Z0-9@._+\-]+$`)

// validatePkgName checks that a package name contains only safe characters.
func validatePkgName(name string) error {
	if name == "" {
		return fmt.Errorf("empty package name")
	}
	if !validPkgName.MatchString(name) {
		return fmt.Errorf("invalid package name: %q", name)
	}
	return nil
}

// validatePkgNames validates a slice of package names.
func validatePkgNames(names []string) error {
	for _, n := range names {
		if err := validatePkgName(n); err != nil {
			return err
		}
	}
	return nil
}

// editorIsSafe checks that the EDITOR value is a safe single binary path —
// rejects values containing shell metacharacters or flags.
func editorIsSafe(editor string) bool {
	if editor == "" || strings.ContainsAny(editor, "|;&$`\\'\"()<>!") {
		return false
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 || strings.HasPrefix(parts[0], "-") {
		return false
	}
	if len(parts) > 1 {
		return false
	}
	return true
}

func configSymbols() (ok, warn, arrow string) {
	s := config.Load().Style
	return s.SymOK, s.SymWarn, s.SymArrow
}

func warnStderr(format string, a ...any) {
	cfg := config.Load()
	s := cfg.Style
	text := fmt.Sprintf(format, a...)
	fmt.Fprintf(os.Stderr, "  %s%s%s  %s%s\n", s.ColorWarning, s.SymWarn, s.ColorReset, text, s.ColorReset)
}

// readLine reads a line from stdin.
func readLine() string {
	scanner := bufio.NewReader(os.Stdin)
	line, _ := scanner.ReadString('\n')
	return strings.TrimSpace(line)
}

// readYesNo prompts for yes/no.
func readYesNo(prompt string, defaultYes bool) (bool, error) {
	if defaultYes {
		fmt.Printf("%s [Y/n] ", prompt)
	} else {
		fmt.Printf("%s [y/N] ", prompt)
	}
	line := readLine()
	if line == "" {
		return defaultYes, nil
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		fmt.Printf("  Please enter y or n: ")
		line = readLine()
		switch strings.ToLower(line) {
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
}

func stripVerConstraint(dep string) string {
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if idx := strings.Index(dep, op); idx != -1 {
			return dep[:idx]
		}
	}
	return dep
}

func dedup(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func aurCacheDir(pkgName string) (string, error) {
	root, err := AURCacheRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, pkgName)
	clean := filepath.Clean(dir)
	if !strings.HasPrefix(clean, root+string(filepath.Separator)) && clean != root {
		return "", fmt.Errorf("path traversal detected: %q escapes cache root", pkgName)
	}
	return clean, nil
}

func AURCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "alps", "aur"), nil
}

// CleanCache removes the build cache.
func CleanCache(pkgName string) error {
	if pkgName != "" {
		if err := validatePkgName(pkgName); err != nil {
			return err
		}
	}
	ok, _, _ := configSymbols()
	var target string
	var err error
	if pkgName == "" {
		target, err = AURCacheRoot()
	} else {
		target, err = aurCacheDir(pkgName)
	}
	if err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to remove cache: %w", err)
	}
	fmt.Printf("  %s  cache removed: %s\n", ok, target)
	return nil
}
