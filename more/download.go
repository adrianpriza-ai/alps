package more

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// executeDownload downloads a file using Go's HTTP client with progress display.
func executeDownload(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("DOWNLOAD requires at least 1 argument: URL [file]")
	}

	url := macro.Args[0]
	if !isSafeDownloadURL(url) {
		return "", fmt.Errorf("DOWNLOAD requires HTTPS: %s", url)
	}

	file, err := resolveDownloadPath(macro.Args, ctx)
	if err != nil {
		return "", err
	}

	if err := prepareDownloadDirectory(file); err != nil {
		return "", err
	}

	return performDownload(url, file, ctx)
}

// resolveDownloadPath resolves the download path from macro arguments.
func resolveDownloadPath(args []string, ctx *MacroContext) (string, error) {
	file := ""
	if len(args) > 1 {
		file = args[1]
	} else {
		file = filepath.Base(args[0])
	}

	if err := validateSafePath(file); err != nil {
		return "", fmt.Errorf("DOWNLOAD invalid file path: %w", err)
	}

	// Resolve relative to build directory
	if !filepath.IsAbs(file) {
		file = filepath.Join(ctx.BuildDir, file)
	}

	if ctx.BuildDir != "" {
		cleanBuildDir := filepath.Clean(ctx.BuildDir)
		cleanFile := filepath.Clean(file)
		rel, err := filepath.Rel(cleanBuildDir, cleanFile)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			return "", fmt.Errorf("file path %q escapes build directory %q", file, ctx.BuildDir)
		}
	}

	return file, nil
}

// prepareDownloadDirectory creates the directory for the download file.
func prepareDownloadDirectory(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

// performDownload executes the HTTP download for the DOWNLOAD macro.
// Security: downloads are capped at maxDownloadSize so a bad or malicious
// mirror cannot make alps transfer unbounded data. A server that declares an
// oversized Content-Length is rejected before any data is transferred;
// downloadToFile enforces the cap for servers that lie about or omit it.
func performDownload(url, file string, ctx *MacroContext) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: HTTP %d", url, resp.StatusCode)
	}

	if resp.ContentLength > maxDownloadSize {
		return "", fmt.Errorf("download too large from %s: %d bytes exceeds the %d-byte limit", url, resp.ContentLength, maxDownloadSize)
	}

	// contentLength is used for the progress display; when it is unknown or
	// zero the body is streamed without progress. downloadToFile re-checks the
	// actual byte count so a server that lies about Content-Length cannot slip
	// past the cap.
	return downloadToFile(resp.Body, file, resp.ContentLength, ctx, maxDownloadSize)
}

// requireNextSha256 returns the expected SHA-256 digest for the next download
// in the entry, consuming one entry from the sha256sums list.
//
// Security: downloads are a code-execution trust boundary. In strict mode
// (the default) a manifest that triggers a download MUST provide a matching
// sha256sums entry, otherwise we refuse to fetch anything. In free mode the
// user has opted out of guardrails, so missing digests are allowed; the
// reduced-safety notice is shown at install confirmation time.
func requireNextSha256(ctx *MacroContext, what string) (string, error) {
	if len(ctx.SHA256Sums) > 0 && ctx.SHA256Index < len(ctx.SHA256Sums) {
		expected := ctx.SHA256Sums[ctx.SHA256Index]
		ctx.SHA256Index++ // Increment index for next download
		if isValidSha256(expected) {
			return expected, nil
		}
	}
	return sha256Missing(ctx, what)
}

// sha256Missing handles a download with no usable digest. In strict mode this
// is a hard error (unverified downloads are never fetched or executed). In
// free mode the download is allowed; the reduced-safety notice is shown at
// install confirmation time (see the repo backend install preview) rather than
// per download.
func sha256Missing(ctx *MacroContext, what string) (string, error) {
	if ctx.Safety == "free" {
		return "", nil
	}
	switch {
	case len(ctx.SHA256Sums) == 0:
		return "", fmt.Errorf("%s requires a sha256sums entry in the manifest — refusing to download unverified content (strict mode)", what)
	case ctx.SHA256Index >= len(ctx.SHA256Sums):
		return "", fmt.Errorf("%s is missing a sha256sums entry (download %d of %d) — refusing to download unverified content (strict mode)", what, ctx.SHA256Index+1, len(ctx.SHA256Sums))
	default:
		return "", fmt.Errorf("invalid sha256sums entry %q for %s (want exactly 64 hex characters)", ctx.SHA256Sums[ctx.SHA256Index-1], what)
	}
}

// isValidSha256 reports whether s is a 64-character hexadecimal SHA-256 digest.
func isValidSha256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// downloadToFile is the single file-download path used by the DOWNLOAD macro.
// Security: reads at most maxSize bytes (the +1 makes an exactly-maxSize body
// distinguishable from an oversized one), streams to a temp file in the same
// directory, verifies the SHA-256 digest declared in sha256sums, then
// atomically renames into place — a crash, size overrun or hash mismatch never
// leaves a partial or unverified file at the destination. Progress is rendered
// only when the total size is known (contentLength > 0) and stdout can display
// it (see progressCapable).
func downloadToFile(body io.Reader, file string, contentLength int64, ctx *MacroContext, maxSize int64) (string, error) {
	displayName := filepath.Base(file)

	// Write to a temp file in the same directory (ensures rename is atomic)
	tmpPath := file + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", tmpPath, err)
	}
	defer out.Close()

	// Security: stop reading past maxSize bytes so a mirror that lies about
	// (or omits) Content-Length cannot stream unbounded data to disk.
	limitedBody := io.LimitReader(body, maxSize+1)

	// Use a tee reader to compute SHA256 while downloading
	hasher := sha256.New()
	teeReader := io.TeeReader(limitedBody, hasher)

	reader := io.Reader(teeReader)
	showProgress := contentLength > 0 && progressCapable(os.Stdout)
	if showProgress {
		progress := setupProgressDisplay(contentLength, displayName)
		reader = &progressReader{
			reader: teeReader,
			total:  contentLength,
			onProgress: func(bytesRead int) {
				progress.update(bytesRead)
			},
		}
	}

	written, err := io.Copy(out, reader)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write file %s: %w", tmpPath, err)
	}
	if written > maxSize {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("download too large for %s: exceeds %d bytes", displayName, maxSize)
	}
	if showProgress {
		fmt.Println() // end the progress line
	}

	// Compute SHA256 of downloaded content
	computedHash := fmt.Sprintf("%x", hasher.Sum(nil))

	// The manifest must declare a digest for every download.
	expectedHash, err := requireNextSha256(ctx, displayName)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	// Free mode may opt out of digest verification (expectedHash == "").
	if expectedHash != "" && computedHash != expectedHash {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", file, expectedHash, computedHash)
	}

	// Atomically move the verified file to its final destination
	if err := os.Rename(tmpPath, file); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to move file to %s: %w", file, err)
	}
	return "", nil
}

// progressDisplay manages the download progress display.
type progressDisplay struct {
	startTime     time.Time
	downloaded    int64
	lastUpdate    time.Time
	contentLength int64
	barWidth      int
	nameColWidth  int
	truncatedName string
}

// progressCapable reports whether f can show a live progress bar. The bar
// redraws itself with a carriage return and an erase-line escape, which only
// makes sense on a character device (a terminal) whose TERM promises cursor
// control. Piped or redirected output collects those control characters as
// literal noise, so it gets a silent download instead.
func progressCapable(f *os.File) bool {
	if f == nil {
		return false
	}
	switch os.Getenv("TERM") {
	case "", "dumb":
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// setupProgressDisplay initializes the progress display.
func setupProgressDisplay(contentLength int64, displayName string) *progressDisplay {
	startTime := time.Now()
	termWidth := getTerminalWidth()

	const barWidth = 20
	// Total fixed width of non-name components (sizeStr 10, speedStr 12, timeStr 5, bar 22, percent 4, spacers 8) = 61 chars.
	// Leave a 2-character right margin to guarantee line length stays strictly less than termWidth and never auto-wraps.
	targetMaxLen := termWidth - 2
	nameColWidth := targetMaxLen - 61
	if nameColWidth < 3 {
		nameColWidth = 3
	}

	truncatedName := displayName
	if len(displayName) > nameColWidth {
		if nameColWidth > 3 {
			truncatedName = displayName[:nameColWidth-3] + "..."
		} else {
			truncatedName = displayName[:nameColWidth]
		}
	}

	return &progressDisplay{
		startTime:     startTime,
		lastUpdate:    startTime,
		contentLength: contentLength,
		barWidth:      barWidth,
		nameColWidth:  nameColWidth,
		truncatedName: truncatedName,
	}
}

// update updates the progress display.
func (p *progressDisplay) update(bytesRead int) {
	p.downloaded += int64(bytesRead)
	now := time.Now()

	if !p.shouldUpdate(now) {
		return
	}
	p.lastUpdate = now

	stats := p.calculateStats(now)
	p.renderProgress(stats)
}

// shouldUpdate determines if the display should be updated.
func (p *progressDisplay) shouldUpdate(now time.Time) bool {
	return now.Sub(p.lastUpdate) >= 50*time.Millisecond || p.downloaded >= p.contentLength
}

// progressStats holds calculated progress statistics.
type progressStats struct {
	percent   float64
	speed     float64
	remaining float64
}

// calculateStats computes progress statistics.
func (p *progressDisplay) calculateStats(now time.Time) progressStats {
	percent := float64(p.downloaded) / float64(p.contentLength) * 100
	elapsed := now.Sub(p.startTime).Seconds()

	var speed float64
	if elapsed > 0 {
		speed = float64(p.downloaded) / elapsed
	}

	var remaining float64
	if speed > 0 {
		remaining = float64(p.contentLength-p.downloaded) / speed
	}

	return progressStats{
		percent:   percent,
		speed:     speed,
		remaining: remaining,
	}
}

// renderProgress displays the progress bar and statistics.
func (p *progressDisplay) renderProgress(stats progressStats) {
	var sizeSB strings.Builder
	formatSize(&sizeSB, p.downloaded)
	sizeStr := fmt.Sprintf("%10s", sizeSB.String())

	var speedSB strings.Builder
	formatSize(&speedSB, int64(stats.speed))
	speedStr := fmt.Sprintf("%12s", speedSB.String()+"/s")

	var timeSB strings.Builder
	formatTime(&timeSB, stats.remaining)

	percent := stats.percent
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100 * float64(p.barWidth))
	if filled > p.barWidth {
		filled = p.barWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", p.barWidth-filled)

	// Print with carriage return and clear the rest of the line, then flush
	fmt.Fprintf(os.Stdout, "\r%-*s  %s  %s  %s [%s] %3.0f%%\033[K",
		p.nameColWidth, p.truncatedName, sizeStr, speedStr, timeSB.String(), bar, percent)
}

// progressReader wraps a reader to track download progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	onProgress func(int)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 && pr.onProgress != nil {
		pr.onProgress(n)
	}
	return
}

// formatSize formats bytes to a strings.Builder for efficiency.
func formatSize(sb *strings.Builder, bytes int64) {
	if bytes <= 0 {
		sb.WriteString("0 B")
		return
	}

	const unit = 1024

	if bytes < unit {
		sb.WriteString(fmt.Sprintf("%d B", bytes))
		return
	}

	// Explicit thresholds for each unit
	const (
		KB = unit
		MB = unit * unit
		GB = unit * unit * unit
		TB = unit * unit * unit * unit
	)

	var value float64
	var unitName string

	if bytes < MB {
		value = float64(bytes) / KB
		unitName = "KiB"
	} else if bytes < GB {
		value = float64(bytes) / MB
		unitName = "MiB"
	} else if bytes < TB {
		value = float64(bytes) / GB
		unitName = "GiB"
	} else {
		value = float64(bytes) / TB
		unitName = "TiB"
	}

	// Round to 1 decimal place
	roundedValue := float64(int(value*10+0.5)) / 10.0

	intPart := int(roundedValue)
	decPart := int((roundedValue-float64(intPart))*10 + 0.5)

	if decPart >= 10 {
		intPart++
		decPart = 0
	}

	sb.WriteString(fmt.Sprintf("%d,%d %s", intPart, decPart, unitName))
}

// formatTime formats time to a strings.Builder for efficiency.
func formatTime(sb *strings.Builder, seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	if seconds > 5999 { // cap at 99m 59s to preserve 5-char width (99:59)
		seconds = 5999
	}
	mins := int(seconds / 60)
	secs := int(seconds) % 60
	sb.WriteString(fmt.Sprintf("%02d:%02d", mins, secs))
}

// getTerminalWidth returns the current terminal width, or 80 if detection fails.
func getTerminalWidth() int {
	// Try to get terminal width using ioctl
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}

	ws := &winsize{}

	var tiocgwinsz uintptr = 0x5413 // Linux
	if runtime.GOOS == "darwin" {
		tiocgwinsz = 0x40087468 // macOS
	}

	// Try stdout first, then stdin
	fd := uintptr(syscall.Stdout)
	retCode, _, _ := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		tiocgwinsz,
		uintptr(unsafe.Pointer(ws)),
	)

	if int(retCode) == -1 || ws.Col == 0 {
		fd = uintptr(syscall.Stdin)
		retCode, _, _ = syscall.Syscall(
			syscall.SYS_IOCTL,
			fd,
			tiocgwinsz,
			uintptr(unsafe.Pointer(ws)),
		)
	}

	if int(retCode) == -1 || ws.Col == 0 {
		// Fallback to stty if ioctl fails
		if cmd := exec.Command("stty", "size"); cmd != nil {
			cmd.Stdin = os.Stdin
			if out, err := cmd.Output(); err == nil {
				// stty size returns "rows cols"
				parts := strings.Split(strings.TrimSpace(string(out)), " ")
				if len(parts) == 2 {
					if cols, err := fmt.Sscanf(parts[1], "%d", &ws.Col); err == nil && cols == 1 && ws.Col > 0 {
						return int(ws.Col)
					}
				}
			}
		}
		// Default fallback
		return 80
	}

	return int(ws.Col)
}
