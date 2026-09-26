package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

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
		// Keep the tail, which names the repository, from a segment start.
		slug = slug[len(slug)-maxSlug:]
		if _, rest, ok := strings.Cut(slug, "-"); ok {
			slug = rest
		}
		slug = strings.TrimLeft(slug, "-.")
	}
	if slug == "" {
		slug = "repo"
	}
	return slug + "-" + hex.EncodeToString(sum[:6])
}

const maxSlug = 64

var slugRe = regexp.MustCompile(`[^A-Za-z0-9._]+`)

var defaultPorts = map[string]string{"https": "443", "http": "80", "ssh": "22", "git": "9418"}

// isLocal reports whether a clone URL is a local path: neither
// scheme://... nor scp-like [user@]host:path.
func isLocal(s string) bool {
	if strings.Contains(s, "://") {
		return false
	}
	i := strings.Index(s, ":")
	return i <= 0 || strings.Contains(s[:i], "/")
}

// NormalizeURL reduces a clone URL to what identifies the repository: the
// host in lower case (with a non-default port) and the full path, without
// scheme, credentials, surrounding slashes or ".git". So "git@host:o/r",
// "ssh://git@host/o/r" and "https://user:token@HOST/o/r.git" are all
// "host/o/r". Local paths and file:// URLs become absolute slash paths.
// Resolve a repo value of the manifest first (Hosts.Resolve).
func NormalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if isLocal(s) {
		return localPath(s)
	}
	if !strings.Contains(s, "://") {
		host, p, _ := strings.Cut(s, ":") // scp-like [user@]host:path
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		return strings.ToLower(host) + "/" + trimRepoPath(p)
	}
	u, err := url.Parse(s)
	if err != nil {
		// Unparsable: keep what follows the scheme, minus anything up to
		// an "@" in the authority, so no credentials reach the key.
		_, rest, _ := strings.Cut(s, "://")
		authority, p, _ := strings.Cut(rest, "/")
		if j := strings.LastIndex(authority, "@"); j >= 0 {
			authority = authority[j+1:]
		}
		return strings.ToLower(authority) + "/" + trimRepoPath(p)
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

var idCharsRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// NewID derives an ID for a checkout or a [project.from] entry from the
// repository URL or repo value: its name, lowercased, with other characters
// replaced by "-". taken holds the IDs in use; a clash gets a number.
func NewID(repo string, taken map[string]bool) string {
	id := strings.Trim(idCharsRe.ReplaceAllString(strings.ToLower(strings.TrimSuffix(RepoName(repo), ".git")), "-"), "-_")
	if len(id) > 60 {
		id = strings.Trim(id[:60], "-_")
	}
	if id == "" {
		id = "skills"
	}
	out := id
	for n := 2; taken[out]; n++ {
		out = fmt.Sprintf("%s-%d", id, n)
	}
	return out
}
