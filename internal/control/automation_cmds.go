package control

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

// Local-Automations kommandon (veckoarkiv, synk, deploy, inloggningar, tjänster) som knappar och
// `devctl automation <kommando>`. Bara kommandona i listan kan köras, med kontrollerade argument.
// Snabba kommandon som bara läser körs direkt och svarar med utskriften. Resten öppnas i ett eget
// tmux-fönster, så att man ser förloppet och kan svara på frågor (t.ex. bekräftelsen vid permanent borttagning).

type AutomationCmd struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	Label string `json:"label"`
	Help  string `json:"help"`
	// Argumentets namn; tomt om kommandot inte tar något
	Arg      string   `json:"arg,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	// Körs direkt och svarar med utskriften (bara kommandon som läser)
	Inline bool `json:"inline"`
	// Ändrar eller tar bort något permanent; gränssnittet frågar först
	Danger bool `json:"danger,omitempty"`
	argv   func(arg string) []string
}

var (
	weekRE   = regexp.MustCompile(`^\d{4}-V\d{2}$`)
	emailRE  = regexp.MustCompile(`^[^\s@'"]+@[^\s@'"]+\.[a-z]{2,}$`)
	services = map[string]string{
		"sync": "com.erikjin.local-automation-sync", "archive": "com.erikjin.local-automation-archive",
		"ui": "com.erikjin.local-automation-ui",
	}
	serviceScript = map[string]string{"sync": "sync/install-sync.sh", "archive": "sync/install-archive.sh", "ui": "ui/install-ui.sh"}
)

func node(args ...string) []string { return append([]string{"node"}, args...) }

var automationCmds = []AutomationCmd{
	{ID: "archive-list", Group: "Veckoarkiv", Label: "Arkiv i molnet", Help: "Veckorna som finns i molnet och deras storlek", Inline: true,
		argv: func(string) []string { return node("sync/archive.mjs", "list") }},
	{ID: "archive-preview", Group: "Veckoarkiv", Label: "Förhandsvisa", Help: "Vad som skulle arkiveras nu, utan att ändra något", Inline: true,
		argv: func(string) []string { return node("sync/archive.mjs", "--dry-run") }},
	{ID: "archive-run", Group: "Veckoarkiv", Label: "Arkivera nu", Help: "Arkivera Macens lokala filer från avslutade veckor (körs annars måndagar 01:00; Firestore arkiveras i molnet 00:30)",
		argv: func(string) []string { return node("sync/archive.mjs") }},
	{ID: "archive-restore", Group: "Veckoarkiv", Label: "Hämta tillbaka", Help: "Packa upp en vecka till valvet: Arkiv/<vecka>/", Arg: "vecka",
		argv: func(w string) []string { return node("sync/archive.mjs", "restore", w) }},
	{ID: "archive-delete", Group: "Veckoarkiv", Label: "Ta bort permanent", Help: "Ta bort en vecka från molnet. Går inte att ångra; terminalen frågar en gång till", Arg: "vecka", Danger: true,
		argv: func(w string) []string { return node("sync/archive.mjs", "delete", w) }},
	{ID: "archive-log", Group: "Veckoarkiv", Label: "Logg", Help: "Slutet av logs/archive.log", Inline: true,
		argv: func(string) []string { return []string{"tail", "-n", "80", "logs/archive.log"} }},

	{ID: "sync-run", Group: "Synk", Label: "Synka nu", Help: "Synka Apple Notes och valven med molnet (körs annars var 10:e minut)",
		argv: func(string) []string { return node("sync/sync.mjs") }},
	{ID: "sync-log", Group: "Synk", Label: "Logg", Help: "Slutet av logs/sync.log", Inline: true,
		argv: func(string) []string { return []string{"tail", "-n", "80", "logs/sync.log"} }},

	{ID: "deploy", Group: "Moln", Label: "Deploya", Help: "Bygg och deploya jobb, API, webbapp och scheman, och ladda upp tokens från .env. Tar några minuter",
		Arg: "e-post", Optional: true,
		argv: func(e string) []string { return []string{"./cloud/deploy.sh", e} }},

	{ID: "google-login", Group: "Inloggningar", Label: "Google", Help: "Logga in Google-kontot (privat eller jobb) i webbläsaren", Arg: "konto", Choices: []string{"privat", "jobb"}, Optional: true,
		argv: func(k string) []string {
			if k == "jobb" {
				return []string{"./mcp/google-login.sh", "work"}
			}
			return []string{"./mcp/google-login.sh"}
		}},

	{ID: "services", Group: "Tjänster", Label: "Status", Help: "Vilka av Local-Automations tjänster som är installerade på Macen", Inline: true}, // argv nil: devctl svarar själv
	{ID: "service-install", Group: "Tjänster", Label: "Installera", Help: "Installera eller starta om en tjänst (launchd)", Arg: "tjänst", Choices: []string{"sync", "archive", "ui"},
		argv: func(s string) []string { return []string{"./" + serviceScript[s]} }},
	{ID: "service-remove", Group: "Tjänster", Label: "Stäng av", Help: "Avinstallera en tjänst (launchd)", Arg: "tjänst", Choices: []string{"sync", "archive", "ui"}, Danger: true,
		argv: func(s string) []string { return []string{"./" + serviceScript[s], "--remove"} }},
}

func findAutomationCmd(id string) *AutomationCmd {
	for i := range automationCmds {
		if automationCmds[i].ID == id {
			return &automationCmds[i]
		}
	}
	return nil
}

func (c *Controller) automationCommands(Args) (any, error) { return automationCmds, nil }

func (cmd *AutomationCmd) checkArg(arg string) (string, error) {
	if cmd.Arg == "" {
		return "", nil
	}
	if arg == "" && cmd.Optional {
		return "", nil
	}
	switch {
	case len(cmd.Choices) > 0:
		for _, ch := range cmd.Choices {
			if arg == ch {
				return arg, nil
			}
		}
		return "", fmt.Errorf("%s måste vara %s", cmd.Arg, strings.Join(cmd.Choices, " eller "))
	case cmd.Arg == "vecka" && !weekRE.MatchString(arg):
		return "", fmt.Errorf("ange veckan som ÅÅÅÅ-Vvv, t.ex. 2026-V44")
	case cmd.Arg == "e-post" && !emailRE.MatchString(arg):
		return "", fmt.Errorf("ogiltig e-postadress")
	}
	return arg, nil
}

// ResolveAutomation tar fram katalog och kommandorad för ett kommando, med kontrollerat argument.
// argv är tomt för kommandon som devctl utför själv (status för tjänsterna).
func ResolveAutomation(cfg config.Config, id, arg string) (cmd *AutomationCmd, dir string, argv []string, err error) {
	dir = cfg.Automation.Dir
	if dir == "" {
		return nil, "", nil, fmt.Errorf("sätt automation.dir i %s", config.Path())
	}
	if _, err := os.Stat(filepath.Join(dir, "sync.json")); err != nil {
		return nil, "", nil, fmt.Errorf("hittar inte Local-Automation i %s", dir)
	}
	if cmd = findAutomationCmd(id); cmd == nil {
		return nil, "", nil, fmt.Errorf("okänt kommando %q (se devctl automation)", id)
	}
	if arg, err = cmd.checkArg(arg); err != nil {
		return nil, "", nil, err
	}
	if cmd.argv == nil {
		return cmd, dir, nil, nil
	}
	if cmd.ID == "deploy" && arg == "" {
		// Deployen behöver ägarens Google-konto; standard är kontot gcloud är inloggat med
		out, _ := sysx.Run(10*time.Second, "gcloud", "config", "get-value", "account")
		if arg = strings.TrimSpace(out); !emailRE.MatchString(arg) {
			return nil, "", nil, errors.New("ange e-postadressen för Google-kontot som äger webbappen")
		}
	}
	return cmd, dir, cmd.argv(arg), nil
}

// automationCmd kör ett kommando: direkt om det bara läser, annars i ett eget tmux-fönster.
func (c *Controller) automationCmd(a Args) (any, error) {
	cmd, dir, argv, err := ResolveAutomation(c.cfg, a.S("cmd"), a.S("arg"))
	if err != nil {
		return nil, err
	}
	if argv == nil {
		return map[string]string{"output": ServiceStatus()}, nil
	}
	if cmd.Inline {
		out, err := sysx.RunIn(dir, 2*time.Minute, argv[0], argv[1:]...)
		if err != nil && strings.TrimSpace(out) == "" {
			return nil, err
		}
		return map[string]string{"output": strings.TrimRight(out, "\n")}, nil
	}

	wid, pane, err := newWindowIn(c.cfg.Session, "la:"+cmd.ID, dir)
	if err != nil {
		return nil, err
	}
	_, _ = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
	quoted := make([]string, len(argv))
	for i, s := range argv {
		quoted[i] = shellQuote(s)
	}
	line := strings.Join(quoted, " ") + `; echo; echo "── klart (exit $?) ──"`
	c.col.Refresh(false)
	return map[string]string{"pane": pane, "window": wid, "command": strings.Join(argv, " ")}, runIn(pane, line)
}

// AutomationCommands är listan över kommandon, för CLI:ts hjälptext.
func AutomationCommands() []AutomationCmd { return automationCmds }

// ServiceStatus visar vilka launchd-tjänster som är installerade.
func ServiceStatus() string {
	uid := fmt.Sprint(os.Getuid())
	var b strings.Builder
	for _, k := range []string{"sync", "archive", "ui"} {
		label := services[k]
		out, err := sysx.Run(5*time.Second, "launchctl", "print", "gui/"+uid+"/"+label)
		status := "inte installerad"
		if err == nil {
			status = "installerad"
			for _, l := range strings.Split(out, "\n") {
				if s := strings.TrimSpace(l); strings.HasPrefix(s, "state = ") {
					status += ", " + strings.TrimPrefix(s, "state = ")
					break
				}
			}
		}
		fmt.Fprintf(&b, "%-8s %-40s %s\n", k, label, status)
	}
	return strings.TrimRight(b.String(), "\n")
}
