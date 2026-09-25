package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path/filepath"
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

// CacheKey is the directory name of the vendor cache for a clone URL (as
// resolved by git, after url.<base>.insteadOf): a readable slug of
// NormalizeURL plus the first 12 hex digits of its SHA-256, for example
// "github.com-tt-a1i-archify-0123456789ab". The directory is flat, so one
// repository path never nests inside another (gitlab.com/a/b and
// gitlab.com/a/b/c), and the hash keeps keys apart when the slug does not
// (case-insensitive file systems, characters replaced by "-", long paths).
func CacheKey(resolved string) string {
	n := NormalizeURL(resolved)
	sum := sha256.Sum256([]byte(n))
	slug := strings.Trim(slugRe.ReplaceAllString(n, "-"), "-.")
	if len(slug) > maxSlug {
		slug = strings.TrimLeft(slug[len(slug)-maxSlug:], "-.")
	}
	if slug == "" {
		slug = "repo"
	}
	return slug + "-" + hex.EncodeToString(sum[:6])
}

const maxSlug = 64

var slugRe = regexp.MustCompile(`[^A-Za-z0-9._]+`)

var defaultPorts = map[string]string{"https": "443", "http": "80", "ssh": "22", "git": "9418"}

// NormalizeURL reduces a clone URL to what identifies the repository: the
// host in lower case (with a non-default port) and the full path, without
// scheme, credentials, surrounding slashes or ".git". So "git@host:o/r",
// "ssh://git@host/o/r" and "https://user:token@HOST/o/r.git" are all
// "host/o/r". Local paths and file:// URLs become absolute slash paths.
// "owner/repo" shorthands are expanded with RepoURL first.
func NormalizeURL(raw string) string {
	s := RepoURL(strings.TrimSpace(raw))
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return s
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme == "file" {
			return localPath(u.Path)
		}
		host := strings.ToLower(u.Hostname())
		if port := u.Port(); port != "" && port != defaultPorts[scheme] {
			host += ":" + port
		}
		return host + "/" + trimRepoPath(u.Path)
	}
	if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
		host := s[:i] // scp-like [user@]host:path
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		return strings.ToLower(host) + "/" + trimRepoPath(s[i+1:])
	}
	return localPath(s)
}

func trimRepoPath(p string) string {
	return strings.Trim(strings.TrimSuffix(strings.Trim(p, "/"), ".git"), "/")
}

func localPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return "/" + trimRepoPath(filepath.ToSlash(p))
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
