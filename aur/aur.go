package aur

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const aurRPC = "https://aur.archlinux.org/rpc/v5/search/"
const aurInfo = "https://aur.archlinux.org/rpc/v5/info/"
const aurGit = "https://aur.archlinux.org/"

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

// Search mencari package di AUR
func Search(query string) ([]Package, error) {
	resp, err := http.Get(aurRPC + query)
	if err != nil {
		return nil, fmt.Errorf("gagal koneksi ke AUR: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result rpcResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("gagal parse response AUR: %v", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("AUR error: %s", result.Error)
	}
	return result.Results, nil
}

// Info mengambil info detail satu package
func Info(name string) (*Package, error) {
	resp, err := http.Get(aurInfo + name)
	if err != nil {
		return nil, fmt.Errorf("gagal koneksi ke AUR: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result rpcResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("gagal parse response AUR: %v", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("AUR error: %s", result.Error)
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("package '%s' tidak ditemukan di AUR", name)
	}
	return &result.Results[0], nil
}

// Install clone PKGBUILD dari AUR lalu makepkg -si
func Install(pkgName string, noConfirm bool) error {
	// Cek package ada di AUR
	pkg, err := Info(pkgName)
	if err != nil {
		return err
	}

	// Buat temp dir
	tmpDir, err := os.MkdirTemp("", "alps-aur-*")
	if err != nil {
		return fmt.Errorf("gagal buat temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneURL := aurGit + pkg.URLPath
	// URLPath biasanya /cgit/aur.git/snapshot/pkgname.tar.gz
	// Tapi lebih reliable pakai git clone
	gitURL := fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg.Name)

	fmt.Printf("  Cloning %s...\n", gitURL)
	cloneCmd := exec.Command("git", "clone", "--depth=1", gitURL, filepath.Join(tmpDir, pkg.Name))
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("gagal clone: %v", err)
	}

	pkgDir := filepath.Join(tmpDir, pkg.Name)

	// Tampilkan PKGBUILD untuk review
	pkgbuild := filepath.Join(pkgDir, "PKGBUILD")
	if err := reviewPKGBUILD(pkgbuild); err != nil {
		return err
	}

	// makepkg -si
	makepkgArgs := []string{"-si"}
	if noConfirm {
		makepkgArgs = append(makepkgArgs, "--noconfirm")
	}

	fmt.Printf("\n  Building %s %s...\n", pkg.Name, pkg.Version)
	makepkg := exec.Command("makepkg", makepkgArgs...)
	makepkg.Dir = pkgDir
	makepkg.Stdout = os.Stdout
	makepkg.Stderr = os.Stderr
	makepkg.Stdin = os.Stdin
	if err := makepkg.Run(); err != nil {
		return fmt.Errorf("makepkg gagal: %v", err)
	}

	_ = cloneURL
	return nil
}

// Remove pakai pacman -R
func Remove(pkgName string, noConfirm bool) error {
	args := []string{"-R", pkgName}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	cmd := exec.Command("sudo", append([]string{"pacman"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// Exists cek apakah package ada di AUR
func Exists(name string) bool {
	_, err := Info(name)
	return err == nil
}

func reviewPKGBUILD(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("gagal baca PKGBUILD: %v", err)
	}

	fmt.Println("\n  \033[1;33m⚠  Review PKGBUILD:\033[0m")
	fmt.Println("  " + strings.Repeat("─", 40))

	lines := strings.Split(string(content), "\n")
	// Tampilkan hanya baris penting (pkgname, pkgver, source, md5sums)
	important := []string{"pkgname", "pkgver", "pkgrel", "source", "sha", "md5", "url="}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, key := range important {
			if strings.HasPrefix(strings.ToLower(trimmed), key) {
				fmt.Printf("  \033[2m%s\033[0m\n", trimmed)
				break
			}
		}
	}
	fmt.Println("  " + strings.Repeat("─", 40))

	fmt.Print("\n  Lanjutkan install? [Y/n] ")
	var input string
	fmt.Scanln(&input)
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "n" || input == "no" {
		return fmt.Errorf("install dibatalkan")
	}
	return nil
}
