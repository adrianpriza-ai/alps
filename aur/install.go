package aur

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/priv"
)

// Install - AUR install pipeline (helper or built-in)

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

// DetectHelper returns the name of an installed AUR helper — "paru" or "yay" —
// or "" if neither is available. paru is preferred when both are installed.
func DetectHelper() string {
	for _, helper := range []string{"paru", "yay"} {
		if _, err := exec.LookPath(helper); err == nil {
			return helper
		}
	}
	return ""
}

// Install installs AUR packages.
func Install(pkgNames []string, noConfirm bool) error {
	if len(pkgNames) == 0 {
		return nil
	}
	if err := validatePkgNames(pkgNames); err != nil {
		return err
	}

	if helper := DetectHelper(); helper != "" {
		return installWithHelper(helper, pkgNames)
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

// installWithHelper delegates the install to an installed AUR helper
// (paru/yay), which handles dependency resolution and prompting itself.
// --noconfirm is intentionally never passed: AUR installs must always be
// confirmed interactively, even when the caller requested no confirmations.
func installWithHelper(helper string, pkgNames []string) error {
	_, _, arrow := configSymbols()
	args := append([]string{"-S"}, pkgNames...)
	fmt.Printf("  %s using %s: %s\n\n", arrow, helper, strings.Join(pkgNames, " "))
	if err := priv.Invalidate(); err != nil {
		warnStderr("failed to invalidate sudo credentials before %s: %v", helper, err)
	}
	cmd := exec.Command(helper, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", helper, err)
	}
	return nil
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

	// Check for conflicts with installed packages before building
	var allConflicts []string
	for _, pkg := range ordered {
		if conflicts := checkConflicts(pkg.Conflicts); len(conflicts) > 0 {
			for _, conflict := range conflicts {
				allConflicts = append(allConflicts, fmt.Sprintf("%s conflicts with %s", pkg.Name, conflict))
			}
		}
	}
	if len(allConflicts) > 0 {
		fmt.Printf("  :: WARNING: The following conflicts were detected:\n")
		for _, conflict := range allConflicts {
			fmt.Printf("     - %s\n", conflict)
		}
		fmt.Printf("  :: These conflicts will cause pacman -U to fail after compilation.\n")
		fmt.Printf("  :: You may need to manually remove conflicting packages first.\n")
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

		if visited[name] {
			continue
		}

		if cache.InRepo(name) {
			*repoDeps = append(*repoDeps, name)
			continue
		}

		if cache.HasProvider(name) {
			continue
		}

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

// reviewAURPKGBUILDs syncs each planned package's AUR repo and walks the
// user through reviewing it before building.
//
// On upgrades (#18) the checkout usually already exists: the previous HEAD is
// captured before syncing, and if upstream moved, the user is offered a
// focused git diff of what changed instead of wading through the entire
// PKGBUILD again — much harder to miss a malicious change in a diff.
// Fresh installs (no prior checkout) fall back to the full-file review.
func reviewAURPKGBUILDs(plan *installPlan, arrow string) error {
	if ok, err := readYesNo(fmt.Sprintf("  %s Review PKGBUILDs before building?", arrow), false); err != nil {
		return err
	} else if ok {
		for _, p := range plan.AURPackages {
			pkgDir, err := aurCacheDir(p.Name)
			if err != nil {
				return err
			}
			// Capture the pre-sync HEAD so we can tell whether this is an
			// upgrade with actual repository changes.
			oldHead := gitHead(pkgDir)
			if err := cloneAUR(p, pkgDir); err != nil {
				return err
			}
			if oldHead != "" && oldHead != gitHead(pkgDir) {
				shown, err := reviewPKGBUILDUpdate(pkgDir, oldHead, arrow)
				if err != nil {
					return err
				}
				if shown {
					continue // change set was reviewed; skip the full-file dump
				}
			}
			if err := reviewPKGBUILD(filepath.Join(pkgDir, "PKGBUILD")); err != nil {
				return err
			}
		}
	}
	return nil
}

// gitHead returns the current HEAD commit of the git repository at dir, or
// "" when dir is not a repository (or has no commits yet).
func gitHead(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// reviewPKGBUILDUpdate presents a diff-based review of an upgraded package:
// a per-file stat overview followed by (on request) the full PKGBUILD patch
// between oldHead and the freshly fetched HEAD.
// It returns true when the user engaged with (or consciously skipped) the
// change set, and false when the caller should fall back to reviewing the
// complete file — e.g. when the shallow history no longer contains oldHead
// and the diff cannot be computed.
func reviewPKGBUILDUpdate(pkgDir, oldHead, arrow string) (bool, error) {
	statOut, err := exec.Command("git", "-C", pkgDir, "--no-pager", "diff", "--stat", oldHead, "HEAD").Output()
	if err != nil || strings.TrimSpace(string(statOut)) == "" {
		return false, nil
	}

	fmt.Printf("\n  :: %s changed since your last build:\n", filepath.Base(pkgDir))
	for _, line := range strings.Split(strings.TrimRight(string(statOut), "\n"), "\n") {
		fmt.Printf("     %s\n", line)
	}

	viewDiff, err := readYesNo(fmt.Sprintf("  %s View the PKGBUILD diff? (recommended)", arrow), true)
	if err != nil {
		return false, err
	}
	if !viewDiff {
		return true, nil // declined the detailed view; treat as reviewed
	}

	cmd, err := unprivilegedCommand("git", "-C", pkgDir, "--no-pager", "diff", oldHead, "HEAD", "--", "PKGBUILD")
	if err != nil {
		return false, err
	}
	cmd.Env = safeMakepkgEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Shallow-clone pruning can drop the old snapshot; fall back to a
		// full review rather than skipping the security gate entirely.
		warnStderr("could not render PKGBUILD diff for %s: %v", filepath.Base(pkgDir), err)
		return false, nil
	}
	return true, nil
}

// collectUserInputs gathers confirmations before building.
func collectUserInputs(plan *installPlan, noConfirm bool) error {
	_, warn, arrow := configSymbols()

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

func installRepoDeps(deps []string, noConfirm bool, arrow string) error {
	if len(deps) == 0 {
		return nil
	}
	fmt.Printf("\n  %s installing repo deps: %s\n\n", arrow, strings.Join(deps, " "))
	args := []string{"pacman", "-S", "--needed"}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	args = append(args, deps...)
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
	rmArgs := []string{"pacman", "-Rns"}
	if noConfirm {
		rmArgs = append(rmArgs, "--noconfirm")
	}
	rmArgs = append(rmArgs, toRemove...)
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

	if err := installRepoDeps(plan.RepoDeps, noConfirm, arrow); err != nil {
		return err
	}

	// Fetch/clone every planned AUR repository up front, in parallel (#19),
	// so the sequential build loop below starts with all sources in place
	// instead of paying a network round-trip before each build.
	if err := prefetchAURRepos(plan.AURPackages); err != nil {
		return err
	}

	var builtDirs []string
	for _, pkg := range plan.AURPackages {
		pkgDir, err := buildAndInstall(pkg, noConfirm)
		if err != nil {
			return fmt.Errorf("failed to build %s: %w", pkg.Name, err)
		}
		// Filter out empty paths — buildAndInstall returns "" when a package
		// is skipped (--needed), and passing "" to os.RemoveAll is dangerous.
		if pkgDir != "" {
			builtDirs = append(builtDirs, pkgDir)
		}
	}

	if err := removeMakeDeps(plan.MakeDepsAdded, noConfirm, ok); err != nil {
		return err
	}

	cleanupBuildCaches(builtDirs, noConfirm, ok)
	return nil
}

// prefetchWorkers caps how many AUR repositories are cloned/fetched at once
// during the parallel prefetch phase. The work is network-bound, so a small
// fixed pool keeps latency down without hammering the AUR or spawning
// unbounded git processes.
const prefetchWorkers = 8

// prefetchAURRepos clones or updates every package's AUR checkout in
// parallel. Any failure aborts the plan before any build starts — better to
// fail fast than to compile for minutes and then discover a broken source.
func prefetchAURRepos(pkgs []*Package) error {
	return prefetchRepos(pkgs, syncAURRepo)
}

// repoSyncFunc mirrors syncAURRepo's signature so tests can inject a stub.
type repoSyncFunc func(pkgName, pkgDir string, quiet bool) error

// prefetchRepos runs syncFn over all package cache dirs using a bounded
// worker pool. Output stays quiet during the fetches (parallel progress
// lines would interleave into garbage); results are reported sequentially
// once every worker has finished.
func prefetchRepos(pkgs []*Package, syncFn repoSyncFunc) error {
	if len(pkgs) == 0 {
		return nil
	}
	_, _, arrow := configSymbols()
	fmt.Printf("\n  %s fetching %d AUR repositories (%d workers)...\n",
		arrow, len(pkgs), min(prefetchWorkers, len(pkgs)))

	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(pkgs))
	sem := make(chan struct{}, prefetchWorkers)
	var wg sync.WaitGroup

	for _, pkg := range pkgs {
		wg.Add(1)
		go func(p *Package) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pkgDir, err := aurCacheDir(p.Name)
			if err == nil {
				err = syncFn(p.Name, pkgDir, true)
			}
			results <- result{name: p.Name, err: err}
		}(pkg)
	}
	wg.Wait()
	close(results)

	var failures []string
	for r := range results {
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.name, r.err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("failed to fetch AUR repositories:\n     %s",
			strings.Join(failures, "\n     "))
	}
	return nil
}

// verifyPGPSignature checks the GPG signature of a built package.
// AUR packages are user-produced content, so GPG verification is the
// one hard security gate before installation.
func verifyPGPSignature(pkgPath string) error {
	// Look for a corresponding .sig file alongside the package
	sigPath := pkgPath + ".sig"
	if _, err := os.Stat(sigPath); os.IsNotExist(err) {
		// No .sig file — AUR packages often don't ship signatures.
		// Print a warning but allow the install to proceed, since most
		// AUR PKGBUILDs don't produce detached signatures.
		fmt.Printf("  :: No GPG signature found for %s\n", filepath.Base(pkgPath))
		fmt.Printf("     (this is common for AUR packages — proceed with caution)\n")
		return nil
	}
	// Verify the .sig against the package
	cmd := exec.Command("gpg", "--verify", sigPath, pkgPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("GPG verification FAILED for %s:\n%s\n\nRefusing to install a package with an invalid signature.",
			filepath.Base(pkgPath), string(out))
	}
	fmt.Printf("  :: GPG signature verified for %s\n", filepath.Base(pkgPath))
	return nil
}

// findBuiltPackages returns all .pkg.tar.* files (excluding .sig files) in dir.
func findBuiltPackages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.Contains(name, ".pkg.tar.") && !strings.HasSuffix(name, ".sig") {
			pkgs = append(pkgs, filepath.Join(dir, name))
		}
	}
	return pkgs, nil
}

// installBuiltPackages installs pre-built .pkg.tar.* files via pacman -U,
// verifying GPG signatures before each install.
func installBuiltPackages(pkgPaths []string, noConfirm bool) error {
	for _, p := range pkgPaths {
		if err := verifyPGPSignature(p); err != nil {
			return err
		}
	}
	args := []string{"pacman", "-U"}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	args = append(args, pkgPaths...)
	cmd, err := priv.Command(args...)
	if err != nil {
		return fmt.Errorf("privilege escalation failed: %w", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pacman -U failed: %w", err)
	}
	return nil
}

// buildAndInstall clones and builds pkg.
func buildAndInstall(pkg *Package, noConfirm bool) (string, error) {
	ok, _, arrow := configSymbols()

	if err := validatePkgName(pkg.Name); err != nil {
		return "", fmt.Errorf("refusing to build package with invalid name %q: %w", pkg.Name, err)
	}

	// --needed: skip building if the installed version already satisfies the target.
	// Pacman's --needed skips when installed >= target. We mirror that: if the
	// installed version is greater than or equal to the AUR version, no rebuild
	// is needed. Using vercmp <= 0 (instead of == 0) also avoids unnecessary
	// downgrades when a user has a newer local build installed.
	//
	// VCS packages (-git, -svn, …) are exempt: their versions encode upstream
	// commits (e.g. r1234.abc5678), so the AUR RPC version typically equals the
	// installed one even when upstream has moved ahead. The upgrade path relies
	// on precise VCS detection to queue these rebuilds, so comparing static
	// versions here would silently cancel them.
	if !IsVCSPackage(pkg.Name) {
		if installedVer := pkgInstalledVersion(pkg.Name); installedVer != "" {
			if vercmp(pkg.Version, installedVer) <= 0 {
				fmt.Printf("  %s  %s %s is already installed (satisfies %s)\n", ok, pkg.Name, installedVer, pkg.Version)
				return "", nil
			}
		}
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
	if err := priv.Invalidate(); err != nil {
		warnStderr("failed to invalidate sudo credentials before makepkg: %v", err)
	}

	// Build only (no install) — we verify GPG signatures before installing.
	makepkgArgs := []string{"-s", "--nodeps"}
	if noConfirm {
		makepkgArgs = append(makepkgArgs, "--noconfirm")
	}
	makepkg, err := unprivilegedCommand("makepkg", makepkgArgs...)
	if err != nil {
		return "", err
	}
	makepkg.Env = safeMakepkgEnv()
	makepkg.Dir = pkgDir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return "", fmt.Errorf("makepkg failed: %w", err)
	}

	// Verify GPG signatures on all built packages before installing
	builtPkgs, err := findBuiltPackages(pkgDir)
	if err != nil {
		return "", fmt.Errorf("failed to list built packages: %w", err)
	}
	if len(builtPkgs) == 0 {
		return "", fmt.Errorf("makepkg produced no packages in %s", pkgDir)
	}
	if err := installBuiltPackages(builtPkgs, noConfirm); err != nil {
		return "", err
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
		// Verify package integrity before reusing cached package
		// pacman -Qkp checks if the package file is valid and readable
		if err := exec.Command("pacman", "-Qkp", path).Run(); err != nil {
			fmt.Printf("  :: Warning: cached package %s is corrupted, skipping\n", filepath.Base(path))
			continue
		}
		built, err := inspectBuiltPackage(path)
		if err != nil {
			continue
		}
		// For split packages, check if the built package matches the requested name.
		// The PackageBase field helps identify packages from the same PKGBUILD.
		nameMatch := built.Name == pkg.Name
		// Also check if this package provides the requested package
		providesMatch := false
		for _, provides := range pkg.Provides {
			if stripVerConstraint(provides) == built.Name {
				providesMatch = true
				break
			}
		}
		// For split packages, also match if they share the same PackageBase
		// This handles cases where multiple packages are built from one PKGBUILD
		pkgbaseMatch := pkg.PackageBase != "" && built.Name == pkg.PackageBase

		if !nameMatch && !providesMatch && !pkgbaseMatch {
			continue
		}
		if built.Version != pkg.Version {
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

func installBuiltPackage(path string, noConfirm bool) error {
	// Verify GPG signature before installing, even for cached packages.
	// This prevents installing tampered or corrupted .pkg.tar.* files
	// from ~/.cache/alps/aur/.
	if err := verifyPGPSignature(path); err != nil {
		return err
	}
	args := []string{"pacman", "-U"}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	args = append(args, path)
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

// verifyAURRemote checks that the git remote URL for a cached AUR repo
// matches the expected AUR origin exactly. This prevents silent source
// hijack via ~/.gitconfig URL rewriting or DNS manipulation.
func verifyAURRemote(pkgDir, expectedURL string) error {
	out, err := exec.Command("git", "-C", pkgDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return fmt.Errorf("failed to read git remote for %s: %w", pkgDir, err)
	}
	remoteURL := strings.TrimSpace(string(out))
	if remoteURL != expectedURL {
		return fmt.Errorf("remote URL mismatch for %s: got %q, expected %q\n"+
			"This could indicate URL rewriting (~/.gitconfig) or a DNS hijack.",
			pkgDir, remoteURL, expectedURL)
	}
	return nil
}

// Clone clones an AUR package repository to the current directory for inspection.
func Clone(pkgName string) error {
	if err := validatePkgName(pkgName); err != nil {
		return fmt.Errorf("refusing to clone package with invalid name %q: %w", pkgName, err)
	}

	// Get package info to verify it exists in AUR
	pkg, err := Info(pkgName)
	if err != nil {
		return fmt.Errorf("package %s not found in AUR: %w", pkgName, err)
	}

	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkgName)
	targetDir := pkgName

	// Check if directory already exists
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("directory %s already exists", targetDir)
	}

	s := config.Load().Style
	fmt.Printf("  %s cloning %s from AUR...\n", s.SymArrow, pkgName)
	cmd, err := unprivilegedCommand("git", "clone", "--depth=1", gitURL, targetDir)
	if err != nil {
		return err
	}
	cmd.Env = safeMakepkgEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	fmt.Printf("Successfully cloned %s to %s\n", pkgName, targetDir)
	fmt.Printf("Version: %s\n", pkg.Version)
	fmt.Printf("Description: %s\n", pkg.Description)
	return nil
}

// cloneAUR clones or updates the AUR git repo for pkg into pkgDir,
// printing progress messages.
func cloneAUR(pkg *Package, pkgDir string) error {
	return syncAURRepo(pkg.Name, pkgDir, false)
}

// syncAURRepo ensures pkgDir holds a fresh checkout of pkgName's AUR
// repository: an existing checkout is verified (remote URL) and fetched,
// otherwise a shallow clone is created.
//
// The same routine backs both the install pipeline and VCS update checks;
// when quiet is true no progress output is printed (update scans stay silent),
// but security failures — such as a tampered remote URL — are always fatal.
func syncAURRepo(pkgName, pkgDir string, quiet bool) error {
	var _, _, arrow = configSymbols()

	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkgName)
	if _, err := os.Stat(filepath.Join(pkgDir, ".git")); err == nil {
		if !quiet {
			fmt.Printf("  %s updating %s...\n", arrow, pkgName)
		}

		// Verify the remote URL BEFORE fetching to prevent communicating
		// with a hijacked origin. If the cached repo was tampered with,
		// we must not send any network traffic to the wrong server.
		if err := verifyAURRemote(pkgDir, gitURL); err != nil {
			return err
		}

		// Fetch the latest commit — use HEAD instead of hardcoding master
		// to support AUR repos with different default branches.
		fetch, err := unprivilegedCommand("git", "-C", pkgDir, "fetch", "--depth=1", "origin", "HEAD")
		if err != nil {
			return err
		}
		fetch.Env = safeMakepkgEnv()
		fetch.Stdout = nil
		fetch.Stderr = stderrFor(quiet)
		if err := fetch.Run(); err != nil {
			return fmt.Errorf("git fetch failed for %s: %w", pkgName, err)
		}

		reset, err := unprivilegedCommand("git", "-C", pkgDir, "reset", "--hard", "FETCH_HEAD")
		if err != nil {
			return err
		}
		reset.Env = safeMakepkgEnv()
		reset.Stdout = nil
		reset.Stderr = stderrFor(quiet)
		if err := reset.Run(); err != nil {
			return fmt.Errorf("git reset failed for %s: %w", pkgName, err)
		}
		return nil
	}

	if _, err := os.Stat(pkgDir); err == nil {
		os.RemoveAll(pkgDir)
	}
	if err := os.MkdirAll(filepath.Dir(pkgDir), 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	if !quiet {
		fmt.Printf("  %s cloning %s...\n", arrow, gitURL)
	}
	cmd, err := unprivilegedCommand("git", "clone", "--depth=1", gitURL, pkgDir)
	if err != nil {
		return err
	}
	cmd.Env = safeMakepkgEnv()
	cmd.Stdout = nil
	cmd.Stderr = stderrFor(quiet)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed for %s: %w", pkgName, err)
	}
	return nil
}

// stderrFor returns the destination for subprocess diagnostics: visible
// during normal installs, suppressed during quiet background syncs.
func stderrFor(quiet bool) io.Writer {
	if quiet {
		return nil
	}
	return os.Stderr
}

// Local builds and ABS fetching

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
	if err := priv.Invalidate(); err != nil {
		warnStderr("failed to invalidate sudo credentials before makepkg: %v", err)
	}

	makepkgArgs := []string{"-s", "--nodeps"}
	if noConfirm {
		makepkgArgs = append(makepkgArgs, "--noconfirm")
	}
	makepkg, err := unprivilegedCommand("makepkg", makepkgArgs...)
	if err != nil {
		return err
	}
	makepkg.Env = safeMakepkgEnv()
	makepkg.Dir = dir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return fmt.Errorf("makepkg failed: %w", err)
	}

	// Verify GPG signatures on all built packages before installing
	builtPkgs, err := findBuiltPackages(dir)
	if err != nil {
		return fmt.Errorf("failed to list built packages in %s: %w", dir, err)
	}
	if len(builtPkgs) == 0 {
		return fmt.Errorf("makepkg produced no packages in %s", dir)
	}

	if err := installBuiltPackages(builtPkgs, noConfirm); err != nil {
		return err
	}

	fmt.Printf("  %s  %s built and installed\n", ok, pkgname)
	return nil
}

// BuildLocal builds from a local PKGBUILD.
func BuildLocal(dir string, noConfirm bool) error {
	ok, warn, arrow := configSymbols()

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

	fmt.Println()
	fmt.Printf("  %s  Building from a local PKGBUILD — review before proceeding.\n", warn)
	fmt.Println()
	fmt.Printf("  :: Building local package: %s\n", pkgname)

	allDeps := append(deps, makedeps...)
	repoDeps, aurOrdered, err := resolveLocalDeps(allDeps, newPkgCache(), warn)
	if err != nil {
		return err
	}

	if err := installRepoDeps(repoDeps, noConfirm, arrow); err != nil {
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
	cmd, err := unprivilegedCommand("git", "clone", "--depth=1", gitURL, outDir)
	if err != nil {
		return "", err
	}
	cmd.Env = safeMakepkgEnv()
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to fetch %s from ABS: %w\n  (is it a valid official package?)", pkgName, err)
	}
	return outDir, nil
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
		case strings.HasPrefix(t, "depends=(") || strings.HasPrefix(t, "depends+=("):
			inDeps = true
			inner := strings.TrimPrefix(t, "depends=(")
			inner = strings.TrimPrefix(inner, "+")
			inner = strings.TrimSuffix(inner, ")")
			deps = append(deps, parseArrayLine(inner)...)
			if strings.Contains(t, ")") {
				inDeps = false
			}
		case strings.HasPrefix(t, "makedepends=(") || strings.HasPrefix(t, "makedepends+=("):
			inMake = true
			inner := strings.TrimPrefix(t, "makedepends=(")
			inner = strings.TrimPrefix(inner, "+")
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
		cmd.Run()		} else {
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

// - Root detection & privilege dropping for makepkg -
// makepkg refuses to run as root ("Running makepkg as root is not allowed").
// When invoked via sudo/doas, unprivilegedCommand drops privileges to the
// original user so makepkg and git run safely without root permissions.
// Running as pure root (no SUDO_USER / DOAS_USER) is rejected with an error.

// originalUser returns the non-root user who invoked the command via sudo/doas,
// or "" if not running under privilege escalation.
func originalUser() string {
	if u := os.Getenv("SUDO_USER"); u != "" && u != "root" {
		return u
	}
	if u := os.Getenv("DOAS_USER"); u != "" && u != "root" {
		return u
	}
	return ""
}

// unprivilegedCommand creates an exec.Cmd configured to run as a non-root user.
// If the process is running as root under sudo or doas, it executes the command
// via sudo -u $SUDO_USER -H (or doas -u $DOAS_USER).
// If running as pure root (no invoking user), it returns an error because makepkg
// strictly forbids root execution.
func unprivilegedCommand(name string, args ...string) (*exec.Cmd, error) {
	if os.Geteuid() != 0 {
		return exec.Command(name, args...), nil
	}

	orig := originalUser()
	if orig == "" {
		return nil, fmt.Errorf(
			"refusing to run %s as root: Arch Linux makepkg does not allow root execution.\n" +
				"Please run alps as a regular user (alps will request sudo privileges for pacman when needed).",
			name,
		)
	}

	if os.Getenv("DOAS_USER") != "" {
		fullArgs := append([]string{"-u", orig, "--", name}, args...)
		return exec.Command("doas", fullArgs...), nil
	}

	fullArgs := append([]string{"-u", orig, "-H", "--", name}, args...)
	return exec.Command("sudo", fullArgs...), nil
}

// - Environment variable sanitization for makepkg -
// makepkg executes PKGBUILD shell functions (prepare, build, package) which
// can read any exported env var. Sensitive vars like GITHUB_TOKEN, AWS keys,
// DATABASE_URL, etc. must not leak into the build subprocess.

// isSensitiveEnvKey returns true if key contains patterns associated with secrets.
func isSensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, sub := range []string{
		"TOKEN", "SECRET", "PASSWORD", "PASSWD", "AUTH",
		"API_KEY", "APIKEY", "CREDENTIAL", "AWS_", "GITHUB_",
		"GITLAB_", "NPM_", "DOCKER_", "SSH_AUTH_SOCK",
	} {
		if strings.Contains(upper, sub) {
			return true
		}
	}
	return false
}

// safeMakepkgEnv returns a minimal, sanitized environment for makepkg and git.
// Only variables needed for building are kept; everything else (especially
// tokens and secrets) is stripped.
func safeMakepkgEnv() []string {
	allowed := map[string]bool{
		"PATH":            true,
		"HOME":            true,
		"USER":            true,
		"LOGNAME":         true,
		"SHELL":           true,
		"LANG":            true,
		"LC_ALL":          true,
		"LC_COLLATE":      true,
		"LC_CTYPE":        true,
		"LC_MESSAGES":     true,
		"LC_NUMERIC":      true,
		"LC_TIME":         true,
		"TERM":            true,
		"COLORTERM":       true,
		"TMPDIR":          true,
		"CC":              true,
		"CXX":             true,
		"CFLAGS":          true,
		"CXXFLAGS":        true,
		"CPPFLAGS":        true,
		"LDFLAGS":         true,
		"MAKEFLAGS":       true,
		"MAKEJ":           true,
		"NINJAFLAGS":      true,
		"RUSTFLAGS":       true,
		"GOFLAGS":         true,
		"GOPROXY":         true,
		"PKG_CONFIG_PATH": true,
		"XDG_DATA_HOME":   true,
		"XDG_CONFIG_HOME": true,
		"XDG_CACHE_HOME":  true,
		"XDG_RUNTIME_DIR": true,
		"PKGEXT":          true,
		"SRCEXT":          true,
		"BUILDDIR":        true,
		"SRCPKGNAME":      true,
		"startdir":        true,
		"srcdir":          true,
		"pkgdir":          true,
		"pkgbase":         true,
		"pkgver":          true,
		"pkgrel":          true,
		"arch":            true,
		"MAKEPKG_CONF":    true,
		"PACMAN_CONF":     true,
		"http_proxy":      true,
		"https_proxy":     true,
		"ftp_proxy":       true,
		"all_proxy":       true,
		"no_proxy":        true,
		"HTTP_PROXY":      true,
		"HTTPS_PROXY":     true,
		"FTP_PROXY":       true,
		"ALL_PROXY":       true,
		"NO_PROXY":        true,
	}

	var env []string
	for _, e := range os.Environ() {
		key := e
		if idx := strings.Index(e, "="); idx >= 0 {
			key = e[:idx]
		}
		if allowed[key] && !isSensitiveEnvKey(key) {
			env = append(env, e)
		}
	}
	// Always ensure TERM is set for colored output
	hasTerm := false
	for _, e := range env {
		if strings.HasPrefix(e, "TERM=") {
			hasTerm = true
			break
		}
	}
	if !hasTerm {
		env = append(env, "TERM=xterm-256color")
	}
	return env
}
