// Package schemagen generates the JSON Schemas in schemas/ from the Go
// types that parse the files: skenvfile.Repository ([repository]),
// skenvfile.Manifest ([environment]) and config.Config (the tool config).
//
// Property names and types come from the json tags, descriptions from the
// doc comments of the types and fields, and the constraints the parser
// enforces (patterns, enums, required keys, defaults) from the constants
// the parser itself uses, so the schema cannot drift from it. `make schemas`
// writes the files; TestSchemasUpToDate fails when they are stale.
package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/qunaxis/skenv/internal/model/agents"
	"github.com/qunaxis/skenv/internal/model/config"
	"github.com/qunaxis/skenv/internal/model/skenvfile"
	"github.com/qunaxis/skenv/internal/model/skillname"
	"github.com/qunaxis/skenv/schemas"
)

// Draft is the JSON Schema version of the generated files.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// RelPathPattern is a clean relative path inside a repository, the rule
// of the parser's cleanRel: no leading "/", no empty, "." or ".."
// segments except a lone ".". The empty string means the default.
const RelPathPattern = `^(\.|([^/.][^/]*|\.[^/.][^/]*|\.\.[^/]+)(/([^/.][^/]*|\.[^/.][^/]*|\.\.[^/]+))*)?$`

const relPathMessage = `A relative path inside the repository ("." for its root), without "..", empty segments or a trailing "/".`

// Schema is the subset of JSON Schema 2020-12 that skenv uses, in the
// order the keywords are written.
type Schema struct {
	Schema               string   `json:"$schema,omitempty"`
	ID                   string   `json:"$id,omitempty"`
	Ref                  string   `json:"$ref,omitempty"`
	Title                string   `json:"title,omitempty"`
	Description          string   `json:"description,omitempty"`
	Type                 string   `json:"type,omitempty"`
	Enum                 []string `json:"enum,omitempty"`
	Const                any      `json:"const,omitempty"`
	Pattern              string   `json:"pattern,omitempty"`
	MinLength            int      `json:"minLength,omitempty"`
	MaxLength            int      `json:"maxLength,omitempty"`
	Items                *Schema  `json:"items,omitempty"`
	MinItems             int      `json:"minItems,omitempty"`
	UniqueItems          bool     `json:"uniqueItems,omitempty"`
	Properties           Props    `json:"properties,omitempty"`
	Required             []string `json:"required,omitempty"`
	AdditionalProperties any      `json:"additionalProperties,omitempty"` // false or *Schema
	PropertyNames        *Schema  `json:"propertyNames,omitempty"`
	Not                  *Schema  `json:"not,omitempty"`
	If                   *Schema  `json:"if,omitempty"`
	Then                 *Schema  `json:"then,omitempty"`
	Default              any      `json:"default,omitempty"`
	Examples             []any    `json:"examples,omitempty"`
	Defs                 Props    `json:"$defs,omitempty"`

	// Messages that VS Code's JSON and YAML language servers show instead
	// of their generic ones. Validators ignore unknown keywords.
	ErrorMessage        string `json:"errorMessage,omitempty"`
	PatternErrorMessage string `json:"patternErrorMessage,omitempty"`
}

// Prop is one named schema of Props.
type Prop struct {
	Name   string
	Schema *Schema
}

// Props is an ordered map of schemas: properties keep the order of the
// struct fields.
type Props []Prop

// MarshalJSON writes Props as an object in order.
func (p Props) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, x := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshal(x.Name)
		if err != nil {
			return nil, err
		}
		v, err := marshal(x.Schema)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (p Props) get(name string) *Schema {
	for _, x := range p {
		if x.Name == name {
			return x.Schema
		}
	}
	panic("schemagen: no property " + name)
}

func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// Generate returns the schemas by file name, with "$id" for version (""
// for the unversioned URL, which the committed files carry). root is the
// module root: the doc comments are read from the sources.
func Generate(root, version string) (map[string][]byte, error) {
	docs, err := readDocs(root, "internal/model/skenvfile", "internal/model/config")
	if err != nil {
		return nil, err
	}
	g := &gen{docs: docs}
	out := map[string][]byte{}
	for name, s := range map[string]*Schema{schemas.Skenv: g.skenv(), schemas.Config: g.config()} {
		s.Schema = Draft
		s.ID = schemas.URL(name, version)
		b, err := marshal(s)
		if err != nil {
			return nil, err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, b, "", "  "); err != nil {
			return nil, err
		}
		pretty.WriteByte('\n')
		out[name] = pretty.Bytes()
	}
	return out, nil
}

type gen struct {
	docs map[string]string // "pkg.Type" and "pkg.Type.Field" → doc comment
}

// skenv is the schema of the skenv file, built one function per $def,
// each at most 40 statements (funlen): a new key goes into the function
// of its table.
func (g *gen) skenv() *Schema {
	ci, github, gitlab := g.ciDefs()
	return &Schema{
		Title: "skenv file",
		Description: "The skenv file of a repository: skenv.toml, skenv.yaml, skenv.yml or skenv.json in its root. " +
			"[repository] is the development tooling of a skills repository, [user] the manifest (the skills of your agents), " +
			"[project] the skills a project repository carries. " +
			"Rules the schema cannot check are enforced by skenv: skill names are unique across checkouts and dependencies " +
			"(and across dependencies and from entries of [project]), user.unmanaged must not match a selected skill, " +
			"project.dir and project.mirrors do not overlap, and a directory holds only one skenv file. " +
			"Docs: https://qunaxis.github.io/skenv/skenv-file",
		Type: "object",
		Properties: Props{
			{schemaKey, &Schema{Type: "string", Description: "The URL of this schema, for editors. skenv ignores it."}},
			{"repository", &Schema{Ref: "#/$defs/repository"}},
			{"user", &Schema{Ref: "#/$defs/user"}},
			{"project", &Schema{Ref: "#/$defs/project"}},
		},
		AdditionalProperties: false,
		If: &Schema{
			Required:   []string{"repository"},
			Properties: Props{{"repository", &Schema{Required: []string{"visibility"}, Properties: Props{{"visibility", &Schema{Const: "public"}}}}}},
		},
		Then: &Schema{Properties: Props{{"user", &Schema{
			Description: "A public repository must not carry [user]: the manifest is personal " +
				"(home paths, machine names, which skills you use). Keep it in a private repository.",
			Not:          &Schema{},
			ErrorMessage: "A public repository must not carry [user]: the manifest is personal. Keep it in a private repository.",
		}}}},
		Defs: Props{
			{"repository", g.repositoryDef()},
			{"ci", ci},
			{"github", github},
			{"gitlab", gitlab},
			{"user", g.userDef()},
			{"checkout", g.checkoutDef()},
			{"dependency", g.dependencyDef()},
			{"machine", g.machineDef()},
			{"agents", g.agentsDef()},
			{"storage", g.storageDef()},
			{"gitHost", g.gitHostDef()},
			{"project", g.projectDef()},
			{"from", g.fromDef()},
		},
	}
}

// commitMessage explains skenvfile.RevPattern.
const commitMessage = "A full 40-character lowercase commit SHA; branches, tags and short SHAs are not allowed."

// idNames is the schema of checkout and from IDs as property names.
func idNames() *Schema {
	return &Schema{Pattern: skenvfile.IDPattern, PatternErrorMessage: `An ID is lowercase letters, digits, "-" and "_", starting with a letter or digit.`}
}

// skillNames is the schema of skill names as property names.
func skillNames() *Schema {
	s := skillName()
	s.Type = ""
	return s
}

// hostNames is the schema of git_hosts aliases as property names.
func hostNames() *Schema {
	return &Schema{
		Pattern:             skenvfile.AliasPattern,
		PatternErrorMessage: `An alias is lowercase letters, digits and "-", starting with a letter.`,
		Not:                 &Schema{Enum: skenvfile.ReservedAliases, ErrorMessage: "This prefix is built in and cannot be declared."},
	}
}

// relPath makes s a relative path inside the repository.
func relPath(s *Schema) {
	s.Pattern = RelPathPattern
	s.PatternErrorMessage = relPathMessage
}

// selection constrains the include and exclude lists of s.
func selection(s *Schema) {
	include := s.Properties.get("include")
	include.UniqueItems = true
	include.Items.Pattern = `^[^/]+$`
	include.Items.PatternErrorMessage = `A skill name or a glob over names, without "/".`
	exclude := s.Properties.get("exclude").Items
	exclude.Pattern = `^[^/]+$`
	exclude.PatternErrorMessage = `A skill name or a glob over names, without "/".`
}

func (g *gen) userDef() *Schema {
	user := g.object(reflect.TypeFor[skenvfile.Manifest]())
	checkouts := user.Properties.get("checkouts")
	checkouts.AdditionalProperties = &Schema{Ref: "#/$defs/checkout"}
	checkouts.PropertyNames = idNames()
	deps := user.Properties.get("dependencies")
	deps.AdditionalProperties = &Schema{Ref: "#/$defs/dependency"}
	deps.PropertyNames = skillNames()
	user.Properties.get("machines").AdditionalProperties = &Schema{Ref: "#/$defs/machine"}
	unmanaged := user.Properties.get("unmanaged").Items
	unmanaged.Pattern = `^[^/]+$`
	unmanaged.PatternErrorMessage = `A glob over entry names, without "/".`
	hosts := user.Properties.get("git_hosts")
	hosts.AdditionalProperties = &Schema{Ref: "#/$defs/gitHost"}
	hosts.PropertyNames = hostNames()
	return user
}

func (g *gen) gitHostDef() *Schema {
	gitHost := g.object(reflect.TypeFor[skenvfile.GitHost]())
	gitHost.Required = []string{"base_url"}
	hostURL := gitHost.Properties.get("base_url")
	hostURL.Pattern = `^([Hh][Tt][Tt][Pp][Ss]?://[^/@?#]+|[Ss][Ss][Hh]://([^/:@?#]+@)?[^/@?#]+)(/[^?#]*)?$`
	hostURL.PatternErrorMessage = `A base URL such as "https://git.example.com" or "ssh://git@git.example.com", without credentials: ` +
		`use a git credential helper or an ssh key (https://qunaxis.github.io/skenv/git-hosts#authentication).`
	hostURL.Examples = []any{"https://git.example.com", "ssh://git@git.example.com"}
	provider := gitHost.Properties.get("provider")
	provider.Enum = skenvfile.HostTypes
	provider.Default = skenvfile.TypeGeneric
	return gitHost
}

func (g *gen) storageDef() *Schema {
	storage := g.object(reflect.TypeFor[skenvfile.Storage]())
	storage.Properties.get("dir").Examples = []any{"~/.agents/skills", "~/.local/share/skenv/skills"}
	return storage
}

func (g *gen) agentsDef() *Schema {
	agentsDef := g.object(reflect.TypeFor[skenvfile.Agents]())
	// Codex is the agent people expect here; it reads the store instead.
	agentHint := "Add other directories to user.agents.extra_dirs; Codex needs no entry: " +
		"it reads the store (user.storage.dir, default ~/.agents/skills) directly."
	enabled := agentsDef.Properties.get("enabled")
	enabled.UniqueItems = true
	enabled.Items.Enum = agents.Names
	enabled.Items.ErrorMessage = agentHint
	agentPaths := agentsDef.Properties.get("paths")
	agentPaths.AdditionalProperties = &Schema{Type: "string", MinLength: 1}
	agentPaths.PropertyNames = &Schema{Enum: agents.Names, ErrorMessage: agentHint}
	agentsDef.Properties.get("extra_dirs").Items.MinLength = 1
	return agentsDef
}

func (g *gen) machineDef() *Schema {
	machine := g.object(reflect.TypeFor[skenvfile.Machine]())
	selection(machine)
	machineDirs := machine.Properties.get("checkout_dirs")
	machineDirs.AdditionalProperties = &Schema{Type: "string", MinLength: 1}
	machineDirs.PropertyNames = idNames()
	return machine
}

func (g *gen) checkoutDef() *Schema {
	checkout := g.object(reflect.TypeFor[skenvfile.Checkout]())
	selection(checkout)
	branch := checkout.Properties.get("branch")
	branch.Pattern = skenvfile.BranchPattern
	branch.PatternErrorMessage = "A git branch name."
	branch.Examples = []any{"main"}
	checkout.Required = []string{"repo", "checkout_dir"}
	checkout.Properties.get("repo").MinLength = 1
	checkout.Properties.get("checkout_dir").MinLength = 1
	skillsDir := checkout.Properties.get("skills_dir")
	relPath(skillsDir)
	skillsDir.Default = skenvfile.DefaultSkillsDir
	return checkout
}

func (g *gen) dependencyDef() *Schema {
	dependency := g.object(reflect.TypeFor[skenvfile.Dependency]())
	dependency.Required = []string{"repo", "commit"}
	dependency.Properties.get("repo").MinLength = 1
	skillDir := dependency.Properties.get("skill_dir")
	relPath(skillDir)
	skillDir.Default = "."
	commit := dependency.Properties.get("commit")
	commit.Pattern = skenvfile.RevPattern
	commit.PatternErrorMessage = commitMessage
	return dependency
}

func (g *gen) projectDef() *Schema {
	project := g.object(reflect.TypeFor[skenvfile.Project]())
	projectHosts := project.Properties.get("git_hosts")
	projectHosts.AdditionalProperties = &Schema{Ref: "#/$defs/gitHost"}
	projectHosts.PropertyNames = hostNames()
	projectDeps := project.Properties.get("dependencies")
	projectDeps.AdditionalProperties = &Schema{Ref: "#/$defs/dependency"}
	projectDeps.PropertyNames = skillNames()
	projectFrom := project.Properties.get("from")
	projectFrom.AdditionalProperties = &Schema{Ref: "#/$defs/from"}
	projectFrom.PropertyNames = idNames()
	dir := project.Properties.get("dir")
	relPath(dir)
	dir.Not = &Schema{Const: ".", ErrorMessage: `dir must be a directory inside the repository, not its root.`}
	dir.Default = skenvfile.DefaultProjectDir
	mirrors := project.Properties.get("mirrors")
	mirrors.UniqueItems = true
	mirrors.Items.MinLength = 1
	relPath(mirrors.Items)
	mirrors.Items.Not = &Schema{Const: ".", ErrorMessage: `A mirror is a directory inside the repository, not its root.`}
	mirrors.Items.Examples = []any{".claude/skills"}
	mode := project.Properties.get("mirrors_mode")
	mode.Enum = skenvfile.MirrorModes
	mode.Default = skenvfile.MirrorSymlink
	return project
}

func (g *gen) fromDef() *Schema {
	from := g.object(reflect.TypeFor[skenvfile.From]())
	from.Required = []string{"repo", "skills", "commit"}
	from.Properties.get("repo").MinLength = 1
	fromDir := from.Properties.get("skills_dir")
	relPath(fromDir)
	fromDir.Default = skenvfile.DefaultSkillsDir
	fromSkills := from.Properties.get("skills")
	fromSkills.MinItems = 1
	fromSkills.UniqueItems = true
	fromSkills.Items = skillName()
	fromCommit := from.Properties.get("commit")
	*fromCommit = Schema{Type: "string", Description: fromCommit.Description, Pattern: skenvfile.RevPattern, PatternErrorMessage: commitMessage}
	return from
}

func (g *gen) repositoryDef() *Schema {
	repo := g.object(reflect.TypeFor[skenvfile.Repository]())
	repo.Required = []string{"template_version", "visibility"}
	h := repo.Properties.get("template_version")
	h.Pattern = skenvfile.VersionPattern
	h.PatternErrorMessage = "A version such as " + skenvfile.LatestTemplates + "."
	h.Examples = []any{skenvfile.LatestTemplates}
	repo.Properties.get("visibility").Enum = []string{"private", "public"}
	return repo
}

// ciDefs are the $defs of [repository.ci] and its github and gitlab
// tables.
func (g *gen) ciDefs() (ci, github, gitlab *Schema) {
	ci = g.object(reflect.TypeFor[skenvfile.CIConfig]())
	github = g.object(reflect.TypeFor[skenvfile.GitHubCI]())
	gitlab = g.object(reflect.TypeFor[skenvfile.GitLabCI]())
	ci.Not = &Schema{Required: []string{skenvfile.CIGitHub, skenvfile.CIGitLab}, ErrorMessage: "Keep one CI table: github or gitlab."}
	runner := func(s *Schema, key string) {
		p := s.Properties.get(key)
		p.Default = skenvfile.DefaultRunner
		p.Items.Pattern = `^[A-Za-z0-9._:/-]+$`
		p.Items.PatternErrorMessage = "A runner label: letters, digits and . _ : / -"
	}
	runner(github, "runs_on")
	runner(gitlab, "tags")
	return ci, github, gitlab
}

// skillName is the schema of a skill name in the manifest.
func skillName() *Schema {
	return &Schema{
		Type:                "string",
		Pattern:             skillname.Pattern,
		MaxLength:           skillname.MaxLen,
		PatternErrorMessage: "A skill name is " + skillname.Rule + ".",
		Not: &Schema{Const: skenvfile.Reserved, ErrorMessage: `"` + skenvfile.Reserved + `" is reserved: ~/.claude/skills/` +
			skenvfile.Reserved + ` is managed by Claude.`},
	}
}

// schemaKey names the schema of a document, for editors; the parsers
// accept it at the top level.
const schemaKey = "$schema"

func (g *gen) config() *Schema {
	s := g.object(reflect.TypeFor[config.Config]())
	for _, p := range s.Properties {
		if p.Name == "manifest" {
			p.Schema.Description += fmt.Sprintf(" Overridden by the flag --%s and the environment variable %s; "+
				"precedence: flag, environment variable, this file, default.", p.Name, config.EnvVar(p.Name))
			continue
		}
		p.Schema.Description += fmt.Sprintf(" Overridden by the environment variable %s; "+
			"precedence: environment variable, this file, default.", config.EnvVar(p.Name))
	}
	s.Properties = append(Props{{schemaKey, &Schema{Type: "string", Description: "The URL of this schema, for editors. skenv ignores it."}}}, s.Properties...)
	s.Title = "skenv tool config"
	s.Description = "skenv's own configuration, one per machine: ~/.config/skenv/config.toml " +
		"(or config.yaml, config.yml, config.json; only one of them). Unknown keys are errors. " +
		"Docs: https://qunaxis.github.io/skenv/configuration"
	return s
}

// object is the schema of struct t: its fields with json tags, strictly
// (additionalProperties: false), described by the doc comments. Nested
// structs are referenced as $defs by their json name.
func (g *gen) object(t reflect.Type) *Schema {
	key := path.Base(t.PkgPath()) + "." + t.Name()
	s := &Schema{Type: "object", Description: describe(t.Name(), g.docs[key]), AdditionalProperties: false}
	for f := range t.Fields() {
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		p := g.typeSchema(f.Type, tag)
		p.Description = describe(f.Name, g.docs[key+"."+f.Name])
		s.Properties = append(s.Properties, Prop{tag, p})
	}
	return s
}

func (g *gen) typeSchema(t reflect.Type, name string) *Schema {
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Slice:
		item := g.typeSchema(t.Elem(), name)
		if t.Elem().Kind() == reflect.Struct {
			item = &Schema{Ref: "#/$defs/" + name}
		}
		return &Schema{Type: "array", Items: item}
	case reflect.Map:
		return &Schema{Type: "object"}
	case reflect.Struct:
		return &Schema{Ref: "#/$defs/" + name}
	}
	panic("schemagen: unsupported type " + t.String())
}

// describe turns the doc comment of a field into its description:
// "Rev is the full commit SHA." becomes "The full commit SHA.", "Skip
// lists skills" becomes "Lists skills".
func describe(field, doc string) string {
	rest, ok := strings.CutPrefix(doc, field+" ")
	if !ok {
		return doc
	}
	for _, verb := range []string{"is ", "are "} {
		if r, cut := strings.CutPrefix(rest, verb); cut {
			rest = r
			break
		}
	}
	r := []rune(rest)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// readDocs collects the doc comments of the types and struct fields in
// dirs, keyed "pkg.Type" and "pkg.Type.Field" (pkg is the directory name),
// as single paragraphs.
func readDocs(root string, dirs ...string) (map[string]string, error) {
	docs := map[string]string{}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		pkg := filepath.Base(dir)
		matches, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			return nil, err
		}
		for _, file := range matches {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			typeDocs(docs, pkg, f)
		}
	}
	return docs, nil
}

// typeDocs adds the doc comments of the types and struct fields of f to
// docs, as readDocs keys them.
func typeDocs(docs map[string]string, pkg string, f *ast.File) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			doc := ts.Doc
			if doc == nil {
				doc = gd.Doc
			}
			docs[pkg+"."+ts.Name.Name] = firstParagraph(doc)
			if st, ok := ts.Type.(*ast.StructType); ok {
				fieldDocs(docs, pkg+"."+ts.Name.Name, st)
			}
		}
	}
}

// fieldDocs adds the doc comments of the fields of st to docs, keyed
// "<typ>.Field".
func fieldDocs(docs map[string]string, typ string, st *ast.StructType) {
	for _, fld := range st.Fields.List {
		for _, n := range fld.Names {
			docs[typ+"."+n.Name] = clean(fld.Doc)
		}
	}
}

func clean(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return strings.Join(strings.Fields(cg.Text()), " ")
}

// firstParagraph is the user-facing part of a type comment; later
// paragraphs are notes for developers.
func firstParagraph(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	para, _, _ := strings.Cut(cg.Text(), "\n\n")
	return strings.Join(strings.Fields(para), " ")
}
