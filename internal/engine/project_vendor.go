package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// VendorAdd pins a third-party skill in [project] and syncs the project.
func (e *ProjectEngine) VendorAdd(o VendorAddOptions) (int, error) {
	v, err := e.resolveVendor(o)
	if err != nil {
		return ExitFatal, err
	}
	if _, ok := e.p.Skill(v.Name); ok {
		return ExitFatal, fmt.Errorf("project skill %q is already in [project] of %s; use `skenv vendor update --project %s`", v.Name, e.show(e.file), v.Name)
	}
	dst := e.abs(e.p.Dir, v.Name)
	if _, err := os.Lstat(dst); err == nil && !e.opts.Adopt {
		if _, err := readMarker(dst); err != nil {
			return ExitFatal, fmt.Errorf("%s is a project-own skill; pass --name to vendor %s under another name, "+
				"or --adopt to back it up and replace it", e.rel(dst), v.Repo)
		}
	}
	if err := e.edit(func(data []byte, ext string) ([]byte, error) {
		return manifest.AppendVendor(data, ext, skenvfile.Project, v)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("add vendor %s (%s@%.12s, %s) to [project] of %s", v.Name, v.Repo, v.Rev, v.Path, e.show(e.file))
	return e.syncAfterEdit("add vendor skill " + v.Name)
}

// VendorUpdate moves project skills to a new commit and syncs the project:
// the named ones, or every entry of [project] without names. A skill of a
// [[project.from]] entry moves the whole entry, since its skills share one
// rev.
func (e *ProjectEngine) VendorUpdate(names []string, rev string) (int, error) {
	var vendors []string
	var froms []int
	if len(names) == 0 {
		for _, v := range e.p.Vendor {
			vendors = append(vendors, v.Name)
		}
		for i := range e.p.From {
			froms = append(froms, i)
		}
		if len(vendors)+len(froms) == 0 {
			e.infof("no vendor or from entries in [project] of %s", e.show(e.file))
			return e.summary("vendor"), nil
		}
	}
	for _, name := range names {
		s, ok := e.p.Skill(name)
		switch {
		case !ok:
			return ExitFatal, fmt.Errorf("project skill %q is not in [project] of %s", name, e.show(e.file))
		case s.Vendor != nil:
			vendors = append(vendors, name)
		case !slices.Contains(froms, s.FromAt):
			froms = append(froms, s.FromAt)
			if len(s.From.Skills) > 1 {
				e.infof("%s comes from [[project.from]] %s; updating the whole entry (%s)", name, s.Repo, strings.Join(s.From.Skills, ", "))
			}
		}
	}
	vendors = slices.Compact(slices.Sorted(slices.Values(vendors)))
	var updated []string
	for _, name := range vendors {
		s, _ := e.p.Skill(name)
		newRev, err := e.nextRev("vendor "+name, name, s.Repo, s.Path, s.Rev, rev)
		if err == nil && newRev != "" {
			err = e.edit(func(data []byte, ext string) ([]byte, error) {
				return manifest.SetVendorRev(data, ext, skenvfile.Project, name, newRev)
			})
		}
		switch {
		case err != nil:
			e.errorf("update vendor %s: %v", name, err)
		case newRev != "":
			e.changef("update vendor %s %.12s → %.12s in [project] of %s", name, s.Rev, newRev, e.show(e.file))
			updated = append(updated, fmt.Sprintf("%s to %.12s", name, newRev))
		}
	}
	slices.Sort(froms)
	for _, i := range froms {
		f := e.p.From[i]
		what := fmt.Sprintf("from %s (%s)", f.Repo, strings.Join(f.Skills, ", "))
		newRev, err := e.nextRev(what, f.Repo, f.Repo, f.SkillsDir, f.Rev, rev)
		if err == nil && newRev != "" {
			err = e.edit(func(data []byte, ext string) ([]byte, error) {
				return manifest.SetFromRev(data, ext, i, newRev)
			})
		}
		switch {
		case err != nil:
			e.errorf("update %s: %v", what, err)
		case newRev != "":
			e.changef("update %s %.12s → %.12s in [project] of %s", what, f.Rev, newRev, e.show(e.file))
			updated = append(updated, fmt.Sprintf("%s to %.12s", strings.Join(f.Skills, ", "), newRev))
		}
	}
	hint := ""
	if len(updated) > 0 {
		hint = "update " + strings.Join(updated, "; ")
	}
	return e.syncAfterEdit(hint)
}

// VendorRemove drops a [[project.vendor]] entry and syncs the project,
// which removes its copy and mirrors.
func (e *ProjectEngine) VendorRemove(name string) (int, error) {
	s, ok := e.p.Skill(name)
	if !ok {
		return ExitFatal, fmt.Errorf("project skill %q is not in [project] of %s", name, e.show(e.file))
	}
	if s.From != nil {
		return ExitFatal, fmt.Errorf("project skill %q comes from [[project.from]] %s; remove it from the skills of that entry "+
			"(or the entry) in %s, then run `skenv sync`", name, s.Repo, e.show(e.file))
	}
	if err := e.edit(func(data []byte, ext string) ([]byte, error) {
		return manifest.RemoveVendor(data, ext, skenvfile.Project, name)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("remove vendor %s from [project] of %s", name, e.show(e.file))
	return e.syncAfterEdit("remove vendor skill " + name)
}

// edit applies fn to the skenv file, validates the result, writes it
// (unless --dry-run) and reloads [project] from it.
func (e *ProjectEngine) edit(fn func(data []byte, ext string) ([]byte, error)) error {
	data, err := e.readSkenvFile(e.file)
	if err != nil {
		return err
	}
	ext := filepath.Ext(e.file)
	out, err := fn(data, ext)
	if err != nil {
		return err
	}
	if out, err = skenvfile.Stamp(out, ext, false); err != nil {
		return err
	}
	p, err := manifest.ParseProject(out, ext)
	if err != nil {
		return err
	}
	if err := e.writeSkenvFile(e.file, out); err != nil {
		return fmt.Errorf("write %s: %w", e.show(e.file), err)
	}
	e.p = p
	e.hosts = p.Hosts
	return nil
}

// syncAfterEdit syncs the whole project after an edit of [project].
func (e *ProjectEngine) syncAfterEdit(hint string) (int, error) {
	e.syncCopies()
	e.syncMirrors()
	if hint != "" {
		e.commitHint(hint)
	}
	return e.summary("vendor"), nil
}
