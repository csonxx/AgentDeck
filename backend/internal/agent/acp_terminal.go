package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"
)

type acpTerminal struct {
	cmd       *exec.Cmd
	mu        sync.Mutex
	output    []byte
	limit     int
	truncated bool
	done      chan struct{}
	exit      *acp.TerminalExitStatus
}

func (t *acpTerminal) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output = append(t.output, b...)
	if len(t.output) > t.limit {
		t.output = t.output[len(t.output)-t.limit:]
		for len(t.output) > 0 && !utf8.RuneStart(t.output[0]) {
			t.output = t.output[1:]
		}
		t.truncated = true
	}
	return len(b), nil
}
func (r *acpRuntime) terminal(id acp.SessionId, key string) (*acpTerminal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.valid(id) {
		return nil, errors.New("wrong session")
	}
	t := r.terminals[key]
	if t == nil {
		return nil, errors.New("terminal unavailable")
	}
	return t, nil
}
func (r *acpRuntime) CreateTerminal(ctx context.Context, p acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	r.mu.Lock()
	valid, project := r.valid(p.SessionId), r.s.Project
	r.mu.Unlock()
	if !valid || p.Command == "" {
		return acp.CreateTerminalResponse{}, errors.New("invalid terminal request")
	}
	dir := project
	if p.Cwd != nil {
		dir = *p.Cwd
	}
	path, e := r.file(p.SessionId, dir, false)
	if e != nil {
		return acp.CreateTerminalResponse{}, e
	}
	st, e := os.Stat(path)
	if e != nil || !st.IsDir() {
		return acp.CreateTerminalResponse{}, errors.New("terminal directory unavailable")
	}
	// The native ACP agent owns permission decisions and sends
	// session/request_permission when its policy requires user confirmation.
	// Do not manufacture a second approval for terminal/create. Identity,
	// project-directory and resource bounds still apply to every request.
	r.mu.Lock()
	if len(r.terminals) >= 8 {
		r.mu.Unlock()
		return acp.CreateTerminalResponse{}, errors.New("terminal limit reached")
	}
	defer r.mu.Unlock()
	limit := 256 << 10
	if p.OutputByteLimit != nil && *p.OutputByteLimit > 0 && *p.OutputByteLimit < limit {
		limit = *p.OutputByteLimit
	}
	cmd := exec.CommandContext(nonNil(r.c.m.Context), p.Command, p.Args...)
	cmd.Dir = path
	cmd.Env = os.Environ()
	for _, v := range p.Env {
		if strings.ContainsAny(v.Name, "=\x00") || strings.ContainsRune(v.Value, '\x00') {
			return acp.CreateTerminalResponse{}, errors.New("invalid environment")
		}
		cmd.Env = append(cmd.Env, v.Name+"="+v.Value)
	}
	terminalProcessGroup(cmd)
	t := &acpTerminal{cmd: cmd, done: make(chan struct{}), limit: limit}
	cmd.Stdout = t
	cmd.Stderr = t
	if e = cmd.Start(); e != nil {
		return acp.CreateTerminalResponse{}, errors.New("terminal start failed")
	}
	key := UUID()
	r.terminals[key] = t
	go func() {
		_ = cmd.Wait()
		code := cmd.ProcessState.ExitCode()
		t.mu.Lock()
		t.exit = &acp.TerminalExitStatus{ExitCode: &code}
		t.mu.Unlock()
		close(t.done)
	}()
	return acp.CreateTerminalResponse{TerminalId: key}, nil
}
func (r *acpRuntime) TerminalOutput(ctx context.Context, p acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	t, e := r.terminal(p.SessionId, p.TerminalId)
	if e != nil {
		return acp.TerminalOutputResponse{}, e
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return acp.TerminalOutputResponse{Output: strings.ToValidUTF8(string(t.output), "�"), Truncated: t.truncated, ExitStatus: t.exit}, nil
}
func (r *acpRuntime) WaitForTerminalExit(ctx context.Context, p acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	t, e := r.terminal(p.SessionId, p.TerminalId)
	if e != nil {
		return acp.WaitForTerminalExitResponse{}, e
	}
	select {
	case <-ctx.Done():
		return acp.WaitForTerminalExitResponse{}, ctx.Err()
	case <-t.done:
		t.mu.Lock()
		defer t.mu.Unlock()
		return acp.WaitForTerminalExitResponse{ExitCode: t.exit.ExitCode, Signal: t.exit.Signal}, nil
	}
}
func (r *acpRuntime) KillTerminal(ctx context.Context, p acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	t, e := r.terminal(p.SessionId, p.TerminalId)
	if e != nil {
		return acp.KillTerminalResponse{}, e
	}
	killTerminalProcess(t.cmd)
	return acp.KillTerminalResponse{}, nil
}
func (r *acpRuntime) ReleaseTerminal(ctx context.Context, p acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	t, e := r.terminal(p.SessionId, p.TerminalId)
	if e != nil {
		return acp.ReleaseTerminalResponse{}, e
	}
	killTerminalProcess(t.cmd)
	r.mu.Lock()
	delete(r.terminals, p.TerminalId)
	r.mu.Unlock()
	return acp.ReleaseTerminalResponse{}, nil
}
func (r *acpRuntime) killTerminals() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, t := range r.terminals {
		killTerminalProcess(t.cmd)
		delete(r.terminals, key)
	}
}
