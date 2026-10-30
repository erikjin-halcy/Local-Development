package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Egna actions ligger lokalt, en YAML-fil per repo: ~/.config/devctl/actions/<repo>.yaml.
// De läggs aldrig i repot, så teamets repos påverkas inte.

// CustomAction är en egen action för ett repo.
type CustomAction struct {
	Label  string `yaml:"label" json:"label"`
	Run    string `yaml:"run" json:"run"`
	Dir    string `yaml:"dir,omitempty" json:"dir,omitempty"`
	Group  string `yaml:"group,omitempty" json:"group,omitempty"`
	Danger bool   `yaml:"danger,omitempty" json:"danger,omitempty"`
}

func ActionsDir() string { return filepath.Join(Dir(), "actions") }

func safeName(repo string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(repo)
}

// ActionsFile är sökvägen till repots fil med egna actions.
func ActionsFile(repo string) string { return filepath.Join(ActionsDir(), safeName(repo)+".yaml") }

// RepoActions läser repots egna actions (inga om filen saknas). Fel i filen returneras.
func RepoActions(repo string) ([]CustomAction, error) {
	b, err := os.ReadFile(ActionsFile(repo))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var f struct {
		Actions []CustomAction `yaml:"actions"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f.Actions, nil
}

// ActionsTemplate är innehållet i en ny actions-fil.
func ActionsTemplate(repo string) string {
	return `# Egna actions för ` + repo + `. De visas under "Egna" i Kör-menyn i devctl.
# Filen ligger bara på den här datorn (~/.config/devctl/actions/), inte i repot.
#
# label:  namnet i menyn
# run:    kommandot (körs i ett tmux-fönster i den valda worktreen)
# dir:    underkatalog att köra i (valfri)
# group:  Egna, Starta, Deploy, Bygg, Kontroll, Databas eller Övrigt (valfri, standard Egna)
# danger: true = fråga innan (deploy, migrering, borttagning)

actions:
  # - label: Deploy staging
  #   run: ./scripts/deploy.sh staging
  #   group: Deploy
  #   danger: true
  # - label: Metro (rensa cache)
  #   run: npx expo start --clear
  #   dir: apps/mobile
`
}
