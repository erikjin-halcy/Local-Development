// Package config läser ~/.config/devctl/config.json och skapar den med standardvärden första gången.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Obsidian struct {
	// Valv där devctl skriver anteckningar per branch och den dagliga loggen
	Vault string `json:"vault"`
	// Mapp i valvet med en undermapp per repo: <projectDir>/<repo>/<repo>.md och .../Branches/<branch>.md
	ProjectDir string `json:"projectDir"`
	LogDir     string `json:"logDir"`
	// Valv som orkestratorn får läsa som kontext
	ReadVaults []string `json:"readVaults"`
	// Repomapp per repo utanför arbetsvalvet (för repon vars material inte får synkas): repo-sidan,
	// Branches/, Claude/ och sessionsrapporterna hamnar där. Standard: <vault>/<projectDir>/<repo>.
	RepoDirs map[string]string `json:"repoDirs,omitempty"`
	// Mapp för sessionsrapporter per repo. Standard: <repomappen>/Claude/Sessioner.
	SessionReports map[string]string `json:"sessionReports,omitempty"`
}

// Automation är Local-Automation (schemalagda Claude-uppgifter) som visas i en egen flik.
type Automation struct {
	// Adress till Local-Automations lokala gränssnitt
	URL string `json:"url"`
	// Adress till molnversionen av webbappen (valfri)
	CloudURL string `json:"cloudURL"`
	// Repots katalog; används för att starta gränssnittet om det inte körs (valfri)
	Dir string `json:"dir"`
}

// Cloud: devctl skickar en sammanfattning till ett Firestore-dokument så att läget syns på mobilen.
type Cloud struct {
	Project      string `json:"project"`      // GCP-projekt ("" = av)
	Doc          string `json:"doc"`          // dokumentets sökväg, t.ex. agent/devctl
	EveryMinutes int    `json:"everyMinutes"` // hur ofta (skrivs bara om något ändrats, och minst en gång i timmen)
}

type Config struct {
	Port      int      `json:"port"`
	Roots     []string `json:"roots"`
	ScanDepth int      `json:"scanDepth"`
	// Session där orkestratorn och workers skapas
	Session         string     `json:"session"`
	OrchestratorCmd string     `json:"orchestratorCmd"`
	WorkerCmd       string     `json:"workerCmd"`
	Editor          string     `json:"editor"`
	Obsidian        Obsidian   `json:"obsidian"`
	Automation      Automation `json:"automation"`
	Cloud           Cloud      `json:"cloud"`
	// Minuter mellan automatiska uppdateringar av dagens logg (0 = av)
	LogEveryMinutes int `json:"logEveryMinutes"`
}

func Home(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, strings.TrimPrefix(p, "~"))
	}
	return p
}

func Dir() string {
	if d := os.Getenv("DEVCTL_HOME"); d != "" {
		return d
	}
	return Home("~/.config/devctl")
}

func CacheDir() string {
	d := Home("~/.cache/devctl")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func defaults() Config {
	return Config{
		Port: 7878, Roots: []string{"~/Desktop"}, ScanDepth: 3, Session: "devctl",
		OrchestratorCmd: "claude", WorkerCmd: "claude", Editor: "code",
		Obsidian:        Obsidian{ProjectDir: "Projekt", LogDir: "Dev-logg", ReadVaults: []string{}},
		LogEveryMinutes: 30,
		Automation:      Automation{URL: "http://localhost:4777"},
		Cloud:           Cloud{Doc: "agent/devctl", EveryMinutes: 15},
	}
}

// Load läser konfigurationen. Saknade fält får standardvärden.
func Load() (Config, error) {
	c := defaults()
	path := filepath.Join(Dir(), "config.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		_ = os.MkdirAll(Dir(), 0o755)
		out, _ := json.MarshalIndent(c, "", "  ")
		_ = os.WriteFile(path, append(out, '\n'), 0o644)
	} else if err != nil {
		return c, err
	} else if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if p := os.Getenv("DEVCTL_PORT"); p != "" {
		_ = json.Unmarshal([]byte(p), &c.Port)
	}
	for i, r := range c.Roots {
		c.Roots[i] = Home(r)
	}
	for i, r := range c.Obsidian.ReadVaults {
		c.Obsidian.ReadVaults[i] = Home(r)
	}
	c.Obsidian.Vault = Home(c.Obsidian.Vault)
	c.Automation.Dir = Home(c.Automation.Dir)
	return c, nil
}

func Path() string { return filepath.Join(Dir(), "config.json") }
