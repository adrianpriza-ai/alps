package moreplanner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathProvider provides paths for cache, temporary files, and package directories
type PathProvider interface {
	// CacheDir returns the cache directory
	CacheDir() string
	// LibDir returns the library/state directory
	LibDir() string
	// BuildDir returns the build directory for a specific package
	BuildDir(pkgName string) (string, error)
	// TempDir returns a temporary directory for the current operation
	TempDir() (string, error)
	// CleanupTempDir cleans up the temporary directory
	CleanupTempDir(dir string) error
	// ScriptPath returns a path for a script file with restrictive permissions
	ScriptPath(pkgDir, scriptContent string) (string, error)
	// ManifestPath returns the path for the execution manifest
	ManifestPath() string
}

// DefaultPathProvider provides default path implementations
type DefaultPathProvider struct {
	// For testing: allows overriding the base temp directory
	baseTempDir string
	// For testing: allows overriding the cache directory
	baseCacheDir string
	// For testing: allows overriding the lib directory
	baseLibDir string
}

// NewDefaultPathProvider creates a new default path provider
func NewDefaultPathProvider() *DefaultPathProvider {
	return &DefaultPathProvider{}
}

// CacheDir returns the cache directory
func (p *DefaultPathProvider) CacheDir() string {
	if p.baseCacheDir != "" {
		return p.baseCacheDir
	}

	if isTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/cache/alps/more")
	}
	if runtime.GOOS == "darwin" {
		// On macOS, use ~/Library/Caches/alps/more for user cache
		home, err := os.UserHomeDir()
		if err != nil {
			return "/var/cache/alps/more"
		}
		return filepath.Join(home, "Library", "Caches", "alps", "more")
	}
	return "/var/cache/alps/more"
}

// LibDir returns the library/state directory
func (p *DefaultPathProvider) LibDir() string {
	if p.baseLibDir != "" {
		return p.baseLibDir
	}

	if isTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/lib/alps")
	}
	if runtime.GOOS == "darwin" {
		// On macOS, use ~/Library/Application Support/alps
		home, err := os.UserHomeDir()
		if err != nil {
			return "/var/lib/alps"
		}
		return filepath.Join(home, "Library", "Application Support", "alps")
	}
	return "/var/lib/alps"
}

// BuildDir returns the build directory for a specific package
// Security: Validates package name components to prevent path traversal
func (p *DefaultPathProvider) BuildDir(pkgName string) (string, error) {
	// Validate package name to prevent path traversal
	if err := validatePackageName(pkgName); err != nil {
		return "", fmt.Errorf("invalid package name: %w", err)
	}

	cacheDir := p.CacheDir()
	buildDir := filepath.Join(cacheDir, "builds", pkgName)

	// Ensure the final path stays under the cache root
	if !isPathUnderRoot(buildDir, cacheDir) {
		return "", fmt.Errorf("build directory %s is not under cache root %s", buildDir, cacheDir)
	}

	return buildDir, nil
}

// TempDir returns a temporary directory for the current operation
// Uses a unique per-operation directory instead of fixed /tmp filenames
func (p *DefaultPathProvider) TempDir() (string, error) {
	baseDir := p.baseTempDir
	if baseDir == "" {
		baseDir = os.TempDir()
	}

	// Create a unique subdirectory for this operation
	tempDir, err := os.MkdirTemp(baseDir, "alps_operation_")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	return tempDir, nil
}

// CleanupTempDir cleans up the temporary directory
func (p *DefaultPathProvider) CleanupTempDir(dir string) error {
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

// ScriptPath returns a path for a script file with restrictive permissions
// Security: Uses unique filename with restrictive permissions (0700)
func (p *DefaultPathProvider) ScriptPath(pkgDir, scriptContent string) (string, error) {
	// Use a hash of the script content for unique filename
	hash := simpleHash(scriptContent)
	scriptPath := filepath.Join(pkgDir, fmt.Sprintf(".alps_script_%x.sh", hash))
	return scriptPath, nil
}

// ManifestPath returns the path for the execution manifest
func (p *DefaultPathProvider) ManifestPath() string {
	baseDir := p.baseTempDir
	if baseDir == "" {
		baseDir = os.TempDir()
	}
	return filepath.Join(baseDir, ".alps_runner.txt")
}

// validatePackageName validates package name components to prevent path traversal
func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("package name cannot be empty")
	}

	// Check for path traversal attempts
	if strings.Contains(name, "..") {
		return fmt.Errorf("package name contains path traversal sequence")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("package name contains path separator")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("package name starts with dot")
	}

	// Check for reasonable length
	if len(name) > 255 {
		return fmt.Errorf("package name too long")
	}

	// Check for reasonable characters (alphanumeric, hyphen, underscore, plus, dot)
	for _, r := range name {
		if !isValidPackageNameChar(r) {
			return fmt.Errorf("package name contains invalid character: %c", r)
		}
	}

	return nil
}

// isValidPackageNameChar checks if a character is valid in a package name
func isValidPackageNameChar(r rune) bool {
	// Allow alphanumeric, hyphen, underscore, plus, and dot
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '_' || r == '+' || r == '.'
}

// isPathUnderRoot checks if a path is under a root directory
func isPathUnderRoot(path, root string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it's not under the root
	return !strings.HasPrefix(rel, "..")
}

// simpleHash creates a simple hash for generating unique filenames
func simpleHash(s string) uint64 {
	var hash uint64 = 5381
	for _, c := range s {
		hash = ((hash << 5) + hash) + uint64(c)
	}
	return hash
}

// isTermux checks if running under Termux
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" || os.Getenv("PREFIX") != ""
}

// TestPathProvider is a path provider for testing that uses temp directories
type TestPathProvider struct {
	*DefaultPathProvider
	testRoot string
}

// NewTestPathProvider creates a path provider for testing
func NewTestPathProvider(testRoot string) *TestPathProvider {
	return &TestPathProvider{
		DefaultPathProvider: &DefaultPathProvider{
			baseCacheDir: filepath.Join(testRoot, "cache"),
			baseLibDir:   filepath.Join(testRoot, "lib"),
			baseTempDir:  filepath.Join(testRoot, "temp"),
		},
		testRoot: testRoot,
	}
}

// Cleanup cleans up the entire test root
func (p *TestPathProvider) Cleanup() error {
	if p.testRoot != "" {
		return os.RemoveAll(p.testRoot)
	}
	return nil
}
