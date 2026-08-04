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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/priv"
)

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
	// Reject empty or values with shell-dangerous characters.
	if editor == "" || strings.ContainsAny(editor, "|;&$`\\'\"()<>!") {
		return false
	}
	// Reject if it looks like it has flags (starts with -).
	parts := strings.Fields(editor)
	if len(parts) == 0 || strings.HasPrefix(parts[0], "-") {
		return false
	}
	// Must be a single command or an absolute/relative path with no arguments.
	// We allow at most the binary name — any flags are rejected.
	if len(parts) > 1 {
		return false
	}
	return true
}

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

// installPlan holds resolved install work.
type installPlan struct {
	AURPackages   []*Package // in build order (deps first)
	RepoDeps      []string   // installed from pacman before building
	MakeDepsAdded []string   // makedeps not pre-installed; offered for removal after
}

type builtPackage struct {
	Path    string
	Name    string
	Version string
	ModTime time.Time
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

// DetectHelper returns "yay" if available.
func DetectHelper() string {
	if _, err := exec.LookPath("yay"); err == nil {
		return "yay"
	}
	return ""
}

// checkRequirements verifies that required tools are available.
// It uses pacman -Qq (package DB) as the primary check, with exec.LookPath
// as a fallback — OR logic means either one confirming presence is sufficient.
// For base-devel: it has been a real package since late 2022; we also fall
// back to checking "fakeroot" in PATH as a secondary sentinel.
func checkRequirements(needGit, needBaseDevel bool) error {
	var missing []string
	if needGit {
		if !pkgInstalled("git") && !hasInPath("git") {
			missing = append(missing, "git")
		}
	}
	if needBaseDevel {
		// base-devel is a proper package since Arch 2022; fakeroot is the fallback sentinel.
		if !pkgInstalled("base-devel") && !hasInPath("fakeroot") {
			missing = append(missing, "base-devel")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	installHint := "sudo pacman -S " + strings.Join(missing, " ")
	return fmt.Errorf(
		"missing required tools: %s\n  Run: %s",
		strings.Join(missing, ", "),
		installHint,
	)
}

// pkgInstalled reports whether a package is registered in the pacman DB.
func pkgInstalled(name string) bool {
	return exec.Command("pacman", "-Qq", name).Run() == nil
}

// hasInPath reports whether a binary is reachable via PATH.
func hasInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// fetchRPC performs an AUR RPC request with up to aurMaxRetries attempts.
// Network errors and HTTP 429/5xx responses are retried with exponential
// backoff (base 1 s, doubled each attempt) plus ±25 % jitter.
// Bad request errors (4xx other than 429) and AUR-level errors are returned
// immediately without retrying.
func fetchRPC(rawURL string) (*rpcResponse, error) {
	// Building the request can only fail for malformed URLs — no point retrying.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("AUR request build failed (%s): %w", rawURL, err)
	}

	var lastErr error
	backoff := time.Second // initial wait before first retry

	for attempt := 1; attempt <= aurMaxRetries; attempt++ {
		// Clone the request so the body (none here) is reusable across retries.
		attemptReq := req.Clone(req.Context())

		resp, err := aurHTTPClient.Do(attemptReq)
		if err != nil {
			// Transport / network error — always retry.
			lastErr = fmt.Errorf("AUR request failed (%s): %w", rawURL, err)
		} else {
			// Got a response; check the status code.
			switch {
			case resp.StatusCode == http.StatusOK:
				// Success path — read, parse, and return.
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					lastErr = fmt.Errorf("failed to read AUR response: %w", readErr)
					// Treat a read error as transient and fall through to retry.
					break
				}
				var result rpcResponse
				if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
					// Malformed JSON is unlikely to be transient.
					return nil, fmt.Errorf("failed to parse AUR response: %w", jsonErr)
				}
				if result.Error != "" {
					// AUR application-level error — not transient.
					return nil, fmt.Errorf("AUR error: %s", result.Error)
				}
				return &result, nil

			case resp.StatusCode == http.StatusTooManyRequests ||
				resp.StatusCode >= http.StatusInternalServerError:
				// Rate-limited or server-side error — retry.
				resp.Body.Close()
				lastErr = fmt.Errorf("AUR returned HTTP %d for %s", resp.StatusCode, rawURL)

			default:
				// 4xx client error (other than 429) — not worth retrying.
				resp.Body.Close()
				return nil, fmt.Errorf("AUR returned HTTP %d for %s", resp.StatusCode, rawURL)
			}
		}

		if attempt < aurMaxRetries {
			// Exponential backoff with ±25 % jitter — retries are silent.
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
				// "not found in AUR" is expected for removed/renamed packages;
				// only propagate real transport or server errors.
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

// Install installs AUR packages.
func Install(pkgNames []string, noConfirm bool) error {
	if len(pkgNames) == 0 {
		return nil
	}
	if err := validatePkgNames(pkgNames); err != nil {
		return err
	}

	if DetectHelper() == "yay" {
		return installWithYay(pkgNames, noConfirm)
	}

	// Installing from AUR requires git (to clone) and base-devel (to build).
	if err := checkRequirements(true, true); err != nil {
		return err
	}

	plan, err := buildInstallPlan(pkgNames)
	if err != nil {
		return err
	}
	if err := collectUserInputs(plan, noConfirm); err != nil {
		return err
	}
	return executeInstallPlan(plan, noConfirm)
}

func installWithYay(pkgNames []string, noConfirm bool) error {
	_, _, arrow := configSymbols()
	args := append([]string{"-S"}, pkgNames...)
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	fmt.Printf("  %s using yay: %s\n\n", arrow, strings.Join(pkgNames, " "))
	if err := priv.Invalidate(); err != nil {
		warnStderr("failed to invalidate sudo credentials before yay: %v", err)
	}
	cmd := exec.Command("yay", args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yay failed: %w", err)
	}
	return nil
}

// pkgCache caches pacman query results.
type pkgCache struct {
	mu        sync.Mutex
	installed map[string]bool
	inRepo    map[string]bool
	provided  map[string]bool
}

func newPkgCache() *pkgCache {
	return &pkgCache{
		installed: make(map[string]bool),
		inRepo:    make(map[string]bool),
		provided:  make(map[string]bool),
	}
}

func (c *pkgCache) IsInstalled(name string) bool {
	c.mu.Lock()
	if v, ok := c.installed[name]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := isInstalled(name)
	c.mu.Lock()
	c.installed[name] = v
	c.mu.Unlock()
	return v
}

func (c *pkgCache) InRepo(name string) bool {
	c.mu.Lock()
	if v, ok := c.inRepo[name]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := inPacmanRepo(name)
	c.mu.Lock()
	c.inRepo[name] = v
	c.mu.Unlock()
	return v
}

func (c *pkgCache) HasProvider(name string) bool {
	c.mu.Lock()
	if v, ok := c.provided[name]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := hasProvider(name)
	c.mu.Lock()
	c.provided[name] = v
	c.mu.Unlock()
	return v
}

// buildInstallPlan resolves the dep tree.
func buildInstallPlan(names []string) (*installPlan, error) {
	_, warn, _ := configSymbols()
	visited := make(map[string]bool)
	var ordered []*Package
	var repoDeps []string
	cache := newPkgCache()

	for _, name := range names {
		pkg, err := Info(name)
		if err != nil {
			return nil, err
		}
		if err := resolveDepTree(pkg, visited, &ordered, &repoDeps, warn, cache); err != nil {
			return nil, err
		}
	}

	// Track which makedeps aren't currently installed (for post-build offer)
	var makeAdded []string
	for _, pkg := range ordered {
		for _, dep := range pkg.MakeDepends {
			n := stripVerConstraint(dep)
			if !cache.IsInstalled(n) {
				makeAdded = append(makeAdded, n)
			}
		}
	}

	return &installPlan{
		AURPackages:   ordered,
		RepoDeps:      dedup(repoDeps),
		MakeDepsAdded: dedup(makeAdded),
	}, nil
}

// resolveDepTree resolves dependencies recursively.
func resolveDepTree(pkg *Package, visited map[string]bool, ordered *[]*Package, repoDeps *[]string, warn string, cache *pkgCache) error {
	if visited[pkg.Name] {
		return nil
	}
	visited[pkg.Name] = true

	allDeps := append(pkg.Depends, pkg.MakeDepends...)
	for _, dep := range unsatisfiedDeps(allDeps) {
		name := stripVerConstraint(dep)

		// Already handled above us in the tree?
		if visited[name] {
			continue
		}

		// Official repo? (cached)
		if cache.InRepo(name) {
			*repoDeps = append(*repoDeps, name)
			continue
		}

		// Provided by something already installed? (cached)
		if cache.HasProvider(name) {
			continue
		}

		// AUR — find the right package (exact match or user-selected provider)
		depPkg, err := findAURPackage(name)
		if err != nil {
			return fmt.Errorf("dep %q required by %s: %w", name, pkg.Name, err)
		}

		fmt.Printf("  %s  AUR dep: %s (required by %s)\n", warn, depPkg.Name, pkg.Name)
		if err := resolveDepTree(depPkg, visited, ordered, repoDeps, warn, cache); err != nil {
			return err
		}
	}

	*ordered = append(*ordered, pkg)
	return nil
}

// findAURPackage looks up a dep by name in AUR.
func findAURPackage(name string) (*Package, error) {
	pkg, err := Info(name)
	if err == nil {
		return pkg, nil
	}

	// Exact lookup failed — search and let the user choose a provider
	results, serr := Search(name)
	if serr != nil || len(results) == 0 {
		return nil, fmt.Errorf("not found in AUR")
	}

	limit := 5
	if len(results) < limit {
		limit = len(results)
	}
	fmt.Printf("\n  :: No exact AUR match for %q — select a provider:\n", name)
	for i, p := range results[:limit] {
		fmt.Printf("  %d) aur/%s %s — %s\n", i+1, p.Name, p.Version, p.Description)
	}
	fmt.Print("  0) abort\n  Choice: ")

	line := readLine()
	var choice int
	if _, err := fmt.Sscan(line, &choice); err != nil || choice < 1 || choice > limit {
		return nil, fmt.Errorf("no provider selected for %q", name)
	}
	selected := results[choice-1]
	return &selected, nil
}

func printInstallSummary(plan *installPlan, warn string) {
	if len(plan.RepoDeps) > 0 {
		fmt.Printf("  :: Repo dependencies: %s\n", strings.Join(plan.RepoDeps, "  "))
	}

	fmt.Printf("\n  :: AUR packages to build (%d):\n", len(plan.AURPackages))
	for i, p := range plan.AURPackages {
		ood := ""
		if p.OutOfDate != 0 {
			ood = " [out-of-date]"
		}
		fmt.Printf("  %d. aur/%s %s%s\n", i+1, p.Name, p.Version, ood)
		if p.Description != "" {
			fmt.Printf("     %s\n", p.Description)
		}
	}

	for _, p := range plan.AURPackages {
		if p.OutOfDate != 0 {
			fmt.Printf("\n  %s  %s is flagged out-of-date\n", warn, p.Name)
		}
	}
}

func reviewAURPKGBUILDs(plan *installPlan, arrow string) error {
	if ok, err := readYesNo(fmt.Sprintf("  %s Review PKGBUILDs before building?", arrow), false); err != nil {
		return err
	} else if ok {
		for _, p := range plan.AURPackages {
			pkgDir, err := aurCacheDir(p.Name)
			if err != nil {
				return err
			}
			if err := cloneAUR(p, pkgDir); err != nil {
				return err
			}
			if err := reviewPKGBUILD(filepath.Join(pkgDir, "PKGBUILD")); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectUserInputs gathers confirmations before building.
func collectUserInputs(plan *installPlan, noConfirm bool) error {
	_, warn, arrow := configSymbols()

	// Concise AUR trust notice — same trust model as yay.
	fmt.Println()
	fmt.Printf("  %s  AUR packages are user-produced content. Use at your own risk.\n", warn)
	fmt.Println()

	printInstallSummary(plan, warn)

	// --noconfirm: show the plan (above) but skip interactive prompts.
	if noConfirm {
		return nil
	}

	if err := reviewAURPKGBUILDs(plan, arrow); err != nil {
		return err
	}

	// Single proceed prompt for everything
	label := "Proceed with install?"
	if len(plan.AURPackages) > 1 {
		label = fmt.Sprintf("Proceed with all %d builds?", len(plan.AURPackages))
	}
	ok, err := readYesNo(fmt.Sprintf("  %s %s", arrow, label), true)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("install cancelled by user")
	}
	return nil
}

func installRepoDeps(deps []string, arrow string) error {
	if len(deps) == 0 {
		return nil
	}
	fmt.Printf("\n  %s installing repo deps: %s\n\n", arrow, strings.Join(deps, " "))
	args := append([]string{"pacman", "-S", "--noconfirm", "--needed"}, deps...)
	cmd, err := priv.Command(args...)
	if err != nil {
		return fmt.Errorf("privilege escalation failed: %w", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install repo deps: %w", err)
	}
	return nil
}

func removeMakeDeps(makeDeps []string, noConfirm bool, okStr string) error {
	if len(makeDeps) == 0 {
		return nil
	}
	var toRemove []string
	for _, dep := range makeDeps {
		if isInstalled(dep) {
			toRemove = append(toRemove, dep)
		}
	}
	if len(toRemove) == 0 {
		return nil
	}
	fmt.Printf("\n  :: Build dependencies installed during build: %s\n", strings.Join(toRemove, "  "))
	if !noConfirm {
		rmOK, err := readYesNo("  Remove build dependencies?", false)
		if err != nil {
			return err
		}
		if !rmOK {
			return nil
		}
	}
	rmArgs := append([]string{"pacman", "-Rns", "--noconfirm"}, toRemove...)
	rmCmd, err := priv.Command(rmArgs...)
	if err != nil {
		return fmt.Errorf("privilege escalation failed for makedep removal: %w", err)
	}
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	rmCmd.Stdin = os.Stdin
	if err := rmCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove build deps: %w", err)
	}
	fmt.Printf("  %s  build dependencies removed\n", okStr)
	return nil
}

func cleanupBuildCaches(builtDirs []string, noConfirm bool, okStr string) {
	if noConfirm || len(builtDirs) == 0 {
		return
	}
	fmt.Printf("\n  :: Build caches:\n")
	for _, dir := range builtDirs {
		fmt.Printf("     %s\n", dir)
	}
	keep, err := readYesNo("  Keep build caches?", false)
	if err == nil && !keep {
		for _, dir := range builtDirs {
			os.RemoveAll(dir)
		}
		fmt.Printf("  %s  caches removed\n", okStr)
	}
}

// executeInstallPlan installs repo deps and builds AUR packages.
func executeInstallPlan(plan *installPlan, noConfirm bool) error {
	ok, _, arrow := configSymbols()

	if err := installRepoDeps(plan.RepoDeps, arrow); err != nil {
		return err
	}

	var builtDirs []string
	// Build each AUR package in dep order
	for _, pkg := range plan.AURPackages {
		pkgDir, err := buildAndInstall(pkg, noConfirm)
		if err != nil {
			return fmt.Errorf("failed to build %s: %w", pkg.Name, err)
		}
		builtDirs = append(builtDirs, pkgDir)
	}

	// Offer makedep removal once, at the very end
	if err := removeMakeDeps(plan.MakeDepsAdded, noConfirm, ok); err != nil {
		return err
	}

	// Cache cleanup at the end
	cleanupBuildCaches(builtDirs, noConfirm, ok)
	return nil
}

// buildAndInstall clones and builds pkg.
func buildAndInstall(pkg *Package, noConfirm bool) (string, error) {
	ok, _, arrow := configSymbols()

	if err := validatePkgName(pkg.Name); err != nil {
		return "", fmt.Errorf("refusing to build package with invalid name %q: %w", pkg.Name, err)
	}

	pkgDir, err := aurCacheDir(pkg.Name)
	if err != nil {
		return "", fmt.Errorf("failed to resolve cache dir: %w", err)
	}

	if cached, err := findReusableBuiltPackage(pkg, pkgDir); err != nil {
		return "", err
	} else if cached != nil {
		useCached := true
		if !noConfirm {
			fmt.Printf("\n  :: Found built package for aur/%s %s\n", pkg.Name, pkg.Version)
			fmt.Printf("     %s\n", cached.Path)
			var promptErr error
			useCached, promptErr = readYesNo(fmt.Sprintf("  %s Install this existing build instead of rebuilding?", arrow), true)
			if promptErr != nil {
				return "", promptErr
			}
		}
		if useCached {
			if err := installBuiltPackage(cached.Path, noConfirm); err != nil {
				return "", err
			}
			fmt.Printf("  %s  %s installed from existing build\n", ok, pkg.Name)
			return pkgDir, nil
		}
	}

	if err := cloneAUR(pkg, pkgDir); err != nil {
		return "", err
	}

	fmt.Printf("\n  %s building %s %s...\n\n", arrow, pkg.Name, pkg.Version)
	args := []string{"-si", "--noconfirm"} // noconfirm here: user already confirmed above
	if err := priv.Invalidate(); err != nil {
		warnStderr("failed to invalidate sudo credentials before makepkg: %v", err)
	}
	makepkg := exec.Command("makepkg", args...)
	makepkg.Env = append(os.Environ(), "TERM=xterm-256color")
	makepkg.Dir = pkgDir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return "", fmt.Errorf("makepkg failed: %w", err)
	}
	fmt.Printf("  %s  %s installed\n", ok, pkg.Name)

	return pkgDir, nil
}

func findReusableBuiltPackage(pkg *Package, pkgDir string) (*builtPackage, error) {
	entries, err := os.ReadDir(pkgDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read build cache %s: %w", pkgDir, err)
	}

	var matches []builtPackage
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.Contains(name, ".pkg.tar.") || strings.HasSuffix(name, ".sig") {
			continue
		}
		path := filepath.Join(pkgDir, name)
		built, err := inspectBuiltPackage(path)
		if err != nil {
			continue
		}
		if built.Name != pkg.Name || built.Version != pkg.Version {
			continue
		}
		if info, err := entry.Info(); err == nil {
			built.ModTime = info.ModTime()
		}
		matches = append(matches, *built)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModTime.After(matches[j].ModTime)
	})
	return &matches[0], nil
}

func inspectBuiltPackage(path string) (*builtPackage, error) {
	out, err := exec.Command("pacman", "-Qp", path).Output()
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return nil, fmt.Errorf("unexpected pacman -Qp output for %s", path)
	}
	return &builtPackage{
		Path:    path,
		Name:    fields[0],
		Version: fields[1],
	}, nil
}

func installBuiltPackage(path string, _ bool) error {
	args := []string{"pacman", "-U", "--noconfirm", path}
	cmd, err := priv.Command(args...)
	if err != nil {
		return fmt.Errorf("privilege escalation failed: %w", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pacman -U failed for %s: %w", path, err)
	}
	return nil
}

// cloneAUR clones the AUR git repo.
func cloneAUR(pkg *Package, pkgDir string) error {
	_, _, arrow := configSymbols()

	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg.Name)
	if _, err := os.Stat(filepath.Join(pkgDir, ".git")); err == nil {
		fmt.Printf("  %s updating %s...\n", arrow, pkg.Name)
		fetch := exec.Command("git", "-C", pkgDir, "fetch", "--depth=1", "origin", "master")
		fetch.Env = append(os.Environ(), "TERM=xterm-256color")
		fetch.Stdout = nil
		fetch.Stderr = os.Stderr
		if err := fetch.Run(); err != nil {
			return fmt.Errorf("git fetch failed for %s: %w", pkg.Name, err)
		}

		reset := exec.Command("git", "-C", pkgDir, "reset", "--hard", "FETCH_HEAD")
		reset.Env = append(os.Environ(), "TERM=xterm-256color")
		reset.Stdout = nil
		reset.Stderr = os.Stderr
		if err := reset.Run(); err != nil {
			return fmt.Errorf("git reset failed for %s: %w", pkg.Name, err)
		}
		return nil
	}

	if _, err := os.Stat(pkgDir); err == nil {
		os.RemoveAll(pkgDir)
	}
	if err := os.MkdirAll(filepath.Dir(pkgDir), 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	fmt.Printf("  %s cloning %s...\n", arrow, gitURL)
	cmd := exec.Command("git", "clone", "--depth=1", gitURL, pkgDir)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed for %s: %w", pkg.Name, err)
	}
	return nil
}

func resolveLocalDeps(allDeps []string, cache *pkgCache, warn string) ([]string, []*Package, error) {
	var repoDeps []string
	var aurOrdered []*Package
	visited := make(map[string]bool)

	for _, dep := range unsatisfiedDeps(allDeps) {
		name := stripVerConstraint(dep)
		if cache.InRepo(name) {
			repoDeps = append(repoDeps, name)
			continue
		}
		if cache.HasProvider(name) {
			continue
		}
		aurPkg, err := findAURPackage(name)
		if err != nil {
			return nil, nil, fmt.Errorf("dep %q: %w", name, err)
		}
		if err := resolveDepTree(aurPkg, visited, &aurOrdered, &repoDeps, warn, cache); err != nil {
			return nil, nil, err
		}
	}
	return dedup(repoDeps), aurOrdered, nil
}

func reviewAndConfirmLocalBuild(pkgbuildPath string, noConfirm bool, arrow string) error {
	if noConfirm {
		return nil
	}
	if err := reviewPKGBUILD(pkgbuildPath); err != nil {
		return err
	}
	proceed, err := readYesNo(fmt.Sprintf("  %s Proceed with build?", arrow), true)
	if err != nil {
		return err
	}
	if !proceed {
		return fmt.Errorf("build cancelled by user")
	}
	return nil
}

func runMakepkg(dir string, pkgname string, noConfirm bool, arrow, ok string) error {
	fmt.Printf("\n  %s building %s from local PKGBUILD...\n\n", arrow, pkgname)
	args := []string{"-si"}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	if err := priv.Invalidate(); err != nil {
		warnStderr("failed to invalidate sudo credentials before makepkg: %v", err)
	}
	makepkg := exec.Command("makepkg", args...)
	makepkg.Env = append(os.Environ(), "TERM=xterm-256color")
	makepkg.Dir = dir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return fmt.Errorf("makepkg failed: %w", err)
	}
	fmt.Printf("  %s  %s built and installed\n", ok, pkgname)
	return nil
}

// BuildLocal builds from a local PKGBUILD.
func BuildLocal(dir string, noConfirm bool) error {
	ok, warn, arrow := configSymbols()

	// Building requires base-devel; git is needed if any AUR deps must be cloned.
	if err := checkRequirements(true, true); err != nil {
		return err
	}

	pkgbuildPath := filepath.Join(dir, "PKGBUILD")
	if _, err := os.Stat(pkgbuildPath); os.IsNotExist(err) {
		return fmt.Errorf("no PKGBUILD found in %s", dir)
	}

	deps, makedeps, pkgname, err := parsePKGBUILD(pkgbuildPath)
	if err != nil {
		return fmt.Errorf("failed to parse PKGBUILD: %w", err)
	}

	// Trust notice for local builds
	fmt.Println()
	fmt.Printf("  %s  Building from a local PKGBUILD — review before proceeding.\n", warn)
	fmt.Println()
	fmt.Printf("  :: Building local package: %s\n", pkgname)

	allDeps := append(deps, makedeps...)
	repoDeps, aurOrdered, err := resolveLocalDeps(allDeps, newPkgCache(), warn)
	if err != nil {
		return err
	}

	if err := installRepoDeps(repoDeps, arrow); err != nil {
		return err
	}

	var builtDirs []string
	for _, aurPkg := range aurOrdered {
		fmt.Printf("\n  %s building AUR dep: %s\n", arrow, aurPkg.Name)
		pkgDir, err := buildAndInstall(aurPkg, noConfirm)
		if err != nil {
			return fmt.Errorf("failed to build dep %s: %w", aurPkg.Name, err)
		}
		builtDirs = append(builtDirs, pkgDir)
	}

	if err := reviewAndConfirmLocalBuild(pkgbuildPath, noConfirm, arrow); err != nil {
		return err
	}

	if err := runMakepkg(dir, pkgname, noConfirm, arrow, ok); err != nil {
		return err
	}

	cleanupBuildCaches(builtDirs, noConfirm, ok)

	return nil
}

// FetchABS downloads a PKGBUILD from the Arch Build System.
func FetchABS(pkgName string) (string, error) {
	if err := validatePkgName(pkgName); err != nil {
		return "", err
	}
	_, _, arrow := configSymbols()

	outDir, err := aurCacheDir("abs-" + pkgName)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(outDir); err == nil {
		os.RemoveAll(outDir)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create dir: %w", err)
	}

	// asp is cleaner when available; no git required for this path
	if _, err := exec.LookPath("asp"); err == nil {
		fmt.Printf("  %s fetching %s from ABS via asp...\n", arrow, pkgName)
		cmd := exec.Command("asp", "export", pkgName)
		cmd.Dir = outDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("asp export failed: %w", err)
		}
		// asp writes to a subdir named after the package
		sub := filepath.Join(outDir, pkgName)
		if _, err := os.Stat(sub); err == nil {
			return sub, nil
		}
		return outDir, nil
	}

	// Fallback: Arch GitLab — git is required for this path
	if err := checkRequirements(true, false); err != nil {
		return "", fmt.Errorf("asp not found and git unavailable for fallback: %w", err)
	}
	gitURL := fmt.Sprintf("%s%s.git", absGitLab, pkgName)
	fmt.Printf("  %s fetching %s from Arch GitLab...\n", arrow, pkgName)
	cmd := exec.Command("git", "clone", "--depth=1", gitURL, outDir)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to fetch %s from ABS: %w\n  (is it a valid official package?)", pkgName, err)
	}
	return outDir, nil
}

// unsatisfiedDeps finds unmet dependencies.
func unsatisfiedDeps(deps []string) []string {
	if len(deps) == 0 {
		return nil
	}
	stripped := make([]string, len(deps))
	for i, d := range deps {
		stripped[i] = stripVerConstraint(d)
	}
	out, err := exec.Command("pacman", append([]string{"-T"}, stripped...)...).CombinedOutput()
	if err == nil {
		return nil // all satisfied
	}
	var result []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// hasProvider checks if a dep is satisfied.
func hasProvider(dep string) bool {
	return exec.Command("pacman", "-T", dep).Run() == nil
}

// checkDeps checks dependencies.
func checkDeps(pkg *Package) (missingRepo []string, aurOnly []string, err error) {
	cache := newPkgCache()
	allDeps := append(pkg.Depends, pkg.MakeDepends...)
	for _, dep := range unsatisfiedDeps(allDeps) {
		name := stripVerConstraint(dep)
		if cache.InRepo(name) {
			missingRepo = append(missingRepo, name)
			continue
		}
		if Exists(name) {
			aurOnly = append(aurOnly, name)
		} else {
			err = fmt.Errorf("dep %q not found anywhere", name)
			return
		}
	}
	return
}

// parsePKGBUILD extracts info from a PKGBUILD.
func parsePKGBUILD(path string) (deps, makedeps []string, pkgname string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	var inDeps, inMake bool
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "pkgname="):
			pkgname = strings.Trim(t[8:], `'"() `)
		case strings.HasPrefix(t, "depends=("):
			inDeps = true
			inner := strings.TrimPrefix(t, "depends=(")
			inner = strings.TrimSuffix(inner, ")")
			deps = append(deps, parseArrayLine(inner)...)
			if strings.Contains(t, ")") {
				inDeps = false
			}
		case strings.HasPrefix(t, "makedepends=("):
			inMake = true
			inner := strings.TrimPrefix(t, "makedepends=(")
			inner = strings.TrimSuffix(inner, ")")
			makedeps = append(makedeps, parseArrayLine(inner)...)
			if strings.Contains(t, ")") {
				inMake = false
			}
		case inDeps:
			if strings.Contains(t, ")") {
				inDeps = false
				t = strings.TrimSuffix(t, ")")
			}
			deps = append(deps, parseArrayLine(t)...)
		case inMake:
			if strings.Contains(t, ")") {
				inMake = false
				t = strings.TrimSuffix(t, ")")
			}
			makedeps = append(makedeps, parseArrayLine(t)...)
		}
	}
	return deps, makedeps, pkgname, nil
}

func parseArrayLine(s string) []string {
	var out []string
	for _, tok := range strings.Fields(s) {
		tok = strings.Trim(tok, `'"`)
		if tok != "" && !strings.HasPrefix(tok, "#") {
			out = append(out, tok)
		}
	}
	return out
}

// reviewPKGBUILD reviews a PKGBUILD.
func reviewPKGBUILD(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PKGBUILD: %w", err)
	}
	lines := strings.Split(string(content), "\n")
	important := []string{"pkgname", "pkgver", "pkgrel", "arch", "license", "source", "sha", "md5", "url=", "depends", "makedepends"}

	fmt.Println("\n  :: PKGBUILD summary ::")
	fmt.Printf("  NOTE: This is a summary only — it is NOT a security review.\n")
	fmt.Printf("        PKGBUILDs can run arbitrary shell commands. Review the full\n")
	fmt.Printf("        file below or in your editor before proceeding.\n")
	fmt.Println("  " + strings.Repeat("-", 44))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		lower := strings.ToLower(t)
		for _, key := range important {
			if strings.HasPrefix(lower, key) {
				fmt.Printf("     %s\n", t)
				break
			}
		}
	}
	fmt.Println("  " + strings.Repeat("-", 44))

	editor := os.Getenv("EDITOR")
	if editor == "" || !editorIsSafe(editor) {
		editor = "nano"
	}
	openEd, err := readYesNo(fmt.Sprintf("  Open PKGBUILD in editor (%s)?", editor), false)
	if err != nil {
		return err
	}
	if openEd {
		cmd := exec.Command(editor, path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	} else {
		view, err := readYesNo("  View full PKGBUILD in terminal?", false)
		if err != nil {
			return err
		}
		if view {
			fmt.Println()
			for _, line := range lines {
				fmt.Printf("  %s\n", line)
			}
			fmt.Println()
		}
	}
	return nil
}

// Remove removes a package.
func Remove(pkgName string, noConfirm bool) error {
	if err := validatePkgName(pkgName); err != nil {
		return err
	}
	args := []string{"pacman", "-R", pkgName}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	cmd, err := priv.Command(args...)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pacman -R %s failed: %w", pkgName, err)
	}
	return nil
}

// GetInstalledAUR returns installed AUR packages.
func GetInstalledAUR() (map[string]string, error) {
	out, err := exec.Command("pacman", "-Qm").Output()
	if err != nil {
		return nil, fmt.Errorf("pacman -Qm failed: %w", err)
	}
	installed := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 {
			installed[parts[0]] = parts[1]
		}
	}
	return installed, nil
}

// ListInstalledAUR returns installed AUR packages.
func ListInstalledAUR() (map[string]string, error) {
	return GetInstalledAUR()
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
		// Re-prompt once on invalid input
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

func isInstalled(name string) bool {
	return exec.Command("pacman", "-Qi", name).Run() == nil
}

func inPacmanRepo(name string) bool {
	return exec.Command("pacman", "-Si", name).Run() == nil
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
	// Ensure the resolved path stays inside the cache root.
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

// PacmanConfig holds pacman settings parsed from pacman-conf
type PacmanConfig struct {
	IgnorePkg   []string
	IgnoreGroup []string
}

// ReadPacmanConf reads pacman configuration using pacman-conf.
func ReadPacmanConf() (*PacmanConfig, error) {
	out, err := exec.Command("pacman-conf").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run pacman-conf: %w", err)
	}

	conf := &PacmanConfig{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if val == "" {
			continue
		}

		switch key {
		case "IgnorePkg":
			conf.IgnorePkg = append(conf.IgnorePkg, strings.Fields(val)...)
		case "IgnoreGroup":
			conf.IgnoreGroup = append(conf.IgnoreGroup, strings.Fields(val)...)
		}
	}
	return conf, nil
}
