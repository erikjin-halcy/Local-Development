package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func writeCache(name, content string) (string, error) {
	p := filepath.Join(config.CacheDir(), name)
	return p, os.WriteFile(p, []byte(content), 0o600)
}

func openTerminal(cmd string) error {
	script := fmt.Sprintf("tell application \"Terminal\" to do script %q\ntell application \"Terminal\" to activate", cmd)
	_, err := sysx.Run(10*time.Second, "osascript", "-e", script)
	return err
}

func git(dir string, args ...string) (string, error) {
	out, err := sysx.RunIn(dir, 60*time.Second, "git", args...)
	return strings.TrimSpace(out), err
}

// FindRepo hittar ett repo på namn (skiftlägesokänsligt), sökväg eller sökväg till någon av dess worktrees.
func FindRepo(snap state.Snapshot, key string) (*state.Repo, error) {
	key = config.Home(key)
	for i := range snap.Repos {
		r := &snap.Repos[i]
		if strings.EqualFold(r.Name, key) || r.Path == key {
			return r, nil
		}
		for _, wt := range r.Worktrees {
			if wt.Path == key {
				return r, nil
			}
		}
	}
	return nil, fmt.Errorf("hittar inget repo som heter %q (se devctl status)", key)
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (c *Controller) worktreeAdd(a Args) (any, error) {
	if err := a.need("repo", "branch"); err != nil {
		return nil, err
	}
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	branch := a.S("branch")
	for _, wt := range r.Worktrees {
		if wt.Branch == branch {
			return map[string]any{"path": wt.Path, "branch": branch, "created": false, "repo": r.Name}, nil
		}
	}
	path := config.Home(a.S("path"))
	if path == "" {
		path = filepath.Join(filepath.Dir(r.Path), r.Name+".worktrees", unsafe.ReplaceAllString(branch, "-"))
	}
	var args []string
	switch {
	case exists(r.Path, "refs/heads/"+branch):
		args = []string{"worktree", "add", path, branch}
	case exists(r.Path, "refs/remotes/origin/"+branch):
		args = []string{"worktree", "add", "--track", "-b", branch, path, "origin/" + branch}
	default:
		base := a.S("base")
		if base == "" {
			base = r.Default
		}
		if base == "" {
			base = "HEAD"
		}
		args = []string{"worktree", "add", "-b", branch, path, base}
	}
	if out, err := git(r.Path, args...); err != nil {
		return nil, fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, out)
	}
	c.col.AddDir(path)
	return map[string]any{"path": path, "branch": branch, "created": true, "repo": r.Name}, nil
}

func exists(repo, ref string) bool {
	_, err := git(repo, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

func (c *Controller) worktreeRemove(a Args) (any, error) {
	if err := a.need("path"); err != nil {
		return nil, err
	}
	path := config.Home(a.S("path"))
	r, err := FindRepo(c.col.Snapshot(), path)
	if err != nil {
		return nil, err
	}
	if r.Worktrees[0].Path == path {
		return nil, errors.New("huvud-worktreen kan inte tas bort")
	}
	args := []string{"worktree", "remove", path}
	if a.B("force", false) {
		args = []string{"worktree", "remove", "--force", path}
	}
	if _, err := git(r.Path, args...); err != nil {
		return nil, fmt.Errorf("%v (har worktreen ändringar? använd force)", err)
	}
	return nil, nil
}

// ensureSession ser till att sessionen finns och returnerar nytt fönster i den.
func newWindowIn(session, name, cwd string) (wid, pane string, err error) {
	if _, err := tmux("has-session", "-t", "="+session); err != nil {
		out, err := tmuxOut("new-session", "-d", "-s", session, "-n", name, "-c", cwd, "-P", "-F", fields("window_id", "pane_id"))
		if err != nil {
			return "", "", err
		}
		p := strings.Split(out, "\x1f")
		return p[0], p[1], nil
	}
	out, err := tmuxOut("new-window", "-d", "-t", "="+session+":", "-n", name, "-c", cwd, "-P", "-F", fields("window_id", "pane_id"))
	if err != nil {
		return "", "", err
	}
	p := strings.Split(out, "\x1f")
	return p[0], p[1], nil
}

// workerStart skapar (vid behov) en worktree för branchen och ett tmux-fönster med en Claude-worker där.
func (c *Controller) workerStart(a Args) (any, error) {
	res, err := c.worktreeAdd(a)
	if err != nil {
		return nil, err
	}
	wt := res.(map[string]any)
	path, branch, repo := wt["path"].(string), wt["branch"].(string), wt["repo"].(string)
	session := a.S("session")
	if session == "" {
		session = c.cfg.Session
	}
	wid, pane, err := newWindowIn(session, repo+"/"+branch, path)
	if err != nil {
		return nil, err
	}
	_, _ = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
	_, _ = tmux("set-option", "-w", "-t", wid, "@kind", "worker")
	_, _ = tmux("set-option", "-p", "-t", pane, "@role", "worker")
	_, _ = tmux("select-pane", "-t", pane, "-T", branch)
	if o := c.col.Snapshot().Orchestrator; o != nil {
		_ = setParent(wid, o.Window)
	}
	cmd := c.cfg.WorkerCmd
	if v, ok := a["cmd"].(string); ok {
		cmd = strings.TrimSpace(v)
	}
	if task := strings.TrimSpace(a.Raw("task")); task != "" && cmd != "" && strings.Fields(cmd)[0] == "claude" {
		file, err := writeCache(fmt.Sprintf("task-%s.md", strings.TrimPrefix(wid, "@")), task)
		if err != nil {
			return nil, err
		}
		cmd += ` "$(cat ` + shellQuote(file) + `)"`
	}
	_ = runIn(pane, cmd)
	return map[string]any{"window": wid, "pane": pane, "path": path, "branch": branch, "repo": repo, "session": session}, nil
}

func (c *Controller) openWorktree(a Args) (any, error) {
	if err := a.need("path"); err != nil {
		return nil, err
	}
	path := config.Home(a.S("path"))
	name := filepath.Base(path)
	if r, err := FindRepo(c.col.Snapshot(), path); err == nil {
		for _, wt := range r.Worktrees {
			if wt.Path == path && wt.Branch != "" {
				name = r.Name + "/" + wt.Branch
			}
		}
	}
	session := a.S("session")
	if session == "" {
		session = c.cfg.Session
	}
	wid, pane, err := newWindowIn(session, name, path)
	if err != nil {
		return nil, err
	}
	_, _ = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
	return map[string]string{"window": wid, "pane": pane}, runIn(pane, a.S("command"))
}

func (c *Controller) graph(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	text, err := state.Graph(r.Path, a.I("lines", 80))
	return map[string]string{"text": text, "repo": r.Name}, err
}

func (c *Controller) editor(a Args) (any, error) {
	path := config.Home(a.S("path"))
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("katalogen finns inte: %s", path)
	}
	if c.cfg.Editor != "" && sysx.Has(c.cfg.Editor) {
		_, err := sysx.Run(15*time.Second, c.cfg.Editor, path)
		return nil, err
	}
	_, err := sysx.Run(10*time.Second, "open", path)
	return nil, err
}

// killProcess stoppar en lokal server. Bara processer som lyssnar på en port kan stoppas härifrån.
func (c *Controller) killProcess(a Args) (any, error) {
	pid := a.I("pid", 0)
	ok := false
	for _, s := range c.col.Snapshot().Servers {
		if s.PID == pid && !s.App && s.Kind != "docker" {
			ok = true
		}
	}
	if !ok || pid <= 1 {
		return nil, errors.New("processen är inte en lokal server i listan")
	}
	return nil, syscall.Kill(pid, syscall.SIGTERM)
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func (c *Controller) docker(a Args) (any, error) {
	op, id := a.S("op"), a.S("id")
	if op != "start" && op != "stop" && op != "restart" {
		return nil, errors.New("op måste vara start, stop eller restart")
	}
	if !idRe.MatchString(id) {
		return nil, errors.New("ogiltigt container-id")
	}
	_, err := sysx.Run(90*time.Second, "docker", op, id)
	return nil, err
}

func (c *Controller) dockerLogs(a Args) (any, error) {
	id := a.S("id")
	if !idRe.MatchString(id) {
		return nil, errors.New("ogiltigt container-id")
	}
	text, err := sysx.Combined(15*time.Second, "docker", "logs", "--tail", fmt.Sprint(a.I("lines", 200)), id)
	return map[string]string{"text": text}, err
}

func (c *Controller) openURL(a Args) (any, error) {
	u := a.S("url")
	if !strings.HasPrefix(u, "http://localhost:") && !strings.HasPrefix(u, "http://127.0.0.1:") &&
		!strings.HasPrefix(u, "https://github.com/") && !strings.HasPrefix(u, "obsidian://") &&
		(c.cfg.Automation.CloudURL == "" || !strings.HasPrefix(u, c.cfg.Automation.CloudURL)) {
		return nil, errors.New("bara localhost-, GitHub-, Obsidian- och Local-Automation-länkar")
	}
	_, err := sysx.Run(10*time.Second, "open", u)
	return nil, err
}

// ---------- Obsidian ----------

// obsidianURI kodar mellanslag som %20; Obsidian tolkar inte "+" som mellanslag.
func obsidianURI(path string) string {
	return "obsidian://open?path=" + strings.ReplaceAll(url.QueryEscape(path), "+", "%20")
}

// obsidianVaults är valven Obsidian känner till (de som öppnats i programmet någon gång).
func obsidianVaults() []string {
	b, err := os.ReadFile(config.Home("~/Library/Application Support/obsidian/obsidian.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Vaults map[string]struct {
			Path string `json:"path"`
		} `json:"vaults"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	var out []string
	for _, v := range f.Vaults {
		out = append(out, v.Path)
	}
	return out
}

// openInObsidian öppnar en fil i Obsidian. Obsidian kan bara öppna filer i valv som redan öppnats
// i programmet, så annars förklaras hur valvet läggs till.
func openInObsidian(path string) error {
	known := false
	for _, v := range obsidianVaults() {
		if strings.HasPrefix(path, v+"/") {
			known = true
		}
	}
	if !known {
		vault := filepath.Base(config.Home("~"))
		for d := filepath.Dir(path); d != "/" && d != "."; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, ".obsidian")); err == nil {
				vault = d
				break
			}
		}
		where := vault
		// iCloud-valv ligger i den dolda mappen Library; i Finder heter platsen iCloud Drive › Obsidian
		if strings.Contains(vault, "/Library/Mobile Documents/iCloud~md~obsidian/Documents/") {
			where = "iCloud Drive › Obsidian › " + filepath.Base(vault)
		}
		return fmt.Errorf("filen är sparad, men valvet %q är inte öppnat i Obsidian än. Gör så här en gång: Obsidian → valvikonen nere till vänster → Open another vault → Open folder as vault → välj %s (eller tryck ⌘⇧G och klistra in %s)", filepath.Base(vault), where, vault)
	}
	_, err := sysx.Run(10*time.Second, "open", obsidianURI(path))
	return err
}

// Note skapar branchens anteckning om den saknas och returnerar sökvägen.
func (c *Controller) Note(repo, branch string) (string, bool, error) {
	if c.cfg.Obsidian.Vault == "" {
		return "", false, fmt.Errorf("inget Obsidian-valv valt; sätt obsidian.vault i %s", config.Path())
	}
	r, err := FindRepo(c.col.Snapshot(), repo)
	if err != nil {
		return "", false, err
	}
	path := state.NotePath(c.cfg, r.Name, branch)
	if path == "" {
		return "", false, errors.New("ogiltig branch")
	}
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	wtPath, gh, pr := "", "", ""
	for _, wt := range r.Worktrees {
		if wt.Branch == branch {
			wtPath = wt.Path
			if wt.PR != nil {
				pr = fmt.Sprintf(" · [PR #%d](%s)", wt.PR.Number, wt.PR.URL)
			}
		}
	}
	if r.GitHub != "" {
		gh = fmt.Sprintf(" · [GitHub](https://github.com/%s/tree/%s)", r.GitHub, branch)
	}
	body := fmt.Sprintf(`---
repo: %s
branch: %s
worktree: %q
created: %s
tags: [branch]
---
# %s

**Repo:** %s%s%s

## Mål


## Anteckningar


## Status
- [ ]
`, r.Name, branch, wtPath, time.Now().Format("2006-01-02"), branch, r.Name, gh, pr)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	return path, true, os.WriteFile(path, []byte(body), 0o644)
}

func (c *Controller) note(a Args) (any, error) {
	if err := a.need("repo", "branch"); err != nil {
		return nil, err
	}
	path, created, err := c.Note(a.S("repo"), a.S("branch"))
	if err != nil {
		return nil, err
	}
	res := map[string]any{"path": path, "created": created, "uri": obsidianURI(path)}
	if a.B("open", false) {
		return res, openInObsidian(path)
	}
	return res, nil
}

const logStart, logEnd = "<!-- devctl:start -->", "<!-- devctl:end -->"

// DailyLog skriver dagens utvecklingslogg. Bara delen mellan devctl-markeringarna skrivs om,
// så egna anteckningar i filen bevaras.
func (c *Controller) DailyLog() (string, error) {
	if c.cfg.Obsidian.Vault == "" {
		return "", fmt.Errorf("inget Obsidian-valv valt; sätt obsidian.vault i %s", config.Path())
	}
	snap := c.col.Snapshot()
	now := time.Now()
	path := filepath.Join(c.cfg.Obsidian.Vault, c.cfg.Obsidian.LogDir, now.Format("2006-01-02")+".md")
	section := logSection(c.cfg, snap, now)

	old, err := os.ReadFile(path)
	var out string
	if err == nil && strings.Contains(string(old), logStart) && strings.Contains(string(old), logEnd) {
		s := string(old)
		i, j := strings.Index(s, logStart), strings.Index(s, logEnd)+len(logEnd)
		out = s[:i] + section + s[j:]
	} else if err == nil {
		out = string(old) + "\n" + section + "\n"
	} else {
		out = fmt.Sprintf("---\ndate: %s\ntags: [dev-logg]\n---\n# Dev-logg %s\n\n%s\n\n## Mina anteckningar\n\n", now.Format("2006-01-02"), now.Format("2006-01-02"), section)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(out), 0o644)
}

func logSection(cfg config.Config, snap state.Snapshot, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n_Uppdaterad %s av devctl._\n\n## Branches som rörts idag\n", logStart, now.Format("15:04"))
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	any := false
	for _, r := range snap.Repos {
		if !r.Active {
			continue
		}
		out, err := git(r.Path, "log", "--all", "--source", "--since="+midnight.Format(time.RFC3339), "--format=%h%x1f%s%x1f%S")
		byBranch := map[string][]string{}
		if err == nil && out != "" {
			for _, l := range strings.Split(out, "\n") {
				p := strings.Split(l, "\x1f")
				if len(p) == 3 && strings.HasPrefix(p[2], "refs/heads/") {
					br := strings.TrimPrefix(p[2], "refs/heads/")
					byBranch[br] = append(byBranch[br], p[1])
				}
			}
		}
		// Ocommittade ändringar räknas bara om något körs i worktreen eller den har en färsk commit
		for _, wt := range r.Worktrees {
			live := len(wt.Panes)+len(wt.Servers)+len(wt.Containers) > 0 || now.Unix()-wt.Last.Unix < 24*3600
			if wt.Changes > 0 && wt.Branch != "" && live {
				if _, ok := byBranch[wt.Branch]; !ok {
					byBranch[wt.Branch] = nil
				}
			}
		}
		if len(byBranch) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(&b, "### %s\n", r.Name)
		names := make([]string, 0, len(byBranch))
		for k := range byBranch {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, br := range names {
			subjects := byBranch[br]
			line := "- **" + br + "**"
			if n := state.NotePath(cfg, r.Name, br); n != "" {
				if _, err := os.Stat(n); err == nil {
					rel, _ := filepath.Rel(cfg.Obsidian.Vault, n)
					line += " ([[" + strings.TrimSuffix(rel, ".md") + "|anteckning]])"
				}
			}
			if len(subjects) > 0 {
				shown := subjects
				if len(shown) > 5 {
					shown = shown[:5]
				}
				line += fmt.Sprintf(" — %d commits: %s", len(subjects), strings.Join(shown, "; "))
			}
			for _, wt := range r.Worktrees {
				if wt.Branch == br {
					if wt.Changes > 0 {
						line += fmt.Sprintf(" · %d ocommittade ändringar", wt.Changes)
					}
					if wt.PR != nil {
						line += fmt.Sprintf(" · [PR #%d](%s)", wt.PR.Number, wt.PR.URL)
					}
				}
			}
			b.WriteString(line + "\n")
		}
	}
	if !any {
		b.WriteString("_Inget än._\n")
	}

	b.WriteString("\n## Öppna PR:er\n")
	anyPR := false
	for _, r := range snap.Repos {
		for _, p := range r.PRs {
			anyPR = true
			draft := ""
			if p.Draft {
				draft = " (utkast)"
			}
			fmt.Fprintf(&b, "- %s [#%d %s](%s)%s\n", r.Name, p.Number, p.Title, p.URL, draft)
		}
	}
	if !anyPR {
		b.WriteString("_Inga._\n")
	}

	b.WriteString("\n## Igång just nu\n")
	for _, s := range snap.Servers {
		if s.App || s.Worktree == nil {
			continue
		}
		fmt.Fprintf(&b, "- :%d %s (%s) — %s@%s\n", s.Port, s.Kind, s.Process, s.Worktree.Repo, s.Worktree.Branch)
	}
	running := 0
	for _, ct := range snap.Docker.Containers {
		if ct.State == "running" {
			running++
		}
	}
	fmt.Fprintf(&b, "- %d containrar igång (%s)\n", running, snap.Docker.Context)
	var sess []string
	for _, s := range snap.Sessions {
		sess = append(sess, fmt.Sprintf("%s (%d fönster)", s.Name, len(s.Windows)))
	}
	if len(sess) > 0 {
		b.WriteString("- tmux: " + strings.Join(sess, ", ") + "\n")
	}
	b.WriteString(logEnd)
	return b.String()
}

func (c *Controller) dailyLog(a Args) (any, error) {
	path, err := c.DailyLog()
	if err == nil && a.B("open", false) {
		err = openInObsidian(path)
	}
	return map[string]string{"path": path, "uri": obsidianURI(path)}, err
}

// ---------- global orkestrator ----------

// Brief är systemprompten för den globala orkestratorn.
func (c *Controller) Brief() string {
	vault := c.cfg.Obsidian.Vault
	if vault == "" {
		vault = "(inget valt)"
	}
	read := "- (inga)"
	if len(c.cfg.Obsidian.ReadVaults) > 0 {
		read = "- " + strings.Join(c.cfg.Obsidian.ReadVaults, "\n- ")
	}
	return fmt.Sprintf(`# devctl-orkestrator

Du är användarens globala orkestrator för utvecklingsarbete (full stack). Med kommandot `+"`devctl`"+` ser du
alla git-repos och worktrees, tmux-sessioner, lokala servrar och Docker/OrbStack-containrar, och du kan
starta workers (egna Claude Code-sessioner) i egna worktrees. Webbgränssnittet: http://localhost:%d

## Läsa läget
- `+"`devctl status`"+` – aktiva repos, worktrees per branch med ändringar och PR, och vilka panes,
  servrar och containrar som hör till varje branch. `+"`devctl status --all`"+` visar alla repos.
- `+"`devctl status --json`"+` – allt som JSON.
- `+"`devctl pane capture %%ID -n 120`"+` – vad en pane visar.
- `+"`devctl graph <repo>`"+` – commitgraf över alla branches.

## Göra saker
- `+"`devctl worker start <repo> <branch> [--base main] [--task \"...\"]`"+` – skapar worktree om den saknas
  och ett tmux-fönster (under ditt fönster) där en Claude-worker får uppgiften.
- `+"`devctl worktree add <repo> <branch> [--base x]`"+` / `+"`devctl worktree rm <sökväg>`"+`
- `+"`devctl pane send %%ID \"text\"`"+` / `+"`devctl pane keys %%ID Escape`"+`
- `+"`devctl note <repo> <branch>`"+` – skapar branchens anteckning i Obsidian och skriver ut sökvägen;
  läs och skriv sedan filen direkt (mål, beslut, status).
- `+"`devctl log`"+` – uppdaterar dagens utvecklingslogg i Obsidian.

## Obsidian
- Arbetsvalv: %s (branch-anteckningar i %s/<repo>/, dagliga loggar i %s/).
- Valv du får läsa som kontext (ändra inte i dem):
%s

## Regler
- En worker per worktree. Låt aldrig två workers ändra samma filer.
- Stoppa inte servrar eller containrar, ta inte bort worktrees, pusha inte och öppna inte PR:er
  utan att användaren bett om det.
- Be workers avsluta med en rad `+"`KLAR: <sammanfattning>`"+` och polla dem med rimliga intervall.
- Svara kort. Längre planer och beslut skriver du i branchens anteckning.
- Vänta på användarens uppdrag innan du startar workers.
`, c.cfg.Port, vault, c.cfg.Obsidian.BranchDir, c.cfg.Obsidian.LogDir, read)
}

func (c *Controller) briefAction(Args) (any, error) {
	return map[string]string{"text": c.Brief()}, nil
}

func (c *Controller) orchestratorStart(a Args) (any, error) {
	if o := c.col.Snapshot().Orchestrator; o != nil {
		return o, nil
	}
	path, err := writeCache("orchestrator-global.md", c.Brief())
	if err != nil {
		return nil, err
	}
	cwd := config.Home("~")
	if len(c.cfg.Roots) > 0 {
		cwd = c.cfg.Roots[0]
	}
	wid, pane, err := newWindowIn(c.cfg.Session, "orchestrator", cwd)
	if err != nil {
		return nil, err
	}
	_, _ = tmux("set-option", "-w", "-t", wid, "@kind", "global-orchestrator")
	_, _ = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
	_, _ = tmux("set-option", "-p", "-t", pane, "@role", "orchestrator")
	_, _ = tmux("select-pane", "-t", pane, "-T", "orchestrator")
	cmd := c.cfg.OrchestratorCmd
	if cmd != "" && strings.Fields(cmd)[0] == "claude" {
		// devctl-kommandon är förhandsgodkända och valven läsbara; allt annat frågar som vanligt
		cmd += ` --allowedTools 'Bash(devctl:*)'`
		for _, v := range append([]string{c.cfg.Obsidian.Vault}, c.cfg.Obsidian.ReadVaults...) {
			if v != "" {
				cmd += " --add-dir " + shellQuote(v)
			}
		}
		cmd += ` --append-system-prompt "$(cat ` + shellQuote(path) + `)"`
	}
	c.col.Refresh(false)
	return &state.Orchestrator{Session: c.cfg.Session, Window: wid, Pane: pane}, runIn(pane, cmd)
}

// automationStart startar Local-Automations gränssnitt med repots eget installationsskript.
func (c *Controller) automationStart(Args) (any, error) {
	d := c.cfg.Automation.Dir
	if d == "" {
		return nil, fmt.Errorf("sätt automation.dir i %s", config.Path())
	}
	script := filepath.Join(d, "ui", "install-ui.sh")
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("hittar inte %s", script)
	}
	out, err := sysx.Combined(30*time.Second, script)
	return map[string]string{"output": strings.TrimSpace(out)}, err
}

// worktreePrune tar bort git:s uppgifter om worktrees vars katalog inte längre finns.
func (c *Controller) worktreePrune(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	_, err = git(r.Path, "worktree", "prune")
	return nil, err
}

// archive gömmer ett repo från översikten (eller återställer det). Inget i repot ändras.
func (c *Controller) archive(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	return nil, config.SetArchived(r.Path, a.B("archived", true))
}

// openPrivacy öppnar Systeminställningar → Integritet och säkerhet → Filer och mappar.
func (c *Controller) openPrivacy(Args) (any, error) {
	_, err := sysx.Run(10*time.Second, "open", "x-apple.systempreferences:com.apple.preference.security?Privacy_FilesAndFolders")
	return nil, err
}

// runAction kör en av repots actions (skript, make-mål, deploy, egna) i ett tmux-fönster i worktreen.
// Riskabla actions (deploy, migrering, borttagning …) kräver confirm: true.
func (c *Controller) runAction(a Args) (any, error) {
	if err := a.need("path", "id"); err != nil {
		return nil, err
	}
	path := config.Home(a.S("path"))
	r, err := FindRepo(c.col.Snapshot(), path)
	if err != nil {
		return nil, err
	}
	var wt *state.Worktree
	for i := range r.Worktrees {
		if r.Worktrees[i].Path == path {
			wt = &r.Worktrees[i]
		}
	}
	if wt == nil {
		wt = &r.Worktrees[0]
	}
	// Läs om från worktreen själv; skripten kan skilja sig mellan branches
	var act *state.Action
	custom, err := config.RepoActions(r.Name)
	if err != nil {
		return nil, fmt.Errorf("fel i %s: %v", config.ActionsFile(r.Name), err)
	}
	for _, x := range state.RepoActions(wt.Path, custom) {
		if x.ID == a.S("id") || strings.EqualFold(x.Label, a.S("id")) {
			x := x
			act = &x
			break
		}
	}
	if act == nil {
		return nil, fmt.Errorf("%s har ingen action %q (se devctl actions %s)", r.Name, a.S("id"), r.Name)
	}
	if act.Danger && !a.B("confirm", false) {
		return map[string]any{"needsConfirm": true, "action": act}, fmt.Errorf("%q kräver bekräftelse", act.Label)
	}
	dir := filepath.Join(wt.Path, act.Dir)
	branch := wt.Branch
	if branch == "" {
		branch = wt.Head
	}
	wid, pane, err := newWindowIn(c.cfg.Session, r.Name+"/"+branch+": "+act.Label, dir)
	if err != nil {
		return nil, err
	}
	_, _ = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
	_, _ = tmux("set-option", "-w", "-t", wid, "@kind", "action")
	if o := c.col.Snapshot().Orchestrator; o != nil && a.B("underOrchestrator", false) {
		_ = setParent(wid, o.Window)
	}
	line := act.Run + `; echo; echo "── klart (exit $?) ──"`
	return map[string]any{"window": wid, "pane": pane, "action": act, "dir": dir}, runIn(pane, line)
}

// editActions öppnar repots fil med egna actions i editorn och skapar den med en mall om den saknas.
func (c *Controller) editActions(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	path := config.ActionsFile(r.Name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(config.ActionsDir(), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(config.ActionsTemplate(r.Name)), 0o644); err != nil {
			return nil, err
		}
	}
	if c.cfg.Editor != "" && sysx.Has(c.cfg.Editor) {
		_, err = sysx.Run(15*time.Second, c.cfg.Editor, path)
	} else {
		_, err = sysx.Run(10*time.Second, "open", "-t", path)
	}
	c.col.Refresh(true)
	return map[string]string{"path": path}, err
}
