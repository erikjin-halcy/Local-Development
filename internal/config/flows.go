package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Arbetsflöden per repo: i vilken ordning repots Claude-skills används (/start → /implement → … → /pr).
// En YAML-fil per repo i ~/.config/devctl/workflows/<repo>.yaml. Filen ligger hos devctl, aldrig i repot,
// och är något annat än workflows i actions-filen (som är kommandokedjor i tmux).

// FlowStep är ett steg i arbetsflödet med de skills som hör till steget.
type FlowStep struct {
	Label  string   `yaml:"label" json:"label"`
	Skills []string `yaml:"skills" json:"skills"`
	// Lägen där steget är det aktuella: fresh, changes, committed, pushed, pr, landed
	When []string `yaml:"when,omitempty" json:"when,omitempty"`
}

// FlowGroup är skills som inte hör till ett visst steg, t.ex. planering eller drift.
type FlowGroup struct {
	Label  string   `yaml:"label" json:"label"`
	Skills []string `yaml:"skills" json:"skills"`
}

// FlowLinear säger hur branchnamn kopplas till Linear-ärenden: team-nyckeln (HAL) och ärendelänkens början.
type FlowLinear struct {
	Team string `yaml:"team" json:"team"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

// Flow är repots arbetsflöde.
type Flow struct {
	Label  string      `yaml:"label,omitempty" json:"label,omitempty"`
	Linear *FlowLinear `yaml:"linear,omitempty" json:"linear,omitempty"`
	// Skills som deployar, mergar eller tar bort något; de ska bara föreslås och kräva bekräftelse
	Confirm []string    `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	Steps   []FlowStep  `yaml:"steps" json:"steps"`
	Groups  []FlowGroup `yaml:"groups,omitempty" json:"groups,omitempty"`
}

func FlowsDir() string { return filepath.Join(Dir(), "workflows") }

// FlowFile är sökvägen till repots arbetsflödesfil.
func FlowFile(repo string) string { return filepath.Join(FlowsDir(), safeName(repo)+".yaml") }

// RepoFlow läser repots arbetsflöde (nil om filen saknas). Fel i filen returneras.
func RepoFlow(repo string) (*Flow, error) {
	b, err := os.ReadFile(FlowFile(repo))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var f Flow
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// FlowTemplate är innehållet i en ny arbetsflödesfil; skills är repots skills som förslag.
func FlowTemplate(repo string, skills []string) string {
	list := ""
	for _, s := range skills {
		list += "#   " + s + "\n"
	}
	if list == "" {
		list = "#   (inga hittades i .claude/skills/)\n"
	}
	return `# Arbetsflöde för ` + repo + `: i vilken ordning repots Claude-skills används.
# Filen ligger bara på den här datorn (~/.config/devctl/workflows/), inte i repot.
#
# steps:   stegen i ordning, med de skills som hör till varje steg
# when:    lägen där steget är det aktuella för en worktree:
#            fresh     inga egna commits än
#            changes   ocommittade ändringar
#            committed commits som inte är pushade
#            pushed    pushad, ingen PR
#            pr        öppen PR
#            landed    branchens commits finns på huvudbranchen
# groups:  skills utanför stegen, t.ex. planering och drift
# confirm: skills som deployar eller mergar; de föreslås bara och kräver bekräftelse
# linear:  team-nyckel (branchen feat/hal-12-x → HAL-12) och början på ärendelänken
#
# Repots skills:
` + list + `
steps:
  # - label: Bygg
  #   skills: [implement]
  #   when: [fresh]
  # - label: Committa
  #   skills: [commit]
  #   when: [changes]
groups:
  # - label: Planering
  #   skills: [to-spec]
confirm: []
`
}
