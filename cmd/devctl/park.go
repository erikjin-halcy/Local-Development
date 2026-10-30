package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

// currentRepo: --repo, annars repot som den aktuella katalogen ligger i ("" utanför repos).
func currentRepo(flags map[string]string) string {
	if r := flags["repo"]; r != "" {
		return r
	}
	dir, _ := os.Getwd()
	if out, err := sysx.RunIn(dir, 5*time.Second, "git", "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		if common := strings.TrimSpace(out); filepath.Base(common) == ".git" {
			return filepath.Base(filepath.Dir(common))
		}
	}
	return ""
}

// todo: devctl todo add "<titel>" [--repo r] [--context "…"] [--background "…"] [--from "…"]
//
//	devctl todo [list] [--all] | done|start|open <id>
//
// devctl park "<text>" är samma sak som todo add (för skillen parkera).
func todo(cfg config.Config, pos []string, flags map[string]string, park bool) {
	sub := "list"
	if park {
		sub = "add"
		if len(pos) > 0 && (pos[0] == "list" || pos[0] == "done" || pos[0] == "open" || pos[0] == "start") {
			sub, pos = pos[0], pos[1:]
		}
	} else if len(pos) > 0 {
		sub, pos = pos[0], pos[1:]
	}
	switch sub {
	case "list", "ls":
		_, all := flags["all"]
		repo := flags["repo"]
		n := 0
		for _, t := range config.Todos(cfg) {
			if (t.Status == "klar" && !all) || (repo != "" && !strings.EqualFold(t.Repo, repo)) {
				continue
			}
			n++
			box := map[string]string{"öppen": "[ ]", "pågår": "[/]", "klar": "[x]"}[t.Status]
			r := ""
			if t.Repo != "" {
				r = " · " + t.Repo
			}
			fmt.Printf("%s %s  %s%s · %s\n", box, t.ID, t.Title, r, t.Created)
		}
		if n == 0 {
			fmt.Println("Inget att göra.")
		} else {
			fmt.Printf("\n%d · %s\n", n, config.TodoIndex(cfg))
		}
	case "add":
		title := strings.Join(pos, " ")
		if strings.TrimSpace(title) == "" {
			fail(errors.New(`användning: devctl todo add "<titel>" [--context "…"] [--background "…"] [--repo r]`))
		}
		src := flags["from"]
		if src == "" {
			src = "devctl"
		}
		t, err := config.AddTodo(cfg, title, currentRepo(flags), flags["context"], flags["background"], src)
		must(err)
		fmt.Printf("Tillagt i To do: %s (id %s)\n%s\n", t.Title, t.ID, t.File)
	case "done", "start", "open", "reopen":
		if len(pos) < 1 {
			fail(fmt.Errorf("användning: devctl todo %s <id>", sub))
		}
		status := map[string]string{"done": "klar", "start": "pågår", "open": "öppen", "reopen": "öppen"}[sub]
		t, err := config.SetTodoStatus(cfg, pos[0], status)
		must(err)
		fmt.Printf("%s: %s\n", t.Title, status)
	default:
		fail(fmt.Errorf("okänt: devctl todo %s (list, add, start, done, open)", sub))
	}
}
