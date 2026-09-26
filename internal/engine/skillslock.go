package engine

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qunaxis/skenv/internal/atomicfile"
	"github.com/qunaxis/skenv/internal/manifest"
)

// The versions of the lock files that the vercel `skills` CLI writes
// (checked against skills 1.7.0): the global ~/.agents/.skill-lock.json
// and the skills-lock.json of a project.
const (
	skillsLockVersion  = 3
	projectLockVersion = 1
	projectLockName    = "skills-lock.json"
)

// lockEntry is one skill of a lock of the `skills` CLI. It pins no commit:
// skillFolderHash (global lock) is the git tree id of the skill folder at
// install or update time, taken from the GitHub Trees API, or for other
// sources the sha256 of computeSkillFolderHash; computedHash (project
// lock) is always the latter.
type lockEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	Ref             string `json:"ref"`
	SkillPath       string `json:"skillPath"`
	SkillFolderHash string `json:"skillFolderHash"`
	ComputedHash    string `json:"computedHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// hash is the content fingerprint of the entry and the name of its field.
func (le lockEntry) hash() (value, field string) {
	if le.ComputedHash != "" {
		return le.ComputedHash, "computedHash"
	}
	return le.SkillFolderHash, "skillFolderHash"
}

// skillsLock is a lock file of the `skills` CLI as read.
type skillsLock struct {
	path    string
	version int
	mode    fs.FileMode
	top     map[string]json.RawMessage
	skills  map[string]json.RawMessage
	// entries are keyed by the directory the skills CLI installs a skill
	// in (dirName of the key); keys maps that name back to the key.
	entries map[string]lockEntry
	keys    map[string]string
}

var unsafeName = regexp.MustCompile(`[^a-z0-9._]+`)

// dirName is the directory name the skills CLI gives a skill (its
// sanitizeName): lowercase, runs of other characters as "-", no leading or
// trailing "." or "-". The lock is keyed by the name in SKILL.md.
func dirName(key string) string {
	n := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(key), "-"), ".-")
	if len(n) > 255 {
		n = n[:255]
	}
	if n == "" {
		return "unnamed-skill"
	}
	return n
}

// skillsLockPath is where the `skills` CLI keeps its global lock:
// $XDG_STATE_HOME/skills/.skill-lock.json when XDG_STATE_HOME is set,
// ~/.agents/.skill-lock.json otherwise.
func (e *Engine) skillsLockPath() string {
	if dir := e.env.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "skills", ".skill-lock.json")
	}
	return filepath.Join(e.env.Home, ".agents", ".skill-lock.json")
}

// readSkillsLock reads the lock at p, which must have the given version; a
// missing file is an empty lock.
func readSkillsLock(p string, version int) (*skillsLock, error) {
	l := &skillsLock{path: p, version: version, mode: 0o644, skills: map[string]json.RawMessage{}, entries: map[string]lockEntry{}, keys: map[string]string{}}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(p); err == nil {
		l.mode = fi.Mode().Perm()
	}
	if err := json.Unmarshal(data, &l.top); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	var got int
	if err := json.Unmarshal(l.top["version"], &got); err != nil || got != version {
		return nil, fmt.Errorf("%s: version %s is not supported; skenv reads version %d (skills CLI 1.x)", p, l.top["version"], version)
	}
	if raw, ok := l.top["skills"]; ok {
		if err := json.Unmarshal(raw, &l.skills); err != nil {
			return nil, fmt.Errorf("%s: skills: %w", p, err)
		}
	}
	for _, key := range sortedKeys(l.skills) {
		var le lockEntry
		if err := json.Unmarshal(l.skills[key], &le); err != nil {
			return nil, fmt.Errorf("%s: skill %q: %w", p, key, err)
		}
		if _, dup := l.entries[dirName(key)]; !dup {
			l.entries[dirName(key)], l.keys[dirName(key)] = le, key
		}
	}
	return l, nil
}

// without writes the lock without the skills installed in the named
// directories; every other key and entry stays. The file is read again
// first, so an entry added since readSkillsLock is kept. A project lock
// left without skills is removed: the skills CLI has nothing to restore.
func (l *skillsLock) without(names []string) error {
	now, err := readSkillsLock(l.path, l.version)
	if err != nil {
		return err
	}
	drop := map[string]bool{}
	for _, n := range names {
		drop[l.keys[n]] = true
	}
	skills := map[string]json.RawMessage{}
	for key, raw := range now.skills {
		if !drop[key] {
			skills[key] = raw
		}
	}
	if l.version == projectLockVersion && len(skills) == 0 {
		return os.Remove(l.path)
	}
	l.top = now.top
	var top any = struct {
		Version json.RawMessage            `json:"version"`
		Skills  map[string]json.RawMessage `json:"skills"`
	}{now.top["version"], skills} // the order writeLocalLock writes
	if l.version != projectLockVersion {
		all := map[string]any{}
		for k, v := range l.top {
			all[k] = v
		}
		all["skills"] = skills
		top = all
	}
	// Like JSON.stringify(lock, null, 2) of the skills CLI: no HTML escapes,
	// and a final newline in the project lock only.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(top); err != nil {
		return err
	}
	// A lock kept elsewhere (dotfiles) behind a symlink stays a symlink.
	target := l.path
	if p, err := filepath.EvalSymlinks(l.path); err == nil {
		target = p
	}
	out := b.Bytes()
	if l.version != projectLockVersion {
		out = bytes.TrimSuffix(out, []byte("\n"))
	}
	return atomicfile.Write(target, out, l.mode)
}

// lockRepo is the repo value of a lock entry: owner/repo for GitHub; for
// other git hosts the short form on a built-in host or one of hosts
// ("gitlab:group/repo", "<alias>:path"), else the clone URL.
func lockRepo(le lockEntry, hosts manifest.Hosts) (string, error) {
	switch le.SourceType {
	case "github":
		if manifest.IsShortRepo(le.Source) {
			return le.Source, nil
		}
		if repo, ok := manifest.Hosts(nil).ShortForm(le.SourceURL); ok && manifest.IsShortRepo(repo) {
			return repo, nil
		}
	case "git", "gitlab":
		u := le.SourceURL
		if u == "" && strings.Contains(le.Source, "://") {
			u = le.Source // a project lock records the URL as source
		}
		if u != "" {
			if hasCredentials(u) {
				return "", errors.New("its sourceUrl carries credentials; add it with `skenv vendor add` and a URL without them")
			}
			if repo, ok := hosts.ShortForm(u); ok {
				return repo, nil
			}
			return u, nil
		}
	}
	return "", fmt.Errorf("installed from a %q source, not a git repository; `skenv vendor add` needs one", le.SourceType)
}

// hasCredentials reports whether a git URL carries a secret: a password,
// or any user part in an http(s) URL (a token). ssh://git@host is fine.
func hasCredentials(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return false
	}
	_, pw := u.User.Password()
	return pw || u.Scheme == "http" || u.Scheme == "https"
}

// lockFolder is the skill folder of skillPath ("…/SKILL.md") the way the
// `skills` CLI derives it for skillFolderHash; "" is the repository root.
func lockFolder(skillPath string) string {
	f := strings.ReplaceAll(skillPath, "\\", "/")
	if strings.HasSuffix(strings.ToLower(f), "skill.md") {
		f = f[:len(f)-len("skill.md")]
	}
	return strings.Trim(f, "/")
}

// How a rev was found for a lock entry.
const (
	revByHash = iota // a commit's folder has the hash of the entry
	revByCopy        // a commit's folder has the files of the installed copy
	revByHead        // nothing matched: HEAD of ref or the default branch
)

// lockRev resolves the commit of a lock entry: the newest commit on its
// ref (or the default branch) whose skill folder has the hash of the
// entry, searched from updatedAt (else installedAt) back. A tree id is
// compared with the folder's tree, a sha256 with computeSkillFolderHash of
// the folder's files. When no commit matches (history rewritten, ref
// deleted), the commit whose folder holds the files of the installed copy
// at dir (when there is one), and at last the tip. tip names the branch
// searched.
func (e *base) lockRev(cache, repo string, le lockEntry, dir string) (rev string, how int, tip string, err error) {
	tip = "the default branch"
	if le.Ref != "" {
		tip = le.Ref
	}
	head, err := e.lockTip(cache, repo, le.Ref)
	if err != nil {
		return "", 0, tip, err
	}
	folder := lockFolder(le.SkillPath)
	commits, err := e.candidates(cache, head, folder, le)
	if err != nil {
		return "", 0, tip, err
	}
	hash, _ := le.hash()
	switch {
	case treeIDRe.MatchString(hash):
		for _, c := range commits {
			if tree, err := e.folderTree(cache, c, folder); err == nil && tree == hash {
				return c, revByHash, tip, nil
			}
		}
	case folderHashRe.MatchString(hash):
		c, err := e.matchFolderHash(cache, commits, folder, hash)
		if err != nil {
			return "", 0, tip, err
		}
		if c != "" {
			return c, revByHash, tip, nil
		}
	}
	if dir == "" {
		return head, revByHead, tip, nil
	}
	if files, err := copyBlobs(dir); err == nil {
		for _, c := range commits {
			if got, err := e.commitBlobs(cache, c, folder); err == nil && sameBlobs(got, files) {
				return c, revByCopy, tip, nil
			}
		}
	}
	return head, revByHead, tip, nil
}

// lockTip is the commit at ref: a branch, a tag or a commit SHA (short
// ones too); HEAD of the default branch when ref is empty.
func (e *base) lockTip(cache, repo, ref string) (string, error) {
	if ref == "" {
		return e.resolveRev(cache, repo, "")
	}
	if strings.HasPrefix(ref, "-") || !e.env.Git.OK(e.ctx, cache, "check-ref-format", "--allow-onelevel", ref) {
		return "", fmt.Errorf("ref %q is not a valid git ref", ref)
	}
	commit := func(r string) string {
		c, err := e.env.Git.Run(e.ctx, cache, "rev-parse", "--verify", "--quiet", "--end-of-options", r+"^{commit}")
		if err != nil {
			return ""
		}
		return c
	}
	for _, r := range []string{"refs/remotes/origin/" + ref, "refs/tags/" + ref, ref} {
		if c := commit(r); c != "" {
			return c, nil
		}
	}
	if _, err := e.env.Git.Run(e.ctx, cache, "fetch", "--quiet", "origin", "--end-of-options", ref); err == nil {
		if c := commit("FETCH_HEAD"); c != "" {
			return c, nil
		}
	}
	return "", fmt.Errorf("ref %q not found in %s", ref, repo)
}

// candidates lists the commits of head that touch folder, newest first,
// starting with the ones made by updatedAt (else installedAt): the
// installed version cannot be newer. Later commits follow, in case of
// clock skew.
func (e *base) candidates(cache, head, folder string, le lockEntry) ([]string, error) {
	args := []string{"log", "--format=%H %ct", head}
	if folder != "" {
		args = append(args, "--", folder)
	}
	out, err := e.env.Git.Run(e.ctx, cache, args...)
	if err != nil {
		return nil, err
	}
	var since int64 = -1
	for _, ts := range []string{le.UpdatedAt, le.InstalledAt} {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			since = t.Unix()
			break
		}
	}
	var before, after []string
	for _, line := range strings.Split(out, "\n") {
		sha, ct, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if t, err := strconv.ParseInt(ct, 10, 64); err == nil && since >= 0 && t > since {
			after = append(after, sha)
			continue
		}
		before = append(before, sha)
	}
	return append(before, after...), nil
}

// folderTree is the tree id of folder at commit.
func (e *base) folderTree(cache, commit, folder string) (string, error) {
	return e.env.Git.Run(e.ctx, cache, "rev-parse", "--verify", "--quiet", commit+":"+folder)
}

// skipCopied reports whether the `skills` CLI leaves a file or directory
// out of the copies it installs.
func skipCopied(name string, dir bool) bool {
	if dir {
		return name == ".git" || name == "__pycache__" || name == "__pypackages__"
	}
	return name == "metadata.json"
}

// commitBlobs maps each file of folder at commit to its blob id, without
// the files the `skills` CLI does not copy.
func (e *base) commitBlobs(cache, commit, folder string) (map[string]string, error) {
	out, err := e.env.Git.Run(e.ctx, cache, "ls-tree", "-r", "-z", commit+":"+folder)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "blob" || copiedOut(name) {
			continue
		}
		files[name] = f[2]
	}
	return files, nil
}

// copiedOut reports whether a path inside a skill folder is left out of
// installed copies.
func copiedOut(rel string) bool {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		if skipCopied(p, i < len(parts)-1) {
			return true
		}
	}
	return false
}

// copyBlobs maps each file of the installed copy at dir to its git blob
// id.
func copyBlobs(dir string) (map[string]string, error) { return dirBlobs(dir, skipCopied) }

// dirBlobs maps each file and symlink under dir to its git blob id, a
// symlink by its target, without what skip says to leave out.
func dirBlobs(dir string, skip func(name string, dir bool) bool) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if skip(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		var content []byte
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			dest, err := os.Readlink(p)
			if err != nil {
				return err
			}
			content = []byte(dest)
		case d.Type().IsRegular():
			if content, err = os.ReadFile(p); err != nil {
				return err
			}
		default:
			return nil
		}
		files[filepath.ToSlash(rel)] = blobID(content)
		return nil
	})
	return files, err
}

// blobID is the git object id of a blob with content.
func blobID(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

func sameBlobs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// lineDiff renders the change from before to after as a diff of the lines
// that differ, with up to two lines of context; "" when they are equal.
func lineDiff(name string, before, after []byte) string {
	if bytes.Equal(before, after) {
		return ""
	}
	a, b := splitKeep(before), splitKeep(after)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var s strings.Builder
	fmt.Fprintf(&s, "--- %s\n+++ %s\n", name, name)
	for _, l := range a[max(0, pre-2):pre] {
		s.WriteString(" " + l)
	}
	for _, l := range a[pre : len(a)-suf] {
		s.WriteString("-" + l)
	}
	for _, l := range b[pre : len(b)-suf] {
		s.WriteString("+" + l)
	}
	for _, l := range a[len(a)-suf : min(len(a), len(a)-suf+2)] {
		s.WriteString(" " + l)
	}
	return s.String()
}

// splitKeep splits text into lines that each end with "\n".
func splitKeep(data []byte) []string {
	var out []string
	for _, l := range strings.SplitAfter(string(data), "\n") {
		if l == "" {
			continue
		}
		if !strings.HasSuffix(l, "\n") {
			l += "\n"
		}
		out = append(out, l)
	}
	return out
}

// sortedKeys returns the keys of m in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// skillDirs lists the subdirectories of dir that hold a SKILL.md.
func skillDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), ".") || !de.IsDir() {
			continue
		}
		if fileExists(filepath.Join(dir, de.Name(), "SKILL.md")) {
			out = append(out, de.Name())
		}
	}
	return out
}

// vendorPath is the manifest path of a lock folder.
func vendorPath(folder string) string {
	if folder == "" {
		return "."
	}
	return path.Clean(folder)
}
