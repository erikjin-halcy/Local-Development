package control

// Stänga worker-panes och skriva sessionsrapporter enligt tmux-reglerna (skillen tmux).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

var paneIDRe = regexp.MustCompile(`^%\d+$`)

// paneClose avslutar Claude i en worker-pane och stänger panen. Kräver confirm; vägrar huvudpanen.
func (c *Controller) paneClose(a Args) (any, error) {
	pane := a.S("pane")
	if !paneIDRe.MatchString(pane) {
		return nil, errors.New("ange panen som %id")
	}
	role, err := tmuxOut("display-message", "-p", "-t", pane, "#{@role}")
	if err != nil {
		return nil, fmt.Errorf("hittar ingen pane %s", pane)
	}
	if strings.TrimSpace(role) == "main" && !a.B("main", false) {
		return nil, errors.New("det här är huvudpanen (@role main); den stängs bara om användaren uttryckligen ber om det (--main)")
	}
	if !a.B("confirm", false) {
		return nil, errors.New("att stänga en pane kräver bekräftelse (--yes) efter att användaren sagt ja")
	}
	if cmd, _ := tmuxOut("display-message", "-p", "-t", pane, "#{pane_current_command}"); strings.Contains(cmd, "claude") || strings.Contains(cmd, "node") || regexp.MustCompile(`^\d+\.\d+`).MatchString(cmd) {
		_, _ = tmux("send-keys", "-t", pane, "-l", "/exit")
		_, _ = tmux("send-keys", "-t", pane, "Enter")
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if cur, _ := tmuxOut("display-message", "-p", "-t", pane, "#{pane_current_command}"); cur != cmd {
				break
			}
		}
	}
	_, err = tmux("kill-pane", "-t", pane)
	return map[string]string{"closed": pane}, err
}

// ReportDir är mappen för ett repos sessionsrapporter.
func ReportDir(cfg config.Config, repo string) string {
	if d := cfg.Obsidian.SessionReports[repo]; d != "" {
		return config.Home(d)
	}
	if cfg.Obsidian.Vault == "" {
		return filepath.Join(config.Dir(), "reports", repo)
	}
	return filepath.Join(cfg.Obsidian.Vault, cfg.Obsidian.ProjectDir, repo, "Claude", "Sessioner")
}

// report skriver ett utkast till sessionsrapport för en pane: frontmatter, git-läge och sessionens
// sammanfattning är ifyllda; Gjort, Kvar och Bra att veta fyller Claude i efteråt.
func (c *Controller) report(a Args) (any, error) {
	pane := a.S("pane")
	if !paneIDRe.MatchString(pane) {
		return nil, errors.New("ange panen som %id")
	}
	info, err := tmuxOut("display-message", "-p", "-t", pane, fields("@repo", "@feature", "pane_title", "pane_current_path"))
	if err != nil {
		return nil, fmt.Errorf("hittar ingen pane %s", pane)
	}
	p := strings.Split(info, "\x1f")
	repo, feature, title, cwd := p[0], p[1], p[2], p[3]
	// Worktreen är den längsta worktree-sökvägen som panens mapp ligger i
	branch, wtPath, best := "", cwd, 0
	for _, r := range c.col.Snapshot().Repos {
		for _, wt := range r.Worktrees {
			if strings.HasPrefix(cwd+"/", wt.Path+"/") && len(wt.Path) > best {
				best, branch, wtPath = len(wt.Path), wt.Branch, wt.Path
				if repo == "" {
					repo = r.Name
				}
			}
		}
	}
	if repo == "" {
		return nil, fmt.Errorf("%s hör inte till något känt repo; ange --repo", pane)
	}
	if branch == "" {
		if out, err := git(wtPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			branch = strings.TrimSpace(out)
		}
	}
	if v := a.S("repo"); v != "" {
		repo = v
	}
	if v := a.S("feature"); v != "" {
		feature = v
	}
	if feature == "" {
		feature = strings.TrimSpace(strings.TrimPrefix(title, repo+" · "))
	}
	if feature == "" {
		feature = branch
	}

	gitOut := func(args ...string) string {
		out, err := git(wtPath, args...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	status := gitOut("status", "--short")
	unpushed := gitOut("log", "--oneline", "@{u}..")
	upstream := gitOut("rev-parse", "--abbrev-ref", "@{u}")
	if upstream == "" {
		unpushed = gitOut("log", "--oneline", "main..")
	}
	recent := gitOut("log", "--oneline", "-10")
	summary := ""
	if res, err := c.summarize(Args{"target": pane}); err == nil {
		summary = res.(map[string]string)["text"]
	}

	now := time.Now()
	dir := ReportDir(c.cfg, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := strings.Join(strings.Fields(regexp.MustCompile(`[/\\:*?"<>|#^\[\]]+`).ReplaceAllString(feature, " ")), " ")
	file := filepath.Join(dir, now.Format("2006-01-02 1504")+" "+name+".md")
	if _, err := os.Stat(file); err == nil {
		return nil, fmt.Errorf("rapporten finns redan: %s", file)
	}
	or := func(s, empty string) string {
		if s == "" {
			return empty
		}
		return s
	}
	block := func(s string) string { return "```\n" + s + "\n```" }
	upLine := "Upstream: " + or(upstream, "ingen (jämfört med main)")
	md := fmt.Sprintf(`---
repo: %s
branch: %s
worktree: %q
feature: %q
pane: %q
datum: %s
status: klar
---
# %s · %s

## Uppdrag
_Fyll i: vad sessionen skulle göra och vem som bad om det._

## Gjort
_Fyll i: ändringar med filer och commits, och beslut som togs och varför._

Senaste commits:
%s

## Läge
- Ocommittat: %s
- Ej pushat (%s): %s
- PR, deploy, tester: _fyll i_

## Kvar och nästa steg
_Fyll i._

## Bra att veta
_Fyll i._

## Underlag (automatiskt, %s)
%s
`, repo, branch, wtPath, feature, pane+" "+title, now.Format("2006-01-02"), repo, feature,
		block(or(recent, "inga commits")),
		or(map[bool]string{true: "\n" + block(status)}[status != ""], "inget"),
		upLine, or(map[bool]string{true: "\n" + block(unpushed)}[unpushed != ""], "inget"),
		now.Format("15:04"), or(summary, "_Ingen Claude-session hittades i panen._"))
	if err := os.WriteFile(file, []byte(md), 0o644); err != nil {
		return nil, err
	}
	return map[string]string{"path": file}, nil
}
