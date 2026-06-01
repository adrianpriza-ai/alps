package aur

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/adrianpriza-ai/alps/priv"
)

const (
	aurRPCSearch = "https://aur.archlinux.org/rpc/v5/search/"
	aurRPCInfo   = "https://aur.archlinux.org/rpc/v5/info/"
	absGitLab    = "https://gitlab.archlinux.org/archlinux/packaging/packages/"
)

// Package represents an AUR package from the RPC API.
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

// installPlan holds the fully resolved work for an install.
// All user input is collected from this before any build starts.
type installPlan struct {
	AURPackages   []*Package // in build order (deps first)
	RepoDeps      []string   // installed from pacman before building
	MakeDepsAdded []string   // makedeps not pre-installed; offered for removal after
}

// symSet returns TTY-safe symbols.
func symSet() (ok, warn, arrow string) {
	t := os.Getenv("TERM")
	if t == "linux" || t == "dumb" || t == "" {
		return " OK ", "WARN", "->"
	}
	return "✓", "⚠", "→"
}

// DetectHelper returns "yay" if available, otherwise "".
func DetectHelper() string {
	if _, err := exec.LookPath("yay"); err == nil {
		return "yay"
	}
	return ""
}

func fetchRPC(url string) (*rpcResponse, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("AUR request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AUR returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read AUR response: %w", err)
	}
	var result rpcResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse AUR response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("AUR error: %s", result.Error)
	}
	return &result, nil
}

// Search searches AUR sorted by votes.
func Search(query string) ([]Package, error) {
	result, err := fetchRPC(aurRPCSearch + query)
	if err != nil {
		return nil, err
	}
	sort.Slice(result.Results, func(i, j int) bool {
		return result.Results[i].Votes > result.Results[j].Votes
	})
	return result.Results, nil
}

// SearchNarrow searches AUR using all words in query.
// The first word hits the AUR RPC; remaining words narrow results in-memory
// against name and description (case-insensitive). Mirrors yay's behaviour.
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

// Info fetches detailed info for a single package by exact name.
func Info(name string) (*Package, error) {
	result, err := fetchRPC(aurRPCInfo + name)
	if err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("package %q not found in AUR", name)
	}
	return &result.Results[0], nil
}

// InfoBatch fetches info for multiple packages in parallel.
func InfoBatch(names []string) map[string]*Package {
	var mu sync.Mutex
	results := make(map[string]*Package)
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			pkg, err := Info(n)
			if err == nil {
				mu.Lock()
				results[n] = pkg
				mu.Unlock()
			}
		}(name)
	}
	wg.Wait()
	return results
}

// Exists reports whether a package exists in AUR.
func Exists(name string) bool {
	_, err := Info(name)
	return err == nil
}

// PrintSearchResult prints a single search result pacman-style.
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

// PrintPackageInfo prints full package details.
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

// Install

// Install installs one or more AUR packages.
// Uses yay if available; otherwise falls back to makepkg with full dep
// resolution and all user prompts collected before any build starts.
func Install(pkgNames []string, noConfirm bool) error {
	if len(pkgNames) == 0 {
		return nil
	}
	if DetectHelper() == "yay" {
		return installWithYay(pkgNames, noConfirm)
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
	_, _, arrow := symSet()
	args := append([]string{"-S"}, pkgNames...)
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	fmt.Printf("  %s using yay: %s\n\n", arrow, strings.Join(pkgNames, " "))
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

// Dep resolution

// buildInstallPlan resolves the full dep tree for the requested packages and
// returns them in build order (all deps before the package that needs them).
func buildInstallPlan(names []string) (*installPlan, error) {
	_, warn, _ := symSet()
	visited := make(map[string]bool)
	var ordered []*Package
	var repoDeps []string

	for _, name := range names {
		pkg, err := Info(name)
		if err != nil {
			return nil, err
		}
		if err := resolveDepTree(pkg, visited, &ordered, &repoDeps, warn); err != nil {
			return nil, err
		}
	}

	// Track which makedeps aren't currently installed (for post-build offer)
	var makeAdded []string
	for _, pkg := range ordered {
		for _, dep := range pkg.MakeDepends {
			n := stripVerConstraint(dep)
			if !isInstalled(n) {
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

// resolveDepTree recursively walks deps for pkg, appending to ordered in
// topological build order (dependencies always come before the package
// that requires them). Uses pacman -T for accurate satisfier checking.
func resolveDepTree(pkg *Package, visited map[string]bool, ordered *[]*Package, repoDeps *[]string, warn string) error {
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

		// Official repo?
		if inPacmanRepo(name) {
			*repoDeps = append(*repoDeps, name)
			continue
		}

		// Provided by something already installed?
		if hasProvider(name) {
			continue
		}

		// AUR — find the right package (exact match or user-selected provider)
		depPkg, err := findAURPackage(name)
		if err != nil {
			return fmt.Errorf("dep %q required by %s: %w", name, pkg.Name, err)
		}

		fmt.Printf("  %s  AUR dep: %s (required by %s)\n", warn, depPkg.Name, pkg.Name)
		if err := resolveDepTree(depPkg, visited, ordered, repoDeps, warn); err != nil {
			return err
		}
	}

	*ordered = append(*ordered, pkg)
	return nil
}

// findAURPackage looks up a dep by exact name in AUR.
// If no exact match exists, searches and asks the user to pick a provider.
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

	var choice int
	fmt.Scan(&choice)
	if choice < 1 || choice > limit {
		return nil, fmt.Errorf("no provider selected for %q", name)
	}
	selected := results[choice-1]
	return &selected, nil
}

// Up-front user input collection

// collectUserInputs gathers ALL confirmations and PKGBUILD reviews before any
// build starts. After this returns nil, executeInstallPlan runs non-interactively.
func collectUserInputs(plan *installPlan, noConfirm bool) error {
	_, warn, arrow := symSet()

	// Summary of everything about to happen
	if len(plan.RepoDeps) > 0 {
		fmt.Printf("\n  :: Repo dependencies: %s\n", strings.Join(plan.RepoDeps, "  "))
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

	if noConfirm {
		return nil
	}

	// Warn about out-of-date packages
	for _, p := range plan.AURPackages {
		if p.OutOfDate != 0 {
			fmt.Printf("\n  %s  %s is flagged out-of-date\n", warn, p.Name)
		}
	}

	// PKGBUILD review — clone all packages up-front so the user can read them
	// before committing. buildAndInstall reuses the clone if it already exists.
	fmt.Printf("\n  %s Review PKGBUILDs before building? [y/N] ", arrow)
	var inp string
	fmt.Scanln(&inp)
	if strings.ToLower(strings.TrimSpace(inp)) == "y" {
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

	// Single proceed prompt for everything
	label := "Proceed with install?"
	if len(plan.AURPackages) > 1 {
		label = fmt.Sprintf("Proceed with all %d builds?", len(plan.AURPackages))
	}
	fmt.Printf("\n  %s %s [Y/n] ", arrow, label)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) == "n" {
		return fmt.Errorf("install cancelled by user")
	}
	return nil
}

// Build execution

// executeInstallPlan installs repo deps then builds AUR packages in dep order.
// By this point all user input has already been collected.
func executeInstallPlan(plan *installPlan, noConfirm bool) error {
	ok, warn, arrow := symSet()

	// Install repo deps first
	if len(plan.RepoDeps) > 0 {
		fmt.Printf("\n  %s installing repo deps: %s\n\n", arrow, strings.Join(plan.RepoDeps, " "))
		args := append([]string{"pacman", "-S", "--noconfirm", "--needed"}, plan.RepoDeps...)
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
	}

	// Build each AUR package in dep order
	for _, pkg := range plan.AURPackages {
		if err := buildAndInstall(pkg, noConfirm); err != nil {
			return fmt.Errorf("failed to build %s: %w", pkg.Name, err)
		}
	}

	// Offer makedep removal once, at the very end
	if len(plan.MakeDepsAdded) > 0 {
		var toRemove []string
		for _, dep := range plan.MakeDepsAdded {
			if isInstalled(dep) {
				toRemove = append(toRemove, dep)
			}
		}
		if len(toRemove) > 0 {
			fmt.Printf("\n  :: Build dependencies installed during build: %s\n", strings.Join(toRemove, "  "))
			if !noConfirm {
				fmt.Print("  Remove build dependencies? [y/N] ")
				var inp string
				fmt.Scanln(&inp)
				if strings.ToLower(strings.TrimSpace(inp)) != "y" {
					return nil
				}
			}
			rmArgs := append([]string{"pacman", "-Rns", "--noconfirm"}, toRemove...)
			rmCmd, err := priv.Command(rmArgs...)
			if err != nil {
				fmt.Printf("  %s  privilege escalation failed: %v\n", warn, err)
			} else {
				rmCmd.Stdout = os.Stdout
				rmCmd.Stderr = os.Stderr
				rmCmd.Stdin = os.Stdin
				if err := rmCmd.Run(); err != nil {
					fmt.Printf("  %s  failed to remove build deps: %v\n", warn, err)
				} else {
					fmt.Printf("  %s  build dependencies removed\n", ok)
				}
			}
		}
	}
	return nil
}

// buildAndInstall clones (if not already present from review) and builds pkg.
func buildAndInstall(pkg *Package, noConfirm bool) error {
	ok, _, arrow := symSet()

	pkgDir, err := aurCacheDir(pkg.Name)
	if err != nil {
		return fmt.Errorf("failed to resolve cache dir: %w", err)
	}

	// Reuse clone from PKGBUILD review if already there
	if _, err := os.Stat(filepath.Join(pkgDir, "PKGBUILD")); os.IsNotExist(err) {
		if err := cloneAUR(pkg, pkgDir); err != nil {
			return err
		}
	}

	fmt.Printf("\n  %s building %s %s...\n\n", arrow, pkg.Name, pkg.Version)
	args := []string{"-si", "--noconfirm"} // noconfirm here: user already confirmed above
	makepkg := exec.Command("makepkg", args...)
	makepkg.Env = append(os.Environ(), "TERM=xterm-256color")
	makepkg.Dir = pkgDir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return fmt.Errorf("makepkg failed: %w", err)
	}
	fmt.Printf("  %s  %s installed\n", ok, pkg.Name)

	// Cache cleanup
	if !noConfirm {
		fmt.Printf("\n  :: Build cache: %s\n", pkgDir)
		fmt.Print("  Keep build cache? [y/N] ")
		var keep string
		fmt.Scanln(&keep)
		if strings.ToLower(strings.TrimSpace(keep)) != "y" {
			os.RemoveAll(pkgDir)
			fmt.Printf("  %s  cache removed\n", ok)
		}
	}
	return nil
}

// cloneAUR clones the AUR git repo for pkg into pkgDir.
// Removes any existing dir first so the clone is always fresh.
func cloneAUR(pkg *Package, pkgDir string) error {
	_, _, arrow := symSet()
	if _, err := os.Stat(pkgDir); err == nil {
		os.RemoveAll(pkgDir)
	}
	if err := os.MkdirAll(filepath.Dir(pkgDir), 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}
	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg.Name)
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

// Local PKGBUILD

// BuildLocal builds a package from a local directory containing a PKGBUILD.
// Resolves and installs any AUR or repo deps before building.
func BuildLocal(dir string, noConfirm bool) error {
	ok, _, arrow := symSet()

	pkgbuildPath := filepath.Join(dir, "PKGBUILD")
	if _, err := os.Stat(pkgbuildPath); os.IsNotExist(err) {
		return fmt.Errorf("no PKGBUILD found in %s", dir)
	}

	deps, makedeps, pkgname, err := parsePKGBUILD(pkgbuildPath)
	if err != nil {
		return fmt.Errorf("failed to parse PKGBUILD: %w", err)
	}
	fmt.Printf("\n  :: Building local package: %s\n", pkgname)

	allDeps := append(deps, makedeps...)
	var repoDeps []string
	var aurOrdered []*Package
	visited := make(map[string]bool)
	_, warn, _ := symSet()

	for _, dep := range unsatisfiedDeps(allDeps) {
		name := stripVerConstraint(dep)
		if inPacmanRepo(name) {
			repoDeps = append(repoDeps, name)
			continue
		}
		if hasProvider(name) {
			continue
		}
		aurPkg, err := findAURPackage(name)
		if err != nil {
			return fmt.Errorf("dep %q: %w", name, err)
		}
		if err := resolveDepTree(aurPkg, visited, &aurOrdered, &repoDeps, warn); err != nil {
			return err
		}
	}

	repoDeps = dedup(repoDeps)

	if len(repoDeps) > 0 {
		fmt.Printf("  %s installing repo deps: %s\n", arrow, strings.Join(repoDeps, "  "))
		args := append([]string{"pacman", "-S", "--noconfirm", "--needed"}, repoDeps...)
		cmd, err := priv.Command(args...)
		if err != nil {
			return err
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install repo deps: %w", err)
		}
	}

	for _, aurPkg := range aurOrdered {
		fmt.Printf("\n  %s building AUR dep: %s\n", arrow, aurPkg.Name)
		if err := buildAndInstall(aurPkg, noConfirm); err != nil {
			return fmt.Errorf("failed to build dep %s: %w", aurPkg.Name, err)
		}
	}

	if !noConfirm {
		if err := reviewPKGBUILD(pkgbuildPath); err != nil {
			return err
		}
		fmt.Printf("  %s Proceed with build? [Y/n] ", arrow)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.ToLower(strings.TrimSpace(scanner.Text())) == "n" {
			return fmt.Errorf("build cancelled by user")
		}
	}

	fmt.Printf("\n  %s building %s from local PKGBUILD...\n\n", arrow, pkgname)
	args := []string{"-si"}
	if noConfirm {
		args = append(args, "--noconfirm")
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

// ABS — fetch official PKGBUILDs

// FetchABS downloads an official package's PKGBUILD from the Arch Build System.
// Uses asp if installed; otherwise clones directly from Arch GitLab.
// Returns the directory containing the PKGBUILD.
func FetchABS(pkgName string) (string, error) {
	_, _, arrow := symSet()

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

	// asp is cleaner when available
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

	// Fallback: Arch GitLab
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

// Dep helpers

// unsatisfiedDeps uses pacman -T to find which deps are not currently met.
// pacman -T exits non-zero and prints only the unsatisfied ones.
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

// hasProvider checks whether any installed package satisfies the dep.
func hasProvider(dep string) bool {
	return exec.Command("pacman", "-T", dep).Run() == nil
}

// checkDeps is kept for backwards compatibility.
func checkDeps(pkg *Package) (missingRepo []string, aurOnly []string, err error) {
	allDeps := append(pkg.Depends, pkg.MakeDepends...)
	for _, dep := range unsatisfiedDeps(allDeps) {
		name := stripVerConstraint(dep)
		if inPacmanRepo(name) {
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

// PKGBUILD parser

// parsePKGBUILD extracts pkgname, depends, and makedepends from a PKGBUILD.
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

// PKGBUILD review

func reviewPKGBUILD(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PKGBUILD: %w", err)
	}
	lines := strings.Split(string(content), "\n")
	important := []string{"pkgname", "pkgver", "pkgrel", "arch", "license", "source", "sha", "md5", "url=", "depends", "makedepends"}

	fmt.Println("\n  :: PKGBUILD summary ::")
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

	fmt.Print("\n  View full PKGBUILD? [y/N] ")
	var view string
	fmt.Scanln(&view)
	if strings.ToLower(strings.TrimSpace(view)) == "y" {
		fmt.Println()
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
		fmt.Println()
	}
	return nil
}

// Remove / list / clean

// Remove removes a package using pacman -R.
func Remove(pkgName string, noConfirm bool) error {
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

// GetInstalledAUR returns a map of AUR-installed packages: name → version.
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

// ListInstalledAUR wraps GetInstalledAUR for external use.
func ListInstalledAUR() (map[string]string, error) {
	return GetInstalledAUR()
}

// CleanCache removes the build cache for pkgName, or all caches if pkgName is "".
func CleanCache(pkgName string) error {
	ok, _, _ := symSet()
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

// Misc helpers

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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "alps", "aur", pkgName), nil
}

func AURCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "alps", "aur"), nil
}
