package aur

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/adrianpriza-ai/alps/priv"
)

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

func isInstalled(name string) bool {
	return exec.Command("pacman", "-Qi", name).Run() == nil
}

func inPacmanRepo(name string) bool {
	return exec.Command("pacman", "-Si", name).Run() == nil
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
