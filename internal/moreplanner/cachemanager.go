package moreplanner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CacheManager handles cache operations with atomic writes and validation
type CacheManager struct {
	pathProvider PathProvider
}

// NewCacheManager creates a new cache manager
func NewCacheManager(provider PathProvider) *CacheManager {
	return &CacheManager{
		pathProvider: provider,
	}
}

// WriteCache writes data to a cache path atomically
// Security: Write to temporary file, fsync, then atomic rename to prevent partial writes
func (m *CacheManager) WriteCache(filename string, data []byte) error {
	cacheDir := m.pathProvider.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	targetPath := filepath.Join(cacheDir, filename)
	tmpPath := targetPath + ".tmp"

	// Write to temporary file
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Sync to ensure data is written to disk
	file, err := os.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to open temporary file for sync: %w", err)
	}
	defer file.Close()

	if err := file.Sync(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

// ReadCache reads data from a cache file
func (m *CacheManager) ReadCache(filename string) ([]byte, error) {
	cacheDir := m.pathProvider.CacheDir()
	targetPath := filepath.Join(cacheDir, filename)

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	return data, nil
}

// CacheExists checks if a cache file exists
func (m *CacheManager) CacheExists(filename string) bool {
	cacheDir := m.pathProvider.CacheDir()
	targetPath := filepath.Join(cacheDir, filename)

	info, err := os.Stat(targetPath)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// CacheAge returns the age of a cache file
func (m *CacheManager) CacheAge(filename string) (time.Duration, error) {
	cacheDir := m.pathProvider.CacheDir()
	targetPath := filepath.Join(cacheDir, filename)

	info, err := os.Stat(targetPath)
	if err != nil {
		return 0, fmt.Errorf("failed to stat cache file: %w", err)
	}

	return time.Since(info.ModTime()), nil
}

// ValidateCacheSize checks if a cache file is within size limits
func (m *CacheManager) ValidateCacheSize(filename string, maxSize int64) error {
	cacheDir := m.pathProvider.CacheDir()
	targetPath := filepath.Join(cacheDir, filename)

	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("failed to stat cache file: %w", err)
	}

	if info.Size() > maxSize {
		return fmt.Errorf("cache file exceeds size limit: %d > %d", info.Size(), maxSize)
	}

	return nil
}

// ValidateCacheDigest validates a cache file against an expected SHA-256 digest
func (m *CacheManager) ValidateCacheDigest(filename string, expectedDigest string) error {
	cacheDir := m.pathProvider.CacheDir()
	targetPath := filepath.Join(cacheDir, filename)

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("failed to read cache file for digest validation: %w", err)
	}

	hash := sha256.Sum256(data)
	actualDigest := hex.EncodeToString(hash[:])

	if actualDigest != expectedDigest {
		return fmt.Errorf("cache file digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
	}

	return nil
}

// WriteScript writes a script with restrictive permissions
// Security: Uses unique filename with restrictive permissions (0700)
func (m *CacheManager) WriteScript(pkgDir, scriptContent string) (string, error) {
	scriptPath, err := m.pathProvider.ScriptPath(pkgDir, scriptContent)
	if err != nil {
		return "", fmt.Errorf("failed to get script path: %w", err)
	}

	// Write with restrictive permissions
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0700); err != nil {
		return "", fmt.Errorf("failed to write script: %w", err)
	}

	return scriptPath, nil
}

// WriteManifest writes the execution manifest atomically
func (m *CacheManager) WriteManifest(manifestData []byte) error {
	manifestPath := m.pathProvider.ManifestPath()
	manifestDir := filepath.Dir(manifestPath)

	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		return fmt.Errorf("failed to create manifest directory: %w", err)
	}

	tmpPath := manifestPath + ".tmp"

	// Write to temporary file
	if err := os.WriteFile(tmpPath, manifestData, 0644); err != nil {
		return fmt.Errorf("failed to write temporary manifest: %w", err)
	}

	// Sync to ensure data is written to disk
	file, err := os.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to open temporary manifest for sync: %w", err)
	}
	defer file.Close()

	if err := file.Sync(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temporary manifest: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temporary manifest: %w", err)
	}

	return nil
}

// ReadManifest reads the execution manifest
func (m *CacheManager) ReadManifest() ([]byte, error) {
	manifestPath := m.pathProvider.ManifestPath()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	return data, nil
}

// CleanupTempFiles removes temporary files created during operations
func (m *CacheManager) CleanupTempFiles(tempDir string) error {
	if tempDir == "" {
		return nil
	}
	return m.pathProvider.CleanupTempDir(tempDir)
}

// AtomicWrite performs an atomic write with validation
func (m *CacheManager) AtomicWrite(filename string, data []byte, maxSize int64, expectedDigest string) error {
	// Validate size
	if int64(len(data)) > maxSize {
		return fmt.Errorf("data exceeds size limit: %d > %d", len(data), maxSize)
	}

	// Validate digest if provided
	if expectedDigest != "" {
		hash := sha256.Sum256(data)
		actualDigest := hex.EncodeToString(hash[:])
		if actualDigest != expectedDigest {
			return fmt.Errorf("data digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
		}
	}

	// Write atomically
	return m.WriteCache(filename, data)
}

// DownloadWithValidation downloads content and validates it before caching
func (m *CacheManager) DownloadWithValidation(url string, maxSize int64, expectedDigest string) ([]byte, error) {
	// This would integrate with the fetch package
	// For now, return an error as this needs HTTP client integration
	return nil, fmt.Errorf("download with validation not yet implemented - needs HTTP client integration")
}

// StreamDownloadWithValidation streams download with size and digest validation
func (m *CacheManager) StreamDownloadWithValidation(url string, maxSize int64, expectedDigest string, writer io.Writer) error {
	// This would integrate with the fetch package for streaming downloads
	// For now, return an error as this needs HTTP client integration
	return fmt.Errorf("stream download with validation not yet implemented - needs HTTP client integration")
}

// CopyWithDigestValidation copies data with digest validation
func (m *CacheManager) CopyWithDigestValidation(src io.Reader, dest io.Writer, expectedDigest string) error {
	hasher := sha256.New()
	multiWriter := io.MultiWriter(dest, hasher)

	if _, err := io.Copy(multiWriter, src); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != expectedDigest {
		return fmt.Errorf("digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
	}

	return nil
}

// Buffer represents a buffer for atomic operations
type Buffer struct {
	bytes.Buffer
}

// NewBuffer creates a new buffer for atomic operations
func NewBuffer() *Buffer {
	return &Buffer{}
}

// WriteToAtomically writes the buffer content to a file atomically
func (b *Buffer) WriteToAtomically(filename string, maxSize int64, expectedDigest string) error {
	data := b.Bytes()

	// Validate size
	if int64(len(data)) > maxSize {
		return fmt.Errorf("buffer data exceeds size limit: %d > %d", len(data), maxSize)
	}

	// Validate digest if provided
	if expectedDigest != "" {
		hash := sha256.Sum256(data)
		actualDigest := hex.EncodeToString(hash[:])
		if actualDigest != expectedDigest {
			return fmt.Errorf("buffer digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
		}
	}

	// Create a temporary cache manager for this operation
	// In a real implementation, this would use the existing cache manager
	return fmt.Errorf("atomic write not yet implemented - needs cache manager integration")
}
