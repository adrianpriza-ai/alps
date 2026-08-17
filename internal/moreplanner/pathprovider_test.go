package moreplanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPathProvider_CacheDir(t *testing.T) {
	provider := NewDefaultPathProvider()
	cacheDir := provider.CacheDir()

	if cacheDir == "" {
		t.Error("CacheDir should not be empty")
	}

	// Should contain "alps" or "cache"
	if !containsPathComponent(cacheDir, "alps") && !containsPathComponent(cacheDir, "cache") {
		t.Errorf("CacheDir should contain 'alps' or 'cache', got %s", cacheDir)
	}
}

func TestDefaultPathProvider_LibDir(t *testing.T) {
	provider := NewDefaultPathProvider()
	libDir := provider.LibDir()

	if libDir == "" {
		t.Error("LibDir should not be empty")
	}

	// Should contain "alps" or "lib"
	if !containsPathComponent(libDir, "alps") && !containsPathComponent(libDir, "lib") {
		t.Errorf("LibDir should contain 'alps' or 'lib', got %s", libDir)
	}
}

func TestDefaultPathProvider_BuildDir(t *testing.T) {
	provider := NewDefaultPathProvider()

	tests := []struct {
		name    string
		pkgName string
		wantErr bool
	}{
		{"valid package", "my-package", false},
		{"valid package with numbers", "package123", false},
		{"valid package with underscores", "my_package", false},
		{"valid package with dots", "my.package", false},
		{"valid package with plus", "my+package", false},
		{"empty package name", "", true},
		{"package with path traversal", "../etc", true},
		{"package with slash", "my/package", true},
		{"package with backslash", "my\\package", true},
		{"package starting with dot", ".hidden", true},
		{"package too long", string(make([]byte, 256)), true},
		{"package with invalid chars", "my@package", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildDir, err := provider.BuildDir(tt.pkgName)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && buildDir == "" {
				t.Error("BuildDir should not be empty for valid package")
			}
		})
	}
}

func TestDefaultPathProvider_TempDir(t *testing.T) {
	provider := NewDefaultPathProvider()

	tempDir, err := provider.TempDir()
	if err != nil {
		t.Fatalf("TempDir() error = %v", err)
	}

	if tempDir == "" {
		t.Error("TempDir should not be empty")
	}

	// Verify the directory exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Error("TempDir should create a directory that exists")
	}

	// Clean up
	provider.CleanupTempDir(tempDir)
}

func TestDefaultPathProvider_CleanupTempDir(t *testing.T) {
	provider := NewDefaultPathProvider()

	tempDir, err := provider.TempDir()
	if err != nil {
		t.Fatalf("TempDir() error = %v", err)
	}

	// Create a test file in the temp directory
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Clean up
	if err := provider.CleanupTempDir(tempDir); err != nil {
		t.Errorf("CleanupTempDir() error = %v", err)
	}

	// Verify the directory is gone
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Error("TempDir should be removed after cleanup")
	}
}

func TestDefaultPathProvider_ScriptPath(t *testing.T) {
	provider := NewDefaultPathProvider()
	pkgDir := "/tmp/test_package"
	scriptContent := "#!/bin/bash\necho test"

	scriptPath, err := provider.ScriptPath(pkgDir, scriptContent)
	if err != nil {
		t.Fatalf("ScriptPath() error = %v", err)
	}

	if scriptPath == "" {
		t.Error("ScriptPath should not be empty")
	}

	// Should be under the package directory
	if !filepath.HasPrefix(scriptPath, pkgDir) {
		t.Errorf("ScriptPath should be under package directory, got %s", scriptPath)
	}

	// Should have .sh extension
	if filepath.Ext(scriptPath) != ".sh" {
		t.Errorf("ScriptPath should have .sh extension, got %s", scriptPath)
	}
}

func TestDefaultPathProvider_ManifestPath(t *testing.T) {
	provider := NewDefaultPathProvider()

	manifestPath := provider.ManifestPath()
	if manifestPath == "" {
		t.Error("ManifestPath should not be empty")
	}

	// Should contain .alps_runner.txt
	if !containsPathComponent(manifestPath, ".alps_runner.txt") {
		t.Errorf("ManifestPath should contain .alps_runner.txt, got %s", manifestPath)
	}
}

func TestTestPathProvider(t *testing.T) {
	testRoot := t.TempDir()
	provider := NewTestPathProvider(testRoot)

	// Test that it uses the test root
	cacheDir := provider.CacheDir()
	if !filepath.HasPrefix(cacheDir, testRoot) {
		t.Errorf("TestPathProvider CacheDir should be under test root, got %s", cacheDir)
	}

	libDir := provider.LibDir()
	if !filepath.HasPrefix(libDir, testRoot) {
		t.Errorf("TestPathProvider LibDir should be under test root, got %s", libDir)
	}

	// Test cleanup
	if err := provider.Cleanup(); err != nil {
		t.Errorf("TestPathProvider Cleanup() error = %v", err)
	}

	// Verify the test root is gone
	if _, err := os.Stat(testRoot); !os.IsNotExist(err) {
		t.Error("Test root should be removed after cleanup")
	}
}

func TestValidatePackageName(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		wantErr bool
	}{
		{"valid simple", "mypackage", false},
		{"valid with hyphen", "my-package", false},
		{"valid with underscore", "my_package", false},
		{"valid with dot", "my.package", false},
		{"valid with plus", "my+package", false},
		{"valid mixed", "my-package_1.0+build", false},
		{"empty", "", true},
		{"path traversal", "../etc", true},
		{"absolute path", "/etc/passwd", true},
		{"with slash", "my/package", true},
		{"with backslash", "my\\package", true},
		{"starts with dot", ".hidden", true},
		{"too long", string(make([]byte, 256)), true},
		{"with space", "my package", true},
		{"with special char", "my@package", true},
		{"with null", "my\x00package", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePackageName(tt.pkgName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePackageName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsPathUnderRoot(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		root     string
		expected bool
	}{
		{"same directory", "/tmp/test", "/tmp/test", true},
		{"subdirectory", "/tmp/test/sub", "/tmp/test", true},
		{"different directory", "/tmp/other", "/tmp/test", false},
		{"parent directory", "/tmp", "/tmp/test", false},
		{"path traversal", "/tmp/test/../other", "/tmp/test", false},
		{"absolute paths", "/var/cache/alps", "/var/cache", true},
		{"relative paths", "cache/alps", "cache", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathUnderRoot(tt.path, tt.root)
			if result != tt.expected {
				t.Errorf("isPathUnderRoot() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestSimpleHash(t *testing.T) {
	tests := []struct {
		input   string
		notZero bool
	}{
		{"empty string", true}, // Initial value is 5381, so even empty string returns non-zero
		{"test", true},
		{"different", true},
		{"same", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hash := simpleHash(tt.input)
			if tt.notZero && hash == 0 {
				t.Error("simpleHash should return non-zero for input")
			}
		})
	}

	// Test that same input produces same hash
	hash1 := simpleHash("test")
	hash2 := simpleHash("test")
	if hash1 != hash2 {
		t.Error("simpleHash should be deterministic")
	}

	// Test that different input produces different hash
	hash3 := simpleHash("different")
	if hash1 == hash3 {
		t.Error("simpleHash should produce different hashes for different inputs")
	}
}

func TestIsValidPackageNameChar(t *testing.T) {
	tests := []struct {
		char  rune
		valid bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'-', true},
		{'_', true},
		{'.', true},
		{'+', true},
		{' ', false},
		{'@', false},
		{'#', false},
		{'$', false},
		{'%', false},
		{'/', false},
		{'\\', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.char), func(t *testing.T) {
			result := isValidPackageNameChar(tt.char)
			if result != tt.valid {
				t.Errorf("isValidPackageNameChar(%c) = %v, expected %v", tt.char, result, tt.valid)
			}
		})
	}
}

// Helper function to check if a path contains a specific component
func containsPathComponent(path, component string) bool {
	return strings.Contains(path, component)
}
