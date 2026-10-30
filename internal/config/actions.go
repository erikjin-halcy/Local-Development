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

// WorkflowStep är ett steg i ett lokalt workflow: en befintlig action (action) eller ett kommando (run).
type WorkflowStep struct {
	Action string `yaml:"action,omitempty" json:"action,omitempty"`
	Run    string `yaml:"run,omitempty" json:"run,omitempty"`
	Dir    string `yaml:"dir,omitempty" json:"dir,omitempty"`
	Label  string `yaml:"label,omitempty" json:"label,omitempty"`
	// Vad som ska hända innan nästa steg: "exit" (vänta tills steget är klart och stoppa om det misslyckas,
	// standard), "port 8080" (vänta tills porten svarar), "5s" (vänta så länge) eller "none".
	Wait string `yaml:"wait,omitempty" json:"wait,omitempty"`
}

// CustomWorkflow är ett lokalt workflow: flera steg i följd.
type CustomWorkflow struct {
	Label  string         `yaml:"label" json:"label"`
	Danger bool           `yaml:"danger,omitempty" json:"danger,omitempty"`
	Steps  []WorkflowStep `yaml:"steps" json:"steps"`
}

type actionsFile struct {
	Actions   []CustomAction   `yaml:"actions"`
	Workflows []CustomWorkflow `yaml:"workflows"`
}

func readActionsFile(repo string) (actionsFile, error) {
	var f actionsFile
	b, err := os.ReadFile(ActionsFile(repo))
	if os.IsNotExist(err) {
		return f, nil
	} else if err != nil {
		return f, err
	}
	return f, yaml.Unmarshal(b, &f)
}

// RepoActions läser repots egna actions (inga om filen saknas). Fel i filen returneras.
func RepoActions(repo string) ([]CustomAction, error) {
	f, err := readActionsFile(repo)
	return f.Actions, err
}

// RepoWorkflows läser repots lokala workflows.
func RepoWorkflows(repo string) ([]CustomWorkflow, error) {
	f, err := readActionsFile(repo)
	return f.Workflows, err
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

# Workflows: flera steg i följd, vart och ett i ett eget tmux-fönster.
# Ett steg är en befintlig action (namnet i Kör-menyn, t.ex. dev eller test) eller ett kommando (run).
# wait: exit (standard: vänta tills steget är klart, stoppa om det misslyckas),
#       port 8080 (vänta tills porten svarar), 5s (vänta), none (fortsätt direkt)
workflows:
  # - label: Starta allt
  #   steps:
  #     - action: dev:disposable-db
  #       wait: port 54322
  #     - action: dev
  #       wait: port 8080
  #     - action: Metro (start) · apps/mobile
  #       wait: none
  # - label: Release
  #   danger: true
  #   steps:
  #     - action: test
  #     - action: build
  #     - action: deploy
`
}
