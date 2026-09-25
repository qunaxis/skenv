package manifest

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/qunaxis/skenv/internal/skenvfile"
)

// Defaults of the [project] section.
const (
	DefaultProjectDir = ".agents/skills"
	MirrorSymlink     = "symlink"
	MirrorCopy        = "copy"
)

// MirrorModes are the values of project.mirrors_mode, the default first.
var MirrorModes = []string{MirrorSymlink, MirrorCopy}

// Project is the [project] section of a skenv file: the skills a project
// repository carries, committed with it, so that everyone who clones it
// (and CI, and cloud agents) gets them. `skenv sync` inside the repository
// copies the pinned skills into dir and keeps the mirrors in line.
//
// The section is independent of [repo] and [environment]: a user-level
// sync never reads it. Paths are relative to the repository root.
type Project struct {
	// Dir is the directory, relative to the repository root, that holds
	// the project's skills: the copies skenv makes and the skills authored
	// there, which skenv never changes. Default: ".agents/skills".
	Dir string `toml:"dir" yaml:"dir" json:"dir"`
	// Mirrors are other agent directories, relative to the repository
	// root, that get every skill of dir, for example ".claude/skills" for
	// Claude Code. Default: none.
	Mirrors []string `toml:"mirrors" yaml:"mirrors" json:"mirrors"`
	// MirrorsMode is how a mirror gets a skill: "symlink" makes
	// <mirror>/<name> a relative symlink to <dir>/<name>, "copy" a full
	// copy for tools that do not follow symlinks (or Windows checkouts).
	// Default: "symlink".
	MirrorsMode string `toml:"mirrors_mode" yaml:"mirrors_mode" json:"mirrors_mode"`
	// Hosts declares git servers by alias for the repo values of this
	// section, as environment.hosts does for the manifest: the project
	// resolves them the same for everyone who clones it, without anyone's
	// manifest.
	Hosts Hosts `toml:"hosts" yaml:"hosts" json:"hosts"`
	// Vendor lists third-party skills, each pinned to a commit and copied
	// into dir.
	Vendor []Vendor `toml:"vendor" yaml:"vendor" json:"vendor"`
	// From lists skills repositories (your own, for example) to copy some
	// of their skills from, each pinned to a commit.
	From []From `toml:"from" yaml:"from" json:"from"`
}

// From names skills of a skills repository, copied into the project at a
// pinned commit.
type From struct {
	// Repo is the repository: "owner/repo" on github.com,
	// "gitlab:group/sub/repo", "codeberg:owner/repo", "<alias>:path" of a
	// host in project.hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// SkillsDir is the directory inside the repository whose
	// subdirectories are the skills. Default: "skills".
	SkillsDir string `toml:"skills_dir" yaml:"skills_dir" json:"skills_dir"`
	// Skills lists the skills to copy, by directory name under skills_dir.
	// Required: every copy is committed to the project, so each is named.
	Skills []string `toml:"skills" yaml:"skills" json:"skills"`
	// Rev is the full 40-character lowercase commit SHA to copy the skills
	// at.
	Rev string `toml:"rev" yaml:"rev" json:"rev"`
}

// ProjectSkill is one skill that [project] copies into dir, whichever
// entry it comes from.
type ProjectSkill struct {
	Name string
	Repo string
	Path string // directory with SKILL.md inside the repository
	Rev  string
	// Vendor or From is the entry, From with the index of the entry.
	Vendor *Vendor
	From   *From
	FromAt int
}

// LoadProject reads and validates the [project] section of the skenv file
// at file.
func LoadProject(file string) (*Project, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	p, err := ParseProject(data, filepath.Ext(file))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return p, nil
}

// ErrNoProject means the skenv file has no [project] section.
var ErrNoProject = errors.New("no [project] section")

// ParseProject decodes and validates the [project] section of a skenv file
// in the format of ext.
func ParseProject(data []byte, ext string) (*Project, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if !doc.Has(skenvfile.Project) {
		return nil, ErrNoProject
	}
	var p Project
	if err := doc.Decode(skenvfile.Project, &p); err != nil {
		return nil, err
	}
	if p.Dir == "" {
		p.Dir = DefaultProjectDir
	}
	if p.MirrorsMode == "" {
		p.MirrorsMode = MirrorSymlink
	}
	p.Hosts.fillDefaults()
	for i := range p.Vendor {
		if p.Vendor[i].Path == "" {
			p.Vendor[i].Path = "."
		}
	}
	for i := range p.From {
		if p.From[i].SkillsDir == "" {
			p.From[i].SkillsDir = DefaultSkillsDir
		}
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks the section: clean relative directories that do not
// overlap, full SHAs, valid skill names that are unique across vendor and
// from entries.
func (p *Project) Validate() error {
	var errs []error
	if !cleanRel(p.Dir) || p.Dir == "." {
		errs = append(errs, fmt.Errorf("project.dir %q must be a relative directory inside the repository", p.Dir))
	}
	dirs := []string{p.Dir}
	for _, m := range p.Mirrors {
		if !cleanRel(m) || m == "." {
			errs = append(errs, fmt.Errorf("project.mirrors: %q must be a relative directory inside the repository", m))
			continue
		}
		for _, d := range dirs {
			if overlaps(d, m) {
				errs = append(errs, fmt.Errorf("project.mirrors: %q overlaps %q; dir and mirrors are separate directories", m, d))
			}
		}
		dirs = append(dirs, m)
	}
	for _, err := range p.Hosts.validate() {
		errs = append(errs, fmt.Errorf("project.%w", err))
	}
	if !slices.Contains(MirrorModes, p.MirrorsMode) {
		errs = append(errs, fmt.Errorf("project.mirrors_mode %q must be %s", p.MirrorsMode, strings.Join(quoted(MirrorModes), " or ")))
	}
	for i, v := range p.Vendor {
		where := fmt.Sprintf("project.vendor[%d]", i)
		if v.Name != "" {
			where = fmt.Sprintf("project.vendor %q", v.Name)
		}
		for _, err := range checkVendor(where, v, p.Hosts) {
			errs = append(errs, projectHosts(err))
		}
	}
	for i, f := range p.From {
		where := fmt.Sprintf("project.from[%d] (%s)", i, f.Repo)
		if f.Repo == "" {
			errs = append(errs, fmt.Errorf("project.from[%d]: repo is required", i))
		} else if _, err := p.Hosts.Resolve(f.Repo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, projectHosts(err)))
		}
		if !cleanRel(f.SkillsDir) {
			errs = append(errs, fmt.Errorf("%s: skills_dir %q must be a relative path inside the repository", where, f.SkillsDir))
		}
		if len(f.Skills) == 0 {
			errs = append(errs, fmt.Errorf("%s: skills is required; list the skills to copy", where))
		}
		seen := map[string]bool{}
		for _, n := range f.Skills {
			if err := ValidName(n); err != nil {
				errs = append(errs, fmt.Errorf("%s: skills: %w", where, err))
			} else if seen[n] {
				errs = append(errs, fmt.Errorf("%s: skills lists %q twice", where, n))
			}
			seen[n] = true
		}
		if !shaRe.MatchString(f.Rev) {
			errs = append(errs, fmt.Errorf("%s: rev %q must be a full 40-character lowercase commit SHA", where, f.Rev))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	origin := map[string]string{}
	for _, s := range p.Skills() {
		from := "vendor " + s.Repo
		if s.From != nil {
			from = "from " + s.Repo
		}
		if prev, ok := origin[s.Name]; ok {
			errs = append(errs, fmt.Errorf("project skill %q is defined twice: %s and %s", s.Name, prev, from))
		}
		origin[s.Name] = from
	}
	return errors.Join(errs...)
}

// projectHosts points an error of Hosts.Resolve at project.hosts, where a
// project declares its hosts.
func projectHosts(err error) error {
	msg := strings.ReplaceAll(err.Error(), "[environment.hosts.", "[project.hosts.")
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

// checkVendor validates one vendor entry of [environment] or [project]:
// the name, a repo that hosts resolve, the path and a full SHA.
func checkVendor(where string, v Vendor, hosts Hosts) []error {
	var errs []error
	if err := ValidName(v.Name); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", where, err))
	}
	if v.Repo == "" {
		errs = append(errs, fmt.Errorf("%s: repo is required", where))
	} else if _, err := hosts.Resolve(v.Repo); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", where, err))
	}
	if !cleanRel(v.Path) {
		errs = append(errs, fmt.Errorf("%s: path %q must be a relative path inside the repository (\".\" for the root)", where, v.Path))
	}
	if !shaRe.MatchString(v.Rev) {
		errs = append(errs, fmt.Errorf("%s: rev %q must be a full 40-character lowercase commit SHA", where, v.Rev))
	}
	return errs
}

// overlaps reports whether one of the clean relative paths a and b is
// the other or inside it.
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func quoted(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// Skills lists every skill the section copies, sorted by name. A name
// defined twice appears twice; Validate reports it.
func (p *Project) Skills() []ProjectSkill {
	var out []ProjectSkill
	for i := range p.Vendor {
		v := &p.Vendor[i]
		out = append(out, ProjectSkill{Name: v.Name, Repo: v.Repo, Path: v.Path, Rev: v.Rev, Vendor: v})
	}
	for i := range p.From {
		f := &p.From[i]
		for _, n := range f.Skills {
			out = append(out, ProjectSkill{Name: n, Repo: f.Repo, Path: path.Join(f.SkillsDir, n), Rev: f.Rev, From: f, FromAt: i})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// Skill returns the skill name of the section.
func (p *Project) Skill(name string) (ProjectSkill, bool) {
	for _, s := range p.Skills() {
		if s.Name == name {
			return s, true
		}
	}
	return ProjectSkill{}, false
}
