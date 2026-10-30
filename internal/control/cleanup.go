package control

// Städförslag: worktrees och branches som är mergade och inte används längre. Inget tas bort
// automatiskt; borttagning kräver bekräftelse och använder git branch -d (aldrig -D).

import (
	"errors"
	"fmt"
	"strings"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

type cleanupItem struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path,omitempty"` // worktreens sökväg; tom för en branch utan worktree
	Why    string `json:"why"`
}

func cleanupCandidates(snap state.Snapshot, repo string) []cleanupItem {
	var out []cleanupItem
	for _, r := range snap.Repos {
		if repo != "" && !strings.EqualFold(r.Name, repo) {
			continue
		}
		for _, wt := range r.Worktrees {
			if wt.Main || !wt.Merged || wt.Branch == "" || wt.Branch == r.Default || wt.Locked || wt.Missing ||
				wt.Changes > 0 || len(wt.Panes)+len(wt.Servers)+len(wt.Containers) > 0 {
				continue
			}
			out = append(out, cleanupItem{Repo: r.Name, Branch: wt.Branch, Path: wt.Path, Why: "mergad i " + r.Default + ", inget ocommittat, inget igång"})
		}
		for _, b := range r.Branches {
			if b.Merged && b.Worktree == "" && b.Name != r.Default {
				out = append(out, cleanupItem{Repo: r.Name, Branch: b.Name, Why: "mergad i " + r.Default + ", ingen worktree"})
			}
		}
	}
	return out
}

func (c *Controller) cleanupList(a Args) (any, error) {
	c.col.Refresh(true)
	return cleanupCandidates(c.col.Snapshot(), a.S("repo")), nil
}

// cleanup tar bort en föreslagen worktree (och dess branch) eller en branch. Kräver confirm, och
// villkoren kontrolleras igen mot ett färskt läge.
func (c *Controller) cleanup(a Args) (any, error) {
	if !a.B("confirm", false) {
		return nil, errors.New("städning kräver bekräftelse")
	}
	c.col.Refresh(true)
	snap := c.col.Snapshot()
	var item *cleanupItem
	for _, it := range cleanupCandidates(snap, a.S("repo")) {
		if it.Branch == a.S("branch") {
			it := it
			item = &it
		}
	}
	if item == nil {
		return nil, fmt.Errorf("%s i %s är inte (längre) ett städförslag", a.S("branch"), a.S("repo"))
	}
	r, err := FindRepo(snap, item.Repo)
	if err != nil {
		return nil, err
	}
	if item.Path != "" {
		if _, err := git(r.Path, "worktree", "remove", item.Path); err != nil {
			return nil, fmt.Errorf("worktree remove: %v", err)
		}
	}
	if _, err := git(r.Path, "branch", "-d", item.Branch); err != nil {
		return nil, fmt.Errorf("git branch -d %s: %v", item.Branch, err)
	}
	c.col.Refresh(true)
	return item, nil
}

// ---------- taggar per repo ----------

func (c *Controller) repoTag(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	if err := config.SetTag(r.Name, a.S("tag"), a.B("on", true)); err != nil {
		return nil, err
	}
	c.col.Refresh(true)
	return map[string]any{"repo": r.Name, "tags": config.RepoTags()[r.Name]}, nil
}

func (c *Controller) repoOrg(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	if err := config.SetOrg(r.Name, a.S("org")); err != nil {
		return nil, err
	}
	c.col.Refresh(true)
	return map[string]any{"repo": r.Name, "org": config.RepoOrg(r.Name)}, nil
}

// repos: alla repon med taggar, för andra program (devctl repos --json).
func (c *Controller) reposAction(Args) (any, error) {
	type info struct {
		Name       string   `json:"name"`
		Path       string   `json:"path"`
		GitHub     string   `json:"github"`
		Tags       []string `json:"tags"`
		Compliance string   `json:"compliance"` // "local-only" eller ""
		Org        string   `json:"org"`
		Active     bool     `json:"active"`
		Archived   bool     `json:"archived"`
	}
	out := []info{}
	for _, r := range c.col.Snapshot().Repos {
		i := info{Name: r.Name, Path: r.Path, GitHub: r.GitHub, Tags: r.Tags, Org: r.Org, Active: r.Active, Archived: r.Archived}
		if i.Tags == nil {
			i.Tags = []string{}
		}
		if config.Private(c.cfg, r.Name) {
			i.Compliance = config.TagLocalOnly
		}
		out = append(out, i)
	}
	return out, nil
}
