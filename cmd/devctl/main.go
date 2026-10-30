// devctl: en kontrollpanel för lokalt utvecklingsarbete. Visar git-repos och worktrees, vilka
// tmux-panes, lokala servrar och Docker-containrar som hör till vilken branch, och styr en global
// Claude-orkestrator. Webbgränssnitt på http://localhost:7878 och CLI för skript och agenter.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/control"
	"github.com/erikjin-halcy/Local-Development/internal/server"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

const usage = `devctl – kontrollpanel för lokalt utvecklingsarbete

  devctl                         starta (om det behövs) och öppna webbgränssnittet
  devctl serve                   kör servern i förgrunden
  devctl stop                    stoppa servern
  devctl status [--all] [--json] repos, worktrees, panes, servrar och containrar
  devctl worker start <repo> <branch> [--base b] [--task "..."] [--cmd c]
  devctl worktree add <repo> <branch> [--base b] [--path p]
  devctl worktree rm <sökväg> [--force]
  devctl pane list | send <%%id> <text> | keys <%%id> <tangenter…> | capture <%%id> [-n rader]
  devctl graph <repo>            commitgraf över alla branches
  devctl actions <repo>          skript, make-mål, deploy och egna actions för repot
  devctl run <repo> <action> [--branch b] [--yes]   kör en action i branchens worktree (tmux)
  devctl note <repo> <branch> [--open]   branchens anteckning i Obsidian
  devctl log [--open]            uppdatera dagens utvecklingslogg i Obsidian
  devctl automation [kommando] [arg]   Local-Automation: veckoarkiv, synk, deploy, inloggningar, tjänster
  devctl park "<text>" [--repo r] [--from s]   kom ihåg en fråga eller ett önskemål till senare
  devctl park list [--all] | park done <id>    parkerat för repot du står i (eller --repo)
  devctl orchestrator            starta (eller visa) den globala orkestratorn
  devctl brief                  orkestratorns systemprompt
  devctl autostart [--remove]    starta servern vid inloggning (launchd)
  devctl config                  sökväg till konfigurationen

Konfiguration: %s
`

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(fmt.Errorf("kunde inte läsa %s: %w", config.Path(), err))
	}
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	pos, flags := parse(args)
	c := client{cfg: cfg}

	switch cmd {
	case "", "open":
		must(c.ensure())
		_ = exec.Command("open", fmt.Sprintf("http://localhost:%d", cfg.Port)).Run()
		fmt.Printf("devctl: http://localhost:%d\n", cfg.Port)
	case "serve":
		fail(server.Run(cfg))
	case "stop":
		stop()
	case "status":
		must(c.ensure())
		snap, err := c.state()
		must(err)
		if _, ok := flags["json"]; ok {
			out, _ := json.MarshalIndent(snap, "", "  ")
			fmt.Println(string(out))
			return
		}
		_, all := flags["all"]
		printStatus(snap, all)
	case "worker":
		if len(pos) < 3 || pos[0] != "start" {
			fail(errors.New("användning: devctl worker start <repo> <branch> [--base b] [--task t]"))
		}
		a := map[string]any{"repo": pos[1], "branch": pos[2], "base": flags["base"], "task": flags["task"]}
		if v, ok := flags["cmd"]; ok {
			a["cmd"] = v
		}
		printResult(c.do("worker_start", a))
	case "worktree":
		switch {
		case len(pos) >= 3 && pos[0] == "add":
			printResult(c.do("worktree_add", map[string]any{"repo": pos[1], "branch": pos[2], "base": flags["base"], "path": flags["path"]}))
		case len(pos) >= 2 && (pos[0] == "rm" || pos[0] == "remove"):
			_, force := flags["force"]
			printResult(c.do("worktree_remove", map[string]any{"path": abs(pos[1]), "force": force}))
		default:
			fail(errors.New("användning: devctl worktree add <repo> <branch> | rm <sökväg>"))
		}
	case "pane":
		paneCmd(c, pos, flags)
	case "actions":
		if len(pos) < 1 {
			fail(errors.New("användning: devctl actions <repo>"))
		}
		must(c.ensure())
		snap, err := c.state()
		must(err)
		r, err := control.FindRepo(snap, pos[0])
		must(err)
		group := ""
		for _, a := range r.Actions {
			if a.Group != group {
				group = a.Group
				fmt.Printf("\n%s\n", group)
			}
			warn := ""
			if a.Danger {
				warn = "  ⚠ kräver --yes"
			}
			fmt.Printf("  %-34s %s%s\n", trunc(a.Label, 34), trunc(a.Run, 60), warn)
		}
	case "run":
		if len(pos) < 2 {
			fail(errors.New("användning: devctl run <repo> <action> [--branch b] [--yes]"))
		}
		must(c.ensure())
		snap, err := c.state()
		must(err)
		r, err := control.FindRepo(snap, pos[0])
		must(err)
		path := r.Worktrees[0].Path
		if b := flags["branch"]; b != "" {
			found := false
			for _, wt := range r.Worktrees {
				if wt.Branch == b {
					path, found = wt.Path, true
				}
			}
			if !found {
				fail(fmt.Errorf("%s har ingen worktree för %s (devctl worktree add %s %s)", r.Name, b, r.Name, b))
			}
		}
		_, yes := flags["yes"]
		printResult(c.do("run_action", map[string]any{"path": path, "id": strings.Join(pos[1:], " "), "confirm": yes}))
	case "graph":
		if len(pos) < 1 {
			fail(errors.New("användning: devctl graph <repo>"))
		}
		res, err := c.do("graph", map[string]any{"repo": pos[0], "lines": flags["n"]})
		must(err)
		fmt.Print(res.(map[string]any)["text"])
	case "note":
		if len(pos) < 2 {
			fail(errors.New("användning: devctl note <repo> <branch> [--open]"))
		}
		_, open := flags["open"]
		res, err := c.do("note", map[string]any{"repo": pos[0], "branch": pos[1], "open": open})
		must(err)
		fmt.Println(res.(map[string]any)["path"])
	case "log":
		_, open := flags["open"]
		res, err := c.do("daily_log", map[string]any{"open": open})
		must(err)
		fmt.Println(res.(map[string]any)["path"])
	case "automation":
		automation(cfg, pos)
	case "park":
		park(cfg, pos, flags)
	case "orchestrator":
		res, err := c.do("orchestrator_start", nil)
		must(err)
		o := res.(map[string]any)
		fmt.Printf("Orkestratorn körs i tmux-sessionen %v, pane %v.\nAttacha: tmux attach -t %v   eller chatta i http://localhost:%d\n", o["session"], o["pane"], o["session"], cfg.Port)
	case "brief":
		res, err := c.do("brief", nil)
		must(err)
		fmt.Print(res.(map[string]any)["text"])
	case "autostart":
		_, remove := flags["remove"]
		must(autostart(cfg, remove))
	case "config":
		fmt.Println(config.Path())
	case "help", "-h", "--help":
		fmt.Printf(usage, config.Path())
	default:
		fmt.Fprintf(os.Stderr, usage, config.Path())
		os.Exit(2)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "devctl:", err)
		os.Exit(1)
	}
}

func must(err error) { fail(err) }

func abs(p string) string {
	p = config.Home(p)
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// parse delar upp argument i positionella och --flaggor. Flaggor utan värde får "".
func parse(args []string) ([]string, map[string]string) {
	var pos []string
	flags := map[string]string{}
	boolean := map[string]bool{"json": true, "all": true, "force": true, "open": true, "remove": true, "yes": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 && !strings.HasPrefix(a, "-%") {
			k := strings.TrimLeft(a, "-")
			if k2, v, ok := strings.Cut(k, "="); ok {
				flags[k2] = v
			} else if boolean[k] || i+1 >= len(args) {
				flags[k] = ""
			} else {
				flags[k] = args[i+1]
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return pos, flags
}

// ---------- klient mot servern ----------

type client struct{ cfg config.Config }

func (c client) url(p string) string { return fmt.Sprintf("http://127.0.0.1:%d%s", c.cfg.Port, p) }

func (c client) healthy() bool {
	hc := http.Client{Timeout: 700 * time.Millisecond}
	r, err := hc.Get(c.url("/api/health"))
	if err != nil {
		return false
	}
	defer r.Body.Close()
	// Kontrollera att det är devctl som svarar och inte något annat på porten
	var h struct {
		Port int `json:"port"`
	}
	return r.StatusCode == 200 && json.NewDecoder(r.Body).Decode(&h) == nil && h.Port == c.cfg.Port
}

// ensure startar servern i bakgrunden om den inte redan körs.
func (c client) ensure() error {
	if c.healthy() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(config.CacheDir(), "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "serve")
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(config.CacheDir(), "server.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0o644)
	for i := 0; i < 50; i++ {
		if c.healthy() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("servern startade inte, se %s", filepath.Join(config.CacheDir(), "server.log"))
}

func (c client) state() (state.Snapshot, error) {
	var s state.Snapshot
	hc := http.Client{Timeout: 60 * time.Second}
	r, err := hc.Get(c.url("/api/state?fresh=1"))
	if err != nil {
		return s, err
	}
	defer r.Body.Close()
	return s, json.NewDecoder(r.Body).Decode(&s)
}

func (c client) do(action string, a map[string]any) (any, error) {
	if err := c.ensure(); err != nil {
		return nil, err
	}
	if a == nil {
		a = map[string]any{}
	}
	a["action"] = action
	body, _ := json.Marshal(a)
	req, _ := http.NewRequest("POST", c.url("/api/action"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(server.Header, "1")
	hc := http.Client{Timeout: 3 * time.Minute}
	r, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	var res struct {
		Result any    `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("oväntat svar: %s", raw)
	}
	if res.Error != "" {
		return res.Result, errors.New(res.Error)
	}
	return res.Result, nil
}

func printResult(res any, err error) {
	must(err)
	if res == nil {
		fmt.Println("ok")
		return
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}

func paneCmd(c client, pos []string, flags map[string]string) {
	if len(pos) == 0 {
		fail(errors.New("användning: devctl pane list | send | keys | capture"))
	}
	switch pos[0] {
	case "list":
		must(c.ensure())
		snap, err := c.state()
		must(err)
		for _, s := range snap.Sessions {
			for _, w := range s.Windows {
				for _, p := range w.Panes {
					branch := ""
					if p.Worktree != nil {
						branch = p.Worktree.Repo + "@" + p.Worktree.Branch
					}
					fmt.Printf("%-5s %s:%d %-22s %-10s %-14s %-30s %s\n", p.ID, s.Name, w.Index, trunc(w.Name, 22), p.Command, p.Role, branch, trunc(p.Title, 40))
				}
			}
		}
	case "send":
		if len(pos) < 3 {
			fail(errors.New("användning: devctl pane send <%id> <text>"))
		}
		printResult(c.do("send", map[string]any{"pane": pos[1], "text": strings.Join(pos[2:], " "), "enter": true}))
	case "keys":
		if len(pos) < 3 {
			fail(errors.New("användning: devctl pane keys <%id> <tangenter…>"))
		}
		printResult(c.do("keys", map[string]any{"pane": pos[1], "keys": strings.Join(pos[2:], " ")}))
	case "capture":
		if len(pos) < 2 {
			fail(errors.New("användning: devctl pane capture <%id> [-n rader]"))
		}
		n := 120
		if v, ok := flags["n"]; ok {
			fmt.Sscan(v, &n)
		}
		res, err := c.do("capture", map[string]any{"pane": pos[1], "lines": n})
		must(err)
		fmt.Print(strings.TrimRight(res.(map[string]any)["text"].(string), "\n") + "\n")
	default:
		fail(fmt.Errorf("okänt pane-kommando: %s", pos[0]))
	}
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func short(p string) string {
	h, _ := os.UserHomeDir()
	if strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

// printStatus skriver en kompakt översikt, tänkt för både människor och agenter.
func printStatus(s state.Snapshot, all bool) {
	paneInfo := map[string]string{}
	for _, se := range s.Sessions {
		for _, w := range se.Windows {
			for _, p := range w.Panes {
				paneInfo[p.ID] = fmt.Sprintf("%s(%s:%s %s)", p.ID, se.Name, w.Name, p.Command)
			}
		}
	}
	srvInfo := map[int]string{}
	for _, sv := range s.Servers {
		srvInfo[sv.Port] = fmt.Sprintf(":%d %s", sv.Port, sv.Kind)
	}
	if s.Orchestrator != nil {
		fmt.Printf("Orkestrator: %s i %s\n", s.Orchestrator.Pane, s.Orchestrator.Session)
	} else {
		fmt.Println("Orkestrator: inte startad (devctl orchestrator)")
	}
	fmt.Println()
	hidden := 0
	for _, r := range s.Repos {
		if (!r.Active || r.Archived) && !all {
			hidden++
			continue
		}
		extra := ""
		if r.GitHub != "" {
			extra = " · " + r.GitHub
		}
		if len(r.PRs) > 0 {
			extra += fmt.Sprintf(" · %d öppna PR", len(r.PRs))
		}
		if r.Parked > 0 {
			extra += fmt.Sprintf(" · 📌 %d parkerade (devctl park list --repo %q)", r.Parked, r.Name)
		}
		fmt.Printf("■ %s  %s%s\n", r.Name, short(r.Path), extra)
		for _, wt := range r.Worktrees {
			if wt.Missing {
				continue
			}
			b := wt.Branch
			if b == "" {
				b = "(" + wt.Head + ")"
			}
			var parts []string
			if wt.Changes > 0 {
				parts = append(parts, fmt.Sprintf("±%d", wt.Changes))
			}
			if wt.Ahead+wt.Behind > 0 {
				parts = append(parts, fmt.Sprintf("↑%d↓%d", wt.Ahead, wt.Behind))
			}
			if wt.PR != nil {
				parts = append(parts, fmt.Sprintf("PR #%d", wt.PR.Number))
			}
			if wt.Last.When != "" {
				parts = append(parts, wt.Last.When)
			}
			fmt.Printf("  ├ %-40s %s  [%s]\n", b, short(wt.Path), strings.Join(parts, " "))
			var links []string
			for _, p := range wt.Panes {
				links = append(links, paneInfo[p])
			}
			for i, p := range wt.Servers {
				if i == 8 {
					links = append(links, fmt.Sprintf("+%d portar till", len(wt.Servers)-8))
					break
				}
				links = append(links, srvInfo[p])
			}
			for _, ct := range wt.Containers {
				links = append(links, "🐳"+ct)
			}
			if len(links) > 0 {
				fmt.Printf("  │   ↳ %s\n", strings.Join(links, ", "))
			}
		}
	}
	if hidden > 0 {
		fmt.Printf("(%d inaktiva eller arkiverade repos dolda, --all visar dem)\n", hidden)
	}

	fmt.Println("\nServrar utan repo:")
	n := 0
	for _, sv := range s.Servers {
		if sv.App || sv.Worktree != nil {
			continue
		}
		n++
		fmt.Printf("  :%-6d %-9s %-14s %s\n", sv.Port, sv.Kind, sv.Process, trunc(sv.Command, 70))
	}
	if n == 0 {
		fmt.Println("  (inga)")
	}

	fmt.Printf("\nDocker (%s):", s.Docker.Context)
	if s.Docker.Error != "" {
		fmt.Printf(" %s\n", s.Docker.Error)
	} else {
		groups := map[string][]string{}
		for _, ct := range s.Docker.Containers {
			if ct.State != "running" && !all {
				continue
			}
			g := ct.Project
			if g == "" {
				g = "fristående"
			}
			groups[g] = append(groups[g], ct.Name)
		}
		keys := make([]string, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println()
		for _, k := range keys {
			fmt.Printf("  %s: %d (%s)\n", k, len(groups[k]), trunc(strings.Join(groups[k], ", "), 90))
		}
		if len(keys) == 0 {
			fmt.Println("  (inga igång)")
		}
	}
	if a := s.Automation; a.URL != "" {
		if !a.Online {
			fmt.Printf("\nLocal-Automation: inte igång (%s)\n", a.URL)
		} else {
			on, failed := 0, 0
			for _, t := range a.Tasks {
				if t.Enabled {
					on++
				}
				if t.LastRun != nil && t.LastRun.Exit != nil && *t.LastRun.Exit != 0 {
					failed++
				}
			}
			fmt.Printf("\nLocal-Automation: %d uppgifter, %d schemalagda, %d misslyckades senast (%s)\n", len(a.Tasks), on, failed, a.URL)
		}
	}
	fmt.Println("\ntmux:")
	for _, se := range s.Sessions {
		var ws []string
		for _, w := range se.Windows {
			k := ""
			if w.Kind != "" {
				k = "[" + w.Kind + "]"
			}
			ws = append(ws, fmt.Sprintf("%d:%s%s", w.Index, w.Name, k))
		}
		fmt.Printf("  %s: %s\n", se.Name, strings.Join(ws, "  "))
	}
}

func stop() {
	b, err := os.ReadFile(filepath.Join(config.CacheDir(), "server.pid"))
	var pid int
	if err == nil {
		fmt.Sscan(string(b), &pid)
	}
	if pid > 1 && syscall.Kill(pid, syscall.SIGTERM) == nil {
		fmt.Println("devctl stoppad")
		return
	}
	// Reserv: pid-filen är inaktuell; stoppa den devctl-process som lyssnar på porten
	cfg, _ := config.Load()
	if out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", cfg.Port), "-sTCP:LISTEN", "-t").Output(); err == nil {
		for _, f := range strings.Fields(string(out)) {
			var p int
			fmt.Sscan(f, &p)
			if cmd, _ := exec.Command("ps", "-o", "command=", "-p", f).Output(); p > 1 && strings.Contains(string(cmd), "devctl") && strings.Contains(string(cmd), "serve") {
				if syscall.Kill(p, syscall.SIGTERM) == nil {
					fmt.Println("devctl stoppad")
					return
				}
			}
		}
	}
	fmt.Println("hittade ingen server startad av devctl (körs den via autostart? devctl autostart --remove)")
}

func autostart(cfg config.Config, remove bool) error {
	const label = "com.devctl.server"
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()
	if remove {
		_ = os.Remove(plist)
		fmt.Println("autostart borttagen")
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	logPath := filepath.Join(config.CacheDir(), "server.log")
	path := strings.Join([]string{filepath.Dir(exe), filepath.Join(home, ".local/bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>%s</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, label, exe, path, logPath, logPath)
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("launchctl", "bootstrap", domain, plist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl: %s", out)
	}
	fmt.Printf("devctl startar nu vid inloggning: http://localhost:%d\n", cfg.Port)
	return nil
}
