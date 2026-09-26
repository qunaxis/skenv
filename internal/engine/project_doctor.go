package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/qunaxis/skenv/internal/manifest"
)

// ProjectReport is the result of ProjectEngine.Doctor.
type ProjectReport struct {
	OK          bool     `json:"ok"`
	File        string   `json:"file"`
	Dir         string   `json:"dir"`
	Mirrors     []string `json:"mirrors"`
	MirrorsMode string   `json:"mirrors_mode"`
	Skills      int      `json:"skills"`
	Issues      []Issue  `json:"issues"`
	Warnings    []string `json:"warnings"`
}

// Doctor compares the project with its [project] section without changing
// anything and without the network, so it can run in CI.
func (e *ProjectEngine) Doctor(asJSON bool) (int, error) {
	r := &ProjectReport{File: e.show(e.file), Dir: e.p.Dir, Mirrors: append([]string{}, e.p.Mirrors...),
		MirrorsMode: e.p.MirrorsMode, Issues: []Issue{}, Warnings: []string{}}
	add := func(class, skill, p, detail string) {
		r.Issues = append(r.Issues, Issue{Class: class, Skill: skill, Path: e.rel(p), Detail: detail})
	}
	dir := e.abs(e.p.Dir)
	want := map[string]bool{}
	for _, s := range e.p.Skills() {
		want[s.Name] = true
		e.doctorCopy(s, filepath.Join(dir, s.Name), add)
	}
	entries, _ := os.ReadDir(dir)
	for _, de := range entries {
		name := de.Name()
		p := filepath.Join(dir, name)
		if strings.HasPrefix(name, ".") || want[name] || !de.IsDir() {
			continue
		}
		if _, err := readMarker(p); err == nil {
			add(ClassExtraManaged, name, p, "copied by skenv but no longer in [project]; `skenv sync` removes it")
			continue
		}
		if !fileExists(filepath.Join(p, "SKILL.md")) {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s has no SKILL.md; it is not a skill and is not mirrored", e.rel(p)))
		}
	}
	if w := e.ignoredWarning(); w != "" {
		r.Warnings = append(r.Warnings, w)
	}
	r.Warnings = append(r.Warnings, e.shadowed(e.user)...)
	names := e.skillNames()
	r.Skills = len(names)
	for _, m := range e.p.Mirrors {
		e.doctorMirror(e.abs(m), names, add)
	}
	sortIssues(r.Issues)
	r.OK = len(r.Issues) == 0
	mirrors := "no mirrors"
	if len(e.p.Mirrors) > 0 {
		mirrors = fmt.Sprintf("mirrors %s (%s)", strings.Join(e.p.Mirrors, ", "), e.p.MirrorsMode)
	}
	ok := fmt.Sprintf("ok: %d project skills match %s (dir %s, %s)", r.Skills, e.rel(e.file), e.p.Dir, mirrors)
	if err := e.printReport(r, asJSON, r.Warnings, r.Issues, ok); err != nil {
		return ExitFatal, err
	}
	if !r.OK {
		return ExitProblems, nil
	}
	return ExitOK, nil
}

// doctorCopy checks the copy of s in dir.
func (e *ProjectEngine) doctorCopy(s manifest.ProjectSkill, p string, add func(class, skill, p, detail string)) {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		add(ClassMissing, s.Name, p, "not copied yet; run `skenv sync`")
		return
	}
	if err != nil {
		add(ClassMissing, s.Name, p, err.Error())
		return
	}
	mk, err := readMarker(p)
	if !fi.IsDir() || err != nil {
		add(ClassConflict, s.Name, p, fmt.Sprintf("exists without a %s marker (a project-own skill?); rename it or the entry, "+
			"or run `skenv sync --adopt` to back it up and copy the entry", markerName))
		return
	}
	if remote, _ := e.hosts.Resolve(s.Repo); !mk.matches(remote, s.Path, s.Commit) {
		add(ClassWrongRev, s.Name, p, fmt.Sprintf("the copy is %s@%.12s (%s), [project] wants %s@%.12s (%s); run `skenv sync`",
			mk.Repo, mk.Rev, mk.Path, s.Repo, s.Commit, s.Path))
	}
	if modified, err := e.modified(p, mk); err != nil {
		add(ClassModified, s.Name, p, err.Error())
	} else if modified {
		add(ClassModified, s.Name, p, fmt.Sprintf("edited locally: the content differs from %s@%.12s; move the change upstream "+
			"or into a project-own skill, or run `skenv sync --adopt` to back it up and restore the copy", mk.Repo, mk.Rev))
	}
}

// doctorMirror checks the mirror mdir against the skills of dir.
func (e *ProjectEngine) doctorMirror(mdir string, names []string, add func(class, skill, p, detail string)) {
	copyMode := e.p.MirrorsMode == manifest.MirrorCopy
	in := map[string]bool{}
	for _, name := range names {
		in[name] = true
		p := filepath.Join(mdir, name)
		src := e.abs(e.p.Dir, name)
		fi, err := os.Lstat(p)
		if err != nil {
			add(ClassBrokenMirror, name, p, fmt.Sprintf("missing; run `skenv sync` to %s %s", map[bool]string{true: "copy", false: "link"}[copyMode], e.rel(src)))
			continue
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			dest, _ := os.Readlink(p)
			switch {
			case copyMode:
				add(ClassMirrorDrift, name, p, "a symlink where mirrors_mode = \"copy\" wants a copy; run `skenv sync`")
			case dest != e.linkDest(mdir, name) && (filepath.IsAbs(dest) || !e.inRepo(filepath.Join(mdir, dest))):
				add(ClassBrokenMirror, name, p, fmt.Sprintf("points out of the repository to %s, expected %s; `skenv sync --adopt` backs it up and replaces it",
					dest, e.linkDest(mdir, name)))
			case dest != e.linkDest(mdir, name):
				add(ClassBrokenMirror, name, p, fmt.Sprintf("points to %s, expected %s; run `skenv sync`", dest, e.linkDest(mdir, name)))
			}
			continue
		}
		srcHash, _ := treeHash(src)
		h, err := treeHash(p)
		if err != nil {
			add(ClassMirrorDrift, name, p, err.Error())
			continue
		}
		mk, _ := readMarker(p)
		fix := "run `skenv sync`"
		if (mk.Mirror == "" || mk.Hash != h) && h != srcHash {
			fix = fmt.Sprintf("edited in the mirror? move the change to %s, then run `skenv sync --adopt`", e.rel(src))
		}
		switch {
		case !copyMode:
			add(ClassMirrorDrift, name, p, fmt.Sprintf("a directory where a symlink to %s is expected; %s", e.rel(src), fix))
		case h != srcHash:
			add(ClassMirrorDrift, name, p, fmt.Sprintf("differs from %s; %s", e.rel(src), fix))
		case mk.Mirror == "":
			add(ClassMirrorDrift, name, p, fmt.Sprintf("the same content as %s but no %s marker; run `skenv sync`", e.rel(src), markerName))
		}
	}
	entries, _ := os.ReadDir(mdir)
	for _, de := range entries {
		name := de.Name()
		p := filepath.Join(mdir, name)
		if in[name] || strings.HasPrefix(name, ".") {
			continue
		}
		if e.isMirrorEntry(p, name) {
			add(ClassBrokenMirror, name, p, fmt.Sprintf("no skill %s in %s any more; `skenv sync` removes it", name, e.p.Dir))
			continue
		}
		add(ClassUnmanaged, name, p, fmt.Sprintf("only in this mirror, not in %s; move it to %s so every agent gets it", e.p.Dir, e.p.Dir))
	}
}
