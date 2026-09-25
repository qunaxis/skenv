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

// folderAt is the files of a skill folder at a commit: slash paths
// relative to the folder and their blob ids.
type folderAt struct {
	commit string
	paths  []string
	oids   []string
}

// matchFolderHash returns the first of commits whose folder has the
// fingerprint hash, "" when none does. A fingerprint is either hash the
// skills CLI records: computeSkillFolderHash of a clone (regular and
// executable files, without .git and node_modules; it skips symlinks, and
// submodules are empty directories in a clone), or the snapshot hash of a
// blob install from its own servers (for a few owners, such as
// vercel-labs), the same over the files it installs: without
// metadata.json, __pycache__ and __pypackages__, with node_modules. The
// blobs of every commit are fetched into the partial clone in one go.
func (e *base) matchFolderHash(cache string, commits []string, folder, hash string) (string, error) {
	var list []folderAt
	var trees, oids []string
	for _, c := range commits {
		out, err := e.env.Git.Output(e.ctx, cache, nil, "ls-tree", "-r", "-z", c+":"+folder)
		if err != nil {
			continue // the commit removed the folder
		}
		f := folderAt{commit: c}
		for _, rec := range strings.Split(string(out), "\x00") {
			meta, name, ok := strings.Cut(rec, "\t")
			fields := strings.Fields(meta)
			if !ok || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
				continue
			}
			f.paths = append(f.paths, name)
			f.oids = append(f.oids, fields[2])
		}
		list = append(list, f)
		trees = append(trees, c+":"+folder)
		oids = append(oids, f.oids...)
	}
	contents, err := e.blobs(cache, trees, oids)
	if err != nil {
		return "", err
	}
	for _, f := range list {
		var clone, snapshot []hashFile
		for i, p := range f.paths {
			hf := hashFile{path: p, content: contents[f.oids[i]]}
			if !hashedOut(p) {
				clone = append(clone, hf)
			}
			if !copiedOut(p) {
				snapshot = append(snapshot, hf)
			}
		}
		if skillsFolderHash(clone) == hash || skillsFolderHash(snapshot) == hash {
			return f.commit, nil
		}
	}
	return "", nil
}

// blobs returns the contents of the blobs oids, which the tree-ishes
// trees reach, from the clone cache. The cache is a partial clone
// (--filter=blob:none): the blobs it lacks are listed without fetching
// them (rev-list --missing=print, which also works on git before 2.44) and
// fetched in a few requests, not one per blob.
func (e *base) blobs(cache string, trees, oids []string) (map[string][]byte, error) {
	oids = slices.Compact(slices.Sorted(slices.Values(oids)))
	if len(oids) == 0 {
		return nil, nil
	}
	out, err := e.env.Git.Run(e.ctx, cache, append([]string{"rev-list", "--objects", "--no-object-names", "--missing=print"}, trees...)...)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(line, "?"); ok {
			missing = append(missing, id)
		}
	}
	for chunk := range slices.Chunk(missing, 500) {
		args := append([]string{"-c", "fetch.negotiationAlgorithm=noop", "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
			"--recurse-submodules=no", "--filter=blob:none", "origin"}, chunk...)
		if _, err := e.env.Git.Run(e.ctx, cache, args...); err != nil {
			return nil, err
		}
	}
	batch, err := e.env.Git.Output(e.ctx, cache, []byte(strings.Join(oids, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	contents := map[string][]byte{}
	if err := readBatch(batch, contents); err != nil {
		return nil, err
	}
	return contents, nil
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
