package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

// CIRun är en körning i GitHub Actions.
type CIRun struct {
	ID         int64  `json:"id"`
	Workflow   string `json:"workflow"`
	Title      string `json:"title"`
	Branch     string `json:"branch"`
	Status     string `json:"status"`     // queued, in_progress, completed …
	Conclusion string `json:"conclusion"` // success, failure, cancelled, skipped …
	Event      string `json:"event"`
	URL        string `json:"url"`
	Created    string `json:"created"`
}

// CI är läget för en branch: den senaste körningen per workflow.
type CI struct {
	State string  `json:"state"` // success, failure, running, none
	Runs  []CIRun `json:"runs"`
}

// DispatchWorkflow är en workflow som går att starta för hand (on: workflow_dispatch).
type DispatchWorkflow struct {
	File   string          `json:"file"`
	Name   string          `json:"name"`
	Inputs []DispatchInput `json:"inputs"`
}

type DispatchInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Default     string   `json:"default"`
	Type        string   `json:"type"`
	Options     []string `json:"options"`
}

func loadRuns(repo string) ([]CIRun, error) {
	out, err := sysx.Run(20*time.Second, "gh", "run", "list", "-R", repo, "--limit", "60",
		"--json", "databaseId,workflowName,displayTitle,headBranch,status,conclusion,event,url,createdAt")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID         int64  `json:"databaseId"`
		Workflow   string `json:"workflowName"`
		Title      string `json:"displayTitle"`
		Branch     string `json:"headBranch"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Event      string `json:"event"`
		URL        string `json:"url"`
		Created    string `json:"createdAt"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	runs := make([]CIRun, 0, len(raw))
	for _, r := range raw {
		runs = append(runs, CIRun(r))
	}
	return runs, nil
}

// ciFor sammanfattar en branch: senaste körningen per workflow (listan är nyast först).
func ciFor(runs []CIRun, branch string) *CI {
	if branch == "" {
		return nil
	}
	seen := map[string]bool{}
	ci := &CI{State: "none", Runs: []CIRun{}}
	for _, r := range runs {
		if r.Branch != branch || seen[r.Workflow] {
			continue
		}
		seen[r.Workflow] = true
		ci.Runs = append(ci.Runs, r)
	}
	if len(ci.Runs) == 0 {
		return nil
	}
	ci.State = "success"
	for _, r := range ci.Runs {
		switch {
		case r.Status != "completed":
			if ci.State != "failure" {
				ci.State = "running"
			}
		case r.Conclusion == "failure" || r.Conclusion == "timed_out" || r.Conclusion == "startup_failure":
			ci.State = "failure"
		}
	}
	sort.SliceStable(ci.Runs, func(i, j int) bool { return ci.Runs[i].Workflow < ci.Runs[j].Workflow })
	return ci
}

// dispatchWorkflows läser repots .github/workflows och tar fram de som kan startas för hand.
func dispatchWorkflows(root string) []DispatchWorkflow {
	files, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	out := []DispatchWorkflow{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), "workflow_dispatch") {
			continue
		}
		var doc struct {
			Name string    `yaml:"name"`
			On   yaml.Node `yaml:"on"`
		}
		if yaml.Unmarshal(b, &doc) != nil {
			continue
		}
		w := DispatchWorkflow{File: filepath.Base(f), Name: doc.Name, Inputs: []DispatchInput{}}
		if w.Name == "" {
			w.Name = strings.TrimSuffix(w.File, filepath.Ext(w.File))
		}
		found := false
		switch doc.On.Kind {
		case yaml.ScalarNode:
			found = doc.On.Value == "workflow_dispatch"
		case yaml.SequenceNode:
			for _, n := range doc.On.Content {
				found = found || n.Value == "workflow_dispatch"
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(doc.On.Content); i += 2 {
				if doc.On.Content[i].Value != "workflow_dispatch" {
					continue
				}
				found = true
				var spec struct {
					Inputs map[string]struct {
						Description string   `yaml:"description"`
						Required    bool     `yaml:"required"`
						Default     any      `yaml:"default"`
						Type        string   `yaml:"type"`
						Options     []string `yaml:"options"`
					} `yaml:"inputs"`
				}
				_ = doc.On.Content[i+1].Decode(&spec)
				for name, in := range spec.Inputs {
					def := ""
					if in.Default != nil {
						def = strings.TrimSpace(strings.ReplaceAll(yamlString(in.Default), "\n", " "))
					}
					w.Inputs = append(w.Inputs, DispatchInput{Name: name, Description: in.Description, Required: in.Required,
						Default: def, Type: in.Type, Options: in.Options})
				}
				sort.Slice(w.Inputs, func(a, b int) bool { return w.Inputs[a].Name < w.Inputs[b].Name })
			}
		}
		if found {
			out = append(out, w)
		}
	}
	return out
}

func yamlString(v any) string {
	b, _ := yaml.Marshal(v)
	return strings.TrimSpace(string(b))
}
