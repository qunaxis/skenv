package skenvfile

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The [repository] section: the development tooling of a skills
// repository. Parsing and validation live here, with the rest of the
// file model, so that every feature can read the section; generating the
// files it asks for is internal/features/repository.

// DefaultName is the skenv file that `repo init` creates when the
// repository has none and --format is not given; errors about a file not
// read from disk name it too.
const DefaultName = "skenv.toml"

// LatestTemplates is the template version of the templates that
// internal/features/repository embeds; `repo init` and `repo upgrade`
// write it as repository.template_version, and `repo apply` generates it
// only. The CI workflow installs this skenv release. It lives in the file
// model because validation refuses a template_version newer than it.
const LatestTemplates = "0.6.0"

// DefaultRunner is the runner of private repositories (the self-hosted
// Docker runner): the runs-on labels on GitHub, the tags on GitLab.
var DefaultRunner = []string{"self-hosted", "linux", "docker"}

// runnerRe is a runner label or tag that the CI templates can write
// unquoted into a YAML flow list.
var runnerRe = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

// CI systems, the tables of repository.ci.
const (
	CIGitHub = "github"
	CIGitLab = "gitlab"
)

// CIs lists the tables of repository.ci.
var CIs = []string{CIGitHub, CIGitLab}

// Repository is the [repository] section of the skenv file: the development
// tooling of a skills repository that `skenv repo apply` generates, and its
// publication policy.
//
// The first paragraph of this comment and the comments of the fields are
// the descriptions of the JSON Schema (`make schemas`): write them for
// users.
type Repository struct {
	// TemplateVersion is the version of the templates this repository
	// asks for, which is also the skenv release its generated CI installs.
	// It must not be newer than the templates of the skenv that reads it.
	TemplateVersion string `toml:"template_version" yaml:"template_version" json:"template_version"`
	// Visibility is the declared publication policy: "private" or
	// "public". skenv never reads or changes the access setting on the
	// hosting service. A public repository must not carry [user], and its
	// CI runs `skenv lint --publish` on the runners of the host
	// (ubuntu-latest on GitHub, the shared runners on GitLab).
	Visibility string `toml:"visibility" yaml:"visibility" json:"visibility"`
	// CI is the CI system the templates generate for, by the table
	// present: github (.github/workflows/check.yml) or gitlab
	// (.gitlab-ci.yml), at most one. Without either: github. Switching it
	// makes `skenv repo apply` remove the managed file of the other one.
	CI CIConfig `toml:"ci" yaml:"ci" json:"ci"`

	// Provider is the CI system in use and Runner its runs-on labels or
	// tags, after defaults.
	Provider string   `toml:"-" yaml:"-" json:"-"`
	Runner   []string `toml:"-" yaml:"-" json:"-"`
	// File is the skenv file; HasUser reports whether it also carries the
	// [user] section (a manifest).
	File    string `toml:"-" yaml:"-" json:"-"`
	HasUser bool   `toml:"-" yaml:"-" json:"-"`
	// tables are the tables present under repository.ci.
	tables []string
}

// CIConfig chooses the CI system: one of its tables.
type CIConfig struct {
	// GitHub is GitHub Actions.
	GitHub GitHubCI `toml:"github" yaml:"github" json:"github"`
	// GitLab is GitLab CI.
	GitLab GitLabCI `toml:"gitlab" yaml:"gitlab" json:"gitlab"`
}

// GitHubCI is GitHub Actions (.github/workflows/check.yml).
type GitHubCI struct {
	// RunsOn are the runs-on labels of the jobs of a private repository.
	// Default: ["self-hosted", "linux", "docker"]; use ["ubuntu-latest"]
	// for the GitHub-hosted runners. A public repository always runs on
	// ubuntu-latest, and setting it there is an error.
	RunsOn []string `toml:"runs_on" yaml:"runs_on" json:"runs_on"`
}

// GitLabCI is GitLab CI (.gitlab-ci.yml).
type GitLabCI struct {
	// Tags are the runner tags of the jobs of a private repository.
	// Default: ["self-hosted", "linux", "docker"]; use
	// ["saas-linux-small-amd64"] for the GitLab.com instance runners. A
	// public repository always runs on the shared runners, and setting it
	// there is an error.
	Tags []string `toml:"tags" yaml:"tags" json:"tags"`
}

// decode reads the [repository] section of doc into c and records which
// CI tables it has.
func (c *Repository) decode(doc *Doc) error {
	if err := doc.Decode(SectionRepository, c); err != nil {
		return err
	}
	c.tables = nil
	for _, ci := range CIs {
		if doc.IsDefined(SectionRepository, "ci", ci) {
			c.tables = append(c.tables, ci)
		}
	}
	return nil
}

// ErrNoRepository is returned when the repository has no [repository] section.
var ErrNoRepository = errors.New("no [repository] section in the skenv file; run `skenv repo init`")

// ReadRepository decodes the [repository] section of the skenv file in root
// without validating it; ok is false when there is no skenv file or no
// [repository].
func ReadRepository(root string) (*Repository, bool, error) {
	file, err := Find(root)
	if err != nil || file == "" {
		return nil, false, err
	}
	doc, err := Read(file)
	if err != nil {
		return nil, true, err
	}
	if !doc.Has(SectionRepository) {
		return nil, false, nil
	}
	c := &Repository{File: file, HasUser: doc.Has(SectionUser)}
	if err := c.decode(doc); err != nil {
		return nil, true, fmt.Errorf("%s: %w", file, err)
	}
	return c, true, nil
}

// LoadRepository reads and validates the [repository] section of the skenv
// file in root.
func LoadRepository(root string) (*Repository, error) {
	c, ok, err := ReadRepository(root)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoRepository
	}
	return c, c.Validate()
}

// RepositoryVersion returns the template version recorded in root without
// validating the rest; ok is false when root has no [repository] section.
func RepositoryVersion(root string) (version string, ok bool, err error) {
	c, ok, err := ReadRepository(root)
	if c == nil {
		return "", ok, err
	}
	return c.TemplateVersion, ok, err
}

// VersionPattern is the format of repository.template_version.
const VersionPattern = `^[0-9]+\.[0-9]+\.[0-9]+$`

// VersionRe matches VersionPattern.
var VersionRe = regexp.MustCompile(VersionPattern)

// ParseRepository decodes and validates the [repository] section of a skenv file in
// the format of ext, with the rule that a public repository has no
// [user]. ok is false when there is no [repository].
func ParseRepository(data []byte, ext string) (c *Repository, ok bool, err error) {
	doc, err := Parse(data, ext)
	if err != nil || !doc.Has(SectionRepository) {
		return nil, false, err
	}
	c = &Repository{File: DefaultName, HasUser: doc.Has(SectionUser)}
	if err := c.decode(doc); err != nil {
		return nil, true, err
	}
	if err := c.Validate(); err != nil {
		return nil, true, err
	}
	if c.Visibility == "public" && c.HasUser {
		return nil, true, errors.New(PublicUserReason)
	}
	return c, true, nil
}

// PublicUserReason explains why a public repository must not carry the
// manifest.
const PublicUserReason = "a public repository must not carry [user]: the manifest is personal " +
	"(home paths, machine names, which skills you use); keep it in a private repository"

func (c *Repository) Validate() error {
	name := filepath.Base(c.File)
	if c.File == "" {
		name = DefaultName
	}
	switch len(c.tables) {
	case 0:
		if c.Provider == "" {
			c.Provider = CIGitHub
		}
	case 1:
		c.Provider = c.tables[0]
	default:
		return fmt.Errorf("%s: repository.ci has both github and gitlab; keep the table of the CI system you use", name)
	}
	if !slices.Contains(CIs, c.Provider) {
		return fmt.Errorf("%s: CI %q must be %q or %q", name, c.Provider, CIGitHub, CIGitLab)
	}
	key, set := "repository.ci.github.runs_on", c.CI.GitHub.RunsOn
	if c.Provider == CIGitLab {
		key, set = "repository.ci.gitlab.tags", c.CI.GitLab.Tags
	}
	if len(c.tables) > 0 {
		c.Runner = set
	}
	switch c.Visibility {
	case "private":
		if len(c.Runner) == 0 {
			c.Runner = DefaultRunner
		}
		for _, r := range c.Runner {
			if !runnerRe.MatchString(r) {
				return fmt.Errorf("%s: %s %q is not a runner label: use letters, digits and . _ : / -", name, key, r)
			}
		}
	case "public":
		if len(c.Runner) > 0 {
			return fmt.Errorf("%s: %s is for private repositories: the CI of a public one runs on the runners of the host; remove it", name, key)
		}
	default:
		return fmt.Errorf("%s: repository.visibility must be \"private\" or \"public\", got %q", name, c.Visibility)
	}
	if c.TemplateVersion == "" {
		return fmt.Errorf("%s: repository.template_version is required", name)
	}
	if !VersionRe.MatchString(c.TemplateVersion) {
		return fmt.Errorf("%s: repository.template_version %q must be a version such as %s", name, c.TemplateVersion, LatestTemplates)
	}
	if CompareVersions(c.TemplateVersion, LatestTemplates) > 0 {
		return fmt.Errorf("%s: template_version %s is newer than %s of this skenv; upgrade skenv", name, c.TemplateVersion, LatestTemplates)
	}
	return nil
}

// CompareVersions compares dotted numeric versions ("0.2.0", "v0.10.1"): -1, 0, 1.
// Pre-release or build suffixes are ignored.
func CompareVersions(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := range 3 {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

func parts(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(s)
		out[i] = n
	}
	return out
}
