package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/model/skenvfile"
)

// VendorAdd pins a third-party skill in [project] and syncs the project.
func (e *ProjectScope) VendorAdd(o VendorAddOptions) (int, error) {
	v, err := e.resolveDependency(o)
	if err != nil {
		return ExitFatal, err
	}
	if _, ok := e.project.Skill(v.Name); ok {
		return ExitFatal, fmt.Errorf("project skill %q is already in [project] of %s; use `skenv vendor update --project %s`", v.Name, e.displayPath(e.file), v.Name)
	}
	dst := e.abs(e.project.Dir, v.Name)
	if _, err := os.Lstat(dst); err == nil && !e.opts.Adopt {
		if _, err := readMarker(dst); err != nil {
			return ExitFatal, fmt.Errorf("%s is a project-own skill; pass --name to vendor %s under another name, "+
				"or --adopt to back it up and replace it", e.rel(dst), v.Repo)
		}
	}
	if err := e.edit(func(data []byte, ext string) ([]byte, error) {
		return skenvfile.AppendDependency(data, ext, skenvfile.SectionProject, v)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("add dependency %s (%s@%.12s, %s) to [project] of %s", v.Name, v.Repo, v.Commit, v.SkillDir, e.displayPath(e.file))
	return e.syncAfterEdit("add dependency " + v.Name)
}

// VendorUpdate moves project skills to a new commit and syncs the project:
// the named ones, or every entry of [project] without names. A skill of a
// [project.from.<id>] entry moves the whole entry, since its skills share
// one commit.
func (e *ProjectScope) VendorUpdate(names []string, rev string) (int, error) {
	var vendors, froms []string
	if len(names) == 0 {
		for _, d := range e.project.DependencyList() {
			vendors = append(vendors, d.Name)
		}
		for _, f := range e.project.FromList() {
			froms = append(froms, f.ID)
		}
		if len(vendors)+len(froms) == 0 {
			e.infof("no dependencies or from entries in [project] of %s", e.displayPath(e.file))
			return e.summary("vendor"), nil
		}
	}
	for _, name := range names {
		s, ok := e.project.Skill(name)
		switch {
		case !ok:
			return ExitFatal, fmt.Errorf("project skill %q is not in [project] of %s", name, e.displayPath(e.file))
		case s.Dependency != nil:
			vendors = append(vendors, name)
		case !slices.Contains(froms, s.From.ID):
			froms = append(froms, s.From.ID)
			if len(s.From.Skills) > 1 {
				e.infof("%s comes from project.from.%s (%s); updating the whole entry (%s)", name, s.From.ID, s.Repo, strings.Join(s.From.Skills, ", "))
			}
		}
	}
	vendors = slices.Compact(slices.Sorted(slices.Values(vendors)))
	var updated []string
	for _, name := range vendors {
		s, _ := e.project.Skill(name)
		newRev, err := e.nextRev("dependency "+name, name, s.Repo, s.Path, s.Commit, rev)
		if err == nil && newRev != "" {
			err = e.edit(func(data []byte, ext string) ([]byte, error) {
				return skenvfile.SetDependencyCommit(data, ext, skenvfile.SectionProject, name, newRev)
			})
		}
		switch {
		case err != nil:
			e.errorf("update dependency %s: %v", name, err)
		case newRev != "":
			e.changef("update dependency %s %.12s → %.12s in [project] of %s", name, s.Commit, newRev, e.displayPath(e.file))
			updated = append(updated, fmt.Sprintf("%s to %.12s", name, newRev))
		}
	}
	slices.Sort(froms)
	for _, id := range froms {
		f := e.project.From[id]
		what := fmt.Sprintf("from.%s %s (%s)", id, f.Repo, strings.Join(f.Skills, ", "))
		newRev, err := e.nextRev(what, f.Repo, f.Repo, f.SkillsDir, f.Commit, rev)
		if err == nil && newRev != "" {
			err = e.edit(func(data []byte, ext string) ([]byte, error) {
				return skenvfile.SetFromCommit(data, ext, id, newRev)
			})
		}
		switch {
		case err != nil:
			e.errorf("update %s: %v", what, err)
		case newRev != "":
			e.changef("update %s %.12s → %.12s in [project] of %s", what, f.Commit, newRev, e.displayPath(e.file))
			updated = append(updated, fmt.Sprintf("%s to %.12s", strings.Join(f.Skills, ", "), newRev))
		}
	}
	hint := ""
	if len(updated) > 0 {
		hint = "update " + strings.Join(updated, "; ")
	}
	return e.syncAfterEdit(hint)
}

// VendorRemove drops a [project.dependencies.<name>] entry and syncs the
// project, which removes its copy and mirrors.
func (e *ProjectScope) VendorRemove(name string) (int, error) {
	s, ok := e.project.Skill(name)
	if !ok {
		return ExitFatal, fmt.Errorf("project skill %q is not in [project] of %s", name, e.displayPath(e.file))
	}
	if s.From != nil {
		return ExitFatal, fmt.Errorf("project skill %q comes from project.from.%s (%s); remove it from the skills of that entry "+
			"(or the entry) in %s, then run `skenv sync`", name, s.From.ID, s.Repo, e.displayPath(e.file))
	}
	if err := e.edit(func(data []byte, ext string) ([]byte, error) {
		return skenvfile.RemoveDependency(data, ext, skenvfile.SectionProject, name)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("remove dependency %s from [project] of %s", name, e.displayPath(e.file))
	return e.syncAfterEdit("remove dependency " + name)
}

// edit applies fn to the skenv file, validates the result, writes it
// (unless --dry-run) and reloads [project] from it.
func (e *ProjectScope) edit(fn func(data []byte, ext string) ([]byte, error)) error {
	ext := filepath.Ext(e.file)
	return e.editFile(e.file, func(data []byte) ([]byte, error) { return fn(data, ext) }, func(out []byte) error {
		p, err := skenvfile.ParseProject(out, ext)
		if err != nil {
			return err
		}
		if err := e.writeSkenvFile(e.file, out); err != nil {
			return fmt.Errorf("write %s: %w", e.displayPath(e.file), err)
		}
		e.project = p
		e.hosts = p.GitHosts
		return nil
	})
}

// syncAfterEdit syncs the whole project after an edit of [project].
func (e *ProjectScope) syncAfterEdit(hint string) (int, error) {
	e.syncCopies()
	e.syncMirrors()
	if hint != "" {
		e.commitHint(hint)
	}
	return e.summary("vendor"), nil
}
