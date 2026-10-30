package config

// Taggar per repo, sparade i ~/.config/devctl/tags.json och lästa vid varje anrop (som arkivet), så att
// en ändring gäller direkt. Andra program (t.ex. Local-Automation) läser dem via devctl repos --json.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// TagLocalOnly: repots material får inte lämna Macen (inget i synkade valv, inget till molnet).
const TagLocalOnly = "local-only"

var (
	tagsMu sync.Mutex
	tagRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
)

func tagsPath() string { return filepath.Join(Dir(), "tags.json") }

type tagFile struct {
	Repos map[string][]string `json:"repos"`
	// Organisation per repo (t.ex. Halcy), valfri; grupperar repon för andra program
	Orgs map[string]string `json:"orgs,omitempty"`
}

func readTags() tagFile {
	var f tagFile
	if b, err := os.ReadFile(tagsPath()); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	if f.Repos == nil {
		f.Repos = map[string][]string{}
	}
	if f.Orgs == nil {
		f.Orgs = map[string]string{}
	}
	return f
}

func writeTags(f tagFile) error {
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	tmp := tagsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, tagsPath())
}

// RepoOrg är repots organisation ("" om ingen).
func RepoOrg(repo string) string {
	tagsMu.Lock()
	defer tagsMu.Unlock()
	return readTags().Orgs[repo]
}

// SetOrg sätter (eller tar bort, med "") repots organisation.
func SetOrg(repo, org string) error {
	tagsMu.Lock()
	defer tagsMu.Unlock()
	f := readTags()
	if org = strings.TrimSpace(org); org == "" {
		delete(f.Orgs, repo)
	} else {
		f.Orgs[repo] = org
	}
	return writeTags(f)
}

// RepoTags returnerar taggarna per repo (repots namn).
func RepoTags() map[string][]string {
	tagsMu.Lock()
	defer tagsMu.Unlock()
	return readTags().Repos
}

func HasTag(repo, tag string) bool {
	for r, tags := range RepoTags() {
		if strings.EqualFold(r, repo) {
			for _, t := range tags {
				if t == tag {
					return true
				}
			}
		}
	}
	return false
}

// SetTag lägger till eller tar bort en tagg på ett repo.
func SetTag(repo, tag string, on bool) error {
	if !tagRe.MatchString(tag) {
		return fmt.Errorf("ogiltig tagg %q (gemener, siffror och -)", tag)
	}
	tagsMu.Lock()
	defer tagsMu.Unlock()
	f := readTags()
	all := f.Repos
	set := map[string]bool{}
	for _, t := range all[repo] {
		set[t] = true
	}
	if on {
		set[tag] = true
	} else {
		delete(set, tag)
	}
	list := []string{}
	for t := range set {
		list = append(list, t)
	}
	sort.Strings(list)
	if len(list) == 0 {
		delete(all, repo)
	} else {
		all[repo] = list
	}
	return writeTags(f)
}
