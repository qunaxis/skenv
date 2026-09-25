package engine

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// The `skills` CLI fingerprints a skill in two ways: a git tree id (40 hex
// digits) for GitHub installs recorded in the global lock, and a sha256 of
// the files (64 hex digits, computeSkillFolderHash) everywhere else,
// including every computedHash of a project's skills-lock.json.
var (
	treeIDRe     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	folderHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// hashFile is a file of a skill folder: its slash path relative to the
// folder and its content.
type hashFile struct {
	path    string
	content []byte
}

// pathOrder compares relative paths like JavaScript's
// String.prototype.localeCompare without a locale: the Unicode Collation
// Algorithm with the root collation of CLDR, which the ICU of Node applies
// (checked for every pair of printable ASCII characters against Node 24).
// For ASCII it is not byte order: case is ignored until the last level,
// where lowercase comes first, and punctuation sorts before digits and
// letters in its own order ("_" < "-" < "." < "/"), so "SKILL.md" sorts
// between "scripts/…" and "templates/…".
var pathOrder = collate.New(language.Und)

// skillsFolderHash is computeSkillFolderHash of the skills CLI (1.7.0) over
// files: sha256 of each relative path followed by the file content, in
// pathOrder. Paths equal to the collation break ties by bytes; the CLI
// leaves them in directory order, which only non-ASCII names can reach.
func skillsFolderHash(files []hashFile) string {
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b hashFile) int {
		if c := pathOrder.CompareString(a.path, b.path); c != 0 {
			return c
		}
		return strings.Compare(a.path, b.path)
	})
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.path))
		h.Write(f.content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hashedOut reports whether computeSkillFolderHash leaves a path of a
// skill folder out: anything under a .git or node_modules directory.
func hashedOut(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts[:len(parts)-1] {
		if p == ".git" || p == "node_modules" {
			return true
		}
	}
	return false
}

// commitFolderHash is computeSkillFolderHash of folder at commit, as the
// skills CLI computes it on a checkout: regular and executable files only
// (it skips symlinks, and submodules are empty directories in a clone).
// It is "" when the commit has no such folder.
func (e *base) commitFolderHash(cache, commit, folder string) (string, error) {
	out, err := e.env.Git.Output(e.ctx, cache, nil, "ls-tree", "-r", "-z", commit+":"+folder)
	if err != nil {
		return "", nil //nolint:nilerr // no folder at this commit (it removed the folder): no hash
	}
	var files []hashFile
	var oids []string
	for _, rec := range strings.Split(string(out), "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "blob" || (f[0] != "100644" && f[0] != "100755") || hashedOut(name) {
			continue
		}
		files = append(files, hashFile{path: name})
		oids = append(oids, f[2])
	}
	contents, err := e.blobs(cache, oids)
	if err != nil {
		return "", err
	}
	for i := range files {
		files[i].content = contents[oids[i]]
	}
	return skillsFolderHash(files), nil
}

// blobs returns the contents of the blobs oids of the clone cache, which
// is a partial clone (--filter=blob:none): the missing ones are fetched in
// one request first, instead of one request per blob. Contents read once
// are kept for the rest of the command.
func (e *base) blobs(cache string, oids []string) (map[string][]byte, error) {
	if e.blobCache == nil {
		e.blobCache = map[string][]byte{}
	}
	var want []string
	for _, id := range oids {
		if _, ok := e.blobCache[id]; !ok && !slices.Contains(want, id) {
			want = append(want, id)
		}
	}
	if len(want) > 0 {
		input := []byte(strings.Join(want, "\n") + "\n")
		noLazy := e.env.Git
		noLazy.Env = append(slices.Clip(noLazy.Env), "GIT_NO_LAZY_FETCH=1")
		out, err := noLazy.Output(e.ctx, cache, input, "cat-file", "--batch-check=%(objectname) %(objecttype)")
		if err != nil {
			return nil, err
		}
		var missing []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if id, kind, _ := strings.Cut(line, " "); kind == "missing" {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			args := append([]string{"-c", "fetch.negotiationAlgorithm=noop", "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
				"--recurse-submodules=no", "--filter=blob:none", "origin"}, missing...)
			if _, err := e.env.Git.Run(e.ctx, cache, args...); err != nil {
				return nil, err
			}
		}
		out, err = e.env.Git.Output(e.ctx, cache, input, "cat-file", "--batch")
		if err != nil {
			return nil, err
		}
		if err := readBatch(out, e.blobCache); err != nil {
			return nil, err
		}
	}
	got := make(map[string][]byte, len(oids))
	for _, id := range oids {
		got[id] = e.blobCache[id]
	}
	return got, nil
}

// readBatch parses the output of `git cat-file --batch` into into.
func readBatch(out []byte, into map[string][]byte) error {
	r := bufio.NewReader(bytes.NewReader(out))
	for {
		header, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) && header == "" {
			return nil
		}
		if err != nil {
			return err
		}
		f := strings.Fields(header)
		if len(f) != 3 {
			return fmt.Errorf("git cat-file: unexpected %q", strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return fmt.Errorf("git cat-file: unexpected %q", strings.TrimSpace(header))
		}
		content := make([]byte, size+1) // and the newline after it
		if _, err := io.ReadFull(r, content); err != nil {
			return err
		}
		into[f[0]] = content[:size]
	}
}
