package engine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/state"
)

// isClaudeSynced reports whether p is ~/.claude/skills/synced (or the same
// under $CLAUDE_CONFIG_DIR), which skenv never touches (N1).
func (e *Engine) isClaudeSynced(p string) bool {
	for _, a := range agents.Table(e.env.Home, e.env.Getenv) {
		if a.Name == "claude" && p == filepath.Join(a.Skills, "synced") {
			return true
		}
	}
	return false
}

// owned reports whether p is recorded in the state and still is what skenv
// created there: a symlink for links, a directory with a .skenv marker for
// vendored skills. Anything else put there since is the user's (N1).
func (e *Engine) owned(p string) bool {
	entry, ok := e.st.Managed[p]
	if !ok {
		return false
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	switch entry.Kind {
	case state.Link:
		return fi.Mode()&fs.ModeSymlink != 0
	case state.VendorDir:
		return fi.IsDir() && fileExists(filepath.Join(p, markerName))
	}
	return false
}

// claim makes p available for a managed path. It returns false when p is an
// unmanaged path that may not be replaced (a conflict, reported as an
// error), and true when p is absent, managed, or was moved to the backup
// directory under --adopt.
func (e *Engine) claim(p, skill string) bool {
	if e.isClaudeSynced(p) {
		e.errorf("%s is managed by Claude and never touched by skenv", e.show(p))
		return false
	}
	if e.m.Layout.Ignored(filepath.Base(p)) {
		e.errorf("%s matches layout.ignore and is never touched by skenv", e.show(p))
		return false
	}
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) || e.owned(p) {
		return true
	}
	if !e.opts.Adopt {
		e.errorf("conflict: %s exists and is not managed by skenv (skill %q); inspect it, then rerun with --adopt to move it to %s and replace it",
			e.show(p), skill, e.show(e.layout.Backup()))
		return false
	}
	if err := e.backup(p); err != nil {
		e.errorf("adopt %s: %v", e.show(p), err)
		return false
	}
	return true
}

// backup moves p to backup/<ts>/<path relative to home>.
func (e *Engine) backup(p string) error {
	if e.backupDir == "" {
		e.backupDir = filepath.Join(e.layout.Backup(), e.env.Now().UTC().Format("20060102T150405Z"))
	}
	rel, err := filepath.Rel(e.env.Home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = strings.TrimPrefix(p, string(filepath.Separator))
	}
	dst := filepath.Join(e.backupDir, rel)
	e.changef("adopt %s (old content → %s)", e.show(p), e.show(dst))
	if e.opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(p, dst); err != nil {
		return fmt.Errorf("move to backup: %w", err)
	}
	return nil
}

// placeSymlink creates or replaces the managed symlink p → dest atomically
// (temporary name + rename, N2). The caller has claimed p.
func (e *Engine) placeSymlink(p, dest, skill string) error {
	fi, err := os.Lstat(p)
	if err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if cur, _ := os.Readlink(p); cur == dest {
			e.manage(p, state.Entry{Kind: state.Link, Skill: skill})
			return nil
		}
	}
	e.changef("link %s → %s", e.show(p), dest)
	if e.opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(p), fmt.Sprintf(".skenv-tmp-%s-%d", filepath.Base(p), os.Getpid()))
	_ = os.Remove(tmp)
	if err := os.Symlink(dest, tmp); err != nil {
		return err
	}
	if err := replace(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	e.manage(p, state.Entry{Kind: state.Link, Skill: skill})
	return nil
}

// replace renames src over dst. A directory at dst (a previously vendored
// skill) cannot be renamed over, so it is moved aside first and removed
// after the swap.
func replace(src, dst string) error {
	fi, err := os.Lstat(dst)
	if err != nil || fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return os.Rename(src, dst)
	}
	old := filepath.Join(filepath.Dir(dst), fmt.Sprintf(".skenv-old-%s-%d", filepath.Base(dst), os.Getpid()))
	_ = os.RemoveAll(old)
	if err := os.Rename(dst, old); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	return os.RemoveAll(old)
}

// removeManaged deletes a managed path after checking it still looks like
// something skenv created; otherwise it only forgets it.
func (e *Engine) removeManaged(p string, entry state.Entry) {
	if e.m.Layout.Ignored(filepath.Base(p)) {
		e.warnf("%s matches layout.ignore; leaving it in place and forgetting it", e.show(p))
		if !e.opts.DryRun {
			e.unmanage(p)
		}
		return
	}
	_, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		if !e.opts.DryRun {
			e.unmanage(p)
		}
		return
	}
	if err != nil {
		e.errorf("remove %s: %v", e.show(p), err)
		return
	}
	if !e.owned(p) {
		e.warnf("%s was recorded as managed but has been replaced by something else; leaving it in place and forgetting it", e.show(p))
		if !e.opts.DryRun {
			e.unmanage(p)
		}
		return
	}
	e.changef("remove %s (skill %q is no longer in the manifest)", e.show(p), entry.Skill)
	if e.opts.DryRun {
		return
	}
	if err := os.RemoveAll(p); err != nil {
		e.errorf("remove %s: %v", e.show(p), err)
		return
	}
	e.unmanage(p)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// copyTree copies the directory src to dst (which must not exist), keeping
// file modes and symlinks and skipping .git.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.Name() == ".git" && p != src {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(p, target, info.Mode().Perm())
		}
		return nil
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
