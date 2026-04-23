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
)

const (
	aurRPCSearch = "https://aur.archlinux.org/rpc/v5/search/"
	aurRPCInfo   = "https://aur.archlinux.org/rpc/v5/info/"
)

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

// Search searches for packages in AUR sorted by votes.
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

// PrintPackageInfo prints full package details before install.
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

// Install installs one or more AUR packages.
func Install(pkgNames []string, noConfirm bool) error {
	if len(pkgNames) == 0 {
		return nil
	}
	if helper := DetectHelper(); helper == "yay" {
		return installWithYay(pkgNames, noConfirm)
	}
	for _, name := range pkgNames {
		if err := installWithMakepkg(name, noConfirm); err != nil {
			return err
		}
	}
	return nil
}

func installWithYay(pkgNames []string, noConfirm bool) error {
	args := append([]string{"-S"}, pkgNames...)
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	fmt.Printf("  → using yay: %s\n\n", strings.Join(pkgNames, " "))
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

func installWithMakepkg(pkgName string, noConfirm bool) error {
	pkg, err := Info(pkgName)
	if err != nil {
		return err
	}

	PrintPackageInfo(pkg)

	if pkg.OutOfDate != 0 {
		fmt.Printf("  ⚠  out-of-date. Continue anyway? [y/N] ")
		var inp string
		fmt.Scanln(&inp)
		if strings.ToLower(strings.TrimSpace(inp)) != "y" {
			return fmt.Errorf("install cancelled")
		}
	}

	// Resolve AUR dependencies before building
	if err := resolveAURDeps(pkg, noConfirm); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "alps-aur-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg.Name)
	pkgDir := filepath.Join(tmpDir, pkg.Name)

	fmt.Printf("  → cloning %s...\n", gitURL)
	cloneCmd := exec.Command("git", "clone", "--depth=1", gitURL, pkgDir)
	cloneCmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cloneCmd.Stdout = nil
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	fmt.Printf("  ✓  cloned\n\n")

	if !noConfirm {
		if err := reviewPKGBUILD(filepath.Join(pkgDir, "PKGBUILD")); err != nil {
			return err
		}
	}

	makepkgArgs := []string{"-si"}
	if noConfirm {
		makepkgArgs = append(makepkgArgs, "--noconfirm")
	}

	fmt.Printf("\n  → building %s %s...\n\n", pkg.Name, pkg.Version)
	makepkg := exec.Command("makepkg", makepkgArgs...)
	makepkg.Env = append(os.Environ(), "TERM=xterm-256color")
	makepkg.Dir = pkgDir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return fmt.Errorf("makepkg failed: %w", err)
	}
	return nil
}

// resolveAURDeps checks depends+makedepends of pkg.
// If a dep is not in pacman repo, checks AUR and installs it first (one level only).
// If a dep is not found anywhere, returns error.
func resolveAURDeps(pkg *Package, noConfirm bool) error {
	allDeps := append(pkg.Depends, pkg.MakeDepends...)
	if len(allDeps) == 0 {
		return nil
	}

	var aurDeps []string
	var missing []string

	for _, dep := range allDeps {
		// Strip version constraints e.g. "curl>=7.0" → "curl"
		name := stripVerConstraint(dep)

		// Check if already installed
		if isInstalled(name) {
			continue
		}

		// Check pacman repo
		if inPacmanRepo(name) {
			continue // makepkg -si will install it
		}

		// Check AUR
		if Exists(name) {
			// Check if this AUR dep itself has AUR deps (one level check)
			depPkg, err := Info(name)
			if err == nil {
				for _, d := range append(depPkg.Depends, depPkg.MakeDepends...) {
					n := stripVerConstraint(d)
					if !isInstalled(n) && !inPacmanRepo(n) && Exists(n) {
						return fmt.Errorf(
							"dependency %q of %q requires AUR package %q — install %q manually first, then retry",
							name, pkg.Name, n, n,
						)
					}
				}
			}
			aurDeps = append(aurDeps, name)
		} else {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"missing dependencies not found in repo or AUR: %s",
			strings.Join(missing, ", "),
		)
	}

	if len(aurDeps) > 0 {
		fmt.Printf("  :: AUR dependencies needed: %s\n\n", strings.Join(aurDeps, "  "))
		for _, dep := range aurDeps {
			fmt.Printf("  → installing AUR dependency: %s\n", dep)
			if err := installWithMakepkg(dep, noConfirm); err != nil {
				return fmt.Errorf("failed to install AUR dependency %q: %w", dep, err)
			}
		}
		fmt.Println()
	}

	return nil
}

// stripVerConstraint removes version constraints from dep strings.
// e.g. "curl>=7.0" → "curl", "python>3" → "python"
func stripVerConstraint(dep string) string {
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if idx := strings.Index(dep, op); idx != -1 {
			return dep[:idx]
		}
	}
	return dep
}

// isInstalled checks if a package is installed via pacman.
func isInstalled(name string) bool {
	return exec.Command("pacman", "-Qi", name).Run() == nil
}

// inPacmanRepo checks if a package exists in the sync db.
func inPacmanRepo(name string) bool {
	return exec.Command("pacman", "-Si", name).Run() == nil
}

// Remove removes a package using pacman -R.
func Remove(pkgName string, noConfirm bool) error {
	args := []string{"pacman", "-R", pkgName}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	cmd := exec.Command("sudo", args...)
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

func popularityBar(pop float64) string { return "" }

func reviewPKGBUILD(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PKGBUILD: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	important := []string{"pkgname", "pkgver", "pkgrel", "arch", "license", "source", "sha", "md5", "url=", "depends", "makedepends"}

	fmt.Println("  :: PKGBUILD summary ::")
	fmt.Println("  " + strings.Repeat("-", 44))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, key := range important {
			if strings.HasPrefix(lower, key) {
				fmt.Printf("     %s\n", trimmed)
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

	fmt.Print("\n  Proceed with install? [Y/n] ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) == "n" {
		return fmt.Errorf("install cancelled by user")
	}
	return nil
}
