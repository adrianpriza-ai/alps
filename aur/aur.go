// Package aur implements AUR (Arch User Repository) integration: RPC search
// and info lookups, dependency-resolved installs via the built-in makepkg
// pipeline or an installed AUR helper (paru/yay), local PKGBUILD builds, and
// ABS fetching.
package aur

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/user"
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
// Tests can replace this variable to inject a mock transport or point at a
// fake RPC server: save the original, swap in a test client, and defer
// restoring it.
var aurHTTPClient = &http.Client{Timeout: 15 * time.Second}

// Package represents an AUR package.
type Package struct {
	Name        string   `json:"Name"`
	PackageBase string   `json:"PackageBase"`
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
	Conflicts   []string `json:"Conflicts"`
	Provides    []string `json:"Provides"`
	Replaces    []string `json:"Replaces"`
	Keywords    []string `json:"Keywords"`
	Groups      []string `json:"Groups"`
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

// maxQueryLen is the soft limit on the search query length passed to the
// AUR RPC. The AUR endpoint has a documented max arg length; queries
// exceeding this would produce a malformed URL and HTTP 400.
const maxQueryLen = 200

// Search searches AUR sorted by votes.
func Search(query string) ([]Package, error) {
	query = strings.TrimSpace(query)
	if len(query) > maxQueryLen {
		return nil, fmt.Errorf("search query too long (%d chars, max %d)", len(query), maxQueryLen)
	}
	if err := validatePkgName(query); err != nil {
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
// It searches across name, description, keywords, provides, conflicts, groups, and maintainer.
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
			// Check if word matches in any searchable field
			matches := strings.Contains(strings.ToLower(p.Name), w) ||
				strings.Contains(strings.ToLower(p.Description), w) ||
				fieldContains(p.Keywords, w) ||
				fieldContains(p.Provides, w) ||
				fieldContains(p.Conflicts, w) ||
				fieldContains(p.Groups, w) ||
				strings.Contains(strings.ToLower(p.Maintainer), w)

			if matches {
				filtered = append(filtered, p)
			}
		}
		results = filtered
	}
	return results, nil
}

// fieldContains checks if a search term matches any string in a field array
func fieldContains(field []string, term string) bool {
	for _, item := range field {
		if strings.Contains(strings.ToLower(item), term) {
			return true
		}
	}
	return false
}

// ErrPkgNotFound is returned by Info when a package does not exist in AUR.
// Callers should use errors.Is to check for this condition.
var ErrPkgNotFound = fmt.Errorf("not found in AUR")

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
		return nil, fmt.Errorf("%w: package %q", ErrPkgNotFound, name)
	}
	return &result.Results[0], nil
}

// maxInfoBatchWorkers caps concurrent AUR info lookups to avoid
// overwhelming the AUR RPC endpoint with unbounded parallel requests.
const maxInfoBatchWorkers = 8

// InfoBatch fetches info for multiple packages in parallel using a bounded
// worker pool. NotFound results are silently skipped (the caller can check
// per-package); other errors abort the batch.
func InfoBatch(names []string) (map[string]*Package, error) {
	if err := validatePkgNames(names); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	results := make(map[string]*Package)
	var wg sync.WaitGroup
	var firstErr error
	sem := make(chan struct{}, min(maxInfoBatchWorkers, len(names)))

	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pkg, err := Info(n)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if !errors.Is(err, ErrPkgNotFound) && firstErr == nil {
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

// PrintSearchResult writes a search result to w.
func PrintSearchResult(w io.Writer, idx int, p Package, source string) {
	ood := ""
	if p.OutOfDate != 0 {
		ood = " [out-of-date]"
	}
	orphan := ""
	if p.Maintainer == "" {
		orphan = " (orphaned)"
	}
	fmt.Fprintf(w, "%s/%s %s%s%s\n    %s\n",
		source, p.Name, p.Version, ood, orphan, p.Description)
}

// PrintPackageInfo writes package details to w.
func PrintPackageInfo(w io.Writer, p *Package) {
	ood := ""
	if p.OutOfDate != 0 {
		ood = " [out-of-date]"
	}
	fmt.Fprintf(w, "\naur/%s %s%s\n", p.Name, p.Version, ood)
	if p.Description != "" {
		fmt.Fprintf(w, "    %s\n", p.Description)
	}
	if len(p.License) > 0 {
		fmt.Fprintf(w, "    License     : %s\n", strings.Join(p.License, ", "))
	}
	if p.Maintainer != "" {
		fmt.Fprintf(w, "    Maintainer  : %s\n", p.Maintainer)
	} else {
		fmt.Fprintf(w, "    Maintainer  : (orphaned)\n")
	}
	fmt.Fprintf(w, "    Votes       : %d\n", p.Votes)
	if p.URL != "" {
		fmt.Fprintf(w, "    URL         : %s\n", p.URL)
	}
	if len(p.Depends) > 0 {
		fmt.Fprintf(w, "    Depends     : %s\n", strings.Join(p.Depends, "  "))
	}
	if len(p.MakeDepends) > 0 {
		fmt.Fprintf(w, "    MakeDepends : %s\n", strings.Join(p.MakeDepends, "  "))
	}
	fmt.Fprintln(w)
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

// readYesNo prompts for yes/no. Empty input uses the default.
// After one invalid answer it re-prompts; persistent non-y/n input
// defaults to false (safe/conservative).
func readYesNo(prompt string, defaultYes bool) bool {
	if defaultYes {
		fmt.Printf("%s [Y/n] ", prompt)
	} else {
		fmt.Printf("%s [y/N] ", prompt)
	}
	line := readLine()
	if line == "" {
		return defaultYes
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		fmt.Printf("  Please enter y or n: ")
		line = readLine()
		switch strings.ToLower(line) {
		case "y", "yes":
			return true
		default:
			return false
		}
	}
}

// stripVerConstraint removes version constraints from a dependency string.
// Arch package names are case-sensitive, so this function does not
// normalise case — "Foo" and "foo" are distinct packages.
func stripVerConstraint(dep string) string {
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if idx := strings.Index(dep, op); idx != -1 {
			return dep[:idx]
		}
	}
	return dep
}

// dedup removes duplicate strings from in, preserving the order of first
// occurrence. The output order matches input order because we iterate `in`
// sequentially and mark entries in `seen` as we go — a future refactor that
// iterates `seen` instead would break this guarantee.
//
// Case is preserved as-is (Arch package names are case-sensitive), so
// "Foo" and "foo" are treated as distinct entries.
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

// aurCacheDir returns the build cache directory for a specific AUR package,
// performing path-traversal validation to prevent escaping the cache root.
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

// AURCacheRoot returns the root directory for AUR build caches.
// When running under sudo or doas, it resolves the invoking user's home
// directory so cache files are not written into /root/.cache/alps/aur with
// root ownership.
func AURCacheRoot() (string, error) {
	// If running under sudo or doas, resolve the invoking user's home directory
	// so cache files are not written into /root/.cache/alps/aur with root ownership.
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return filepath.Join(u.HomeDir, ".cache", "alps", "aur"), nil
		}
	}
	if doasUser := os.Getenv("DOAS_USER"); doasUser != "" && doasUser != "root" {
		if u, err := user.Lookup(doasUser); err == nil && u.HomeDir != "" {
			return filepath.Join(u.HomeDir, ".cache", "alps", "aur"), nil
		}
	}
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
