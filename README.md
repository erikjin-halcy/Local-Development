# devctl

En kontrollpanel för lokalt utvecklingsarbete. Ett ställe där du ser och styr allt som rör det du
jobbar med just nu:

- **Git**: alla repos, deras worktrees och branches, med ocommittade ändringar, ahead/behind,
  senaste commit och öppna PR:er från GitHub. Commitgraf per repo.
- **Vad som körs var**: vilka tmux-panes, lokala servrar och Docker/OrbStack-containrar som hör till
  varje worktree, och därmed vilken branch de jobbar på.
- **Lokala servrar**: alla processer som lyssnar på en port, klassade som frontend, backend, databas,
  Docker eller AI, kopplade till repo, branch och den tmux-pane som startade dem.
- **Docker/OrbStack**: containrar grupperade per compose-/Supabase-projekt, med start, stopp och loggar.
- **tmux**: sessioner, fönster och panes som karta eller träd. Skapa, namnge, dela, välj layout,
  bygg fönsterhierarkier och skapa lokala orkestratorfönster med workers.
- **Global orkestrator**: en Claude Code-session som ser hela läget via `devctl` och kan skapa
  worktrees och starta workers på en branch. Du chattar med den direkt i webbgränssnittet.
- **Local-Automation**: dina schemalagda Claude-uppgifter i en egen flik (dess gränssnitt bäddas in),
  med status i menyraden.
- **Obsidian**: en anteckning per branch och en daglig utvecklingslogg i ett valv. Orkestratorn
  får läsa de valv du väljer som kontext.

Allt körs lokalt. Servern lyssnar bara på `127.0.0.1` och avvisar anrop från andra webbplatser.

## Desktop-app (macOS)

`Devctl.app` är ett vanligt Mac-program: öppna det så ser du direkt alla repos, worktrees,
tmux-panes, servrar och containrar. Appen innehåller `devctl` och startar servern själv om den
inte redan körs (och stänger den när du avslutar, om appen startade den). En ikon i menyraden visar
hur många branches som har något igång och ger snabb åtkomst till servrar, worktrees, orkestratorn
och Local-Automation.

**Skal och motor.** Appen är ett tunt skal som nästan aldrig ändras. Motorn (servern och hela
webbgränssnittet) är `~/.local/bin/devctl`. Skalet startar motorn, startar om den när en ny version
installerats och laddar då om fönstret. Eftersom skalet är oförändrat behåller det de behörigheter
macOS gett det (t.ex. att läsa mappen Skrivbord), så du behöver bara godkänna en gång.

```bash
make install       # ny motor; appen byter till den inom några sekunder
make install-app   # som ovan, och byter appskalet bara om dess egen kod ändrats
```

Första gången frågar macOS om appen får läsa mapparna där dina repos ligger. Svara Tillåt. Har du
nekat visar appen en varning med en knapp till Systeminställningar → Integritet och säkerhet →
Filer och mappar. Länkar till GitHub
och Obsidian öppnas i respektive program. ⌘1–⌘5 byter flik, ⌘R laddar om.

**Ladda ned färdigbyggd:** under [Releases](https://github.com/erikjin-halcy/Local-Development/releases)
finns `Devctl-<version>-macos.zip` (appen) och `devctl-<version>-macos-universal.tar.gz` (bara CLI:t),
byggda för både Apple Silicon och Intel. Appen är inte signerad med ett Developer ID, så första
gången: högerklicka → Öppna, eller kör `xattr -dr com.apple.quarantine /Applications/Devctl.app`.

En ny release skapas genom att tagga en version: `git tag v0.2.0 && git push --tags`.

## Installera CLI:t från källkod

Kräver Go 1.24+, `git` och `tmux`. Valfritt: `gh` (PR:er), `docker`/OrbStack, Obsidian, Claude Code.

```bash
go install github.com/erikjin-halcy/Local-Development/cmd/devctl@latest   # eller:
make install         # bygger till ~/.local/bin/devctl
devctl               # startar servern och öppnar http://localhost:7878
devctl autostart     # starta vid inloggning (--remove för att ta bort)
```

## Använda från terminalen

```bash
devctl status [--all] [--json]                      # repos → worktrees → panes/servrar/containrar
devctl graph <repo>                                 # commitgraf över alla branches
devctl worktree add <repo> <branch> [--base main]   # ny worktree (ny eller befintlig branch)
devctl worktree rm <sökväg> [--force]
devctl worker start <repo> <branch> [--task "…"]    # worktree + tmux-fönster med Claude
devctl pane list | send %ID "text" | keys %ID Escape | capture %ID -n 120
devctl note <repo> <branch> [--open]                # branchens anteckning i Obsidian
devctl log [--open]                                 # dagens utvecklingslogg i Obsidian
devctl orchestrator                                 # starta/visa den globala orkestratorn
devctl brief                                        # orkestratorns instruktioner
```

CLI:t pratar med servern (som startas automatiskt), så samma kommandon fungerar för skript och
för agenter. Den globala orkestratorn styr allt genom dem.

## Konfiguration

`~/.config/devctl/config.json` skapas första gången:

```json
{
  "port": 7878,
  "roots": ["~/Desktop"],
  "scanDepth": 3,
  "session": "devctl",
  "orchestratorCmd": "claude",
  "workerCmd": "claude",
  "editor": "code",
  "obsidian": {
    "vault": "~/…/Obsidian/Mitt valv",
    "projectDir": "Projekt",
    "logDir": "Dev-logg",
    "readVaults": ["~/…/Annat valv"]
  },
  "logEveryMinutes": 30,
  "automation": {
    "url": "http://localhost:4777",
    "cloudURL": "https://…web.app",
    "dir": "~/…/Local-Automation"
  }
}
```

- `roots`: var repos letas upp. Repos där en tmux-pane eller server körs hittas även utanför rötterna.
- `session`: tmux-sessionen där orkestratorn och workers skapas.
- `obsidian.vault`: valvet där devctl skriver. Varje repo du jobbar med får en mapp
  `<projectDir>/<repo>/` med startsidan `<repo>.md` (GitHub, branches och öppna PR:er uppdateras
  automatiskt; resten skriver du själv) och branch-anteckningar i `Branches/`. Dagliga loggar
  hamnar i `Dev-logg/ÅÅÅÅ-MM-DD.md`. I loggen skrivs bara delen mellan `devctl`-markeringarna om;
  egna anteckningar i filen bevaras.
- `automation`: Local-Automations lokala gränssnitt (`url`), som visas i Automation-fliken. `cloudURL`
  ger en genväg till molnappen och `dir` gör att devctl kan starta gränssnittet om det inte körs.
- `logEveryMinutes`: hur ofta dagens logg uppdateras medan servern körs (0 = bara på begäran).

## Kör: skript, Metro, deploy och egna actions

**Kör ▾** på varje branchrad listar allt som går att köra i den worktreen, grupperat i Egna, Starta,
Deploy, Bygg, Kontroll, Databas och Övrigt:

- `package.json`-skript, även i workspaces (t.ex. Metro i `apps/mobile`), med rätt pakethanterare
- mål i `Makefile` och `justfile`
- deploy som känns igen från `firebase.json`, `fly.toml` och `vercel.json`
- egna actions i `~/.config/devctl/actions/<repo>.yaml` (lokalt, aldrig i repot):

```yaml
actions:
  - label: Deploy staging
    run: ./scripts/deploy.sh staging
    group: Deploy
    danger: true        # frågar innan
  - label: Metro (rensa cache)
    run: npx expo start --clear
    dir: apps/mobile
```

**Workflows** är flera steg i följd, i samma YAML-fil:

```yaml
workflows:
  - label: Starta allt
    steps:
      - action: dev:disposable-db   # en action ur Kör-menyn, eller run: <kommando>
        wait: port 54322            # exit (standard), port N, 10s eller none
      - action: dev
        wait: port 8080
      - action: Metro (start) · apps/mobile
        wait: none
```

Med `wait: exit` väntar workflowet tills steget är klart och stoppar om det misslyckas. Varje steg körs
i ett eget tmux-fönster och förloppet visas steg för steg.

**GitHub Actions**: varje branch visar CI-status (✓ ✗ ⏳) från GitHub, med loggar och **Kör om** för
misslyckade körningar. Workflows med `workflow_dispatch` går att starta för hand på branchen från
Kör-menyn, med formulär för deras parametrar.

Actions som ser riskabla ut (deploy, migrering, reset, push …) frågar först. De körs i ett eget
tmux-fönster i worktreen. Från terminalen: `devctl actions <repo>` och `devctl run <repo> <action> [--branch b] [--yes]`.

## Claude-kontext per repo

Varje repo du jobbar med har en mapp `Claude/` i sin Obsidian-mapp (`<projectDir>/<repo>/Claude/`):

- **`CLAUDE.md`**: dina personliga instruktioner för repot. devctl lägger en `CLAUDE.local.md` i varje
  worktree som importerar filen, så Claude Code läser in den i alla repots worktrees. `CLAUDE.local.md`
  står i `.git/info/exclude` och committas aldrig. Första gången i ett repo frågar Claude Code en gång
  om importen får läsas in. Har repot en `AGENTS.md` men ingen `CLAUDE.md` importeras även den, eftersom
  Claude annars slutar läsa `AGENTS.md`.
- **`Sessioner/`**: en sammanfattning per Claude-session i repot: uppdraget, senaste önskemål, ändrade
  filer, senaste svaret och kommandot för att fortsätta (`claude --resume`). En session hör till repot
  den startades i, eller annars det repo där den ändrat flest filer. Sammanfattningen uppdateras högst
  en gång i timmen och bara medan sessionen är aktiv (dess logg har ändrats den senaste timmen).

Importen går via `~/.config/devctl/context/<repo>`, en länk utan mellanslag till valvets mapp, eftersom
Claude Codes `@`-import inte klarar mellanslag i sökvägar.

## Var allt ligger

| Vad | Var | På GitHub? |
|---|---|---|
| devctl:s kod | detta repo | ja |
| Motorn (CLI + server + gränssnitt) | `~/.local/bin/devctl` | nej, byggs med `make install` |
| Appen | `/Applications/Devctl.app` | nej, byggs med `make install-app` |
| Inställningar | `~/.config/devctl/config.json` | nej |
| Egna actions | `~/.config/devctl/actions/<repo>.yaml` | nej |
| Arkiverade repos | `~/.config/devctl/archive.json` | nej |
| Loggar, pid, orkestratorns instruktioner | `~/.cache/devctl/` | nej |
| Repo-mappar, branch-anteckningar, Claude-kontext, dagliga loggar | Obsidian-valvet i `obsidian.vault` | nej |
| Personlig `CLAUDE.local.md` i varje worktree | worktreen (utesluten via `.git/info/exclude`) | nej |

Inget av ditt eget sparas i repot, så samma kod fungerar för alla.

## Översikt: aktiva, inaktiva och arkiv

Översikten visar som standard bara **aktiva** repos: där något körs, med commits eller ändringar
de senaste 3 dagarna, eller med öppna PR:er. **Inaktiva** är resten. **Arkivera** gömmer ett repo
(sparas i `~/.config/devctl/archive.json`, inget i repot ändras). Börjar något köras i ett arkiverat
repo visas det ändå bland de aktiva. Knappen **?** i appen förklarar alla fält och knappar.

## Hur det fungerar

```
desktop/macos (Devctl.app, Swift/WKWebView) ─┐
web/index.html (inbäddad)  ──/api──►  internal/server
                                         ├─ internal/state    läser: tmux, git, gh, lsof/ps, docker
                                         └─ internal/control  gör: tmux, worktrees, docker, Obsidian, orkestrator
```

- **Koppling till branch**: varje pane, server och container har en katalog (panens `cwd`,
  processens `cwd`, compose-projektets arbetskatalog). Den matchas mot worktreens sökväg.
- **Server → pane**: processträdet följs uppåt tills en tmux-panes skalprocess hittas.
- **Containrar → server**: OrbStack/Docker-portar matchas mot containrarnas publicerade portar.
- **Uppdatering**: tmux, servrar och Docker läses varannan sekund, git var 10:e sekund, PR:er och
  nya repos varannan minut.
- **Fönsterhierarki** i tmux lagras som fönster-optionen `@parent`; fönstrens roll som `@kind`
  (`global-orchestrator`, `orchestrator`, `worker`) och panes roll som `@role`.

## Claude Code-skill

`skill/SKILL.md` lär Claude Code-sessioner att använda `devctl`. Länka in den:

```bash
ln -s "$PWD/skill" ~/.claude/skills/devctl
```
