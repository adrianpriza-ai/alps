package aur

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyPGPSignatureNoSigFile verifies that a missing .sig file
// produces no error (warning only) since most AUR packages don't ship signatures.
func TestVerifyPGPSignatureNoSigFile(t *testing.T) {
	dir := t.TempDir()
	// Create a dummy package file (no .sig file alongside)
	pkgPath := filepath.Join(dir, "test-pkg-1.0.0-1-x86_64.pkg.tar.zst")
	if err := os.WriteFile(pkgPath, []byte("fake package data"), 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	err := verifyPGPSignature(pkgPath)
	if err != nil {
		t.Errorf("expected no error for missing .sig file, got: %v", err)
	}
}

// TestVerifyPGPSignatureValidSig verifies that a valid GPG signature passes verification.
func TestVerifyPGPSignatureValidSig(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed — skipping GPG signature test")
	}

	// Create an ephemeral GPG homedir to avoid polluting the user's keyring
	gpgHome := t.TempDir()

	// Generate a test key (non-interactive, batch mode)
	keyConfig := `%no-protection
Key-Type: RSA
Key-Length: 2048
Subkey-Type: RSA
Subkey-Length: 2048
Name-Real: Test Signer
Name-Email: test@example.com
Expire-Date: 0
%commit
`
	configPath := filepath.Join(gpgHome, "batch.conf")
	if err := os.WriteFile(configPath, []byte(keyConfig), 0600); err != nil {
		t.Fatalf("failed to write GPG batch config: %v", err)
	}

	genKey := exec.Command("gpg", "--batch", "--homedir", gpgHome, "--gen-key", configPath)
	if out, err := genKey.CombinedOutput(); err != nil {
		t.Fatalf("gpg --gen-key failed: %v\n%s", err, string(out))
	}

	// Create a dummy package file
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "test-pkg-2.0.0-1-x86_64.pkg.tar.zst")
	pkgData := []byte("this is fake package content that we will sign")
	if err := os.WriteFile(pkgPath, pkgData, 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	// Create a detached signature
	sigPath := pkgPath + ".sig"
	sign := exec.Command("gpg", "--batch", "--homedir", gpgHome,
		"--armor", "--detach-sign", "--output", sigPath, pkgPath)
	if out, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("gpg --detach-sign failed: %v\n%s", err, string(out))
	}

	// Verify should succeed — but gpg --verify checks against the default
	// keyring. We need to import the key first or use --keyring.
	// Since verifyPGPSignature calls gpg without --homedir, we import the
	// test key into the user's temporary keyring. Instead, let's import
	// the key into the default keyring for this test (using --homedir on
	// verify is not how the production code works). So we export the public
	// key and import it into the default keyring, then clean up after.

	// Export the public key
	export := exec.Command("gpg", "--homedir", gpgHome, "--armor", "--export", "test@example.com")
	pubKey, err := export.Output()
	if err != nil {
		t.Fatalf("gpg --export failed: %v", err)
	}

	// Import into default keyring
	importCmd := exec.Command("gpg", "--import")
	importCmd.Stdin = strings.NewReader(string(pubKey))
	if out, err := importCmd.CombinedOutput(); err != nil {
		t.Logf("gpg --import note: %v\n%s (may already be imported)", err, string(out))
	}
	defer func() {
		// Clean up: delete the imported key from the default keyring
		exec.Command("gpg", "--batch", "--yes", "--delete-keys", "test@example.com").Run()
	}()

	// Now verify should succeed
	err = verifyPGPSignature(pkgPath)
	if err != nil {
		t.Errorf("expected valid signature to pass verification, got: %v", err)
	}
}

// TestVerifyPGPSignatureInvalidSig verifies that a tampered package
// (valid .sig, but modified file) fails verification.
func TestVerifyPGPSignatureInvalidSig(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed — skipping GPG signature test")
	}

	gpgHome := t.TempDir()

	keyConfig := `%no-protection
Key-Type: RSA
Key-Length: 2048
Name-Real: Tamper Test
Name-Email: tamper@example.com
Expire-Date: 0
%commit
`
	configPath := filepath.Join(gpgHome, "batch.conf")
	if err := os.WriteFile(configPath, []byte(keyConfig), 0600); err != nil {
		t.Fatalf("failed to write GPG batch config: %v", err)
	}

	genKey := exec.Command("gpg", "--batch", "--homedir", gpgHome, "--gen-key", configPath)
	if out, err := genKey.CombinedOutput(); err != nil {
		t.Fatalf("gpg --gen-key failed: %v\n%s", err, string(out))
	}

	// Create and sign a package
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "tampered-pkg-1.0.0-1-x86_64.pkg.tar.zst")
	if err := os.WriteFile(pkgPath, []byte("original content"), 0644); err != nil {
		t.Fatalf("failed to create test package: %v", err)
	}

	sigPath := pkgPath + ".sig"
	sign := exec.Command("gpg", "--batch", "--homedir", gpgHome,
		"--armor", "--detach-sign", "--output", sigPath, pkgPath)
	if out, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("gpg --detach-sign failed: %v\n%s", err, string(out))
	}

	// Import the public key so gpg --verify can find it
	export := exec.Command("gpg", "--homedir", gpgHome, "--armor", "--export", "tamper@example.com")
	pubKey, err := export.Output()
	if err != nil {
		t.Fatalf("gpg --export failed: %v", err)
	}
	importCmd := exec.Command("gpg", "--import")
	importCmd.Stdin = strings.NewReader(string(pubKey))
	importCmd.CombinedOutput() //nolint:errcheck
	defer exec.Command("gpg", "--batch", "--yes", "--delete-keys", "tamper@example.com").Run()

	// Tamper with the package AFTER signing
	if err := os.WriteFile(pkgPath, []byte("TAMPERED content!!!"), 0644); err != nil {
		t.Fatalf("failed to tamper package: %v", err)
	}

	// Verification should FAIL
	err = verifyPGPSignature(pkgPath)
	if err == nil {
		t.Error("expected error for tampered package, got nil")
	}
	if !strings.Contains(err.Error(), "GPG verification FAILED") {
		t.Errorf("expected error message to contain 'GPG verification FAILED', got: %v", err)
	}
}

// TestVerifyPGPSignatureWrongKey verifies that a signature made with a
// different key fails verification.
func TestVerifyPGPSignatureWrongKey(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed — skipping GPG signature test")
	}

	// Generate two separate keyrings with different keys
	signerHome := t.TempDir()
	wrongKeyHome := t.TempDir()

	// Signer key
	signerConfig := `%no-protection
Key-Type: RSA
Key-Length: 2048
Name-Real: Real Signer
Name-Email: real@example.com
Expire-Date: 0
%commit
`
	if err := os.WriteFile(filepath.Join(signerHome, "batch.conf"), []byte(signerConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("gpg", "--batch", "--homedir", signerHome, "--gen-key",
		filepath.Join(signerHome, "batch.conf")).CombinedOutput(); err != nil {
		t.Fatalf("gpg --gen-key (signer) failed: %v\n%s", err, string(out))
	}

	// Wrong key
	wrongConfig := `%no-protection
Key-Type: RSA
Key-Length: 2048
Name-Real: Wrong Key
Name-Email: wrong@example.com
Expire-Date: 0
%commit
`
	if err := os.WriteFile(filepath.Join(wrongKeyHome, "batch.conf"), []byte(wrongConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("gpg", "--batch", "--homedir", wrongKeyHome, "--gen-key",
		filepath.Join(wrongKeyHome, "batch.conf")).CombinedOutput(); err != nil {
		t.Fatalf("gpg --gen-key (wrong) failed: %v\n%s", err, string(out))
	}

	// Create and sign with the signer key
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "signed-pkg-1.0.0-1-x86_64.pkg.tar.zst")
	if err := os.WriteFile(pkgPath, []byte("signed content"), 0644); err != nil {
		t.Fatal(err)
	}

	sigPath := pkgPath + ".sig"
	sign := exec.Command("gpg", "--batch", "--homedir", signerHome,
		"--armor", "--detach-sign", "--output", sigPath, pkgPath)
	if out, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("gpg --detach-sign failed: %v\n%s", err, string(out))
	}

	// Import only the WRONG key into the default keyring
	wrongExport := exec.Command("gpg", "--homedir", wrongKeyHome, "--armor", "--export", "wrong@example.com")
	wrongPub, err := wrongExport.Output()
	if err != nil {
		t.Fatal(err)
	}
	importCmd := exec.Command("gpg", "--import")
	importCmd.Stdin = strings.NewReader(string(wrongPub))
	importCmd.CombinedOutput() //nolint:errcheck
	defer exec.Command("gpg", "--batch", "--yes", "--delete-keys", "wrong@example.com").Run()

	// Verification should FAIL — wrong key can't verify
	err = verifyPGPSignature(pkgPath)
	if err == nil {
		t.Error("expected error for wrong key signature, got nil")
	}
}

// TestFindBuiltPackages verifies that findBuiltPackages correctly identifies
// .pkg.tar.* files while excluding .sig files and directories.
func TestFindBuiltPackages(t *testing.T) {
	dir := t.TempDir()

	// Create various files
	files := []struct {
		name    string
		isDir   bool
		wantPkg bool
	}{
		{"foo-1.0-1-x86_64.pkg.tar.zst", false, true},
		{"foo-1.0-1-x86_64.pkg.tar.zst.sig", false, false},
		{"bar-2.0-1-x86_64.pkg.tar.xz", false, true},
		{"bar-2.0-1-x86_64.pkg.tar.xz.sig", false, false},
		{"not-a-package.txt", false, false},
		{"subdir", true, false},
		{"pkg.tar.zst", false, false}, // no .pkg prefix
	}

	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if f.isDir {
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatalf("failed to create dir %s: %v", f.name, err)
			}
		} else {
			if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
				t.Fatalf("failed to create file %s: %v", f.name, err)
			}
		}
	}

	pkgs, err := findBuiltPackages(dir)
	if err != nil {
		t.Fatalf("findBuiltPackages failed: %v", err)
	}

	// Should find exactly the two .pkg.tar.* files
	expectedCount := 2
	if len(pkgs) != expectedCount {
		t.Errorf("expected %d packages, got %d: %v", expectedCount, len(pkgs), pkgs)
	}

	for _, pkg := range pkgs {
		base := filepath.Base(pkg)
		if !strings.Contains(base, ".pkg.tar.") || strings.HasSuffix(base, ".sig") {
			t.Errorf("unexpected file in results: %s", base)
		}
	}
}

// TestFindBuiltPackagesEmpty verifies that findBuiltPackages returns nil
// for an empty directory.
func TestFindBuiltPackagesEmpty(t *testing.T) {
	dir := t.TempDir()
	pkgs, err := findBuiltPackages(dir)
	if err != nil {
		t.Fatalf("findBuiltPackages failed: %v", err)
	}
	if pkgs != nil {
		t.Errorf("expected nil for empty dir, got %v", pkgs)
	}
}

// TestFindBuiltPackagesNonexistent verifies that findBuiltPackages returns
// an error for a nonexistent directory.
func TestFindBuiltPackagesNonexistent(t *testing.T) {
	_, err := findBuiltPackages("/nonexistent/path/12345")
	if err == nil {
		t.Error("expected error for nonexistent directory, got nil")
	}
}

// TestVerifyAURRemoteMatch verifies that verifyAURRemote succeeds
// when the remote URL matches the expected URL.
func TestVerifyAURRemoteMatch(t *testing.T) {
	dir := t.TempDir()

	// Init a git repo with a known remote URL
	expectedURL := "https://aur.archlinux.org/test-pkg.git"
	init := exec.Command("git", "init", dir)
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(out))
	}
	remote := exec.Command("git", "-C", dir, "remote", "add", "origin", expectedURL)
	if out, err := remote.CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v\n%s", err, string(out))
	}

	err := verifyAURRemote(dir, expectedURL)
	if err != nil {
		t.Errorf("expected no error for matching URL, got: %v", err)
	}
}

// TestVerifyAURRemoteMismatch verifies that verifyAURRemote fails
// when the remote URL does not match the expected URL.
func TestVerifyAURRemoteMismatch(t *testing.T) {
	dir := t.TempDir()

	// Init a git repo with a WRONG remote URL (simulating URL rewriting)
	expectedURL := "https://aur.archlinux.org/test-pkg.git"
	wrongURL := "https://evil.example.com/test-pkg.git"
	init := exec.Command("git", "init", dir)
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(out))
	}
	remote := exec.Command("git", "-C", dir, "remote", "add", "origin", wrongURL)
	if out, err := remote.CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v\n%s", err, string(out))
	}

	err := verifyAURRemote(dir, expectedURL)
	if err == nil {
		t.Error("expected error for mismatched URL, got nil")
	}
	if !strings.Contains(err.Error(), "remote URL mismatch") {
		t.Errorf("expected 'remote URL mismatch' in error, got: %v", err)
	}
}

// TestVerifyAURRemoteNoRemote verifies that verifyAURRemote returns an error
// when the repo has no remote configured.
func TestVerifyAURRemoteNoRemote(t *testing.T) {
	dir := t.TempDir()
	init := exec.Command("git", "init", dir)
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(out))
	}
	// No remote added — get-url will fail
	err := verifyAURRemote(dir, "https://aur.archlinux.org/foo.git")
	if err == nil {
		t.Error("expected error for missing remote, got nil")
	}
}

// TestSafeMakepkgEnvStripsSecrets verifies that sensitive environment variables
// (tokens, secrets, passwords, AWS keys) are stripped from the environment passed to makepkg.
func TestSafeMakepkgEnvStripsSecrets(t *testing.T) {
	// Set test environment variables
	t.Setenv("GITHUB_TOKEN", "ghp_secret_token_12345")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("DATABASE_PASSWORD", "super_secret_db_pass")
	t.Setenv("CUSTOM_API_KEY", "secret_key_value")
	t.Setenv("MY_PRIVATE_AUTH", "bearer_token")
	t.Setenv("MAKEFLAGS", "-j8")
	t.Setenv("PATH", "/usr/bin:/bin")

	env := safeMakepkgEnv()

	// Verify secrets are NOT present
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		key := parts[0]
		if isSensitiveEnvKey(key) {
			t.Errorf("safeMakepkgEnv leaked sensitive key %q: %s", key, e)
		}
		if key == "GITHUB_TOKEN" || key == "AWS_SECRET_ACCESS_KEY" ||
			key == "DATABASE_PASSWORD" || key == "CUSTOM_API_KEY" || key == "MY_PRIVATE_AUTH" {
			t.Errorf("safeMakepkgEnv contained forbidden variable %q", key)
		}
	}

	// Verify safe build flags ARE present
	hasMakeflags := false
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "MAKEFLAGS=") {
			hasMakeflags = true
		}
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
		}
	}
	if !hasMakeflags {
		t.Error("safeMakepkgEnv stripped safe variable MAKEFLAGS")
	}
	if !hasPath {
		t.Error("safeMakepkgEnv stripped safe variable PATH")
	}
}

// TestUnprivilegedCommandNonRoot verifies that when running as non-root,
// unprivilegedCommand returns a direct execution command without sudo.
func TestUnprivilegedCommandNonRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — skipping non-root test")
	}
	cmd, err := unprivilegedCommand("echo", "hello")
	if err != nil {
		t.Fatalf("unprivilegedCommand failed: %v", err)
	}
	if cmd.Path == "" && len(cmd.Args) == 0 {
		t.Fatal("empty command returned")
	}
	if cmd.Args[0] != "echo" {
		t.Errorf("expected command 'echo', got %q", cmd.Args[0])
	}
}

