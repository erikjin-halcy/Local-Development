package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/control"
)

// automation kör Local-Automations kommandon direkt i terminalen: `devctl automation <kommando> [argument]`.
// Utan kommando visas listan. Webbgränssnittet kör samma kommandon (i ett tmux-fönster).
func automation(cfg config.Config, pos []string) {
	if len(pos) == 0 {
		group := ""
		fmt.Println("devctl automation <kommando> [argument]   – Local-Automations kommandon")
		for _, c := range control.AutomationCommands() {
			if c.Group != group {
				group = c.Group
				fmt.Printf("\n%s\n", group)
			}
			arg := ""
			if c.Arg != "" {
				arg = "<" + c.Arg + ">"
				if len(c.Choices) > 0 {
					arg = "<" + strings.Join(c.Choices, "|") + ">"
				}
				if c.Optional {
					arg = "[" + strings.Trim(arg, "<>") + "]"
				}
			}
			fmt.Printf("  %-18s %-16s %s\n", c.ID, arg, c.Help)
		}
		return
	}
	arg := ""
	if len(pos) > 1 {
		arg = pos[1]
	}
	_, dir, argv, err := control.ResolveAutomation(cfg, pos[0], arg)
	must(err)
	if argv == nil {
		fmt.Println(control.ServiceStatus())
		return
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			os.Exit(e.ExitCode())
		}
		fail(err)
	}
}
