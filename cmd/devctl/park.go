package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

// parkTarget väljer listan: --repo, annars repot som den aktuella katalogen ligger i, annars den allmänna.
func parkTarget(cfg config.Config, flags map[string]string) (name, path string) {
	name = flags["repo"]
	if name == "" {
		dir, _ := os.Getwd()
		if out, err := sysx.RunIn(dir, 5*time.Second, "git", "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
			common := strings.TrimSpace(out)
			if filepath.Base(common) == ".git" {
				name = filepath.Base(filepath.Dir(common))
			}
		}
	}
	if name == "" {
		return "Allmänt", config.ParkFile(cfg, "")
	}
	return name, config.ParkFile(cfg, state.RepoDir(cfg, name))
}

// park: devctl park "<text>" | park list [--all] | park done <id> | park open <id>   (alla tar --repo)
func park(cfg config.Config, pos []string, flags map[string]string) {
	name, path := parkTarget(cfg, flags)
	sub := ""
	if len(pos) > 0 {
		sub = pos[0]
	}
	switch sub {
	case "", "list":
		items, err := config.ReadParked(path)
		must(err)
		_, all := flags["all"]
		n := 0
		for _, it := range items {
			if it.Done && !all {
				continue
			}
			n++
			box := "[ ]"
			if it.Done {
				box = "[x]"
			}
			from := ""
			if it.From != "" {
				from = " (från " + it.From + ")"
			}
			fmt.Printf("%s %s  %s · %s%s\n", box, it.ID, it.Date, it.Text, from)
		}
		if n == 0 {
			fmt.Printf("Inget parkerat för %s.\n", name)
		} else {
			fmt.Printf("\n%d för %s · %s\n", n, name, path)
		}
	case "done", "undo", "open":
		if len(pos) < 2 {
			fail(errors.New("användning: devctl park done <id>"))
		}
		must(config.SetParkedDone(path, pos[1], sub == "done"))
		fmt.Printf("%s: %s\n", map[bool]string{true: "avbockad", false: "öppen igen"}[sub == "done"], pos[1])
	default:
		text := strings.Join(pos, " ")
		if sub == "add" {
			text = strings.Join(pos[1:], " ")
		}
		it, err := config.AddParked(path, name, text, flags["from"])
		must(err)
		fmt.Printf("Parkerat för %s: %s (id %s)\n", name, it.Text, it.ID)
	}
}
