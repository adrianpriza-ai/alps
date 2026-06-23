package more

import (
	"fmt"
	"strings"
)

// RemoteRef identifies an ALPSMORE file hosted on a git forge.
type RemoteRef struct {
	Provider string // github, gitlab, codeberg
	Host     string // e.g. github.com, gitlab.archlinux.org
	RepoPath string // namespace/project path
	Branch   string // empty = try default branches
}

func defaultHost(provider string) string {
	switch provider {
	case "github":
		return "github.com"
	case "gitlab":
		return "gitlab.com"
	case "codeberg":
		return "codeberg.org"
	default:
		return ""
	}
}

func providerFromHost(host string) string {
	host = strings.ToLower(host)
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return "github"
	case host == "codeberg.org" || strings.HasSuffix(host, ".codeberg.org"):
		return "codeberg"
	default:
		// Self-hosted GitLab and other GitLab-compatible forges.
		return "gitlab"
	}
}

// ParseRemoteURL parses a user-facing remote URL such as:
//   - github.com/user/repo
//   - github.com/user/repo/main
//   - gitlab.archlinux.org/pacman/pacman/main
//   - codeberg.org/user/repo@dev
func ParseRemoteURL(input string) (*RemoteRef, error) {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimSuffix(input, "/")

	var explicitBranch string
	if at := strings.LastIndex(input, "@"); at > 0 {
		explicitBranch = input[at+1:]
		if explicitBranch == "" {
			return nil, fmt.Errorf("invalid remote URL %q: empty branch after @", input)
		}
		input = input[:at]
	}

	slash := strings.Index(input, "/")
	if slash < 0 {
		return nil, fmt.Errorf("invalid remote URL %q: missing repository path", input)
	}

	host := input[:slash]
	path := strings.Trim(input[slash+1:], "/")
	if path == "" {
		return nil, fmt.Errorf("invalid remote URL %q: missing repository path", input)
	}

	provider := providerFromHost(host)
	if provider == "" {
		return nil, fmt.Errorf("unsupported git host %q", host)
	}

	ref := &RemoteRef{
		Provider: provider,
		Host:     host,
		RepoPath: path,
		Branch:   explicitBranch,
	}
	return ref, nil
}

// ParseSource decodes a stored source string such as:
//   - github:user/repo
//   - github:user/repo@main
//   - gitlab@gitlab.archlinux.org:pacman/pacman@main
//   - codeberg:user/repo@dev
func ParseSource(source string) (*RemoteRef, error) {
	colon := strings.Index(source, ":")
	if colon < 0 {
		return nil, fmt.Errorf("invalid source %q", source)
	}

	prefix := source[:colon]
	rest := source[colon+1:]
	if rest == "" {
		return nil, fmt.Errorf("invalid source %q: missing repository path", source)
	}

	var provider, host string
	if at := strings.Index(prefix, "@"); at >= 0 {
		provider = prefix[:at]
		host = prefix[at+1:]
	} else {
		provider = prefix
		host = defaultHost(provider)
	}

	if host == "" {
		return nil, fmt.Errorf("unknown source provider in %q", source)
	}

	branch := ""
	if branchAt := strings.LastIndex(rest, "@"); branchAt >= 0 {
		branch = rest[branchAt+1:]
		if branch == "" {
			return nil, fmt.Errorf("invalid source %q: empty branch", source)
		}
		rest = rest[:branchAt]
	}

	if rest == "" {
		return nil, fmt.Errorf("invalid source %q: missing repository path", source)
	}

	return &RemoteRef{
		Provider: provider,
		Host:     host,
		RepoPath: rest,
		Branch:   branch,
	}, nil
}

// Source returns the canonical stored source string for this ref.
func (r RemoteRef) Source() string {
	prefix := r.Provider
	if r.Host != "" && r.Host != defaultHost(r.Provider) {
		prefix = r.Provider + "@" + r.Host
	}

	s := prefix + ":" + r.RepoPath
	if r.Branch != "" {
		s += "@" + r.Branch
	}
	return s
}

// DisplayURL returns a user-facing host/path[/branch] string.
func (r RemoteRef) DisplayURL() string {
	s := r.Host + "/" + r.RepoPath
	if r.Branch != "" {
		s += "/" + r.Branch
	}
	return s
}

// IsRemoteURL reports whether input looks like a remote git forge URL.
func IsRemoteURL(input string) bool {
	ref, err := ParseRemoteURL(input)
	return err == nil && ref != nil
}
