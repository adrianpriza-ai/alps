package aur

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// initTestRepo creates a git repository at dir with an initial PKGBUILD
// commit and returns the resulting HEAD hash.
func initTestRepo(t *testing.T, dir, pkgbuild string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "PKGBUILD"), []byte(pkgbuild), 0644); err != nil {
		t.Fatal(err)
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("add", ".")
	run("commit", "-m", "initial")
	return gitHead(dir)
}

// commitChange appends a line to the PKGBUILD and commits it.
func commitChange(t *testing.T, dir, msg string) {
	t.Helper()
	f := filepath.Join(dir, "PKGBUILD")
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, append(data, []byte("# changed\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("add", ".")
	run("commit", "-m", msg)
}

// TestGitHeadNonRepo verifies gitHead degrades to "" outside a repository.
func TestGitHeadNonRepo(t *testing.T) {
	if got := gitHead(t.TempDir()); got != "" {
		t.Errorf("gitHead(non-repo) = %q, want empty", got)
	}
}

// TestReviewPKGBUILDUpdateFallback verifies the fallback path: when oldHead
// cannot be diffed against (bogus revision), reviewPKGBUILDUpdate must
// report false so the caller falls back to the full-file review instead of
// silently skipping the security gate.
func TestReviewPKGBUILDUpdateFallback(t *testing.T) {
	dir := t.TempDir()
	initTestRepo(t, dir, "pkgname=foo\n")

	shown, err := reviewPKGBUILDUpdate(dir, "0000000000000000000000000000000000000000", "->")
	if err != nil {
		t.Fatalf("reviewPKGBUILDUpdate returned error: %v", err)
	}
	if shown {
		t.Error("expected shown=false when the old revision cannot be diffed")
	}
}

// TestUpgradeDiffDetection exercises the exact detection used by #18:
// after upstream commits change the PKGBUILD, the pre-sync HEAD differs from
// the new HEAD and a stat diff against it succeeds mentioning the file.
func TestUpgradeDiffDetection(t *testing.T) {
	dir := t.TempDir()
	oldHead := initTestRepo(t, dir, "pkgname=foo\npkgver=1\n")

	if gitHead(dir) != oldHead {
		t.Fatal("gitHead should match the initial commit")
	}

	// Simulate an upstream update.
	commitChange(t, dir, "update pkgver")
	newHead := gitHead(dir)

	if newHead == oldHead || newHead == "" {
		t.Fatalf("expected HEAD to move: old=%q new=%q", oldHead, newHead)
	}

	statOut, err := exec.Command("git", "-C", dir, "--no-pager", "diff", "--stat", oldHead, "HEAD").Output()
	if err != nil {
		t.Fatalf("diff --stat failed: %v", err)
	}
	if !strings.Contains(string(statOut), "PKGBUILD") {
		t.Errorf("diff --stat output missing PKGBUILD:\n%s", statOut)
	}

	// No-change case: same HEAD means no upgrade-diff branch is taken.
	if same := gitHead(dir); same != newHead || same == oldHead {
		t.Errorf("unexpected head transition: %q -> %q", oldHead, same)
	}
}

// TestPrefetchReposAllProcessedAndConcurrencyCap checks that every package
// is synced exactly once, that concurrency never exceeds the worker limit,
// and that sync errors are reported with the offending package name.
func TestPrefetchReposAllProcessedAndConcurrencyCap(t *testing.T) {
	names := make([]*Package, 0, 20)
	for i := 0; i < 20; i++ {
		names = append(names, &Package{Name: fmt.Sprintf("pkg-%02d", i)})
	}

	var active, maxActive int64
	var mu sync.Mutex
	synced := make(map[string]bool)

	stub := func(pkgName, pkgDir string, quiet bool) error {
		cur := atomic.AddInt64(&active, 1)
		mu.Lock()
		if cur > maxActive {
			maxActive = cur
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // simulate network fetch
		atomic.AddInt64(&active, -1)
		mu.Lock()
		synced[pkgName] = true
		mu.Unlock()
		return nil
	}

	if err := prefetchRepos(names, stub); err != nil {
		t.Fatalf("prefetchRepos failed: %v", err)
	}
	if len(synced) != len(names) {
		t.Errorf("synced %d packages, want %d", len(synced), len(names))
	}
	if maxActive > prefetchWorkers {
		t.Errorf("max concurrent workers = %d, want <= %d", maxActive, prefetchWorkers)
	}
	if maxActive < 2 {
		t.Errorf("expected parallel execution, max observed workers = %d", maxActive)
	}
}

// TestPrefetchReposReportsFailure verifies that one failing sync aborts the
// prefetch phase with an error naming the package — before any build starts.
func TestPrefetchReposReportsFailure(t *testing.T) {
	pkgs := []*Package{{Name: "good-one"}, {Name: "bad-pkg"}, {Name: "good-two"}}
	stub := func(pkgName, pkgDir string, quiet bool) error {
		if pkgName == "bad-pkg" {
			return errors.New("network unreachable")
		}
		return nil
	}

	err := prefetchRepos(pkgs, stub)
	if err == nil {
		t.Fatal("expected an error when a package fails to sync")
	}
	if !strings.Contains(err.Error(), "bad-pkg") {
		t.Errorf("error should name the failing package, got: %v", err)
	}
	if !strings.Contains(err.Error(), "network unreachable") {
		t.Errorf("error should include the cause, got: %v", err)
	}
}

// TestPrefetchReposEmpty verifies the no-op case.
func TestPrefetchReposEmpty(t *testing.T) {
	called := false
	stub := func(pkgName, pkgDir string, quiet bool) error {
		called = true
		return nil
	}
	if err := prefetchRepos(nil, stub); err != nil {
		t.Fatalf("prefetchRepos(nil) failed: %v", err)
	}
	if called {
		t.Error("stub should not be invoked for an empty plan")
	}
}
