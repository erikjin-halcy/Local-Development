package state

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Skill är ett Claude-skill i repot (.claude/skills/<namn>/SKILL.md).
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type skillCache struct {
	mod  int64
	list []Skill
}

var (
	skillsMu sync.Mutex
	skillsBy = map[string]skillCache{}
)

// RepoSkills läser skills i repots .claude/skills/. Listan läses bara om när någon SKILL.md eller katalogen ändrats.
func RepoSkills(root string) []Skill {
	dir := filepath.Join(root, ".claude", "skills")
	files, _ := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	var mod int64
	if st, err := os.Stat(dir); err == nil {
		mod = st.ModTime().UnixNano()
	}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.ModTime().UnixNano() > mod {
			mod = st.ModTime().UnixNano()
		}
	}
	skillsMu.Lock()
	c, ok := skillsBy[dir]
	skillsMu.Unlock()
	if ok && c.mod == mod {
		return c.list
	}
	list := []Skill{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := parseSkill(string(b))
		if s.Name == "" {
			s.Name = filepath.Base(filepath.Dir(f))
		}
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	skillsMu.Lock()
	skillsBy[dir] = skillCache{mod: mod, list: list}
	skillsMu.Unlock()
	return list
}

// parseSkill läser name och description ur frontmatter i en SKILL.md.
func parseSkill(text string) Skill {
	var s Skill
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return s
	}
	front, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return s
	}
	var m struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if yaml.Unmarshal([]byte(front), &m) == nil {
		s.Name, s.Description = strings.TrimSpace(m.Name), strings.TrimSpace(m.Description)
	}
	return s
}

// Lägen för en worktree i arbetsflödet, härledda från git och GitHub.
const (
	StageFresh     = "fresh"     // inga egna commits än
	StageChanges   = "changes"   // ocommittade ändringar
	StageCommitted = "committed" // commits som inte är pushade
	StagePushed    = "pushed"    // pushad, ingen PR
	StagePR        = "pr"        // öppen PR
	StageLanded    = "landed"    // branchens commits finns på huvudbranchen
)

// WorktreeStage härleder var worktreen är i arbetsflödet; "" på huvudbranchen (def) och för detached HEAD.
// Huvudkatalogen får ett läge när den står på en annan branch.
func WorktreeStage(w Worktree, def string) string {
	switch {
	case w.Branch == "" || w.Branch == def || w.Missing:
		return ""
	case w.Changes > 0:
		return StageChanges
	case w.PR != nil:
		return StagePR
	case w.Base == 0 && w.Landed:
		return StageLanded
	case w.Base == 0:
		return StageFresh
	case w.Upstream == "" || w.Ahead > 0:
		return StageCommitted
	default:
		return StagePushed
	}
}

// IssueKey plockar ut ett Linear-ärende ur branchnamnet, t.ex. feat/hal-12-slug → HAL-12.
func IssueKey(branch, team string) string {
	if team == "" {
		return ""
	}
	re := regexp.MustCompile(`(?i)(?:^|[/_-])(` + regexp.QuoteMeta(team) + `)-(\d+)(?:$|[/_-])`)
	m := re.FindStringSubmatch(branch)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}
