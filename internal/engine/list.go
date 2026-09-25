package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/qunaxis/skenv/internal/gitx"
)

// Kinds and states of `skenv list`.
const (
	KindEditable = "editable" // an own skill: linked from a git working copy
	KindPinned   = "pinned"   // a vendored skill: a copy at a commit

	StateInstalled   = "installed"
	StateNotSynced   = "not synced"
	StateConflict    = "conflict"
	StateNotSelected = "not selected"
	StateSkipped     = "skipped on this host"
)

// ListEntry is one skill of the manifest in `skenv list`.
type ListEntry struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source string `json:"source"` // the repo value of the manifest
	// Version is the commit of a pinned skill and the working copy of an
	// editable one.
	Version string `json:"version"`
	State   string `json:"state"`
}

// ListReport is the result of List.
type ListReport struct {
	Manifest string      `json:"manifest"`
	Store    string      `json:"store"`
	Targets  []string    `json:"targets"`
	Skills   []ListEntry `json:"skills"`
	// NotCloned are own repositories without a working copy yet, whose
	// skills are unknown until sync clones them.
	NotCloned []string `json:"not_cloned"`
}

// List prints every skill of the manifest with its source and whether it
// is installed for the agents on this machine. It reads the file system
// only: no fetch, no lock, no writes.
func (e *Engine) List(asJSON bool) (int, error) {
	skills, err := e.skills()
	if err != nil {
		return ExitFatal, err
	}
	r := &ListReport{Manifest: e.show(e.manifestPath), Store: e.show(e.store), Targets: []string{}, Skills: []ListEntry{}, NotCloned: []string{}}
	for _, t := range e.targets {
		r.Targets = append(r.Targets, e.show(t))
	}
	for _, s := range skills {
		r.Skills = append(r.Skills, e.listEntry(s, e.installState(s)))
	}
	// The skills of the manifest that are not installed here: e.unselected
	// holds them once skills() ran.
	for i := range e.m.Own {
		o := &e.m.Own[i]
		found, err := ownFound(e.ownSkillsDir(o))
		if err != nil {
			r.NotCloned = append(r.NotCloned, fmt.Sprintf("%s (%s)", gitx.Mask(o.Repo), e.show(e.ownPath(o))))
			continue
		}
		for _, name := range found {
			if _, ok := e.unselected[name]; !ok {
				continue
			}
			state := StateSkipped
			if !o.Selects(name) {
				state = StateNotSelected
			}
			r.Skills = append(r.Skills, e.listEntry(Skill{Name: name, Own: o}, state))
		}
	}
	for i := range e.m.Vendor {
		if v := &e.m.Vendor[i]; e.unselected[v.Name] != "" {
			r.Skills = append(r.Skills, e.listEntry(Skill{Name: v.Name, Vendor: v}, StateSkipped))
		}
	}
	sort.SliceStable(r.Skills, func(a, b int) bool { return r.Skills[a].Name < r.Skills[b].Name })

	if asJSON {
		enc := json.NewEncoder(e.env.Stdout)
		enc.SetIndent("", "  ")
		return ExitOK, enc.Encode(r)
	}
	return ExitOK, e.printList(r)
}

func (e *Engine) listEntry(s Skill, state string) ListEntry {
	if s.Vendor != nil {
		return ListEntry{Name: s.Name, Kind: KindPinned, Source: gitx.Mask(s.Vendor.Repo), Version: s.Vendor.Rev, State: state}
	}
	return ListEntry{Name: s.Name, Kind: KindEditable, Source: gitx.Mask(s.Own.Repo), Version: e.show(e.ownPath(s.Own)), State: state}
}

// installState is StateInstalled when the store entry of s and its link in
// every agent directory are as sync leaves them, StateConflict when an
// unmanaged path is in the way, StateNotSynced otherwise. It runs the
// checks of doctor.
func (e *Engine) installState(s Skill) string {
	var classes []string
	add := func(class, _, _, _ string) { classes = append(classes, class) }
	e.doctorStore(s, add)
	for _, t := range e.targets {
		p := filepath.Join(t, s.Name)
		if _, err := os.Lstat(p); err != nil {
			classes = append(classes, ClassMissing)
			continue
		}
		e.checkLink(p, e.linkDest(t, s.Name), s.Name, add)
	}
	switch {
	case slices.Contains(classes, ClassConflict):
		return StateConflict
	case len(classes) > 0:
		return StateNotSynced
	}
	return StateInstalled
}

func (e *Engine) printList(r *ListReport) error {
	out := e.env.Stdout
	fmt.Fprintf(out, "manifest  %s\nstore     %s\nagents    %s\n\n", r.Manifest, r.Store, strings.Join(r.Targets, ", "))
	if len(r.Skills) == 0 {
		fmt.Fprintln(out, "no skills yet: `skenv vendor add <repo>` installs one")
	} else {
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "SKILL\tKIND\tSOURCE\tVERSION\tSTATE")
		counts := map[string]int{}
		for _, s := range r.Skills {
			version := s.Version
			if s.Kind == KindPinned {
				version = fmt.Sprintf("%.12s", version)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Kind, s.Source, version, s.State)
			counts[s.State]++
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if n := counts[StateNotSynced]; n > 0 {
			fmt.Fprintf(out, "%d not synced: run `skenv sync`\n", n)
		}
		if n := counts[StateConflict]; n > 0 {
			fmt.Fprintf(out, "%d in conflict with paths skenv does not manage: inspect them, then `skenv sync --adopt`\n", n)
		}
	}
	for _, repo := range r.NotCloned {
		fmt.Fprintf(out, "not cloned: %s; run `skenv sync`\n", repo)
	}
	return nil
}
