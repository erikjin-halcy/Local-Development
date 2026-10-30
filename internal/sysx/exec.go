// Package sysx kör externa kommandon (tmux, git, lsof, docker, gh) med timeout.
package sysx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Env är processens miljö utan TMUX, så att tmux-kommandon fungerar även när devctl startats inifrån tmux.
var Env = func() []string {
	var out []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TMUX=") {
			out = append(out, e)
		}
	}
	return out
}()

// Run kör ett kommando och returnerar stdout. Felet innehåller stderr.
func Run(timeout time.Duration, name string, args ...string) (string, error) {
	return RunIn("", timeout, name, args...)
}

// RunIn kör ett kommando i katalogen dir.
func RunIn(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = name + ": tidsgränsen överskreds"
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

// Has svarar på om ett program finns i PATH.
func Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Combined kör ett kommando och returnerar stdout och stderr tillsammans.
func Combined(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = Env
	out, err := cmd.CombinedOutput()
	return string(out), err
}
