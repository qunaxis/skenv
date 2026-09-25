package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/harness"
)

// Discrepancy classes reported by doctor.
const (
	ClassMissing       = "missing"
	ClassExtraManaged  = "extra-managed"
	ClassUnmanaged     = "unmanaged"
	ClassWrongRev      = "wrong-rev"
	ClassBrokenLink    = "broken-link"
	ClassConflict      = "conflict"
	ClassDirty         = "dirty"
	ClassUnpushed      = "unpushed"
	ClassBehind        = "behind"
	ClassAgentMismatch = "agent-mismatch"
)

// Issue is one discrepancy between the machine and the manifest.
type Issue struct {
	Class  string `json:"class"`
	Skill  string `json:"skill,omitempty"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

// DoctorReport is the result of Doctor.
type DoctorReport struct {
	OK       bool     `json:"ok"`
	Manifest string   `json:"manifest"`
	Store    string   `json:"store"`
	Targets  []string `json:"targets"`
	Skills   int      `json:"skills"`
	Issues   []Issue  `json:"issues"`
	Warnings []string `json:"warnings"`
}

// Doctor compares the machine with the manifest without changing anything
// (other than `git fetch` in own repositories).
func (e *Engine) Doctor(asJSON bool) (int, error) {
	r := &DoctorReport{Manifest: e.show(e.manifestPath), Store: e.show(e.store), Issues: []Issue{}, Warnings: []string{}}
	for _, t := range e.targets {
		r.Targets = append(r.Targets, e.show(t))
	}
	add := func(class, skill, p, detail string) {
		r.Issues = append(r.Issues, Issue{Class: class, Skill: skill, Path: e.show(p), Detail: detail})
	}
	warn := func(format string, args ...any) {
		r.Warnings = append(r.Warnings, gitx.Mask(fmt.Sprintf(format, args...)))
	}

	e.doctorOwn(add, warn)
	skills, err := e.skills()
	if err != nil {
		return ExitFatal, err
	}
	r.Skills = len(skills)
	want := e.desired(skills)

	for _, s := range skills {
		e.doctorStore(s, add)
		var present, absent []string
		for _, t := range e.targets {
			p := filepath.Join(t, s.Name)
			if _, err := os.Lstat(p); err != nil {
				absent = append(absent, p)
				continue
			}
			present = append(present, p)
			e.checkLink(p, e.linkDest(t, s.Name), s.Name, add)
		}
		switch {
		case len(present) == 0 && len(absent) > 0:
			add(ClassMissing, s.Name, absent[0], fmt.Sprintf("not linked into any agent directory (%d targets); run `skenv sync`", len(absent)))
		case len(absent) > 0:
			for _, p := range absent {
				add(ClassAgentMismatch, s.Name, p, "linked for some agents but not this one; run `skenv link`")
			}
		}
	}

	for _, p := range e.st.Paths() {
		if _, ok := want[p]; ok {
			continue
		}
		if _, err := os.Lstat(p); err == nil {
			add(ClassExtraManaged, e.st.Managed[p].Skill, p, "managed by skenv but no longer in the manifest; `skenv sync` removes it")
		}
	}

	for _, dir := range append([]string{e.store}, e.targets...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, de := range entries {
			p := filepath.Join(dir, de.Name())
			if strings.HasPrefix(de.Name(), ".") || e.isClaudeSynced(p) || e.m.Layout.Ignored(de.Name()) || e.owned(p) {
				continue
			}
			if _, ok := want[p]; ok {
				continue // reported as conflict above
			}
			add(ClassUnmanaged, de.Name(), p, "not from the manifest (installed manually or by another tool); left alone")
		}
	}

	sort.SliceStable(r.Issues, func(a, b int) bool {
		if r.Issues[a].Class != r.Issues[b].Class {
			return r.Issues[a].Class < r.Issues[b].Class
		}
		return r.Issues[a].Path < r.Issues[b].Path
	})
	r.OK = len(r.Issues) == 0
	if err := e.printDoctor(r, asJSON); err != nil {
		return ExitFatal, err
	}
	if !r.OK {
		return ExitProblems, nil
	}
	return ExitOK, nil
}

func (e *Engine) doctorOwn(add func(class, skill, p, detail string), warn func(string, ...any)) {
	for i := range e.m.Own {
		o := &e.m.Own[i]
		dir := e.ownPath(o)
		if _, err := os.Stat(dir); err != nil {
			add(ClassMissing, "", dir, fmt.Sprintf("own repo %s is not cloned; run `skenv sync`", o.Repo))
			continue
		}
		if !e.env.Git.OK(e.ctx, dir, "rev-parse", "--is-inside-work-tree") {
			warn("%s is not a git working copy (own repo %s)", e.show(dir), o.Repo)
			continue
		}
		switch v, ok, err := harness.Version(dir); {
		case err != nil:
			warn("%s: %v", e.show(dir), err)
		case ok && harness.Compare(v, harness.Latest) < 0:
			warn("%s: harness %s is older than %s of this skenv; run `skenv repo apply` there", e.show(dir), v, harness.Latest)
		}
		if out, err := e.env.Git.Run(e.ctx, dir, "status", "--porcelain"); err == nil && out != "" {
			n := len(strings.Split(out, "\n"))
			add(ClassDirty, "", dir, fmt.Sprintf("%d uncommitted changes", n))
		}
		if _, err := e.env.Git.Run(e.ctx, dir, "fetch", "--quiet"); err != nil {
			warn("%s: git fetch failed, ahead/behind may be stale: %v", e.show(dir), err)
		}
		out, err := e.env.Git.Run(e.ctx, dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
		if err != nil {
			warn("%s: no upstream branch, cannot compare with origin", e.show(dir))
			continue
		}
		f := strings.Fields(out)
		if len(f) != 2 {
			continue
		}
		ahead, _ := strconv.Atoi(f[0])
		behind, _ := strconv.Atoi(f[1])
		if ahead > 0 {
			add(ClassUnpushed, "", dir, fmt.Sprintf("%d commits ahead of upstream; push them", ahead))
		}
		if behind > 0 {
			add(ClassBehind, "", dir, fmt.Sprintf("%d commits behind upstream; run `skenv sync`", behind))
		}
	}
}

func (e *Engine) doctorStore(s Skill, add func(class, skill, p, detail string)) {
	p := e.storePath(s.Name)
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		add(ClassMissing, s.Name, p, "not in the store; run `skenv sync`")
		return
	}
	if err != nil {
		add(ClassMissing, s.Name, p, err.Error())
		return
	}
	if !e.owned(p) {
		add(ClassConflict, s.Name, p, "exists but is not managed by skenv (or was replaced since); `skenv sync --adopt` backs it up and replaces it")
		return
	}
	if s.OwnDir != "" {
		e.checkLink(p, s.OwnDir, s.Name, add)
		return
	}
	if !fi.IsDir() {
		add(ClassBrokenLink, s.Name, p, "expected a vendored directory; run `skenv sync`")
		return
	}
	mk, err := readMarker(p)
	if err != nil {
		add(ClassWrongRev, s.Name, p, "vendor marker .skenv is missing or unreadable; run `skenv sync`")
		return
	}
	v := s.Vendor
	if mk.Repo != v.Repo || mk.Path != v.Path || mk.Rev != v.Rev {
		add(ClassWrongRev, s.Name, p, fmt.Sprintf("store has %s@%.12s (%s), manifest wants %s@%.12s (%s); run `skenv sync`",
			mk.Repo, mk.Rev, mk.Path, v.Repo, v.Rev, v.Path))
	}
}

// checkLink verifies that p is a managed symlink to dest that resolves.
func (e *Engine) checkLink(p, dest, skill string, add func(class, skill, p, detail string)) {
	if !e.owned(p) {
		add(ClassConflict, skill, p, "exists but is not managed by skenv (or was replaced since); `skenv sync --adopt` backs it up and replaces it")
		return
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		add(ClassBrokenLink, skill, p, "expected a symlink; run `skenv link`")
		return
	}
	cur, _ := os.Readlink(p)
	if cur != dest {
		add(ClassBrokenLink, skill, p, fmt.Sprintf("points to %s, expected %s; run `skenv link`", cur, dest))
		return
	}
	if _, err := os.Stat(p); err != nil {
		add(ClassBrokenLink, skill, p, fmt.Sprintf("dangling symlink to %s", cur))
	}
}

func (e *Engine) printDoctor(r *DoctorReport, asJSON bool) error {
	out := e.env.Stdout
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(e.env.Stderr, "warning: %s\n", w)
	}
	if r.OK {
		fmt.Fprintf(out, "ok: %d skills match %s (store %s, targets %s)\n", r.Skills, r.Manifest, r.Store, strings.Join(r.Targets, ", "))
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CLASS\tSKILL\tPATH\tDETAIL")
	for _, is := range r.Issues {
		skill := is.Skill
		if skill == "" {
			skill = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", is.Class, skill, is.Path, is.Detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d discrepancies\n", len(r.Issues))
	return nil
}
