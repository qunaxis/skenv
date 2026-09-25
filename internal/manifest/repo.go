package manifest

import (
	"net/url"
	"regexp"
	"strings"
)

var shortRepoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// IsShortRepo reports whether repo is an "owner/repo" shorthand.
func IsShortRepo(repo string) bool { return shortRepoRe.MatchString(repo) }

// RepoURL turns "owner/repo" into a github.com clone URL and leaves full git
// URLs (https://, ssh://, git@host:path, file://, local paths) unchanged.
// Users who prefer ssh can map the https prefix with git's url.insteadOf.
func RepoURL(repo string) string {
	if IsShortRepo(repo) {
		return "https://github.com/" + strings.TrimSuffix(repo, ".git") + ".git"
	}
	return repo
}

// RepoName returns the last path element of repo without ".git".
func RepoName(repo string) string {
	_, name := ownerRepo(repo)
	return name
}

// CacheKey is the directory name of the vendor cache for repo:
// "<owner>__<repo>".
func CacheKey(repo string) string {
	owner, name := ownerRepo(repo)
	key := name
	if owner != "" {
		key = owner + "__" + name
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, key)
}

func ownerRepo(repo string) (owner, name string) {
	s := repo
	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		s = u.Path
	} else if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
		s = s[i+1:] // scp-like git@host:owner/repo
	}
	s = strings.TrimSuffix(strings.Trim(s, "/"), ".git")
	parts := strings.Split(s, "/")
	name = parts[len(parts)-1]
	if len(parts) > 1 {
		owner = parts[len(parts)-2]
	}
	return owner, name
}
