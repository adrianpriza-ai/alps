package aur

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// testServerTransport routes requests through the given test server URL,
// preserving the original path and query but replacing scheme and host.
type testServerTransport struct {
	target *url.URL
}

func (t *testServerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

// --- validatePkgName / validatePkgNames ---

func TestValidatePkgNameValid(t *testing.T) {
	valid := []string{
		"bash", "foo-bar", "my_pkg", "test@1.0", "a.b-c+d_e", "UPPERCASE",
	}
	for _, name := range valid {
		if err := validatePkgName(name); err != nil {
			t.Errorf("validatePkgName(%q) returned error: %v", name, err)
		}
	}
}

func TestValidatePkgNameInvalid(t *testing.T) {
	invalid := []string{
		"",          // empty
		"foo bar",   // space
		"foo/bar",   // slash
		"foo;bar",   // semicolon
		"$(rm -rf)", // shell injection
		"pkg\nname", // newline
	}
	for _, name := range invalid {
		if err := validatePkgName(name); err == nil {
			t.Errorf("validatePkgName(%q) expected error, got nil", name)
		}
	}
}

func TestValidatePkgNames(t *testing.T) {
	if err := validatePkgNames([]string{"bash", "coreutils"}); err != nil {
		t.Errorf("validatePkgNames([bash coreutils]) = %v", err)
	}
	if err := validatePkgNames([]string{"bash", "bad name"}); err == nil {
		t.Error("validatePkgNames expected error for invalid name")
	}
}

// --- stripVerConstraint ---

func TestStripVerConstraint(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"bash", "bash"},
		{"bash>=4.0", "bash"},
		{"git<=2.40", "git"},
		{"openssh!=", "openssh"},
		{"pkg>1.0", "pkg"},
		{"dep<2.0", "dep"},
		{"x=y", "x"},
	}
	for _, tc := range tests {
		got := stripVerConstraint(tc.in)
		if got != tc.want {
			t.Errorf("stripVerConstraint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- dedup ---

func TestDedup(t *testing.T) {
	in := []string{"b", "a", "b", "c", "a", "d"}
	got := dedup(in)
	want := []string{"b", "a", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("dedup(%v) = %v, want %v", in, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedup[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- Search / Info with fake server ---

func TestSearchWithFakeServer(t *testing.T) {
	response := rpcResponse{
		Results: []Package{
			{Name: "bash", Version: "5.2.26", Description: "GNU Bourne Again Shell"},
			{Name: "bash-doc", Version: "5.2.26", Description: "Bash documentation"},
		},
	}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	origClient := aurHTTPClient
	testURL, _ := url.Parse(srv.URL)
	aurHTTPClient = &http.Client{Transport: &testServerTransport{target: testURL}}
	defer func() { aurHTTPClient = origClient }()

	results, err := Search("bash")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "bash" {
		t.Errorf("first result = %q, want bash (sorted by votes desc)", results[0].Name)
	}
}

func TestSearchQueryTooLong(t *testing.T) {
	long := strings.Repeat("a", 201)
	_, err := Search(long)
	if err == nil {
		t.Error("Search(expected error for long query) = nil")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got: %v", err)
	}
}

func TestInfoWithFakeServer(t *testing.T) {
	response := rpcResponse{
		Results: []Package{
			{Name: "curl", Version: "8.5.0", Description: "URL transfer client"},
		},
	}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	origClient := aurHTTPClient
	testURL, _ := url.Parse(srv.URL)
	aurHTTPClient = &http.Client{Transport: &testServerTransport{target: testURL}}
	defer func() { aurHTTPClient = origClient }()

	pkg, err := Info("curl")
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}
	if pkg.Name != "curl" {
		t.Errorf("Info().Name = %q, want curl", pkg.Name)
	}
	if pkg.Version != "8.5.0" {
		t.Errorf("Info().Version = %q, want 8.5.0", pkg.Version)
	}
}

func TestInfoNotFound(t *testing.T) {
	response := rpcResponse{Results: []Package{}}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	origClient := aurHTTPClient
	testURL, _ := url.Parse(srv.URL)
	aurHTTPClient = &http.Client{Transport: &testServerTransport{target: testURL}}
	defer func() { aurHTTPClient = origClient }()

	_, err := Info("nonexistent-package-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent package, got nil")
	}
	if !strings.Contains(err.Error(), ErrPkgNotFound.Error()) {
		t.Errorf("expected %q error, got: %v", ErrPkgNotFound, err)
	}
}

func TestInfoBatchWithFakeServer(t *testing.T) {
	packages := map[string]rpcResponse{
		"git":  {Results: []Package{{Name: "git", Version: "2.44.0"}}},
		"curl": {Results: []Package{{Name: "curl", Version: "8.5.0"}}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL path ends with the package name: /rpc/v5/info/{name}
		parts := strings.Split(r.URL.Path, "/")
		name := parts[len(parts)-1]
		resp, ok := packages[name]
		if !ok {
			resp = rpcResponse{Results: []Package{}}
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	origClient := aurHTTPClient
	testURL, _ := url.Parse(srv.URL)
	aurHTTPClient = &http.Client{Transport: &testServerTransport{target: testURL}}
	defer func() { aurHTTPClient = origClient }()

	results, err := InfoBatch([]string{"git", "curl"})
	if err != nil {
		t.Fatalf("InfoBatch failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results["git"] == nil || results["git"].Version != "2.44.0" {
		t.Errorf("results[git] incorrect: %+v", results["git"])
	}
	if results["curl"] == nil || results["curl"].Version != "8.5.0" {
		t.Errorf("results[curl] incorrect: %+v", results["curl"])
	}
}

// --- aurCacheDir path traversal ---

func TestAURCacheDirTraversal(t *testing.T) {
	// Normal names should succeed
	for _, name := range []string{"bash", "foo-bar", "test_pkg"} {
		if _, err := aurCacheDir(name); err != nil {
			t.Errorf("aurCacheDir(%q) unexpected error: %v", name, err)
		}
	}
	// Path traversal attempts should be rejected
	for _, name := range []string{"../etc", "foo/../../bar"} {
		if _, err := aurCacheDir(name); err == nil {
			t.Errorf("aurCacheDir(%q) expected error for traversal attempt, got nil", name)
		}
	}
}
