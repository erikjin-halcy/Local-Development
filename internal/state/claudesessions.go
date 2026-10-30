package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

// ClaudeSession är en körande Claude Code-session enligt Claude Codes register (~/.claude/sessions/<pid>.json).
type ClaudeSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	Tmux      string `json:"tmux"` // session:@fönster.%pane
	Socket    string `json:"messagingSocketPath"`
	Status    string `json:"status"`
}

// ClaudeSessions läser registret. Sessioner vars process inte längre finns hoppas över.
func ClaudeSessions() []ClaudeSession {
	files, _ := filepath.Glob(config.Home("~/.claude/sessions/*.json"))
	var out []ClaudeSession
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s ClaudeSession
		if json.Unmarshal(b, &s) != nil || s.PID <= 0 || syscall.Kill(s.PID, 0) != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

// SessionBySocket hittar sessionen med en viss meddelandeadress (uds:/tmp/cc-socks/<pid>.sock).
func SessionBySocket(addr string) *ClaudeSession {
	path := strings.TrimPrefix(addr, "uds:")
	for _, s := range ClaudeSessions() {
		if s.Socket == path {
			return &s
		}
	}
	return nil
}

// SessionInPane hittar sessionen som körs i en tmux-pane (%id).
func SessionInPane(pane string) *ClaudeSession {
	for _, s := range ClaudeSessions() {
		if strings.HasSuffix(s.Tmux, "."+pane) {
			return &s
		}
	}
	return nil
}

// SessionByPID hittar sessionen med ett visst process-id.
func SessionByPID(pid int) *ClaudeSession {
	for _, s := range ClaudeSessions() {
		if s.PID == pid {
			return &s
		}
	}
	return nil
}
