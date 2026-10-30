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
- **Obsidian**: en anteckning per branch och en daglig utvecklingslogg i ett valv. Orkestratorn
  får läsa de valv du väljer som kontext.

Allt körs lokalt. Servern lyssnar bara på `127.0.0.1` och avvisar anrop från andra webbplatser.

## Desktop-app (macOS)

`Devctl.app` är ett vanligt Mac-program: öppna det så ser du direkt alla repos, worktrees,
tmux-panes, servrar och containrar. Appen innehåller `devctl` och startar servern själv om den
inte redan körs (och stänger den när du avslutar, om appen startade den). Länkar till GitHub
och Obsidian öppnas i respektive program. ⌘1–⌘5 byter flik, ⌘R laddar om.

```bash
make install-app     # bygger dist/Devctl.app, kopierar till /Applications och installerar CLI:t
```

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
    "branchDir": "Branches",
    "logDir": "Dev-logg",
    "readVaults": ["~/…/Annat valv"]
  },
  "logEveryMinutes": 30
}
```

- `roots`: var repos letas upp. Repos där en tmux-pane eller server körs hittas även utanför rötterna.
- `session`: tmux-sessionen där orkestratorn och workers skapas.
- `obsidian.vault`: valvet där branch-anteckningar (`Branches/<repo>/<branch>.md`) och dagliga loggar
  (`Dev-logg/ÅÅÅÅ-MM-DD.md`) skrivs. I loggen skrivs bara delen mellan `devctl`-markeringarna om;
  egna anteckningar i filen bevaras.
- `logEveryMinutes`: hur ofta dagens logg uppdateras medan servern körs (0 = bara på begäran).

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
