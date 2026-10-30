// Package control utför åtgärder: tmux, git-worktrees, Docker, processer, Obsidian och orkestratorn.
package control

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

type Args map[string]any

func (a Args) S(k string) string {
	switch v := a[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprint(int(v))
	}
	return ""
}

// Raw returnerar en sträng utan att trimma (text som skickas till panes).
func (a Args) Raw(k string) string { v, _ := a[k].(string); return v }

func (a Args) I(k string, def int) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case string:
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}

func (a Args) B(k string, def bool) bool {
	if v, ok := a[k].(bool); ok {
		return v
	}
	return def
}

func (a Args) need(keys ...string) error {
	for _, k := range keys {
		if a.S(k) == "" {
			return fmt.Errorf("fältet '%s' saknas", k)
		}
	}
	return nil
}

type Controller struct {
	cfg config.Config
	col *state.Collector
}

func New(cfg config.Config, col *state.Collector) *Controller { return &Controller{cfg: cfg, col: col} }

type action func(Args) (any, error)

func (c *Controller) actions() map[string]action {
	return map[string]action{
		// tmux
		"new_session": c.newSession, "new_window": c.newWindow, "split": c.split,
		"rename_session": c.renameSession, "rename_window": c.renameWindow, "title_pane": c.titlePane,
		"kill": c.kill, "select": c.selectTarget, "zoom": c.zoom, "layout": c.layout, "send": c.send,
		"keys": c.keys, "capture": c.capture, "attach": c.attach, "set_parent": c.setParent,
		"orchestrator_window": c.orchestratorWindow,
		// git
		"worktree_add": c.worktreeAdd, "worktree_remove": c.worktreeRemove, "worktree_prune": c.worktreePrune,
		"archive": c.archive, "open_privacy": c.openPrivacy, "run_action": c.runAction, "edit_actions": c.editActions, "repo_note": c.repoNote, "sync_sessions": c.syncSessionsAction,
		"workflow_status": c.workflowStatus, "workflow_cancel": c.workflowCancel, "ci_rerun": c.ciRerun, "ci_dispatch": c.ciDispatch,
		"kill_processes": c.killProcesses, "open_parked": c.openParked, "orch_history": c.orchHistory, "orchestrator_reload": c.orchestratorReload,
		"commits": c.commits, "cloud_push": c.cloudPush, "cloud_status": c.cloudStatus, "open": c.openTarget, "stop_port": c.stopPort, "sessions": c.sessionsAction, "summarize": c.summarize,
		"agents": c.agentsList, "agent_run": c.agentRun, "agent_finding": c.agentFinding, "agent_notify": c.agentNotify,
		"agent_new": c.agentNew, "agent_edit": c.agentEdit, "agent_toggle": c.agentToggle, "agent_settings": c.agentSettings, "changes": c.changes, "diff": c.diff, "worker_start": c.workerStart,
		"open_worktree": c.openWorktree, "graph": c.graph, "editor": c.editor,
		// processer och Docker
		"kill_process": c.killProcess, "docker": c.docker, "docker_logs": c.dockerLogs,
		// Obsidian och orkestrator
		"note": c.note, "daily_log": c.dailyLog, "orchestrator_start": c.orchestratorStart, "brief": c.briefAction,
		"open_url": c.openURL, "automation_start": c.automationStart,
		// Local-Automations kommandon (automation_cmds.go)
		"automation_commands": c.automationCommands, "automation_cmd": c.automationCmd,
	}
}

// Do utför en åtgärd och begär en ny läsning av läget.
func (c *Controller) Do(name string, a Args) (any, error) {
	fn := c.actions()[name]
	if fn == nil {
		return nil, fmt.Errorf("okänd action: %s", name)
	}
	res, err := fn(a)
	c.col.Refresh(strings.HasPrefix(name, "worktree") || name == "worker_start")
	return res, err
}

// ---------- tmux ----------

var tmux = state.Tmux

func tmuxOut(args ...string) (string, error) {
	out, err := tmux(args...)
	return strings.TrimSpace(out), err
}

func fields(fs ...string) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = "#{" + f + "}"
	}
	return strings.Join(parts, "\x1f")
}

func dir(a Args) (string, error) {
	d := config.Home(a.S("cwd"))
	if d == "" {
		d = config.Home("~")
	}
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return "", fmt.Errorf("katalogen finns inte: %s", d)
	}
	return d, nil
}

func runIn(pane, command string) error {
	if command == "" {
		return nil
	}
	if _, err := tmux("send-keys", "-t", pane, "-l", command); err != nil {
		return err
	}
	_, err := tmux("send-keys", "-t", pane, "Enter")
	return err
}

func (c *Controller) newSession(a Args) (any, error) {
	if err := a.need("name"); err != nil {
		return nil, err
	}
	d, err := dir(a)
	if err != nil {
		return nil, err
	}
	args := []string{"new-session", "-d", "-s", a.S("name"), "-c", d, "-P", "-F", fields("session_id", "pane_id")}
	if a.S("window") != "" {
		args = append(args, "-n", a.S("window"))
	}
	out, err := tmuxOut(args...)
	if err != nil {
		return nil, err
	}
	p := strings.Split(out, "\x1f")
	return map[string]string{"session": p[0], "pane": p[1]}, runIn(p[1], a.S("command"))
}

func (c *Controller) newWindow(a Args) (any, error) {
	if err := a.need("session"); err != nil {
		return nil, err
	}
	d, err := dir(a)
	if err != nil {
		return nil, err
	}
	args := []string{"new-window", "-d", "-t", a.S("session") + ":", "-c", d, "-P", "-F", fields("window_id", "pane_id")}
	if a.S("name") != "" {
		args = append(args, "-n", a.S("name"))
	}
	out, err := tmuxOut(args...)
	if err != nil {
		return nil, err
	}
	p := strings.Split(out, "\x1f")
	if a.S("name") != "" {
		_, _ = tmux("set-option", "-w", "-t", p[0], "automatic-rename", "off")
	}
	if a.S("parent") != "" {
		if err := setParent(p[0], a.S("parent")); err != nil {
			return nil, err
		}
	}
	return map[string]string{"window": p[0], "pane": p[1]}, runIn(p[1], a.S("command"))
}

func (c *Controller) split(a Args) (any, error) {
	if err := a.need("pane"); err != nil {
		return nil, err
	}
	flag := "-h"
	if a.S("direction") == "v" {
		flag = "-v"
	}
	cwd, _ := tmuxOut("display-message", "-p", "-t", a.S("pane"), "#{pane_current_path}")
	if cwd == "" {
		cwd = config.Home("~")
	}
	id, err := tmuxOut("split-window", "-d", flag, "-t", a.S("pane"), "-c", cwd, "-P", "-F", "#{pane_id}")
	if err != nil {
		return nil, err
	}
	if a.S("title") != "" {
		_, _ = tmux("select-pane", "-t", id, "-T", a.S("title"))
	}
	return map[string]string{"pane": id}, runIn(id, a.S("command"))
}

func (c *Controller) renameSession(a Args) (any, error) {
	if err := a.need("session", "name"); err != nil {
		return nil, err
	}
	_, err := tmux("rename-session", "-t", a.S("session"), a.S("name"))
	return nil, err
}

func windowID(target string) (string, error) {
	return tmuxOut("display-message", "-p", "-t", target, "#{window_id}")
}

func (c *Controller) renameWindow(a Args) (any, error) {
	if err := a.need("window", "name"); err != nil {
		return nil, err
	}
	wid, err := windowID(a.S("window"))
	if err != nil {
		return nil, err
	}
	if _, err := tmux("rename-window", "-t", wid, a.S("name")); err != nil {
		return nil, err
	}
	// Behåll namnet även om programmet i fönstret försöker byta det
	_, _ = tmux("set-option", "-w", "-t", wid, "allow-rename", "off")
	_, err = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
	return nil, err
}

func (c *Controller) titlePane(a Args) (any, error) {
	if err := a.need("pane"); err != nil {
		return nil, err
	}
	pane, title := a.S("pane"), a.S("title")
	if title == "" {
		// Tomt namn: låt programmet sätta titeln igen
		_, _ = tmux("set-option", "-p", "-u", "-t", pane, "allow-set-title")
		_, _ = tmux("set-option", "-p", "-u", "-t", pane, "@name")
		return nil, nil
	}
	// Programmet (t.ex. Claude Code) får inte skriva över namnet
	_, _ = tmux("set-option", "-p", "-t", pane, "allow-set-title", "off")
	_, _ = tmux("set-option", "-p", "-t", pane, "@name", title)
	_, err := tmux("select-pane", "-t", pane, "-T", title)
	return nil, err
}

func (c *Controller) kill(a Args) (any, error) {
	cmd := map[string]string{"session": "kill-session", "window": "kill-window", "pane": "kill-pane"}[a.S("kind")]
	if cmd == "" || a.S("target") == "" {
		return nil, errors.New("ogiltigt kill-anrop")
	}
	_, err := tmux(cmd, "-t", a.S("target"))
	return nil, err
}

func (c *Controller) selectTarget(a Args) (any, error) {
	if p := a.S("pane"); p != "" {
		if _, err := tmux("select-window", "-t", p); err != nil {
			return nil, err
		}
		_, err := tmux("select-pane", "-t", p)
		return nil, err
	}
	_, err := tmux("select-window", "-t", a.S("window"))
	return nil, err
}

func (c *Controller) zoom(a Args) (any, error) {
	_, err := tmux("resize-pane", "-Z", "-t", a.S("pane"))
	return nil, err
}

func (c *Controller) layout(a Args) (any, error) {
	if err := a.need("window", "layout"); err != nil {
		return nil, err
	}
	_, err := tmux("select-layout", "-t", a.S("window"), a.S("layout"))
	return nil, err
}

// SendText skickar text och Enter separat; Claude Code-TUI:n behöver en kort paus emellan.
func SendText(pane, text string, enter bool) error {
	if text != "" {
		if _, err := tmux("send-keys", "-t", pane, "-l", text); err != nil {
			return err
		}
	}
	if enter {
		if text != "" {
			time.Sleep(250 * time.Millisecond)
		}
		_, err := tmux("send-keys", "-t", pane, "Enter")
		return err
	}
	return nil
}

func (c *Controller) send(a Args) (any, error) {
	if err := a.need("pane"); err != nil {
		return nil, err
	}
	if a.Raw("text") != "" {
		logSent(sentEntry{Pane: a.S("pane"), Text: a.Raw("text"), Via: a.S("via"), By: a.S("by"), Where: a.S("where")})
	}
	return nil, SendText(a.S("pane"), a.Raw("text"), a.B("enter", true))
}

func (c *Controller) keys(a Args) (any, error) {
	if err := a.need("pane", "keys"); err != nil {
		return nil, err
	}
	_, err := tmux(append([]string{"send-keys", "-t", a.S("pane")}, strings.Fields(a.S("keys"))...)...)
	return nil, err
}

func Capture(pane string, lines int) (string, error) {
	return tmux("capture-pane", "-p", "-J", "-t", pane, "-S", fmt.Sprint(-lines))
}

func (c *Controller) capture(a Args) (any, error) {
	if err := a.need("pane"); err != nil {
		return nil, err
	}
	text, err := Capture(a.S("pane"), a.I("lines", 200))
	return map[string]string{"text": text}, err
}

func (c *Controller) attach(a Args) (any, error) {
	if err := a.need("session"); err != nil {
		return nil, err
	}
	name, err := tmuxOut("display-message", "-p", "-t", a.S("session"), "#{session_name}")
	if err != nil {
		return nil, err
	}
	return nil, openTerminal("tmux attach -t '" + strings.ReplaceAll(name, "'", `'\''`) + "'")
}

func setParent(wid, parent string) error {
	if parent == "" {
		_, err := tmux("set-option", "-w", "-u", "-t", wid, "@parent")
		return err
	}
	pid, err := windowID(parent)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for cur := pid; cur != "" && !seen[cur]; {
		if cur == wid {
			return errors.New("ett fönster kan inte ligga under sig självt eller sina egna underfönster")
		}
		seen[cur] = true
		cur, _ = tmuxOut("show-options", "-w", "-v", "-q", "-t", cur, "@parent")
	}
	_, err = tmux("set-option", "-w", "-t", wid, "@parent", pid)
	return err
}

func (c *Controller) setParent(a Args) (any, error) {
	if err := a.need("window"); err != nil {
		return nil, err
	}
	wid, err := windowID(a.S("window"))
	if err != nil {
		return nil, err
	}
	return nil, setParent(wid, a.S("parent"))
}

// orchestratorWindow skapar ett lokalt orkestratorfönster: en orkestrator-pane och N workers
// (som panes eller som underfönster).
func (c *Controller) orchestratorWindow(a Args) (any, error) {
	name := a.S("name")
	if name == "" {
		name = "orchestrator"
	}
	workers := min(max(a.I("workers", 3), 0), 8)
	d, err := dir(a)
	if err != nil {
		return nil, err
	}
	var session, wid, orch string
	if ns := a.S("new_session"); ns != "" {
		out, err := tmuxOut("new-session", "-d", "-s", ns, "-n", name, "-c", d, "-P", "-F", fields("session_id", "window_id", "pane_id"))
		if err != nil {
			return nil, err
		}
		p := strings.Split(out, "\x1f")
		session, wid, orch = p[0], p[1], p[2]
	} else {
		if err := a.need("session"); err != nil {
			return nil, err
		}
		session = a.S("session")
		out, err := tmuxOut("new-window", "-d", "-t", session+":", "-n", name, "-c", d, "-P", "-F", fields("window_id", "pane_id"))
		if err != nil {
			return nil, err
		}
		p := strings.Split(out, "\x1f")
		wid, orch = p[0], p[1]
	}
	for _, o := range [][]string{
		{"@kind", "orchestrator"}, {"automatic-rename", "off"}, {"main-pane-width", "50%"},
		{"pane-border-status", "top"}, {"pane-border-format", " #{pane_index}: #{@role} · #{pane_title} "},
	} {
		_, _ = tmux("set-option", "-w", "-t", wid, o[0], o[1])
	}
	_, _ = tmux("set-option", "-p", "-t", orch, "@role", "orchestrator")
	_, _ = tmux("select-pane", "-t", orch, "-T", "orchestrator")

	asWindows := a.S("worker_mode") == "windows"
	var ids []string
	last := wid
	for i := 1; i <= workers; i++ {
		var p string
		if asWindows {
			out, err := tmuxOut("new-window", "-d", "-a", "-t", last, "-n", fmt.Sprintf("%s-w%d", name, i), "-c", d, "-P", "-F", fields("window_id", "pane_id"))
			if err != nil {
				return nil, err
			}
			parts := strings.Split(out, "\x1f")
			last, p = parts[0], parts[1]
			_, _ = tmux("set-option", "-w", "-t", last, "@parent", wid)
			_, _ = tmux("set-option", "-w", "-t", last, "@kind", "worker")
			_, _ = tmux("set-option", "-w", "-t", last, "automatic-rename", "off")
		} else {
			prev := orch
			if len(ids) > 0 {
				prev = ids[len(ids)-1]
			}
			out, err := tmuxOut("split-window", "-d", "-t", prev, "-c", d, "-P", "-F", "#{pane_id}")
			if err != nil {
				return nil, err
			}
			p = out
			_, _ = tmux("select-layout", "-t", wid, "tiled")
		}
		_, _ = tmux("set-option", "-p", "-t", p, "@role", fmt.Sprintf("worker-%d", i))
		_, _ = tmux("select-pane", "-t", p, "-T", fmt.Sprintf("worker-%d", i))
		ids = append(ids, p)
	}
	if !asWindows {
		_, _ = tmux("select-layout", "-t", wid, "main-vertical")
	}
	_, _ = tmux("select-pane", "-t", orch)

	brief := localBrief(name, wid, orch, ids, a.S("goal"))
	path, err := writeCache(fmt.Sprintf("orchestrator-%s.md", strings.TrimPrefix(wid, "@")), brief)
	if err != nil {
		return nil, err
	}
	workerCmd := c.cfg.WorkerCmd
	if v, ok := a["worker_cmd"].(string); ok {
		workerCmd = strings.TrimSpace(v)
	}
	for _, p := range ids {
		_ = runIn(p, workerCmd)
	}
	orchCmd := c.cfg.OrchestratorCmd
	if v, ok := a["orch_cmd"].(string); ok {
		orchCmd = strings.TrimSpace(v)
	}
	if orchCmd != "" && strings.Fields(orchCmd)[0] == "claude" {
		orchCmd += ` --append-system-prompt "$(cat ` + shellQuote(path) + `)"`
	}
	_ = runIn(orch, orchCmd)
	return map[string]any{"session": session, "window": wid, "orchestrator": orch, "workers": ids, "brief": path}, nil
}

func localBrief(name, wid, orch string, workers []string, goal string) string {
	var list strings.Builder
	for i, p := range workers {
		fmt.Fprintf(&list, "- `%s` — worker-%d\n", p, i+1)
	}
	if len(workers) == 0 {
		list.WriteString("- (inga workers än)\n")
	}
	g := ""
	if goal != "" {
		g = "\n## Mål\n" + goal + "\n"
	}
	return fmt.Sprintf(`# Orkestrator: %s

Du är **orkestratorn** i tmux-fönstret %s (din egen pane: %s). Du delegerar arbete till worker-panes
som var och en kör en egen Claude Code-session.
%s
## Workers
%s
## Styra workers
- Skicka en uppgift: `+"`devctl pane send %%ID \"instruktion\"`"+`
- Läs av: `+"`devctl pane capture %%ID -n 120`"+`
- Avbryt: `+"`devctl pane keys %%ID Escape`"+`
- Hela läget (repos, worktrees, servrar, Docker): `+"`devctl status`"+`

## Arbetssätt
1. Bryt ner uppdraget i oberoende deluppgifter, en per worker.
2. Ge varje worker en självständig instruktion och be den avsluta med en rad `+"`KLAR: <sammanfattning>`"+`.
3. Polla workers med rimliga intervall och svara på deras frågor.
4. Låt aldrig två workers ändra samma filer samtidigt.
Vänta på att användaren ger dig ett uppdrag innan du startar workers.
`, name, wid, orch, g, list.String())
}
