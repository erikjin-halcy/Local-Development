---
name: devctl
description: Kontrollpanel för lokalt utvecklingsarbete (localhost:7878) – git-repos och worktrees per branch, vilka tmux-panes, lokala servrar (frontend/backend/databas) och Docker/OrbStack-containrar som hör till vilken branch, tmux-styrning (sessioner, fönster, panes, namngivning, fönsterhierarki, orkestratorfönster) och en global Claude-orkestrator. Använd när användaren vill se eller öppna översikten, fråga vad som körs eller vilken branch något jobbar på, skapa worktrees eller workers, styra tmux, se servrar eller containrar, eller prata med orkestratorn. Ersätter tmux-control.
---

# devctl

`devctl` är en Go-binär (källkod: https://github.com/erikjin-halcy/Local-Development) som ger ett
webbgränssnitt på http://localhost:7878 och ett CLI för samma saker.

## Öppna översikten
```bash
devctl            # startar servern vid behov och öppnar webbläsaren
devctl status     # samma läge som text: repos → worktrees → panes/servrar/containrar
```
Flikar går att länka till direkt: `http://localhost:7878/#tmux`, `#servers`, `#docker`, `#orchestrator`.

## Vanliga frågor och kommandon
| Användaren vill … | Kommando |
|---|---|
| se vad som körs på vilken branch | `devctl status` (`--all` för alla repos, `--json` för allt) |
| se branches/commitgraf | `devctl graph <repo>` |
| ny worktree | `devctl worktree add <repo> <branch> [--base main]` |
| starta en Claude-worker på en branch (ny pane i repots session, titel "<repo> · <feature>") | `devctl worker start <repo> <branch> --feature "…" [--task "…"]` |
| stänga en worker-pane efter användarens ja / skriva sessionsrapport | `devctl report %ID` och sedan `devctl pane close %ID --yes` (se skillen tmux) |
| lista/läsa/skriva till tmux-panes | `devctl pane list`, `devctl pane capture %ID -n 120`, `devctl pane send %ID "text"` |
| anteckning för en branch i Obsidian | `devctl note <repo> <branch> [--open]` |
| dagens utvecklingslogg i Obsidian | `devctl log [--open]` |
| starta/visa den globala orkestratorn | `devctl orchestrator` |
| kom ihåg en fråga till senare (se skillen `parkera`) | `devctl park "<text>"`, `devctl park list`, `devctl park done <id>` |
| köra skript, Metro, deploy eller ett workflow | `devctl actions <repo>`, `devctl run <repo> <action>` |
| öppna eller stoppa en lokal server | `devctl open 8080`, `devctl stop 8080 --yes` |
| se vad alla Claude-sessioner gör | `devctl sessions`, `devctl summarize all` |
| agenternas fynd (t.ex. säkerhet) | `devctl agents`, `devctl agents findings sakerhet` |

Repo anges med namn (`Dentio`) eller sökväg. tmux-mål anges med ID (`%12`, `@3`, `$1`).
Allt annat (byta namn på fönster, dela panes, layout, fönsterhierarki, lokala orkestratorfönster,
stoppa servrar, starta om containrar) finns i webbgränssnittet.

## Bra att veta
- Konfiguration: `~/.config/devctl/config.json` (rötter att leta repos i, Obsidian-valv, kommandon).
- Servern startar automatiskt vid inloggning efter `devctl autostart`.
- Orkestratorn kör i tmux-sessionen `devctl` och har förhandsgodkänt bara `devctl`-kommandon.
