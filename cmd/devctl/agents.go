package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// agentsCmd: devctl agents [list] | run <namn> [--repo r] | findings [namn] [--all] | resolve|ignore|reopen <id>
func agentsCmd(c client, pos []string, flags map[string]string) {
	sub := "list"
	if len(pos) > 0 {
		sub = pos[0]
	}
	switch sub {
	case "list", "findings":
		res, err := c.do("agents", nil)
		must(err)
		b, _ := json.Marshal(res)
		var v struct {
			Agents []struct {
				Name, Label, Icon, Every, Notify, Error string
				Enabled                                 bool
				State                                   struct {
					LastRun, Current string
					Running          bool
					Findings         []struct {
						ID, Repo, Branch, Severity, Rule, File, Finding, Suggestion, Status string
						Line                                                                int
					}
				}
			}
		}
		_ = json.Unmarshal(b, &v)
		_, all := flags["all"]
		for _, a := range v.Agents {
			if sub == "findings" && len(pos) > 1 && pos[1] != a.Name {
				continue
			}
			open := 0
			for _, f := range a.State.Findings {
				if f.Status == "öppen" {
					open++
				}
			}
			status := "av"
			if a.Enabled {
				status = "på, var " + a.Every
			}
			if a.State.Running {
				status += ", kör nu: " + a.State.Current
			}
			if a.Error != "" {
				status = "fel: " + a.Error
			}
			fmt.Printf("%s %s (%s) · %s · senast %s · %d öppna fynd\n", a.Icon, a.Label, a.Name, status, a.State.LastRun, open)
			if sub != "findings" {
				continue
			}
			for _, f := range a.State.Findings {
				if f.Status != "öppen" && !all {
					continue
				}
				loc := f.File
				if f.Line > 0 {
					loc += fmt.Sprintf(":%d", f.Line)
				}
				fmt.Printf("  [%s] %s  %s@%s %s · %s\n      %s\n", f.Severity, f.ID, f.Repo, f.Branch, loc, f.Rule, f.Finding)
				if f.Suggestion != "" {
					fmt.Printf("      → %s\n", f.Suggestion)
				}
				if f.Status != "öppen" {
					fmt.Printf("      (%s)\n", f.Status)
				}
			}
		}
	case "run":
		if len(pos) < 2 {
			fail(errors.New("användning: devctl agents run <namn> [--repo r]"))
		}
		_, err := c.do("agent_run", map[string]any{"name": pos[1], "repo": flags["repo"]})
		must(err)
		fmt.Printf("%s körs i bakgrunden; se devctl agents findings %s\n", pos[1], pos[1])
	case "resolve", "ignore", "reopen":
		if len(pos) < 2 {
			fail(fmt.Errorf("användning: devctl agents %s <id>", sub))
		}
		res, err := c.do("agent_finding", map[string]any{"op": sub, "id": pos[1]})
		must(err)
		m := res.(map[string]any)
		fmt.Printf("%v: %v\n", m["id"], m["status"])
	default:
		fail(fmt.Errorf("okänt: devctl agents %s (list, run, findings, resolve, ignore, reopen)", strings.Join(pos, " ")))
	}
}
