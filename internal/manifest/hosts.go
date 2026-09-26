package manifest

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Providers: the software of a git server, the value of
// git_hosts.<alias>.provider.
const (
	TypeGitHub  = "github"
	TypeGitLab  = "gitlab"
	TypeGitea   = "gitea"
	TypeGeneric = "generic"
)

// HostTypes lists the values of git_hosts.<alias>.provider.
var HostTypes = []string{TypeGitHub, TypeGitLab, TypeGitea, TypeGeneric}

// AliasPattern is the rule for the alias of a declared host.
const AliasPattern = `^[a-z][a-z0-9-]*$`

// Hosts are the git servers declared in the manifest, by alias.
type Hosts map[string]GitHost

// GitHost is a git server declared in the manifest, usually a self-hosted
// one: "<alias>:group/repo" is a repository on it. It lives in the manifest
// rather than in the tool config, so the manifest resolves the same on
// every machine.
type GitHost struct {
	// BaseURL is the base of the repositories on the server, and its
	// scheme is the transport: "https://git.example.com" clones
	// <base_url>/group/repo.git over https, "ssh://git@git.example.com"
	// (with an optional port and path prefix) over ssh. It must not carry a
	// password or https credentials; use a git credential helper or an ssh
	// key.
	BaseURL string `toml:"base_url" yaml:"base_url" json:"base_url"`
	// Provider is the software of the server: "github", "gitlab"
	// (subgroups of any depth), "gitea" (Gitea, Forgejo) or "generic" (any
	// path, cloned as written, ".git" is not added). It sets the accepted
	// paths and is recorded for CI templates and imports. Default:
	// "generic".
	Provider string `toml:"provider" yaml:"provider" json:"provider"`
}

// builtins are the hosts of the prefixes that need no declaration; the
// empty prefix is the "owner/repo" shorthand, and "github:" names the same
// host explicitly.
var builtins = []struct {
	prefix string
	host   GitHost
}{
	{"", GitHost{BaseURL: "https://github.com", Provider: TypeGitHub}},
	{"github", GitHost{BaseURL: "https://github.com", Provider: TypeGitHub}},
	{"gitlab", GitHost{BaseURL: "https://gitlab.com", Provider: TypeGitLab}},
	{"codeberg", GitHost{BaseURL: "https://codeberg.org", Provider: TypeGitea}},
}

// Remote is a repo value of the manifest resolved to the repository it
// names.
type Remote struct {
	// URL is the canonical clone URL: the value itself for a full URL, a
	// relative local path made absolute, a short form expanded on its host
	// (https). It identifies the repository in the vendor cache, the .skenv
	// marker and doctor output; compare two of them with NormalizeURL.
	URL string
	// Type is the host type (TypeGitHub, ...): of the prefix, of the
	// declared or built-in host a full URL is on, TypeGeneric otherwise.
	Type string
	// httpsBase and sshBase are the url.insteadOf pair of the host, set for
	// URLs on a built-in or declared host.
	httpsBase, sshBase string
}

// Resolve returns the repository that a repo value names: "owner/repo" or
// "github:owner/repo" on github.com, "gitlab:group/sub/repo",
// "codeberg:owner/repo", "<alias>:path" of a declared host, or a full git
// URL (https://, ssh://, git@host:path, file://, a local path). A prefix
// that is neither built in nor declared is an error, never a fallback to
// GitHub. h may be nil. A relative local path is made absolute against
// the working directory; ResolveIn names another base.
func (h Hosts) Resolve(repo string) (Remote, error) { return h.ResolveIn("", repo) }

// ResolveIn is Resolve with a relative local path resolved against base,
// the directory of the skenv file that holds the value ("" for the
// working directory).
func (h Hosts) ResolveIn(base, repo string) (Remote, error) {
	s := strings.TrimSpace(repo)
	if s == "" {
		return Remote{}, errors.New("repo is empty")
	}
	if strings.Contains(s, "://") {
		return h.remoteAt(s), nil
	}
	if IsShortRepo(s) {
		return onHost(builtins[0].host, s)
	}
	if m := prefixRe.FindStringSubmatch(s); m != nil && !strings.HasPrefix(m[2], "/") {
		host, ok := h.lookup(m[1])
		if !ok {
			return Remote{}, h.unknownPrefix(m[1], s)
		}
		r, err := onHost(host, m[2])
		if err != nil {
			return Remote{}, fmt.Errorf("repo %q: %w", s, err)
		}
		return r, nil
	}
	if isLocal(s) && !filepath.IsAbs(s) {
		if base != "" {
			s = filepath.Join(base, s)
		} else if abs, err := filepath.Abs(s); err == nil {
			s = abs
		}
	}
	return h.remoteAt(s), nil
}

// ShortForm returns the short repo value for a remote URL on a built-in or
// declared host ("owner/repo", "gitlab:group/sub/repo",
// "codeberg:owner/repo", "<alias>:path"), ok false for any other URL.
func (h Hosts) ShortForm(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	prefix, host, rest, ok := h.match(remote)
	if !ok {
		return "", false
	}
	if host.Provider == TypeGeneric && strings.HasSuffix(strings.TrimRight(remote, "/"), ".git") {
		rest += ".git"
	}
	if _, err := onHost(host, rest); err != nil {
		return "", false
	}
	if prefix == "" {
		return rest, true
	}
	return prefix + ":" + rest, true
}

// AccessHint is the advice for a failed clone of r: credentials, and for a
// repository cloned over https from a known host the url.insteadOf that
// clones it over ssh.
func (r Remote) AccessHint() string {
	hint := "check access to the repository: ssh key or git credential helper"
	if r.httpsBase != "" && strings.HasPrefix(r.URL, r.httpsBase) && strings.HasPrefix(r.httpsBase, "http") {
		hint += fmt.Sprintf("; to clone over ssh: git config --global url.%q.insteadOf %q", r.sshBase, r.httpsBase)
	}
	return hint
}

// IsShortRepo reports whether repo is the "owner/repo" shorthand of
// github.com.
func IsShortRepo(repo string) bool {
	segs, ok := repoPath(repo)
	return ok && len(segs) == 2
}

// prefixRe is "<prefix>:<path>". A dot or "@" before the colon makes it
// scp-like [user@]host:path instead; a path starting with "/" too.
var prefixRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*):(.*)$`)

var segmentRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// repoPath splits a repository path into its segments; ok is false for an
// empty segment, "." or "..".
func repoPath(p string) ([]string, bool) {
	segs := strings.Split(p, "/")
	for _, s := range segs {
		if !segmentRe.MatchString(s) || s == "." || s == ".." {
			return nil, false
		}
	}
	return segs, true
}

// onHost expands path p on host: <url>/<p>.git, or <url>/<p> as written
// for a generic host.
func onHost(host GitHost, p string) (Remote, error) {
	if host.Provider == "" {
		host.Provider = TypeGeneric
	}
	p = strings.Trim(p, "/")
	bare := strings.TrimSuffix(p, ".git")
	segs, ok := repoPath(bare)
	if !ok {
		return Remote{}, fmt.Errorf("%q is not a repository path (segments of letters, digits, \".\", \"_\" and \"-\")", p)
	}
	switch host.Provider {
	case TypeGitHub, TypeGitea:
		if len(segs) != 2 {
			return Remote{}, fmt.Errorf("a %s repository is owner/repo, got %q", host.Provider, p)
		}
	case TypeGitLab:
		if len(segs) < 2 {
			return Remote{}, fmt.Errorf("a gitlab repository is group/repo or group/subgroup/.../repo, got %q", p)
		}
	}
	base := strings.TrimRight(host.BaseURL, "/")
	u := base + "/" + bare + ".git"
	if host.Provider == TypeGeneric {
		u = base + "/" + p
	}
	return Remote{URL: u, Type: host.Provider, httpsBase: host.httpsBase(), sshBase: host.sshBase()}, nil
}

// remoteAt is the Remote of a full URL or local path s.
func (h Hosts) remoteAt(s string) Remote {
	r := Remote{URL: s, Type: TypeGeneric}
	if _, host, _, ok := h.match(s); ok {
		r.Type = host.Provider
		r.httpsBase = host.httpsBase()
		r.sshBase = host.sshBase()
	}
	return r
}

func (h Hosts) lookup(prefix string) (GitHost, bool) {
	for _, b := range builtins {
		if b.prefix != "" && b.prefix == prefix {
			return b.host, true
		}
	}
	host, ok := h[prefix]
	return host, ok
}

func (h Hosts) unknownPrefix(prefix, repo string) error {
	known := []string{}
	for _, b := range builtins {
		if b.prefix != "" {
			known = append(known, b.prefix+":")
		}
	}
	for _, a := range slices.Sorted(maps.Keys(h)) {
		known = append(known, a+":")
	}
	return fmt.Errorf("repo %q: unknown host prefix %q (known: %s); declare it under [user.git_hosts.%s], "+
		"or write owner/repo for github.com or a full git URL", repo, prefix+":", strings.Join(known, ", "), prefix)
}

// match finds the built-in or declared host that the URL u is on and
// returns the path after its base: the ssh base for an ssh URL, the url
// base otherwise; the longest base wins.
func (h Hosts) match(u string) (prefix string, host GitHost, rest string, ok bool) {
	n := NormalizeURL(u)
	ssh := strings.HasPrefix(strings.ToLower(u), "ssh://") || (!strings.Contains(u, "://") && !isLocal(u))
	best := -1
	try := func(p string, g GitHost) {
		base := normBase(g.httpsBase())
		if ssh {
			base = normBase(g.sshBase())
		}
		if base != "" && len(base) > best && strings.HasPrefix(n, base+"/") {
			prefix, host, rest, ok, best = p, g, n[len(base)+1:], true, len(base)
		}
	}
	for _, b := range builtins {
		try(b.prefix, b.host)
	}
	for _, a := range slices.Sorted(maps.Keys(h)) {
		try(a, h[a])
	}
	return prefix, host, rest, ok
}

// normBase is NormalizeURL of a base that ends in "/" or ":".
func normBase(base string) string {
	return strings.TrimSuffix(NormalizeURL(base+"x"), "/x")
}

// isSSH reports whether the base URL of g clones over ssh.
func (g GitHost) isSSH() bool { return strings.HasPrefix(strings.ToLower(g.BaseURL), "ssh://") }

// httpsBase is the https prefix of repository paths on the host, ending in
// "/": the base URL, or for an ssh base https://<host>/<prefix>/.
func (g GitHost) httpsBase() string {
	if !g.isSSH() {
		return strings.TrimRight(g.BaseURL, "/") + "/"
	}
	u, err := url.Parse(g.BaseURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return "https://" + u.Hostname() + strings.TrimRight(u.Path, "/") + "/"
}

// sshBase is the ssh prefix of repository paths on the host, the left side
// of url.<ssh>.insteadOf <https base>: the base URL for an ssh base,
// "git@<host>:" for an https one.
func (g GitHost) sshBase() string {
	if g.isSSH() {
		return strings.TrimRight(g.BaseURL, "/") + "/"
	}
	u, err := url.Parse(g.BaseURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return "git@" + u.Hostname() + ":"
}

// ReservedAliases are the built-in prefixes and "github", which the
// shorthand already covers: they cannot be declared.
var ReservedAliases = []string{"github", "gitlab", "codeberg"}

var aliasRe = regexp.MustCompile(AliasPattern)

// fillDefaults sets the provider of hosts that do not name one.
func (h Hosts) fillDefaults() {
	for a, g := range h {
		if g.Provider == "" {
			g.Provider = TypeGeneric
			h[a] = g
		}
	}
}

// validate checks the declared hosts; errors name git_hosts.<alias>.
func (h Hosts) validate() []error {
	var errs []error
	for _, alias := range slices.Sorted(maps.Keys(h)) {
		g := h[alias]
		where := "git_hosts." + alias
		if !aliasRe.MatchString(alias) {
			errs = append(errs, fmt.Errorf("%s: the alias must be lowercase letters, digits and \"-\", starting with a letter", where))
		} else if slices.Contains(ReservedAliases, alias) {
			errs = append(errs, fmt.Errorf("%s: %q is built in and cannot be declared", where, alias))
		}
		if err := checkHostURL(g.BaseURL); err != nil {
			errs = append(errs, fmt.Errorf("%s.base_url: %w", where, err))
		}
		if g.Provider != "" && !slices.Contains(HostTypes, g.Provider) {
			errs = append(errs, fmt.Errorf("%s.provider: %q must be one of %s", where, g.Provider, strings.Join(HostTypes, ", ")))
		}
	}
	return errs
}

func checkHostURL(s string) error {
	if s == "" {
		return errors.New("is required, such as \"https://git.example.com\" or \"ssh://git@git.example.com\"")
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || !strings.Contains(s, "://") || strings.ContainsAny(s, " \t\n") {
		// The value is not echoed: it may hold credentials in a form
		// gitx.Mask does not recognise.
		return errors.New(`must be a base URL such as "https://git.example.com" or "ssh://git@git.example.com"`)
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		if u.User != nil {
			return errors.New("must not carry credentials; use a git credential helper (https://qunaxis.github.io/skenv/git-hosts#authentication)")
		}
	case "ssh":
		if _, pw := u.User.Password(); pw {
			return errors.New("must not carry a password; use an ssh key")
		}
	default:
		return fmt.Errorf("scheme %q: use https:// or ssh://", u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must not have a query or fragment")
	}
	return nil
}
