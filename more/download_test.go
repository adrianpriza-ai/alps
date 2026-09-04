package more

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPerformDownloadRejectsDeclaredOversize verifies that a server which
// declares a Content-Length above maxDownloadSize is rejected before any
// data is transferred, and that no partial file is left at the destination.
func TestPerformDownloadRejectsDeclaredOversize(t *testing.T) {
	t.Setenv("TERM", "") // suppress any progress rendering in test output

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", maxDownloadSize+1))
	}))
	defer srv.Close()

	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	dest := filepath.Join(t.TempDir(), "oversize.bin")
	_, err := performDownload(srv.URL+"/oversize.bin", dest, ctx)
	if err == nil {
		t.Fatal("expected error for declared Content-Length above the limit")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention the size limit, got: %v", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Errorf("destination file %s should not exist after oversize rejection", dest)
	}
}

// TestPerformDownloadSuccess verifies the happy path: a 200 response is
// streamed, verified against the declared sha256sums entry, and written to
// the destination file.
func TestPerformDownloadSuccess(t *testing.T) {
	t.Setenv("TERM", "") // suppress any progress rendering in test output

	content := []byte("download success payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	e := &Entry{Name: "pkg", Safety: "strict", SHA256Sums: []string{sha256hex(content)}}
	ctx := NewMacroContext(e, "")
	dest := filepath.Join(t.TempDir(), "out.bin")
	if _, err := performDownload(srv.URL+"/out.bin", dest, ctx); err != nil {
		t.Fatalf("performDownload returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("cannot read destination file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}

	// The digest was consumed from the entry's sha256sums list.
	if ctx.SHA256Index != 1 {
		t.Errorf("SHA256Index = %d, want 1 (digest consumed)", ctx.SHA256Index)
	}
}

// TestPerformDownloadServerError verifies that non-200 responses surface as
// errors and leave no file behind.
func TestPerformDownloadServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	dest := filepath.Join(t.TempDir(), "err.bin")
	_, err := performDownload(srv.URL+"/err.bin", dest, ctx)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention the HTTP status, got: %v", err)
	}
}

// TestFetchBytesSizeLimit verifies the capped reader used by manifest and
// script downloads: bodies at exactly maxSize are accepted, larger bodies
// are rejected, and an empty body is an error.
func TestFetchBytesSizeLimit(t *testing.T) {
	const maxSize int64 = 64
	noValidate := func(string) error { return nil }

	// Body larger than the cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, maxSize+1))
	}))
	_, err := fetchBytes(srv.URL, 5*time.Second, maxSize, noValidate)
	if err == nil {
		t.Fatal("expected error for response larger than maxSize")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention the size limit, got: %v", err)
	}
	srv.Close()

	// Body exactly at the cap is accepted.
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, maxSize))
	}))
	body, err := fetchBytes(srv.URL, 5*time.Second, maxSize, noValidate)
	if err != nil {
		t.Fatalf("response of exactly maxSize bytes should be accepted: %v", err)
	}
	if int64(len(body)) != maxSize {
		t.Errorf("len(body) = %d, want %d", len(body), maxSize)
	}
	srv.Close()

	// Empty body is rejected.
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err = fetchBytes(srv.URL, 5*time.Second, maxSize, noValidate)
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error should mention the empty response, got: %v", err)
	}
	srv.Close()
}

// TestFetchBytesNon200 verifies that a non-200 status is an error even when
// the response body is empty.
func TestFetchBytesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchBytes(srv.URL, 5*time.Second, maxManifestSize, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for HTTP 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention the HTTP status, got: %v", err)
	}
}

// TestFetchBytesValidatesURL verifies that a rejected URL never results in an
// HTTP request — the validation callback runs first.
func TestFetchBytesValidatesURL(t *testing.T) {
	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
	}))
	defer srv.Close()

	_, err := fetchBytes(srv.URL, 5*time.Second, maxManifestSize, func(u string) error {
		return fmt.Errorf("blocked: %s", u)
	})
	if err == nil {
		t.Fatal("expected error from validation callback")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should come from the validation callback, got: %v", err)
	}
	if requested {
		t.Error("HTTP request should not be made when validation rejects the URL")
	}
}
