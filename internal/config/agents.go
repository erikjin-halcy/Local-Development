package config

// Agenter som lever i devctl: var och en är en YAML-fil i ~/.config/devctl/agents/<namn>.yaml.
// En agent körs av devctl med Claude Code utan terminal (claude -p), bara med de verktyg den får,
// och lämnar fynd i ett fast format. Fynd över en viss allvarlighet skickas till orkestratorn.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Agent struct {
	Name        string `yaml:"-" json:"name"`
	Label       string `yaml:"label" json:"label"`
	Icon        string `yaml:"icon" json:"icon"`
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	Description string `yaml:"description" json:"description"`
	// Hur ofta agenten får köras, t.ex. 2h eller 30m
	Every string `yaml:"every" json:"every"`
	// Kör bara för worktrees som ändrats sedan förra granskningen
	OnlyOnChanges bool `yaml:"only_on_changes" json:"onlyOnChanges"`
	// active (aktiva repos som inte är arkiverade), all, eller en lista med repos
	Repos any `yaml:"repos" json:"repos"`
	// Lägsta allvarlighet som skickas till orkestratorn: hög, medel, låg eller aldrig
	Notify string `yaml:"notify" json:"notify"`
	// Verktyg agenten får använda (Claude Codes namn, t.ex. Read, Grep, Bash(git diff:*))
	Tools []string `yaml:"tools" json:"tools"`
	// Extra filer med regler som agenten ska läsa (t.ex. företagsregler i Obsidian)
	Rules  []string `yaml:"rules" json:"rules"`
	Model  string   `yaml:"model,omitempty" json:"model,omitempty"`
	Prompt string   `yaml:"prompt" json:"prompt"`
}

func AgentsDir() string { return filepath.Join(Dir(), "agents") }

func AgentFile(name string) string { return filepath.Join(AgentsDir(), name+".yaml") }

// Agents läser alla agenter. Fel i en fil returneras per namn.
func Agents() ([]Agent, map[string]error) {
	files, _ := filepath.Glob(filepath.Join(AgentsDir(), "*.yaml"))
	sort.Strings(files)
	var out []Agent
	errs := map[string]error{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		b, err := os.ReadFile(f)
		if err != nil {
			errs[name] = err
			continue
		}
		var a Agent
		if err := yaml.Unmarshal(b, &a); err != nil {
			errs[name] = err
			continue
		}
		a.Name = name
		if a.Label == "" {
			a.Label = name
		}
		if a.Every == "" {
			a.Every = "2h"
		}
		if a.Notify == "" {
			a.Notify = "medel"
		}
		for i, r := range a.Rules {
			a.Rules[i] = Home(r)
		}
		out = append(out, a)
	}
	return out, errs
}

// RepoScope svarar på om agenten ska granska ett repo.
func (a Agent) RepoScope(name string, active, archived bool) bool {
	switch v := a.Repos.(type) {
	case nil:
		return active && !archived
	case string:
		if v == "all" {
			return !archived
		}
		return active && !archived
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.EqualFold(s, name) {
				return true
			}
		}
	}
	return false
}

// CompanyRulesFile är filen med företagsregler i valvet (skapas med en mall).
func CompanyRulesFile(cfg Config) string {
	if cfg.Obsidian.Vault == "" {
		return filepath.Join(Dir(), "Företagsregler.md")
	}
	return filepath.Join(cfg.Obsidian.Vault, "Regler", "Företagsregler.md")
}

const companyRulesTemplate = `---
tags: [regler]
---
# Företagsregler

Regler som säkerhetsagenten i devctl granskar ändringar mot, utöver allmänna säkerhetsprinciper och
repots egna regler (CLAUDE.md, AGENTS.md, SECURITY.md). Skriv en regel per punkt, gärna med ett id.

## Data och integritet
- R1: Personuppgifter (namn, telefon, e-post, personnummer) får inte loggas eller skickas till tredje part utan att det är dokumenterat.
- R2: Kunddata från produktion får inte checkas in eller användas i testdata.

## Hemligheter
- R3: Inga nycklar, tokens eller lösenord i koden eller i git-historiken. Använd hemlighetshanteringen.

## Ändringar
- R4: Databasmigreringar ska kunna köras om och ha en väg tillbaka.
- R5: Ändringar i inloggning, behörigheter eller betalningar ska granskas av en människa innan de mergas.
`

func defaultSecurityAgent(rules string) string {
	return `# Säkerhetsagenten: granskar ändringar i aktiva repos och flaggar det som går emot
# allmänna säkerhetsprinciper, repots egna regler eller företagsreglerna.
label: Säkerhet
icon: 🛡
enabled: true
description: Granskar nya ändringar mot säkerhetsprinciper, repots regler och företagsregler.
every: 2h
only_on_changes: true
repos: active
notify: medel
tools: [Read, Grep, Glob, "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)", "Bash(git ls-files:*)"]
rules:
  - "` + rules + `"
prompt: |
  Du är en säkerhetsgranskare. Granska ändringarna i worktreen nedan och flagga bara det som faktiskt
  bryter mot en regel. Hitta inte på problem; inga fynd är ett bra svar.

  Granska mot:
  1. Allmänna säkerhetsprinciper: hemligheter i kod eller config (nycklar, tokens, lösenord),
     injektion (SQL, kommandon, sökvägar), saknad behörighetskontroll, osäker hantering av
     användardata och personuppgifter, osäkra beroenden eller inställningar (t.ex. CORS *, debug i
     produktion, avstängd TLS-kontroll), loggning av känsliga uppgifter.
  2. Repots egna regler: läs CLAUDE.md, AGENTS.md, SECURITY.md och CONTRIBUTING.md om de finns.
  3. Företagsreglerna i regelfilerna nedan.

  Granska ändringarna, inte hela repot: ocommittade ändringar (git diff HEAD, och nya filer i git
  status) och commits i intervallet nedan (git log -p). Läs omgivande kod när du behöver sammanhang.
  Allvarlighet: hög = kan läcka data eller ge obehörig åtkomst nu; medel = bryter mot en regel men
  utan omedelbar risk; låg = bör åtgärdas.
`
}

// EnsureDefaultAgents skapar säkerhetsagenten och mallen för företagsregler första gången.
func EnsureDefaultAgents(cfg Config) {
	_ = os.MkdirAll(AgentsDir(), 0o755)
	rules := CompanyRulesFile(cfg)
	if _, err := os.Stat(rules); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(rules), 0o755)
		_ = os.WriteFile(rules, []byte(companyRulesTemplate), 0o644)
	}
	if files, _ := filepath.Glob(filepath.Join(AgentsDir(), "*.yaml")); len(files) == 0 {
		short := rules
		if h := Home("~"); strings.HasPrefix(rules, h) {
			short = "~" + strings.TrimPrefix(rules, h)
		}
		_ = os.WriteFile(AgentFile("sakerhet"), []byte(defaultSecurityAgent(short)), 0o644)
	}
}

// AgentTemplate är innehållet i en ny agent som användaren skapar.
func AgentTemplate(label string) string {
	return `# Ny agent i devctl. Den körs med Claude Code utan terminal, i varje repo som omfattas,
# och lämnar fynd som visas i fliken Agenter. Fynd från och med "notify" skickas till orkestratorn.
label: ` + label + `
icon: 🤖
enabled: false        # slå på när prompten är klar
description: Vad agenten gör, i en mening.
every: 6h             # hur ofta den får köras
only_on_changes: true # bara worktrees som ändrats sedan förra körningen
repos: active         # active, all, eller en lista: [Halcy-app, Dentio]
notify: medel         # hög, medel, låg eller aldrig
tools: [Read, Grep, Glob, "Bash(git diff:*)", "Bash(git log:*)"]
rules: []             # filer med regler att läsa, t.ex. i Obsidian
prompt: |
  Beskriv vad agenten ska leta efter och hur allvarligt det är.
`
}
