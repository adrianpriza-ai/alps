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
	"strings"
)

const (
	aurRPCSearch = "https://aur.archlinux.org/rpc/v5/search/"
	aurRPCInfo   = "https://aur.archlinux.org/rpc/v5/info/"
)

type Package struct {
	Name        string  `json:"Name"`
	Version     string  `json:"Version"`
	Description string  `json:"Description"`
	URL         string  `json:"URL"`
	Votes       int     `json:"NumVotes"`
	Popularity  float64 `json:"Popularity"`
	Maintainer  string  `json:"Maintainer"`
	URLPath     string  `json:"URLPath"`
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

// fetchRPC is a shared helper for AUR RPC calls.
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

// Search searches for packages in AUR by keyword.
func Search(query string) ([]Package, error) {
	result, err := fetchRPC(aurRPCSearch + query)
	if err != nil {
		return nil, err
	}
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

// Exists reports whether a package exists in AUR.
func Exists(name string) bool {
	_, err := Info(name)
	return err == nil
}

// Install installs one or more AUR packages.
// Uses yay if available, otherwise falls back to manual makepkg.
func Install(pkgNames []string, noConfirm bool) error {
	if len(pkgNames) == 0 {
		return nil
	}

	if helper := DetectHelper(); helper == "yay" {
		return installWithYay(pkgNames, noConfirm)
	}
	// Fallback: install satu-satu dengan makepkg
	for _, name := range pkgNames {
		if err := installWithMakepkg(name, noConfirm); err != nil {
			return err
		}
	}
	return nil
}

// installWithYay delegates install to yay.
func installWithYay(pkgNames []string, noConfirm bool) error {
	args := append([]string{"-S"}, pkgNames...)
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	fmt.Printf("  Using yay for: %s\n", strings.Join(pkgNames, " "))
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

// installWithMakepkg clones PKGBUILD and builds with makepkg -si.
func installWithMakepkg(pkgName string, noConfirm bool) error {
	pkg, err := Info(pkgName)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "alps-aur-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg.Name)
	pkgDir := filepath.Join(tmpDir, pkg.Name)

	fmt.Printf("  Cloning %s...\n", gitURL)
	cloneCmd := exec.Command("git", "clone", "--depth=1", gitURL, pkgDir)
	cloneCmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	if !noConfirm {
		if err := reviewPKGBUILD(filepath.Join(pkgDir, "PKGBUILD")); err != nil {
			return err
		}
	}

	makepkgArgs := []string{"-si"}
	if noConfirm {
		makepkgArgs = append(makepkgArgs, "--noconfirm")
	}

	fmt.Printf("\n  Building %s %s...\n", pkg.Name, pkg.Version)
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

func reviewPKGBUILD(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read PKGBUILD: %w", err)
	}

	fmt.Println("\n  \033[1;33m⚠  Review PKGBUILD:\033[0m")
	fmt.Println("  " + strings.Repeat("─", 40))

	important := []string{"pkgname", "pkgver", "pkgrel", "source", "sha", "md5", "url="}
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, key := range important {
			if strings.HasPrefix(lower, key) {
				fmt.Printf("  \033[2m%s\033[0m\n", trimmed)
				break
			}
		}
	}
	fmt.Println("  " + strings.Repeat("─", 40))

	fmt.Print("\n  Continue with install? [Y/n] ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) == "n" {
		return fmt.Errorf("install cancelled by user")
	}
	return nil
}
