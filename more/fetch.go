package more

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/runner"
)

const (
	expireDays = 90

	// Security: Pin to specific release version instead of mutable branches
	// These should be updated to specific release tags/commits during releases
	primaryURL  = "https://adrianpriza-ai.github.io/alps-more/main.txt"
	fallbackURL = "https://moreland.codeberg.page/alps-more/main.txt"

	downloadTimeout = 15 * time.Second
	serverTimeout   = 5 * time.Second
	maxRetries      = 3
	retryDelay      = 2 * time.Second

	// Security: maximum response size for manifest downloads (10MB)
	maxManifestSize = 10 * 1024 * 1024
	// Security: maximum response size for script downloads (100MB)
	maxScriptSize = 100 * 1024 * 1024
	// Security: maximum response size for {DOWNLOAD} macro downloads (100MB)
	maxDownloadSize = 100 * 1024 * 1024
)

// defaultServers are the official alps-more mirrors.
var defaultServers = []string{
	"https://adrianpriza-ai.github.io/alps-more/",
	"https://moreland.codeberg.page/alps-more/",
}

// Security: Removed branch fallbacks - require explicit branch specification
// to prevent reliance on mutable references like HEAD, main, master

func getCacheFile() string     { return filepath.Join(platform.CacheDir(), "main.txt") }
func getLastSyncFile() string  { return filepath.Join(platform.CacheDir(), "last_sync") }

// installedFileOverride redirects the installed state file to another path.
// It exists for tests, which point state at a temp dir so they never touch
// (or need privileges for) the real /var/lib/alps/installed.json.
var installedFileOverride string

func getInstalledFile() string {
	if installedFileOverride != "" {
		return installedFileOverride
	}
	return filepath.Join(platform.LibDir(), "installed.json")
}

// ensureCacheDir creates the cache directory.
// Uses the runner package for consistent privilege escalation instead of
// hardcoding sudo — the runner respects platform-specific privilege policies
// (e.g. no escalation on Termux, sudo/doas/pkexec on Linux).
func ensureCacheDir() error {
	dir := platform.CacheDir()
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("mkdir", "-p", dir).WithPrivilege()
	return r.Run(context.Background(), cmd)
}

// ensureLibDir creates the state directory.
// Uses the runner package for consistent privilege escalation instead of
// hardcoding sudo — the runner respects platform-specific privilege policies
// (e.g. no escalation on Termux, sudo/doas/pkexec on Linux).
func ensureLibDir() error {
	dir := platform.LibDir()
	if installedFileOverride != "" {
		// A test redirected the state file into a temp dir; create that dir
		// directly instead of escalating to create the real lib dir.
		return os.MkdirAll(filepath.Dir(installedFileOverride), 0755)
	}
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("mkdir", "-p", dir).WithPrivilege()
	return r.Run(context.Background(), cmd)
}

// writeCacheFile writes data to a cache/state path durably and atomically.
//
// Termux and macOS keep these files in user-owned directories, so a direct
// temp-file + fsync + rename write is enough (writeFileDurable).
//
// Other platforms place them under root-owned directories (/var/cache/alps,
// /var/lib/alps), where a non-root process cannot create or fsync files
// directly. The payload is therefore written to a user-owned temp file — the
// only fd we can reliably fsync — then copied into the target directory by a
// privileged helper under a temporary name, and finally renamed within that
// same directory so readers never observe partial content. All privileged
// work goes through the runner package for consistent sudo/doas/pkexec/su
// escalation instead of hardcoded `sudo`.
func writeCacheFile(path string, data []byte) error {
	// Termux and macOS keep state in user-owned directories. A redirected
	// installed state path (installedFileOverride, tests) is a temp dir owned
	// by the current user as well — all three can write directly.
	if platform.IsTermux() || platform.IsMacOS() || installedFileOverride != "" {
		return writeFileDurable(path, data, 0644)
	}

	// Stage the payload in a user-owned temp file. Writing here means Write
	// and Sync operate on an fd we own, so a successful Sync guarantees the
	// bytes reached the disk before any privileged step runs.
	stage, err := os.CreateTemp("", "alps-write-")
	if err != nil {
		return fmt.Errorf("cannot create staging temp file: %w", err)
	}
	stagedLocal := stage.Name()
	defer os.Remove(stagedLocal) // no-op after successful copy

	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return fmt.Errorf("cannot write staging temp file: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return fmt.Errorf("cannot fsync staging temp file: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("cannot close staging temp file: %w", err)
	}

	r := runner.NewDefaultRunner(false)

	// Copy into the (root-owned) destination directory under the final
	// name's ".tmp" sibling. install(1) sets an explicit mode regardless of
	// umask, matching the previous 0644 behavior.
	staged := path + ".tmp"
	copyCmd := runner.BuildCommand("install", "-m", "0644", stagedLocal, staged).WithPrivilege()
	if err := r.Run(context.Background(), copyCmd); err != nil {
		return fmt.Errorf("privileged copy into %s failed: %w", filepath.Dir(path), err)
	}

	// Same-directory rename is atomic on POSIX filesystems, so concurrent
	// readers of path either see the old or the new file, never a mix.
	moveCmd := runner.BuildCommand("mv", staged, path).WithPrivilege()
	if err := r.Run(context.Background(), moveCmd); err != nil {
		// Best-effort cleanup of the staged copy; the old file stays intact.
		rmCmd := runner.BuildCommand("rm", "-f", staged).WithPrivilege()
		_ = r.Run(context.Background(), rmCmd)
		return fmt.Errorf("atomic move of %s into place failed: %w", staged, err)
	}
	return nil
}

// writeFileDurable writes data to path via temp file + fsync + atomic rename,
// for directories the current user can write to directly.
func writeFileDurable(path string, data []byte, perm os.FileMode) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // no-op after successful rename

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(perm); err != nil {
		tmpFile.Close()
		return err
	}
	// Ensure data is on disk before the rename publishes it.
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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
	return hasValidEntries(data)
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
		if r.err != nil {
			lastErr = r.err
			continue
		}
		if hasValidEntries(r.data) {
			return r.data, r.src, nil
		}
		lastErr = fmt.Errorf("invalid content from %s", r.src)
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

// getBuildCacheRoot returns the root of the per-package build cache
// (~/.cache/alps/more), or an error when the home directory cannot be
// determined. Guessing a home directory here would point the build cache at
// another account's files — /root for a non-root user — so callers surface the
// failure instead.
func getBuildCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for the build cache: %w", err)
	}
	return filepath.Join(home, ".cache", "alps", "more"), nil
}

// CleanCache removes the build cache directory (~/.cache/alps/more).
// The index cache (/var/cache/alps/more) is NOT touched.
func CleanCache() error {
	root, err := getBuildCacheRoot()
	if err != nil {
		return err
	}
	return os.RemoveAll(root)
}

// BuildCacheDir returns the path of the per-package build cache root
// (~/.cache/alps/more). Note: this is NOT platform.CacheDir(), which returns
// the index cache (/var/cache/alps/more).
func BuildCacheDir() (string, error) {
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

// isForgeHost checks whether a URL belongs to a supported git forge.
// Used when fetching ALPSMORE files — only forges that serve raw content are allowed.
func isForgeHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	// Require HTTPS for forge connections
	if u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	// Explicit approved hosts only (no broad suffixes)
	allowedHosts := []string{
		// Major git forges
		"github.com",
		"raw.githubusercontent.com",
		"codeberg.org",
		"gitlab.com",
		// Open-source hosting platforms
		"sr.ht",                   // SourceHut — minimalist, no GitHub dependency
		"git.savannah.gnu.org",    // GNU Project (GCC, Emacs, Bash, etc.)
		"git.kernel.org",          // Linux kernel and related projects
		"git.code.sf.net",         // SourceForge Git hosting
		"gitlab.freedesktop.org",  // Freedesktop.org (X11, Mesa, Wayland)
		"pagure.io",               // Fedora Project's forge
		"salsa.debian.org",        // Debian's GitLab instance
		"git.savannah.nongnu.org", // Non-GNU Savannah projects
		// Chinese open-source platforms
		"gitee.com",   // Gitee — Chinese GitHub equivalent
		"gitcode.com", // GitCode — CSDN's git platform
		"atomgit.com", // AtomGit — open-source by China
		// Gitea / Forgejo instances
		"gitea.com", // Gitea official SaaS
		// AI/ML platform with Git-hosted repos
		"huggingface.co", // Hugging Face Model Hub + Datasets
		// Official alps-more manifest mirrors (GitHub/Codeberg Pages).
		// Exact-host entries only — other *.github.io / *.codeberg.page
		// sites remain rejected.
		"adrianpriza-ai.github.io",
		"moreland.codeberg.page",
	}
	// Exact host matching — third-party Pages hosts (other github.io,
	// codeberg.page, etc.) are excluded because they serve static content,
	// not raw git files.
	for _, h := range allowedHosts {
		if host == h {
			return true
		}
	}
	return false
}

// isSafeDownloadURL checks whether a URL is safe for macro downloads.
// Only enforces HTTPS and no file:// — host is not restricted since
// the ALPSMORE maintainer controls which URLs are in their file.
func isSafeDownloadURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	// Only HTTPS is allowed for downloads
	if u.Scheme != "https" {
		return false
	}
	// Block file:// and other non-network schemes
	if u.Hostname() == "" {
		return false
	}
	return true
}

func downloadOnce(rawURL string) ([]byte, error) {
	return downloadOnceWithSizeLimit(rawURL, maxManifestSize)
}

func downloadOnceWithSizeLimit(rawURL string, maxSize int64) ([]byte, error) {
	return fetchBytes(rawURL, downloadTimeout, maxSize, func(u string) error {
		if !isForgeHost(u) {
			return fmt.Errorf("ALPSMORE fetch: disallowed host/scheme: %s", u)
		}
		return nil
	})
}

// fetchBytes is the single capped HTTP fetch for the package. Every remote
// fetch funnels through it — manifest/ALPSMORE downloads, script downloads
// and macro downloads — so the read-with-size-limit logic lives in one place
// instead of drifting between call sites (the source of the unbounded
// DOWNLOAD regression). validate enforces the caller's URL policy (e.g.
// HTTPS-only or a forge host allowlist) before any request is made.
func fetchBytes(rawURL string, timeout time.Duration, maxSize int64, validate func(string) error) ([]byte, error) {
	if err := validate(rawURL); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	// Security: limit response size to prevent denial of service. Read one
	// extra byte so an exactly-maxSize body is accepted while a larger one is
	// detected as oversized (a plain LimitReader(maxSize) would make the two
	// indistinguishable and reject valid max-size responses).
	limitedReader := io.LimitReader(resp.Body, maxSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from %s", rawURL)
	}
	// Check if we exceeded the size limit
	if int64(len(body)) > maxSize {
		return nil, fmt.Errorf("response too large from %s (exceeds %d bytes)", rawURL, maxSize)
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

func remoteRawURL(ref RemoteRef, branch string) string {
	switch ref.Provider {
	case "github":
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/ALPSMORE", ref.RepoPath, branch)
	case "codeberg":
		return fmt.Sprintf("https://%s/%s/raw/branch/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	case "gitee":
		return fmt.Sprintf("https://%s/%s/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	case "gitcode":
		return fmt.Sprintf("https://%s/%s/-/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	case "atomgit":
		return fmt.Sprintf("https://%s/%s/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	case "gitea":
		return fmt.Sprintf("https://%s/%s/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	case "sourcehut":
		// SourceHut serves raw content on git.sr.ht, not sr.ht
		return fmt.Sprintf("https://git.sr.ht/%s/raw/branch/%s/ALPSMORE", ref.RepoPath, branch)
	case "huggingface":
		return fmt.Sprintf("https://%s/%s/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	default:
		return fmt.Sprintf("https://%s/%s/-/raw/%s/ALPSMORE", ref.Host, ref.RepoPath, branch)
	}
}

func branchesForRef(ref RemoteRef) []string {
	// Security: Require explicit branch specification - no fallbacks
	if ref.Branch == "" {
		return nil
	}
	return []string{ref.Branch}
}

func fetchRemoteRef(ref RemoteRef) (*Entry, RemoteRef, error) {
	// Security: Require explicit branch specification
	if ref.Branch == "" {
		return nil, ref, fmt.Errorf("branch must be specified for remote references (format: provider:repo/branch or provider:repo@branch)")
	}

	resolved := ref
	branches := branchesForRef(ref)
	if len(branches) == 0 {
		return nil, ref, fmt.Errorf("no valid branches found for %s", ref.DisplayURL())
	}

	var lastErr error
	for _, branch := range branches {
		url := remoteRawURL(ref, branch)
		data, err := downloadOnce(url)
		if err != nil {
			lastErr = err
			continue
		}
		e, err := parseALPSMORE(data, ref.RepoPath)
		if err != nil {
			return nil, resolved, err
		}
		return e, resolved, nil
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

	repoName := repoPath
	if idx := strings.LastIndex(repoPath, "/"); idx >= 0 {
		repoName = repoPath[idx+1:]
	}

	// Entry has a [name] header — return it directly.
	if e := pickEntry(entries, repoName); e != nil {
		return e, nil
	}

	// No [name] header — inject repo name as fallback.
	injected := append([]byte("["+repoName+"]\n"), data...)
	entries, err = Parse(injected)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ALPSMORE from %s: %w", repoPath, err)
	}
	if e := pickEntry(entries, repoName); e != nil {
		return e, nil
	}

	return nil, fmt.Errorf("ALPSMORE file from github.com/%s is empty or has no valid content", repoPath)
}

// pickEntry selects the single entry an ALPSMORE file describes, returning nil
// for an empty map. A file with several [name] sections would otherwise resolve
// to a random one, since Go randomizes map iteration order: the section named
// after the repository wins, and any other tie is broken by sorting the names.
func pickEntry(entries map[string]*Entry, repoName string) *Entry {
	if len(entries) == 0 {
		return nil
	}
	if e, ok := entries[repoName]; ok {
		return e
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return entries[names[0]]
}

// --- Remote reference parsing (from remote.go) ---

// RemoteRef identifies an ALPSMORE file hosted on a git forge.
type RemoteRef struct {
	Provider string // github, gitlab, codeberg
	Host     string // e.g. github.com, gitlab.archlinux.org
	RepoPath string // namespace/project path
	Branch   string // empty = try default branches
}

func defaultHost(provider string) string {
	switch provider {
	case "github":
		return "github.com"
	case "gitlab":
		return "gitlab.com"
	case "codeberg":
		return "codeberg.org"
	case "huggingface":
		return "huggingface.co"
	default:
		return ""
	}
}

func providerFromHost(host string) string {
	host = strings.ToLower(host)
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return "github"
	case host == "codeberg.org" || strings.HasSuffix(host, ".codeberg.org"):
		return "codeberg"
	case host == "gitee.com":
		return "gitee"
	case host == "gitcode.com":
		return "gitcode"
	case host == "atomgit.com":
		return "atomgit"
	case host == "gitea.com":
		return "gitea"
	case host == "sr.ht" || strings.HasSuffix(host, ".sr.ht"):
		return "sourcehut"
	case host == "huggingface.co":
		return "huggingface"
	default:
		// Self-hosted GitLab and other GitLab-compatible forges.
		return "gitlab"
	}
}

// ParseRemoteURL parses a user-facing remote URL
func ParseRemoteURL(input string) (*RemoteRef, error) {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimSuffix(input, "/")

	var explicitBranch string
	if at := strings.LastIndex(input, "@"); at > 0 {
		explicitBranch = input[at+1:]
		if explicitBranch == "" {
			return nil, fmt.Errorf("invalid remote URL %q: empty branch after @", input)
		}
		input = input[:at]
	}

	slash := strings.Index(input, "/")
	if slash < 0 {
		return nil, fmt.Errorf("invalid remote URL %q: missing repository path", input)
	}

	host := input[:slash]
	path := strings.Trim(input[slash+1:], "/")
	if path == "" {
		return nil, fmt.Errorf("invalid remote URL %q: missing repository path", input)
	}

	provider := providerFromHost(host)
	if provider == "" {
		return nil, fmt.Errorf("unsupported git host %q", host)
	}

	ref := &RemoteRef{
		Provider: provider,
		Host:     host,
		RepoPath: path,
		Branch:   explicitBranch,
	}
	return ref, nil
}

// ParseSource decodes a stored source string
func ParseSource(source string) (*RemoteRef, error) {
	colon := strings.Index(source, ":")
	if colon < 0 {
		return nil, fmt.Errorf("invalid source %q", source)
	}

	prefix := source[:colon]
	rest := source[colon+1:]
	if rest == "" {
		return nil, fmt.Errorf("invalid source %q: missing repository path", source)
	}

	var provider, host string
	if at := strings.Index(prefix, "@"); at >= 0 {
		provider = prefix[:at]
		host = prefix[at+1:]
	} else {
		provider = prefix
		host = defaultHost(provider)
	}

	if host == "" {
		return nil, fmt.Errorf("unknown source provider in %q", source)
	}

	branch := ""
	if branchAt := strings.LastIndex(rest, "@"); branchAt >= 0 {
		branch = rest[branchAt+1:]
		if branch == "" {
			return nil, fmt.Errorf("invalid source %q: empty branch", source)
		}
		rest = rest[:branchAt]
	}

	if rest == "" {
		return nil, fmt.Errorf("invalid source %q: missing repository path", source)
	}

	return &RemoteRef{
		Provider: provider,
		Host:     host,
		RepoPath: rest,
		Branch:   branch,
	}, nil
}

// Source returns the canonical stored source string for this ref.
func (r RemoteRef) Source() string {
	prefix := r.Provider
	if r.Host != "" && r.Host != defaultHost(r.Provider) {
		prefix = r.Provider + "@" + r.Host
	}

	s := prefix + ":" + r.RepoPath
	if r.Branch != "" {
		s += "@" + r.Branch
	}
	return s
}

// DisplayURL returns a user-facing host/path[/branch] string.
func (r RemoteRef) DisplayURL() string {
	s := r.Host + "/" + r.RepoPath
	if r.Branch != "" {
		s += "/" + r.Branch
	}
	return s
}

// IsRemoteURL reports whether input looks like a remote git forge URL.
func IsRemoteURL(input string) bool {
	ref, err := ParseRemoteURL(input)
	return err == nil && ref != nil
}
