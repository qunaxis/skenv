package skenvfile

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/platform/docedit"
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
// The section is independent of [repository] and [user]: a user-level
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
	// GitHosts declares git servers by alias for the repo values of this
	// section, as user.git_hosts does for the manifest: the project
	// resolves them the same for everyone who clones it, without anyone's
	// manifest.
	GitHosts Hosts `toml:"git_hosts" yaml:"git_hosts" json:"git_hosts"`
	// Dependencies are third-party skills, each pinned to a commit and
	// copied into dir, keyed by the skill name.
	Dependencies map[string]Dependency `toml:"dependencies" yaml:"dependencies" json:"dependencies"`
	// From lists skills repositories (your own, for example) to copy some
	// of their skills from, each pinned to one commit, by an ID of your
	// choice.
	From map[string]From `toml:"from" yaml:"from" json:"from"`
}

// From names skills of a skills repository, copied into the project at a
// pinned commit.
type From struct {
	// Repo is the repository: "owner/repo" or "github:owner/repo" on
	// github.com, "gitlab:group/sub/repo", "codeberg:owner/repo",
	// "<alias>:path" of a host in project.git_hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// SkillsDir is the directory inside the repository whose
	// subdirectories are the skills. Default: "skills".
	SkillsDir string `toml:"skills_dir" yaml:"skills_dir" json:"skills_dir"`
	// Skills lists the skills to copy, by directory name under skills_dir
	// (no patterns). Required: every copy is committed to the project, so
	// each is named.
	Skills []string `toml:"skills" yaml:"skills" json:"skills"`
	// Commit is the full 40-character lowercase commit SHA to copy the
	// skills at.
	Commit string `toml:"commit" yaml:"commit" json:"commit"`

	// ID is the key of the entry in project.from.
	ID string `toml:"-" yaml:"-" json:"-"`
}

// ProjectSkill is one skill that [project] copies into dir, whichever
// entry it comes from.
type ProjectSkill struct {
	Name   string
	Repo   string
	Path   string // directory with SKILL.md inside the repository
	Commit string
	// Dependency or From is the entry.
	Dependency *Dependency
	From       *From
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
	doc, err := Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if !doc.Has(SectionProject) {
		return nil, ErrNoProject
	}
	var p Project
	if err := doc.Decode(SectionProject, &p); err != nil {
		return nil, err
	}
	if p.Dir == "" {
		p.Dir = DefaultProjectDir
	}
	if p.MirrorsMode == "" {
		p.MirrorsMode = MirrorSymlink
	}
	p.GitHosts.fillDefaults()
	defaultDependencies(p.Dependencies)
	for id, f := range p.From {
		f.ID = id
		if f.SkillsDir == "" {
			f.SkillsDir = DefaultSkillsDir
		}
		p.From[id] = f
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks what the schema cannot express (Parse checked the
// rest): repo values resolve, dir and mirrors do not overlap, and skill
// names are unique across vendor and from entries.
func (p *Project) Validate() error {
	var errs []error
	dirs := []string{p.Dir}
	for _, m := range p.Mirrors {
		for _, d := range dirs {
			if overlaps(d, m) {
				errs = append(errs, fmt.Errorf("project.mirrors: %q overlaps %q; dir and mirrors are separate directories", m, d))
			}
		}
		dirs = append(dirs, m)
	}
	for _, err := range p.GitHosts.validate() {
		errs = append(errs, fmt.Errorf("project.%w", err))
	}
	for _, d := range p.DependencyList() {
		errs = append(errs, checkDependency("project.dependencies", d, p.GitHosts, SectionProject, "")...)
	}
	for _, f := range p.FromList() {
		if _, err := p.GitHosts.ResolveIn(SectionProject, "", f.Repo); err != nil {
			errs = append(errs, fmt.Errorf("project.from.%s (%s): %w", f.ID, f.Repo, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	origin := map[string]string{}
	for _, s := range p.Skills() {
		from := "dependency " + s.Name + " (" + s.Repo + ")"
		if s.From != nil {
			from = "from." + s.From.ID + " (" + s.Repo + ")"
		}
		if prev, ok := origin[s.Name]; ok {
			errs = append(errs, fmt.Errorf("project skill %q is defined twice: %s and %s", s.Name, prev, from))
		}
		origin[s.Name] = from
	}
	return errors.Join(errs...)
}

// checkDependency checks that the repo of one dependency of [user] or
// [project] resolves (in section, naming the git_hosts section too; a
// local path against base).
func checkDependency(where string, d *Dependency, hosts Hosts, section, base string) []error {
	if _, err := hosts.ResolveIn(section, base, d.Repo); err != nil {
		return []error{fmt.Errorf("%s.%s: %w", where, docedit.QuoteKey(d.Name), err)}
	}
	return nil
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

// DependencyList returns the dependencies sorted by name.
func (p *Project) DependencyList() []*Dependency { return sortedValues(p.Dependencies) }

// FromList returns the from entries sorted by ID.
func (p *Project) FromList() []*From { return sortedValues(p.From) }

// Skills lists every skill the section copies, sorted by name. A name
// defined twice appears twice; Validate reports it.
func (p *Project) Skills() []ProjectSkill {
	var out []ProjectSkill
	for _, d := range p.DependencyList() {
		out = append(out, ProjectSkill{Name: d.Name, Repo: d.Repo, Path: d.SkillDir, Commit: d.Commit, Dependency: d})
	}
	for _, f := range p.FromList() {
		for _, n := range f.Skills {
			out = append(out, ProjectSkill{Name: n, Repo: f.Repo, Path: path.Join(f.SkillsDir, n), Commit: f.Commit, From: f})
		}
	}
	slices.SortStableFunc(out, func(a, b ProjectSkill) int { return strings.Compare(a.Name, b.Name) })
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
