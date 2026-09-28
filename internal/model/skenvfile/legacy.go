package skenvfile

import (
	"fmt"
	"slices"
	"strings"
)

// MigrationURL is the manual migration from the format before skenv 0.6.
const MigrationURL = "https://qunaxis.github.io/skenv/skenv-file#moving-to-the-0-6-format"

// legacyRepoTable and legacyEnvironmentTable are the table names before
// skenv 0.6 ([repo], now [repository]; [environment], now [user]).
const (
	legacyRepoTable        = "repo"
	legacyEnvironmentTable = "environment"
)

// legacyTopLevelKeys sat at the top of the skenv file before [repository]
// existed. legacyRepositoryKeys are the old field names of [repo] (and, by
// mistake, of the [repository] that replaces it). legacyLayoutKeys are the
// old fields of [environment.layout].
var (
	legacyTopLevelKeys   = []string{"harness", "visibility", "runner"}
	legacyRepositoryKeys = []string{"harness", "runner"}
	legacyLayoutKeys     = []string{"store", "targets", "ignore"}
)

// LegacyKeys returns the dotted table and key paths of the skenv file
// format before 0.6, built from the same names legacyError checks for
// (legacyTopLevelKeys, legacyRepositoryKeys, legacyLayoutKeys,
// legacyUserFields, legacyProjectFields): a rename of any of those is the
// only place TestNoRetiredTerms needs to follow. It omits generic
// per-entry keys (path, name, rev, url, type, skip, ...) and the old
// field names re-checked under the still-current [repository] table
// (repository.harness, repository.runner): both would read as ordinary
// English words in prose and make the scan noisy rather than useful.
func LegacyKeys() []string {
	keys := append([]string{}, legacyTopLevelKeys...)
	keys = append(keys, legacyRepoTable)
	for _, k := range legacyRepositoryKeys {
		keys = append(keys, legacyRepoTable+"."+k)
	}
	keys = append(keys, legacyEnvironmentTable, legacyEnvironmentTable+".layout")
	for _, k := range legacyLayoutKeys {
		keys = append(keys, legacyEnvironmentTable+".layout."+k)
	}
	for _, f := range legacyUserFields {
		keys = append(keys, legacyEnvironmentTable+"."+f.key)
	}
	for _, f := range legacyProjectFields {
		keys = append(keys, "project."+f.key)
	}
	keys = append(keys, "project.from")
	return keys
}

// legacyError reports the keys of the format before skenv 0.6 in raw, the
// whole document, each with the key that replaces it. skenv does not read
// the old format: the error lists every rename at once, so that one pass
// over the file fixes it.
func legacyError(raw map[string]any) error {
	l := &legacy{}
	l.check(raw)
	if len(l.renames) == 0 {
		return nil
	}
	return fmt.Errorf("this skenv file uses keys of skenv before 0.6; rename them (%s):\n  %s",
		MigrationURL, strings.Join(l.renames, "\n  "))
}

type legacy struct{ renames []string }

func (l *legacy) add(old, repl string) {
	r := old + " → " + repl
	if !slices.Contains(l.renames, r) {
		l.renames = append(l.renames, r)
	}
}

func (l *legacy) check(raw map[string]any) {
	for _, k := range legacyTopLevelKeys {
		if _, ok := raw[k]; ok {
			l.add(k+" (top level)", "under [repository]: "+repositoryKey(k))
		}
	}
	if repo, ok := raw[legacyRepoTable]; ok {
		l.add("["+legacyRepoTable+"]", "[repository]")
		l.repository(legacyRepoTable, repo)
	}
	l.repository(SectionRepository, raw[SectionRepository])
	if env, ok := raw[legacyEnvironmentTable]; ok {
		l.add("["+legacyEnvironmentTable+"]", "[user]")
		l.user(legacyEnvironmentTable, env)
	}
	l.user(SectionUser, raw[SectionUser])
	l.project(raw[SectionProject])
}

// repositoryKey is the replacement of a key of [repo].
func repositoryKey(k string) string {
	switch k {
	case "harness":
		return "template_version"
	case "runner":
		return "ci.github.runs_on (GitHub Actions) or ci.gitlab.tags (GitLab CI)"
	}
	return k
}

func (l *legacy) repository(name string, v any) {
	m, _ := v.(map[string]any)
	for _, k := range legacyRepositoryKeys {
		if _, ok := m[k]; ok {
			l.add(name+"."+k, "repository."+repositoryKey(k))
		}
	}
	if ci, ok := m["ci"].(string); ok {
		l.add(fmt.Sprintf("%s.ci = %q", name, ci), fmt.Sprintf("a [repository.ci.%s] table", ci))
	}
}

// legacyField is a retired table of [environment] (old [user]) or
// [project], beyond .layout and .from: the old key, the replacement text
// of the migration error, and the old per-entry keys of its items (an
// empty whereSuffix means items sit directly under name+"."+key, an
// "<alias>"/"<name>" one names the per-entry key in the message).
type legacyField struct {
	key, replace, whereSuffix string
	items                     map[string]string
}

// legacyUserFields are the retired tables of [environment] beyond
// .layout. legacyProjectFields are the retired tables of [project]
// beyond .from ([[project.from]], handled separately in project() since
// its retired form is the array syntax, not the table name).
var (
	legacyUserFields = []legacyField{
		{"own", "user.checkouts.<id>, one table per checkout keyed by an ID ([user.checkouts.<id>])", "", checkoutKeys},
		{"vendor", "user.dependencies.<name>, one table per skill keyed by its name ([user.dependencies.<name>])", "", dependencyKeys},
		{"hosts", "user.git_hosts", ".<alias>", gitHostKeys},
		{"host", "user.machines", ".<name>", machineKeys},
	}
	legacyProjectFields = []legacyField{
		{"vendor", "project.dependencies.<name>, one table per skill keyed by its name ([project.dependencies.<name>])", "", dependencyKeys},
		{"hosts", "project.git_hosts", ".<alias>", gitHostKeys},
	}
)

func (l *legacy) user(name string, v any) {
	m, _ := v.(map[string]any)
	if m == nil {
		return
	}
	if layout, ok := m["layout"]; ok {
		lm, _ := layout.(map[string]any)
		moved := map[string]string{"store": "user.storage.dir", "targets": "user.agents (enabled, paths, extra_dirs)", "ignore": "user.unmanaged"}
		for _, k := range legacyLayoutKeys {
			if _, ok := lm[k]; ok {
				l.add(name+".layout."+k, moved[k])
			}
		}
		if len(lm) == 0 {
			l.add(name+".layout", "user.storage, user.agents and user.unmanaged")
		}
	}
	for _, f := range legacyUserFields {
		if val, ok := m[f.key]; ok {
			l.add(name+"."+f.key, f.replace)
			l.items(val, name+"."+f.key+f.whereSuffix, f.items)
		}
	}
	l.items(m["checkouts"], "user.checkouts.<id>", checkoutKeys)
	l.items(m["dependencies"], "user.dependencies.<name>", dependencyKeys)
	l.items(m["git_hosts"], "user.git_hosts.<alias>", gitHostKeys)
	l.items(m["machines"], "user.machines.<name>", machineKeys)
}

func (l *legacy) project(v any) {
	m, _ := v.(map[string]any)
	if m == nil {
		return
	}
	for _, f := range legacyProjectFields {
		if val, ok := m[f.key]; ok {
			l.add("project."+f.key, f.replace)
			l.items(val, "project."+f.key+f.whereSuffix, f.items)
		}
	}
	switch from := m["from"].(type) {
	case []map[string]any, []any:
		l.add("[[project.from]]", "project.from.<id>, one table per repository keyed by an ID ([project.from.<id>])")
		l.items(from, "project.from", fromKeys)
	case map[string]any:
		l.items(from, "project.from.<id>", fromKeys)
	}
	l.items(m["dependencies"], "project.dependencies.<name>", dependencyKeys)
	l.items(m["git_hosts"], "project.git_hosts.<alias>", gitHostKeys)
}

// Old keys inside an entry, with their replacements.
var (
	checkoutKeys   = map[string]string{"path": "checkout_dir", "skills": "include"}
	dependencyKeys = map[string]string{"name": "the table key ([...dependencies.<name>])", "path": "skill_dir", "rev": "commit"}
	gitHostKeys    = map[string]string{"url": "base_url", "type": "provider", "ssh": "base_url (set it to the ssh address to clone over ssh)"}
	machineKeys    = map[string]string{"skip": "exclude"}
	fromKeys       = map[string]string{"rev": "commit"}
)

// items checks the entries of v, a list of tables (the old arrays) or a
// table of tables (the new keyed form), for the old keys of renamed.
func (l *legacy) items(v any, where string, renamed map[string]string) {
	var entries []map[string]any
	switch v := v.(type) {
	case []map[string]any:
		entries = v
	case []any:
		for _, x := range v {
			if m, ok := x.(map[string]any); ok {
				entries = append(entries, m)
			}
		}
	case map[string]any:
		ids := make([]string, 0, len(v))
		for id := range v {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if m, ok := v[id].(map[string]any); ok {
				entries = append(entries, m)
			}
		}
	}
	keys := make([]string, 0, len(renamed))
	for k := range renamed {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, e := range entries {
		for _, k := range keys {
			if _, ok := e[k]; ok {
				l.add(where+"."+k, renamed[k])
			}
		}
	}
}
