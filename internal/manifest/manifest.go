// Package manifest parses and validates the manifest: the [user] section
// of a skenv file (skenv.toml), the declarative set of skills that skenv
// keeps in sync for the agents of the current OS user; and the [project]
// section, the skills a project repository carries.
package manifest

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/paths"
	"github.com/qunaxis/skenv/internal/skenvfile"
	"github.com/qunaxis/skenv/internal/skillname"
)

// DefaultSkillsDir is used when a checkout does not set skills_dir.
const DefaultSkillsDir = "skills"

// Manifest is the [user] section of the skenv file: the skills made
// available to the agents of the current OS user, across projects, on every
// machine that runs `skenv sync`. Local paths may start with "~"; relative
// ones resolve against the directory of the skenv file.
//
// Paths are kept as written; callers resolve them with Path. The first
// paragraph of the doc comments of these types and the comments of their
// fields are the descriptions of the JSON Schema (`make schemas`): write
// them for users.
type Manifest struct {
	// Checkouts are git repositories kept as editable working copies, by an
	// ID of your choice: every selected skill directory in them is linked,
	// and edits show up immediately.
	Checkouts map[string]Checkout `toml:"checkouts" yaml:"checkouts" json:"checkouts"`
	// Dependencies are single skills pinned to a commit and copied into the
	// store, keyed by the installed skill name. Skill names are unique
	// across checkouts and dependencies.
	Dependencies map[string]Dependency `toml:"dependencies" yaml:"dependencies" json:"dependencies"`
	// Machines holds rules for one machine each, keyed by the machine name:
	// $SKENV_MACHINE, else the tool config `machine`, else the full
	// hostname, else the short one (never both).
	Machines map[string]Machine `toml:"machines" yaml:"machines" json:"machines"`
	// Agents chooses the agent directories that get a link per skill.
	Agents Agents `toml:"agents" yaml:"agents" json:"agents"`
	// Storage is where skenv keeps the skills it installs.
	Storage Storage `toml:"storage" yaml:"storage" json:"storage"`
	// Unmanaged lists glob patterns over entry names (no "/") in the store
	// and the agent directories that belong to other tools: doctor does not
	// report them and sync never touches them, not even with --adopt. A
	// selected skill must not match a pattern.
	Unmanaged []string `toml:"unmanaged" yaml:"unmanaged" json:"unmanaged"`
	// GitHosts declares git servers by alias, usually self-hosted ones:
	// "<alias>:group/repo" in repo is a repository on that server. They
	// live in the manifest so it resolves the same on every machine.
	GitHosts Hosts `toml:"git_hosts" yaml:"git_hosts" json:"git_hosts"`

	// Dir is the directory of the skenv file: relative local paths resolve
	// against it. Load sets it.
	Dir string `toml:"-" yaml:"-" json:"-"`
}

// Checkout is a git repository kept as an editable working copy, so that
// edits show up immediately. Ownership does not matter: your repository, a
// fork or a team repository.
type Checkout struct {
	// Repo is the repository to clone: "owner/repo" or "github:owner/repo"
	// on github.com, "gitlab:group/sub/repo", "codeberg:owner/repo",
	// "<alias>:path" of a host in git_hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// CheckoutDir is where the working copy lives: "~/..." in the home
	// directory, a relative path against the directory of this file ("."
	// is the repository that holds the manifest). Point it at the existing
	// clone, or sync clones a second one there.
	CheckoutDir string `toml:"checkout_dir" yaml:"checkout_dir" json:"checkout_dir"`
	// SkillsDir is the directory inside the repository whose
	// subdirectories with a SKILL.md are the skills. Default: "skills".
	SkillsDir string `toml:"skills_dir" yaml:"skills_dir" json:"skills_dir"`
	// Branch is the branch sync keeps the working copy on: it is cloned
	// with it and fast-forwarded from origin while the working copy is
	// clean and on it. Default: the default branch of the remote. Sync
	// never switches branches: a working copy on another branch is local
	// development state, linked as it is and not updated.
	Branch string `toml:"branch" yaml:"branch" json:"branch"`
	// Include lists skill names and glob patterns over names (no "/") of
	// the skills to install. Omitted: every skill, including ones added
	// later; [] selects none. A name without a pattern that is not a skill
	// of the repository is an error.
	Include []string `toml:"include" yaml:"include" json:"include"`
	// Exclude lists skill names and glob patterns over names (no "/") that
	// are not installed, applied after include; exclude wins. A pattern
	// that matches nothing is fine.
	Exclude []string `toml:"exclude" yaml:"exclude" json:"exclude"`

	// ID is the key of the checkout in checkouts.
	ID string `toml:"-" yaml:"-" json:"-"`
}

// Dependency is a single skill pinned to a commit; `skenv vendor
// add|update|remove` edit these entries (with --project, the ones of
// [project]). Its key is the installed skill name: 1 to 64 lowercase
// letters, digits and single hyphens, with no hyphen at the start or end;
// "synced" is reserved.
type Dependency struct {
	// Repo is the repository: "owner/repo" or "github:owner/repo" on
	// github.com, "gitlab:group/sub/repo", "codeberg:owner/repo",
	// "<alias>:path" of a host in git_hosts, or a full git URL.
	Repo string `toml:"repo" yaml:"repo" json:"repo"`
	// SkillDir is the directory with SKILL.md inside the repository,
	// relative to its root; "." for the root. Default: ".".
	SkillDir string `toml:"skill_dir" yaml:"skill_dir" json:"skill_dir"`
	// Commit is the full 40-character lowercase commit SHA. Branches, tags
	// and short SHAs are rejected: a dependency changes only when you
	// update it.
	Commit string `toml:"commit" yaml:"commit" json:"commit"`

	// Name is the key of the dependency: the skill name.
	Name string `toml:"-" yaml:"-" json:"-"`
}

// Machine holds the rules of one machine. They only narrow the skills the
// sources select: a machine cannot install a skill its checkout excludes.
type Machine struct {
	// Include lists skill names and glob patterns over names (no "/"): on
	// this machine only matching skills are installed. Omitted: all; []
	// selects none.
	Include []string `toml:"include" yaml:"include" json:"include"`
	// Exclude lists skill names and glob patterns over names (no "/") that
	// are not installed on this machine, applied after include.
	Exclude []string `toml:"exclude" yaml:"exclude" json:"exclude"`
	// CheckoutDirs overrides checkout_dir on this machine, by checkout ID.
	CheckoutDirs map[string]string `toml:"checkout_dirs" yaml:"checkout_dirs" json:"checkout_dirs"`
}

// Agents chooses the agent directories that get a link per skill. The
// built-in agents are "claude" ($CLAUDE_CONFIG_DIR/skills, else
// ~/.claude/skills) and "pi" (~/.pi/agent/skills). Codex and other agents
// that follow the ~/.agents/skills convention read the store directly.
type Agents struct {
	// Enabled lists the built-in agents to link skills for, whether or not
	// they are installed. Omitted: each built-in agent whose base directory
	// (~/.claude, ~/.pi/agent) exists; []: none.
	Enabled []string `toml:"enabled" yaml:"enabled" json:"enabled"`
	// Paths overrides the skills directory of a built-in agent, by name;
	// the other agents keep theirs.
	Paths map[string]string `toml:"paths" yaml:"paths" json:"paths"`
	// ExtraDirs are more directories that get a link per skill, added to
	// the agents, never replacing them.
	ExtraDirs []string `toml:"extra_dirs" yaml:"extra_dirs" json:"extra_dirs"`
}

// Storage is where skenv keeps the skills it installs.
type Storage struct {
	// Dir is the store: links to the skills of checkouts, copies of
	// dependencies. Codex reads the default one directly. Default:
	// "~/.agents/skills".
	Dir string `toml:"dir" yaml:"dir" json:"dir"`
}

var (
	shaRe = regexp.MustCompile(RevPattern)
	idRe  = regexp.MustCompile(IDPattern)

	bareKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	branchRe  = regexp.MustCompile(BranchPattern)
)

// RevPattern is a full lowercase commit SHA (M2).
const RevPattern = `^[0-9a-f]{40}$`

// BranchPattern is the rule for checkouts.<id>.branch: a git branch name
// without "..", "@{", a leading "-" or "/" and a trailing "/" or ".lock".
const BranchPattern = `^[A-Za-z0-9_][A-Za-z0-9._/-]*$`

// IDPattern is the rule for the ID of a checkout or a [project.from]
// entry.
const IDPattern = `^[a-z0-9][a-z0-9_-]{0,63}$`

// Reserved is the skill name that skenv never uses:
// ~/.claude/skills/synced is managed by Claude.
const Reserved = "synced"

// Locate returns the skenv file that path names: path itself, or the skenv
// file in path when it is a directory.
func Locate(path string) (string, error) {
	if filepath.Base(path) == "env.toml" {
		return "", skenvfile.OldManifestError(path)
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return path, nil //nolint:nilerr // Load reports a missing file
	}
	file, err := skenvfile.Find(path)
	if err != nil {
		return "", err
	}
	if file == "" {
		return "", fmt.Errorf("no skenv file (%s) in %s", strings.Join(skenvfile.Names, ", "), path)
	}
	return file, nil
}

// Load reads and validates the manifest in the skenv file at file.
func Load(file string) (*Manifest, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w (set --manifest, $SKENV_MANIFEST or run `skenv use <path>`)", file, err)
	}
	m, err := ParseIn(data, filepath.Ext(file), filepath.Dir(file))
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", file, err)
	}
	return m, nil
}

// Parse decodes and validates the [user] section of a skenv file in the
// format of ext (".toml", ".yaml", ".yml", ".json"). Relative paths resolve
// against the working directory; ParseIn names the file's directory.
func Parse(data []byte, ext string) (*Manifest, error) { return ParseIn(data, ext, "") }

// ParseIn is Parse for a skenv file in dir.
func ParseIn(data []byte, ext, dir string) (*Manifest, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if !doc.Has(skenvfile.User) {
		return nil, errors.New("no [user] section: this skenv file is not a manifest")
	}
	var m Manifest
	if err := doc.Decode(skenvfile.User, &m); err != nil {
		return nil, err
	}
	m.Dir = dir
	u := skenvfile.User
	if doc.IsDefined(u, "agents", "enabled") && m.Agents.Enabled == nil {
		m.Agents.Enabled = []string{}
	}
	for id, c := range m.Checkouts {
		c.ID = id
		if c.SkillsDir == "" {
			c.SkillsDir = DefaultSkillsDir
		}
		if doc.IsDefined(u, "checkouts", id, "include") && c.Include == nil {
			c.Include = []string{}
		}
		m.Checkouts[id] = c
	}
	for name, mc := range m.Machines {
		if doc.IsDefined(u, "machines", name, "include") && mc.Include == nil {
			mc.Include = []string{}
			m.Machines[name] = mc
		}
	}
	m.GitHosts.fillDefaults()
	for name, d := range m.Dependencies {
		d.Name = name
		if d.SkillDir == "" {
			d.SkillDir = "."
		}
		m.Dependencies[name] = d
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks the rules that do not need the file system: required
// fields, full SHAs (M2), IDs, patterns and agent names. Name clashes
// between checkouts and dependencies are checked by CheckNames once the
// checkouts are listed.
func (m *Manifest) Validate() error {
	var errs []error
	errs = append(errs, checkGlobs("user.unmanaged", m.Unmanaged)...)
	errs = append(errs, m.GitHosts.validate()...)
	for _, c := range m.CheckoutList() {
		where := "user.checkouts." + c.ID
		if !idRe.MatchString(c.ID) {
			errs = append(errs, fmt.Errorf("%s: the ID must be lowercase letters, digits, \"-\" and \"_\", starting with a letter or digit", where))
		}
		if c.Repo == "" {
			errs = append(errs, fmt.Errorf("%s: repo is required", where))
		} else if _, err := m.Remote(c.Repo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
		}
		if c.CheckoutDir == "" {
			errs = append(errs, fmt.Errorf("%s: checkout_dir is required", where))
		}
		if !cleanRel(c.SkillsDir) {
			errs = append(errs, fmt.Errorf("%s: skills_dir %q must be a relative path inside the repository", where, c.SkillsDir))
		}
		if c.Branch != "" && (!branchRe.MatchString(c.Branch) || strings.Contains(c.Branch, "..") || strings.Contains(c.Branch, "//") ||
			strings.HasSuffix(c.Branch, "/") || strings.HasSuffix(c.Branch, ".lock") || strings.HasSuffix(c.Branch, ".")) {
			errs = append(errs, fmt.Errorf("%s: branch %q is not a branch name", where, c.Branch))
		}
		errs = append(errs, checkSelection(where, c.Include, c.Exclude)...)
	}
	for _, d := range m.DependencyList() {
		errs = append(errs, checkDependency("user.dependencies", d, m.GitHosts, m.Dir)...)
	}
	for _, name := range slices.Sorted(maps.Keys(m.Machines)) {
		mc := m.Machines[name]
		where := fmt.Sprintf("user.machines.%s", quoteKey(name))
		errs = append(errs, checkSelection(where, mc.Include, mc.Exclude)...)
		for _, id := range slices.Sorted(maps.Keys(mc.CheckoutDirs)) {
			if _, ok := m.Checkouts[id]; !ok {
				errs = append(errs, fmt.Errorf("%s.checkout_dirs: %q is not a checkout ID (known: %s)", where, id, strings.Join(slices.Sorted(maps.Keys(m.Checkouts)), ", ")))
			} else if mc.CheckoutDirs[id] == "" {
				errs = append(errs, fmt.Errorf("%s.checkout_dirs.%s is empty", where, id))
			}
		}
	}
	errs = append(errs, m.Agents.validate()...)
	return errors.Join(errs...)
}

func (a Agents) validate() []error {
	var errs []error
	check := func(where, name string) {
		switch {
		case name == "codex":
			errs = append(errs, fmt.Errorf("%s: codex is not a link destination: Codex reads the store (user.storage.dir, default ~/.agents/skills) directly; "+
				"to keep skills away from it, move storage.dir elsewhere, and add ~/.agents/skills to extra_dirs to give it links", where))
		case !slices.Contains(agents.Names, name):
			errs = append(errs, fmt.Errorf("%s: unknown agent %q (built in: %s; add other directories to user.agents.extra_dirs)", where, name, strings.Join(agents.Names, ", ")))
		}
	}
	seen := map[string]bool{}
	for _, n := range a.Enabled {
		check("user.agents.enabled", n)
		if seen[n] {
			errs = append(errs, fmt.Errorf("user.agents.enabled lists %q twice", n))
		}
		seen[n] = true
	}
	for _, n := range slices.Sorted(maps.Keys(a.Paths)) {
		check("user.agents.paths", n)
		if a.Paths[n] == "" {
			errs = append(errs, fmt.Errorf("user.agents.paths.%s is empty", n))
		}
	}
	for _, d := range a.ExtraDirs {
		if d == "" {
			errs = append(errs, errors.New("user.agents.extra_dirs: an entry is empty"))
		}
	}
	return errs
}

// checkSelection validates an include/exclude pair.
func checkSelection(where string, include, exclude []string) []error {
	errs := checkPatterns(where+".include", include)
	errs = append(errs, checkPatterns(where+".exclude", exclude)...)
	seen := map[string]bool{}
	for _, n := range include {
		if seen[n] {
			errs = append(errs, fmt.Errorf("%s.include lists %q twice", where, n))
		}
		seen[n] = true
	}
	return errs
}

// checkGlobs validates glob patterns over entry names: any name, since
// they match paths of other tools too.
func checkGlobs(where string, pats []string) []error {
	var errs []error
	for _, pat := range pats {
		if _, err := path.Match(pat, ""); err != nil || pat == "" || strings.Contains(pat, "/") {
			errs = append(errs, fmt.Errorf("%s: %q must be a glob over entry names (no \"/\")", where, pat))
		}
	}
	return errs
}

// checkPatterns validates skill names and glob patterns over names.
func checkPatterns(where string, pats []string) []error {
	var errs []error
	for _, pat := range pats {
		if _, err := path.Match(pat, ""); err != nil || pat == "" || strings.Contains(pat, "/") {
			errs = append(errs, fmt.Errorf("%s: %q must be a skill name or a glob over names (no \"/\")", where, pat))
		} else if !IsPattern(pat) {
			if err := ValidName(pat); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", where, err))
			}
		}
	}
	return errs
}

// IsPattern reports whether s has glob metacharacters; otherwise it is a
// literal name.
func IsPattern(s string) bool { return strings.ContainsAny(s, `*?[\`) }

// matchAny reports whether name matches one of pats.
func matchAny(pats []string, name string) bool {
	for _, pat := range pats {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

// Selected reports whether include (nil: all) and exclude select name.
func Selected(include, exclude []string, name string) bool {
	return (include == nil || matchAny(include, name)) && !matchAny(exclude, name)
}

// IsUnmanaged reports whether an entry name matches user.unmanaged.
func (m *Manifest) IsUnmanaged(name string) bool { return matchAny(m.Unmanaged, name) }

// Select returns the names in found (the skills of the repository) that c
// installs: include (all when omitted), minus exclude. A literal name in
// include that is not in found is an error.
func (c *Checkout) Select(found []string, dir string) ([]string, error) {
	var missing []string
	for _, n := range c.Include {
		if !IsPattern(n) && !slices.Contains(found, n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("user.checkouts.%s: include lists %s, not found in %s (a directory with SKILL.md)",
			c.ID, quoteAll(missing), dir)
	}
	var out []string
	for _, n := range found {
		if c.Selects(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// Selects reports whether c installs the skill name of its repository.
func (c *Checkout) Selects(name string) bool { return Selected(c.Include, c.Exclude, name) }

// Selects reports whether the machine rules keep the skill name.
func (mc Machine) Selects(name string) bool { return Selected(mc.Include, mc.Exclude, name) }

func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = strconv.Quote(n)
	}
	return strings.Join(q, ", ")
}

// quoteKey writes a table key as TOML needs it in a dotted path.
func quoteKey(k string) string {
	if bareKeyRe.MatchString(k) {
		return k
	}
	return strconv.Quote(k)
}

// ValidName reports whether name can be used as a skill name: the shared
// rule of package skillname, and not Reserved.
func ValidName(name string) error {
	if err := skillname.Check(name); err != nil {
		return err
	}
	if name == Reserved {
		return fmt.Errorf("name %q is reserved (~/.claude/skills/%s is managed by Claude)", Reserved, Reserved)
	}
	return nil
}

func cleanRel(p string) bool {
	if p == "" || path.IsAbs(p) {
		return false
	}
	c := path.Clean(p)
	return c == p && c != ".." && !strings.HasPrefix(c, "../")
}

// CheckoutList returns the checkouts sorted by ID.
func (m *Manifest) CheckoutList() []*Checkout {
	out := make([]*Checkout, 0, len(m.Checkouts))
	for _, id := range slices.Sorted(maps.Keys(m.Checkouts)) {
		c := m.Checkouts[id]
		out = append(out, &c)
	}
	return out
}

// DependencyList returns the dependencies sorted by name.
func (m *Manifest) DependencyList() []*Dependency {
	out := make([]*Dependency, 0, len(m.Dependencies))
	for _, name := range slices.Sorted(maps.Keys(m.Dependencies)) {
		d := m.Dependencies[name]
		out = append(out, &d)
	}
	return out
}

// Remote resolves a repo value of the manifest: a relative local path
// against the directory of the skenv file.
func (m *Manifest) Remote(repo string) (Remote, error) { return m.GitHosts.ResolveIn(m.Dir, repo) }

// SkillRef names one skill and where it comes from.
type SkillRef struct {
	Name       string
	Checkout   *Checkout   // set for skills of a checkout
	Dependency *Dependency // set for dependencies
}

// CheckNames enforces M1 across checkouts and dependencies: selected maps
// each checkout ID to the skill names it selects.
func (m *Manifest) CheckNames(selected map[string][]string) ([]SkillRef, error) {
	origin := map[string]string{}
	var refs []SkillRef
	var errs []error
	add := func(name, from string, ref SkillRef) {
		if prev, ok := origin[name]; ok {
			errs = append(errs, fmt.Errorf("skill %q is defined twice: %s and %s", name, prev, from))
			return
		}
		origin[name] = from
		refs = append(refs, ref)
	}
	for _, c := range m.CheckoutList() {
		for _, name := range selected[c.ID] {
			add(name, "checkout "+c.ID, SkillRef{Name: name, Checkout: c})
		}
	}
	for _, d := range m.DependencyList() {
		add(d.Name, "dependency "+d.Name+" ("+d.Repo+")", SkillRef{Name: d.Name, Dependency: d})
	}
	for _, r := range refs {
		if m.IsUnmanaged(r.Name) {
			errs = append(errs, fmt.Errorf("skill %q matches user.unmanaged; rename it or change the pattern", r.Name))
		}
	}
	sort.Slice(refs, func(a, b int) bool { return refs[a].Name < refs[b].Name })
	return refs, errors.Join(errs...)
}

// Path resolves a local path written in the manifest: "~" against home,
// a relative path against the directory of the skenv file.
func (m *Manifest) Path(home, p string) string { return paths.Resolve(home, m.Dir, p) }
