package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Arkivet är de repos användaren har gömt från översikten, sparat i ~/.config/devctl/archive.json.

var archiveMu sync.Mutex

func archivePath() string { return filepath.Join(Dir(), "archive.json") }

// Archived returnerar de arkiverade reposens sökvägar.
func Archived() map[string]bool {
	archiveMu.Lock()
	defer archiveMu.Unlock()
	var f struct {
		Repos []string `json:"repos"`
	}
	set := map[string]bool{}
	if b, err := os.ReadFile(archivePath()); err == nil && json.Unmarshal(b, &f) == nil {
		for _, r := range f.Repos {
			set[r] = true
		}
	}
	return set
}

// SetArchived arkiverar eller återställer ett repo.
func SetArchived(path string, archived bool) error {
	set := Archived()
	archiveMu.Lock()
	defer archiveMu.Unlock()
	if archived {
		set[path] = true
	} else {
		delete(set, path)
	}
	list := make([]string, 0, len(set))
	for p := range set {
		list = append(list, p)
	}
	sort.Strings(list)
	b, _ := json.MarshalIndent(map[string][]string{"repos": list}, "", "  ")
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(archivePath(), append(b, '\n'), 0o644)
}
