package state

import "github.com/erikjin-halcy/Local-Development/internal/config"

// Workflow är ett lokalt workflow (från repots YAML-fil) som det visas i gränssnittet.
type Workflow struct {
	ID     string                `json:"id"`
	Label  string                `json:"label"`
	Danger bool                  `json:"danger"`
	Steps  []config.WorkflowStep `json:"steps"`
}

// Workflows gör om YAML-filens workflows; workflows utan namn eller steg hoppas över.
func Workflows(list []config.CustomWorkflow) []Workflow {
	out := []Workflow{}
	for _, w := range list {
		if w.Label == "" || len(w.Steps) == 0 {
			continue
		}
		out = append(out, Workflow{ID: "workflow:" + w.Label, Label: w.Label, Danger: w.Danger, Steps: w.Steps})
	}
	return out
}
