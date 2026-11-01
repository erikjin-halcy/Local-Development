package control

// Arbetsflöde: skicka ett av repots Claude-skills (t.ex. /verify) till Claude-sessionen i en worktree.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/erikjin-halcy/Local-Development/internal/state"
)

var (
	dimRe  = regexp.MustCompile(`\x1b\[2m.*?(\x1b\[(?:0|22)?m|$)`)
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
)

// promptInput läser texten i Claude Codes inmatningsruta (mellan de två sista ─-linjerna) ur en
// capture-pane -e. Grå förslagstext räknas inte. ok är false om rutan inte hittas.
func promptInput(capture string) (text string, ok bool) {
	lines := strings.Split(strings.TrimRight(capture, "\n"), "\n")
	var rules []int
	for i, l := range lines {
		if p := strings.TrimSpace(ansiRe.ReplaceAllString(l, "")); strings.Count(p, "─") >= 10 && strings.Trim(p, "─") == "" {
			rules = append(rules, i)
		}
	}
	if len(rules) < 2 {
		return "", false
	}
	a, b := rules[len(rules)-2], rules[len(rules)-1]
	var parts []string
	for _, l := range lines[a+1 : b] {
		l = ansiRe.ReplaceAllString(dimRe.ReplaceAllString(l, ""), "")
		l = strings.TrimLeft(l, "  ")
		l = strings.TrimPrefix(l, "❯")
		if l = strings.Trim(l, "  \t"); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, "\n"), true
}

// flowSend skickar /<skill> [args] till Claude i worktreen. Skills som deployar eller mergar (confirm i
// arbetsflödesfilen) skickas bara med confirm. Utan Claude-session returneras noSession, så att
// gränssnittet kan erbjuda att starta en worker med kommandot.
func (c *Controller) flowSend(a Args) (any, error) {
	if err := a.need("repo", "path", "skill"); err != nil {
		return nil, err
	}
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	var wt *state.Worktree
	for i := range r.Worktrees {
		if r.Worktrees[i].Path == a.S("path") {
			wt = &r.Worktrees[i]
		}
	}
	if wt == nil {
		return nil, fmt.Errorf("%s har ingen worktree i %s", r.Name, a.S("path"))
	}
	skill := strings.TrimPrefix(a.S("skill"), "/")
	if !slices.ContainsFunc(r.Skills, func(s state.Skill) bool { return s.Name == skill }) {
		return nil, fmt.Errorf("%s har inget skill som heter %s", r.Name, skill)
	}
	if r.Flow != nil && slices.Contains(r.Flow.Confirm, skill) && !a.B("confirm", false) {
		return nil, fmt.Errorf("/%s deployar eller mergar och kräver bekräftelse", skill)
	}
	text := "/" + skill
	if args := strings.TrimSpace(a.Raw("args")); args != "" {
		text += " " + args
	}

	var busy []string
	for _, pane := range wt.Panes {
		s := state.SessionInPane(pane)
		if s == nil {
			continue
		}
		if s.Status != "idle" {
			busy = append(busy, pane)
			continue
		}
		capture, err := tmux("capture-pane", "-p", "-e", "-t", pane)
		if err != nil {
			return nil, err
		}
		in, ok := promptInput(capture)
		if !ok {
			return nil, fmt.Errorf("hittar inte Claudes inmatningsrad i %s", pane)
		}
		if in != "" {
			return nil, fmt.Errorf("inmatningsraden i %s har redan text (%q); skicka eller rensa den först", pane, clip(in, 60))
		}
		logSent(sentEntry{Pane: pane, Text: text, Via: "workflow", By: "Arbetsflöde", Where: r.Name + "@" + wt.Branch})
		if err := SendText(pane, text, true); err != nil {
			return nil, err
		}
		return map[string]any{"pane": pane, "text": text}, nil
	}
	if len(busy) > 0 {
		return nil, fmt.Errorf("Claude i %s arbetar; vänta tills den är klar", strings.Join(busy, ", "))
	}
	return map[string]any{"noSession": true, "text": text, "branch": wt.Branch}, nil
}
