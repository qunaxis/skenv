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

// Host types: the software of a git server, the value of
// hosts.<alias>.type.
const (
	TypeGitHub  = "github"
	TypeGitLab  = "gitlab"
	TypeGitea   = "gitea"
	TypeGeneric = "generic"
)

// HostTypes lists the values of hosts.<alias>.type.
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
	// URL is the https base of the server, such as
	// "https://git.example.com": "<alias>:group/repo" is cloned from
	// <url>/group/repo.git. It must not carry credentials; use a git
	// credential helper.
	URL string `toml:"url" yaml:"url" json:"url"`
	// Type is the software of the server: "github", "gitlab" (subgroups of
	// any depth), "gitea" (Gitea, Forgejo) or "generic" (any path, cloned
	// as written, ".git" is not added). It sets the accepted paths and is
	// recorded for CI templates and imports. Default: "generic".
	Type string `toml:"type" yaml:"type" json:"type"`
	// SSH is the ssh address of the server, such as "git@git.example.com"
	// or "ssh://git@git.example.com:2222"; add the path prefix of
	// repositories over ssh when it differs from the one in url. skenv
	// clones over https; this address is recognised in remotes and named in
	// the url.insteadOf hint when access fails. Default: git@<host of url>.
	SSH string `toml:"ssh" yaml:"ssh" json:"ssh"`
}

// builtins are the hosts of the prefixes that need no declaration; the
// empty prefix is the "owner/repo" shorthand.
var builtins = []struct {
	prefix string
	host   GitHost
}{
	{"", GitHost{URL: "https://github.com", Type: TypeGitHub}},
	{"gitlab", GitHost{URL: "https://gitlab.com", Type: TypeGitLab}},
	{"codeberg", GitHost{URL: "https://codeberg.org", Type: TypeGitea}},
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

// Resolve returns the repository that a repo value names: "owner/repo" on
// github.com, "gitlab:group/sub/repo", "codeberg:owner/repo", "<alias>:path"
// of a declared host, or a full git URL (https://, ssh://, git@host:path,
// file://, a local path). A prefix that is neither built in nor declared is
// an error, never a fallback to GitHub. h may be nil.
func (h Hosts) Resolve(repo string) (Remote, error) {
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
		if abs, err := filepath.Abs(s); err == nil {
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
	if host.Type == TypeGeneric && strings.HasSuffix(strings.TrimRight(remote, "/"), ".git") {
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
// repository on a known host the url.insteadOf that clones it over ssh.
func (r Remote) AccessHint() string {
	hint := "check access to the repository: ssh key or git credential helper"
	if r.httpsBase != "" && strings.HasPrefix(r.URL, r.httpsBase) {
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
	if host.Type == "" {
		host.Type = TypeGeneric
	}
	p = strings.Trim(p, "/")
	bare := strings.TrimSuffix(p, ".git")
	segs, ok := repoPath(bare)
	if !ok {
		return Remote{}, fmt.Errorf("%q is not a repository path (segments of letters, digits, \".\", \"_\" and \"-\")", p)
	}
	switch host.Type {
	case TypeGitHub, TypeGitea:
		if len(segs) != 2 {
			return Remote{}, fmt.Errorf("a %s repository is owner/repo, got %q", host.Type, p)
		}
	case TypeGitLab:
		if len(segs) < 2 {
			return Remote{}, fmt.Errorf("a gitlab repository is group/repo or group/subgroup/.../repo, got %q", p)
		}
	}
	base := strings.TrimRight(host.URL, "/")
	u := base + "/" + bare + ".git"
	if host.Type == TypeGeneric {
		u = base + "/" + p
	}
	return Remote{URL: u, Type: host.Type, httpsBase: base + "/", sshBase: host.sshBase()}, nil
}

// remoteAt is the Remote of a full URL or local path s.
func (h Hosts) remoteAt(s string) Remote {
	r := Remote{URL: s, Type: TypeGeneric}
	if _, host, _, ok := h.match(s); ok {
		r.Type = host.Type
		r.httpsBase = strings.TrimRight(host.URL, "/") + "/"
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
	return fmt.Errorf("repo %q: unknown host prefix %q (known: %s); declare it under [environment.hosts.%s], "+
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
		base := normBase(strings.TrimRight(g.URL, "/") + "/")
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

// sshBase is the ssh prefix of repository paths on the host, the left side
// of url.<ssh>.insteadOf <url>/: "git@host:" by default.
func (g GitHost) sshBase() string {
	s := strings.TrimSpace(g.SSH)
	if s == "" {
		u, err := url.Parse(g.URL)
		if err != nil || u.Hostname() == "" {
			return ""
		}
		s = "git@" + u.Hostname() + ":"
	}
	if strings.Contains(s, "://") {
		return strings.TrimRight(s, "/") + "/"
	}
	if !strings.Contains(s, ":") {
		return s + ":"
	}
	if !strings.HasSuffix(s, ":") {
		s = strings.TrimRight(s, "/") + "/"
	}
	return s
}

// ReservedAliases are the built-in prefixes and "github", which the
// shorthand already covers: they cannot be declared.
var ReservedAliases = []string{"github", "gitlab", "codeberg"}

var aliasRe = regexp.MustCompile(AliasPattern)

// validate checks the declared hosts; errors name hosts.<alias>.
func (h Hosts) validate() []error {
	var errs []error
	for _, alias := range slices.Sorted(maps.Keys(h)) {
		g := h[alias]
		where := "hosts." + alias
		if !aliasRe.MatchString(alias) {
			errs = append(errs, fmt.Errorf("%s: the alias must be lowercase letters, digits and \"-\", starting with a letter", where))
		} else if slices.Contains(ReservedAliases, alias) {
			errs = append(errs, fmt.Errorf("%s: %q is built in and cannot be declared", where, alias))
		}
		if err := checkHostURL(g.URL); err != nil {
			errs = append(errs, fmt.Errorf("%s.url: %w", where, err))
		}
		if g.Type != "" && !slices.Contains(HostTypes, g.Type) {
			errs = append(errs, fmt.Errorf("%s.type: %q must be one of %s", where, g.Type, strings.Join(HostTypes, ", ")))
		}
		if strings.ContainsAny(g.SSH, " \t\n") {
			errs = append(errs, fmt.Errorf("%s.ssh: must not contain spaces", where))
		} else if strings.Contains(g.SSH, "://") {
			if u, err := url.Parse(g.SSH); err != nil || u.Scheme != "ssh" || u.Host == "" {
				errs = append(errs, fmt.Errorf("%s.ssh: must be user@host or ssh://user@host[:port]", where))
			} else if _, pw := u.User.Password(); pw {
				errs = append(errs, fmt.Errorf("%s.ssh: must not carry a password", where))
			}
		}
	}
	return errs
}

func checkHostURL(s string) error {
	if s == "" {
		return errors.New("is required, such as \"https://git.example.com\"")
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" || !strings.Contains(s, "://") {
		// The value is not echoed: it may hold credentials in a form
		// gitx.Mask does not recognise.
		return errors.New(`must be a base URL such as "https://git.example.com"`)
	}
	if u.User != nil {
		return errors.New("must not carry credentials; use a git credential helper (https://qunaxis.github.io/skenv/git-hosts#authentication)")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must not have a query or fragment")
	}
	return nil
}
