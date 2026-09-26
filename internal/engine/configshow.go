package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"text/tabwriter"

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
)

// Effective is the configuration `skenv config show` prints: the manifest
// resolved on this machine, with every input from outside the file and
// the reason for each choice (docs/adr/0002-config-format.md, gap 3).
type Effective struct {
	Manifest        Sourced          `json:"manifest"`
	Machine         EffectiveMachine `json:"machine"`
	Home            string           `json:"home"`
	ClaudeConfigDir string           `json:"claude_config_dir"`
	Store           Sourced          `json:"store"`
	Agents          []EffectiveAgent `json:"agents"`
	Checkouts       []EffectiveRepo  `json:"checkouts"`
	Skills          []EffectiveSkill `json:"skills"`
	Unmanaged       []string         `json:"unmanaged"`
	// Reproducible says what the file alone pins down.
	Reproducible string `json:"reproducible"`
}

// Sourced is a value and where it comes from.
type Sourced struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

// EffectiveMachine is the machine name and the rules that apply.
type EffectiveMachine struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	// Rules is the user.machines entry that applies, "" for none.
	Rules string `json:"rules"`
}

// EffectiveAgent is a link destination and why it is on or off.
type EffectiveAgent struct {
	Agent string `json:"agent"` // "" for an extra directory
	Dir   string `json:"dir"`
	On    bool   `json:"on"`
	Why   string `json:"why"`
}

// EffectiveRepo is a checkout resolved on this machine.
type EffectiveRepo struct {
	ID        string `json:"id"`
	Repo      string `json:"repo"`
	URL       string `json:"url"`
	Dir       string `json:"dir"`
	DirSource string `json:"dir_source"`
	Branch    string `json:"branch"`
	State     string `json:"state"`
}

// EffectiveSkill is a skill a source offers and whether this machine
// installs it.
type EffectiveSkill struct {
	Name     string `json:"name"`
	Source   string `json:"source"` // "checkout <id>" or "dependency"
	Selected bool   `json:"selected"`
	Why      string `json:"why"`
}

// ManifestSource resolves the manifest like ResolveManifest and says where
// its location came from.
func ManifestSource(ctx context.Context, env Env, flag string) (string, string, error) {
	m, src, err := config.Resolve(env.Home, env.Getenv, "manifest", flag, "")
	if err != nil {
		return "", "", err
	}
	if m == "" {
		return "", "", noManifest(ctx, env)
	}
	file, err := manifest.Locate(paths.Expand(env.Home, m))
	if err != nil {
		return "", "", err
	}
	switch src {
	case config.FromFlag:
		return file, "--manifest", nil
	case config.FromEnv:
		return file, "$" + config.EnvVar("manifest"), nil
	}
	f, _ := config.Load(env.Home)
	return file, "tool config " + homeShow(env.Home)(f.Path), nil
}

// Explain builds the effective configuration. It reads the file system
// and runs local git commands only: no fetch, no writes.
func (e *Engine) Explain(manifestSource string) (*Effective, error) {
	show := e.show
	r := &Effective{
		Manifest:        Sourced{show(e.manifestPath), manifestSource},
		Machine:         EffectiveMachine{Name: e.machine, Source: e.machineFrom},
		Home:            e.env.Home,
		ClaudeConfigDir: e.env.Getenv("CLAUDE_CONFIG_DIR"),
		Store:           Sourced{show(e.store), "default"},
		Agents:          []EffectiveAgent{},
		Checkouts:       []EffectiveRepo{},
		Skills:          []EffectiveSkill{},
		Unmanaged:       append([]string{}, e.m.Unmanaged...),
		Reproducible: "dependencies are pinned to a commit; checkouts follow their branch and local edits, " +
			"and agent detection, the machine name and $HOME come from this machine, so the file alone does not " +
			"reproduce the skills of checkouts",
	}
	if e.hasRules {
		r.Machine.Rules = e.machineRule()
	}
	if e.m.Storage.Dir != "" {
		r.Store.Source = "user.storage.dir"
	}
	for _, d := range agents.Resolve(e.env.Home, e.env.Getenv, e.agentSelection(), e.store) {
		r.Agents = append(r.Agents, EffectiveAgent{Agent: d.Agent, Dir: show(d.Dir), On: d.On, Why: d.Why})
	}
	for _, c := range e.m.CheckoutList() {
		er := EffectiveRepo{ID: c.ID, Repo: gitx.Mask(c.Repo), Dir: show(e.checkoutPath(c)), DirSource: "checkout_dir " + c.CheckoutDir}
		if remote, err := e.m.Remote(c.Repo); err == nil {
			er.URL = gitx.Mask(remote.URL)
		}
		if _, ok := e.rules.CheckoutDirs[c.ID]; ok && e.hasRules {
			er.DirSource = r.Machine.Rules + ".checkout_dirs"
		}
		dir := e.checkoutPath(c)
		switch fi, err := os.Stat(dir); {
		case err != nil || !fi.IsDir():
			er.State = "not cloned: sync clones it"
			er.Branch = c.Branch
			if er.Branch == "" {
				er.Branch = "(default of origin)"
			}
		case e.checkoutBlocked(c) != "":
			er.State = "not used: " + gitx.Mask(e.checkoutBlocked(c))
		default:
			er.State = "present"
			if target, err := e.targetBranchLocal(dir, c); err == nil {
				er.Branch = target
				if c.Branch == "" {
					er.Branch += " (default branch of origin)"
				}
				if cur := e.currentBranch(dir); cur != target {
					er.State = "present, on " + onBranch(cur) + ": local development state, sync does not update it"
				}
			}
		}
		r.Checkouts = append(r.Checkouts, er)
	}
	skills, err := e.skills()
	if err != nil {
		return nil, err
	}
	installed := map[string]bool{}
	for _, s := range skills {
		installed[s.Name] = true
	}
	for _, c := range e.m.CheckoutList() {
		if e.checkoutBlocked(c) != "" {
			continue
		}
		found, err := e.checkoutSkills(c)
		if err != nil {
			continue
		}
		for _, name := range found {
			why := selectionWhy("include", "exclude", c.Include, c.Exclude, name)
			ok := c.Selects(name)
			if ok {
				why = e.machineWhy(name, why)
			}
			r.Skills = append(r.Skills, EffectiveSkill{Name: name, Source: "checkout " + c.ID, Selected: ok && installed[name], Why: why})
		}
	}
	for _, d := range e.m.DependencyList() {
		why := e.machineWhy(d.Name, fmt.Sprintf("dependency %s at %.12s (%s)", gitx.Mask(d.Repo), d.Commit, d.SkillDir))
		r.Skills = append(r.Skills, EffectiveSkill{Name: d.Name, Source: "dependency", Selected: installed[d.Name], Why: why})
	}
	return r, nil
}

// machineWhy adds the verdict of the machine rules to why.
func (e *Engine) machineWhy(name, why string) string {
	if !e.hasRules || (e.rules.Include == nil && firstMatch(e.rules.Exclude, name) == "") {
		return why
	}
	return why + "; " + selectionWhy(e.machineRule()+".include", e.machineRule()+".exclude", e.rules.Include, e.rules.Exclude, name)
}

// selectionWhy explains how include and exclude (named includeKey and
// excludeKey) decide on name.
func selectionWhy(includeKey, excludeKey string, include, exclude []string, name string) string {
	var why string
	switch {
	case include == nil:
		why = includeKey + " omitted: every skill"
	case len(include) == 0:
		return includeKey + " = [] selects nothing"
	default:
		p := firstMatch(include, name)
		if p == "" {
			return "not matched by " + includeKey
		}
		why = fmt.Sprintf("matched by %s %q", includeKey, p)
	}
	if p := firstMatch(exclude, name); p != "" {
		return fmt.Sprintf("%s, but excluded by %s %q", why, excludeKey, p)
	}
	return why
}

func firstMatch(pats []string, name string) string {
	for _, p := range pats {
		if ok, _ := path.Match(p, name); ok {
			return p
		}
	}
	return ""
}

// targetBranchLocal is targetBranch without asking the remote.
func (e *Engine) targetBranchLocal(dir string, c *manifest.Checkout) (string, error) {
	if c.Branch != "" {
		return c.Branch, nil
	}
	ref, err := e.env.Git.Run(e.ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil || !strings.HasPrefix(ref, "origin/") {
		return "", fmt.Errorf("origin/HEAD is not set")
	}
	return strings.TrimPrefix(ref, "origin/"), nil
}

// PrintEffective prints the effective configuration, as JSON or text.
func (e *Engine) PrintEffective(r *Effective, asJSON bool) error {
	out := e.env.Stdout
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "manifest\t%s (%s)\n", r.Manifest.Value, r.Manifest.Source)
	rules := "no machine rules"
	if r.Machine.Rules != "" {
		rules = "rules " + r.Machine.Rules
	}
	fmt.Fprintf(tw, "machine\t%s (%s; %s)\n", r.Machine.Name, r.Machine.Source, rules)
	fmt.Fprintf(tw, "home\t%s ($HOME)\n", r.Home)
	if r.ClaudeConfigDir != "" {
		fmt.Fprintf(tw, "claude\t%s ($CLAUDE_CONFIG_DIR)\n", e.show(paths.Expand(e.env.Home, r.ClaudeConfigDir)))
	}
	fmt.Fprintf(tw, "store\t%s (%s)\n", r.Store.Value, r.Store.Source)
	if len(r.Unmanaged) > 0 {
		fmt.Fprintf(tw, "unmanaged\t%s (user.unmanaged: never touched)\n", strings.Join(r.Unmanaged, ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nagents")
	tw = tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, a := range r.Agents {
		name, on := a.Agent, "off"
		if name == "" {
			name = "extra"
		}
		if a.On {
			on = "on"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", name, a.Dir, on, a.Why)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(r.Checkouts) > 0 {
		fmt.Fprintln(out, "\ncheckouts")
		tw = tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, c := range r.Checkouts {
			fmt.Fprintf(tw, "  %s\t%s\t%s (%s)\tbranch %s\t%s\n", c.ID, c.Repo, c.Dir, c.DirSource, c.Branch, c.State)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if len(r.Skills) > 0 {
		fmt.Fprintln(out, "\nskills")
		tw = tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, s := range r.Skills {
			verdict := "selected"
			if !s.Selected {
				verdict = "not selected"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", s.Name, s.Source, verdict, s.Why)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "\nnote: %s\n", r.Reproducible)
	return nil
}
