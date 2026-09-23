package completion

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/platform"
)

// TestCacheFileMatchesPlatform pins that the generated scripts read the same
// cache and state paths the program writes. A previous divergence pointed
// macOS completions at /var/cache, where the program never writes.
func TestCacheFileMatchesPlatform(t *testing.T) {
	if got, want := cacheFile(), filepath.Join(platform.CacheDir(), "main.txt"); got != want {
		t.Errorf("cacheFile() = %q, want %q", got, want)
	}
	if got, want := installedFile(), filepath.Join(platform.LibDir(), "installed.json"); got != want {
		t.Errorf("installedFile() = %q, want %q", got, want)
	}
}

// TestEffectiveCmdsIncludeCompletion pins that the completion command itself
// is offered in the generated scripts.
func TestEffectiveCmdsIncludeCompletion(t *testing.T) {
	cmds := effectiveCmds()
	for _, c := range cmds {
		if c == "completion" {
			return
		}
	}
	t.Errorf("effectiveCmds() = %v, want it to contain \"completion\"", cmds)
}

// TestAURNamesCmdUsesCachePath pins that the shell command reading the AUR
// names cache embeds the same path the program's cache writer uses
// (AURNamesCachePath), quoted, instead of a hardcoded $HOME guess.
func TestAURNamesCmdUsesCachePath(t *testing.T) {
	cmd := aurNamesCmd()
	quoted := shellQuote(AURNamesCachePath())
	if !strings.Contains(cmd, quoted) {
		t.Errorf("aurNamesCmd() = %q, want it to embed the quoted cache path %q", cmd, quoted)
	}
	if strings.Contains(cmd, "$HOME") {
		t.Errorf("aurNamesCmd() = %q, want a literal path instead of $HOME", cmd)
	}
}

// TestGenerateDispatch checks that Generate routes each supported shell to
// its generator and reports — rather than exiting — on an unknown shell.
func TestGenerateDispatch(t *testing.T) {
	var buf bytes.Buffer
	for _, shell := range []string{"fish", "bash", "zsh"} {
		buf.Reset()
		if err := Generate(shell, &buf); err != nil {
			t.Errorf("Generate(%q) returned error: %v", shell, err)
		}
		if buf.Len() == 0 {
			t.Errorf("Generate(%q) wrote nothing", shell)
		}
	}

	buf.Reset()
	if err := Generate("tcsh", &buf); err == nil {
		t.Error("Generate(\"tcsh\") expected an error, got nil")
	}
	if buf.Len() != 0 {
		t.Errorf("Generate(\"tcsh\") wrote %q on error, want nothing", buf.String())
	}
}

// inlineDescribeList matches the _describe call form that receives an
// inline '(a b c)' list instead of an array name; such a call only works on
// zsh versions whose grouped-listing path special-cases inline lists and
// offers nothing otherwise.
var inlineDescribeList = regexp.MustCompile(`_describe '[^']*' '\(`)

// TestGeneratedScriptsMatchCliTables renders all three scripts and asserts
// they carry the cli subcommand tables verbatim (which include aur
// update/upgrade, missing from the previous handwritten lists), mention the
// always-available commands, and contain no malformed format verbs. The %!
// check is the guard against verb drift: a missing or extra argument would
// otherwise reach the user's shell completion file.
func TestGeneratedScriptsMatchCliTables(t *testing.T) {
	cmds := effectiveCmds()
	aliases := aliasWords(cmds)
	subcmds := subcommandWords()
	backend := detectBackend()

	generators := map[string]func(io.Writer) error{
		"fish": func(w io.Writer) error { return genFish(w, cmds, aliases, subcmds, backend) },
		"bash": func(w io.Writer) error { return genBash(w, cmds, aliases, subcmds, backend) },
		"zsh":  func(w io.Writer) error { return genZsh(w, cmds, aliases, subcmds, backend) },
	}
	for _, shell := range []string{"fish", "bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			var buf bytes.Buffer
			if err := generators[shell](&buf); err != nil {
				t.Fatalf("%s generator returned error: %v", shell, err)
			}
			out := buf.String()

			if strings.Contains(out, "%!") {
				t.Errorf("%s script contains a malformed format verb (%%!)", shell)
			}
			for _, sys := range []string{"aur", "repo", "winget", "flatpak", "snap"} {
				words := strings.Join(cli.ValidSubCmds(sys), " ")
				if !strings.Contains(out, words) {
					t.Errorf("%s script missing the cli.ValidSubCmds(%q) list %q", shell, sys, words)
				}
			}
			for _, want := range []string{"completion", "edit-sources"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s script does not mention %q", shell, want)
				}
			}

			if shell == "zsh" {
				if inlineDescribeList.MatchString(out) {
					t.Errorf("zsh script passes an inline list to _describe")
				}
				for _, sys := range []string{"repo", "aur", "winget", "flatpak", "snap"} {
					if !strings.Contains(out, "local -a "+sys+"_subcmds") ||
						!strings.Contains(out, "_describe '"+sys+" subcommand' "+sys+"_subcmds") {
						t.Errorf("zsh script missing array-based _describe for %q", sys)
					}
				}
				if !strings.Contains(out, "_describe 'list action' list_actions") {
					t.Errorf("zsh script missing array-based _describe for list actions")
				}
			}
		})
	}
}

// TestAliasWordsResolveToEffectiveCmds checks that the alias word list only
// offers spellings of commands actually offered on this system.
func TestAliasWordsResolveToEffectiveCmds(t *testing.T) {
	cmds := effectiveCmds()
	offered := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		offered[c] = true
	}
	for a, target := range aliasWords(cmds) {
		if !offered[target] {
			t.Errorf("alias %q resolves to %q, which is not in effectiveCmds", a, target)
		}
		if offered[a] {
			t.Errorf("alias %q duplicates an existing command name", a)
		}
	}
}

// TestWritefCatchesVerbDrift pins the guard that keeps a drifted template
// from emitting %!s(MISSING) into a user's shell. The format is passed in a
// variable so go vet's printf check skips the deliberately unbalanced call.
func TestWritefCatchesVerbDrift(t *testing.T) {
	var buf bytes.Buffer
	drifted := "a %s b %s"
	if err := writef(&buf, drifted, "one"); err == nil {
		t.Error("writef with 2 verbs and 1 argument expected an error, got nil")
	}
	if buf.Len() != 0 {
		t.Errorf("writef wrote %q on drift, want nothing", buf.String())
	}
	buf.Reset()
	if err := writef(&buf, "a %s b", "one"); err != nil {
		t.Errorf("writef with balanced verbs returned error: %v", err)
	}
	if got, want := buf.String(), "a one b"; got != want {
		t.Errorf("writef output = %q, want %q", got, want)
	}
}

// TestMoreCmdsQuotePath pins that cache paths interpolated into the
// generated shell commands are single-quoted, so a space or shell
// metacharacter in the path cannot break or hijack the command.
func TestMoreCmdsQuotePath(t *testing.T) {
	const tricky = "/var/cache/alps/more dir/main.txt"
	quoted := shellQuote(tricky)
	for name, fn := range map[string]func(string) string{
		"moreListCmd":      moreListCmd,
		"moreInstalledCmd": moreInstalledCmd,
	} {
		got := fn(tricky)
		if !strings.Contains(got, quoted) {
			t.Errorf("%s(%q) = %q, want the path embedded as %q", name, tricky, got, quoted)
		}
	}
}

// TestMoreListCmdWorksWithSpaces runs the generated grep through sh against
// a real cache file whose name contains a space, proving the generated
// command still works when the path is not a fixed constant.
func TestMoreListCmdWorksWithSpaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache dir", "main.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[alpha]\n[beta]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", "-c", moreListCmd(path))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sh -c %q failed: %v", moreListCmd(path), err)
	}
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}
