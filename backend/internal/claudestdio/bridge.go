// Package claudestdio multiplexes a desktop host's stdin with authenticated
// workstation-owner input. It never uses Claude's peer-message inbox.
package claudestdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const MaxInput = 40 << 20 // Six encrypted attachments total at most 24 MiB before base64.

type Record struct {
	PID     int    `json:"pid"`
	Start   string `json:"procStart"`
	Project string `json:"project"`
	Socket  string `json:"socket"`
}
type Input struct {
	Session string `json:"session_id"`
	UUID    string `json:"uuid"`
	Content any    `json:"content"`
}
type Reply struct {
	Written   bool   `json:"written"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Error     string `json:"error,omitempty"`
}

func private(path string, directory bool) error {
	s, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := s.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || s.Mode().Perm()&0077 != 0 || s.IsDir() != directory || (!directory && !s.Mode().IsRegular()) {
		return errors.New("unsafe Claude stdin bridge path")
	}
	return nil
}

func ReadRecord(root string, pid int) (Record, error) {
	var r Record
	if err := private(root, true); err != nil {
		return r, err
	}
	p := filepath.Join(root, strconv.Itoa(pid)+".json")
	if err := private(p, false); err != nil {
		return r, err
	}
	f, err := os.Open(p)
	if err != nil {
		return r, err
	}
	defer f.Close()
	err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&r)
	if err != nil || r.PID != pid || r.Socket != filepath.Join(root, strconv.Itoa(pid)+".sock") || r.Start == "" {
		return Record{}, errors.New("invalid Claude stdin bridge record")
	}
	return r, nil
}

func birth(pid int) (string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	cmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	b, err := cmd.Output()
	return strings.Join(strings.Fields(string(b)), " "), err
}

func validInput(in Input) bool {
	if len(in.Session) != 36 || (len(in.UUID) != 36 && len(in.UUID) != 32) {
		return false
	}
	for _, id := range []string{in.Session, in.UUID} {
		for i, c := range id {
			if len(id) == 36 && (i == 8 || i == 13 || i == 18 || i == 23) {
				if c != '-' {
					return false
				}
			} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}
	switch c := in.Content.(type) {
	case string:
		return c != "" && len(c) <= MaxInput
	case []any:
		if len(c) == 0 {
			return false
		}
		for _, v := range c {
			b, ok := v.(map[string]any)
			if !ok {
				return false
			}
			switch b["type"] {
			case "text":
				if _, ok := b["text"].(string); !ok || len(b) != 2 {
					return false
				}
			case "image":
				s, ok := b["source"].(map[string]any)
				if !ok || len(b) != 2 || len(s) != 3 || s["type"] != "base64" {
					return false
				}
				mime, ok := s["media_type"].(string)
				if !ok || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" && mime != "image/gif") {
					return false
				}
				if _, ok := s["data"].(string); !ok {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	return false
}

// Run preserves desktop input/output byte-for-byte. Only structured user input
// can enter the extra private socket; shell, RPC and permission controls cannot.
func Run(ctx context.Context, binary, root string, args []string, in io.Reader, out, stderr io.Writer) error {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(root) {
		return errors.New("absolute bridge paths required")
	}
	stream := false
	for i, arg := range args {
		if arg == "--input-format=stream-json" || (arg == "--input-format" && i+1 < len(args) && args[i+1] == "stream-json") {
			stream = true
		}
	}
	if !stream {
		child := exec.CommandContext(ctx, binary, args...)
		child.Stdin = in
		child.Stdout = out
		child.Stderr = stderr
		return child.Run()
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err := private(root, true); err != nil {
		return err
	}
	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	project, err := os.Getwd()
	if err != nil {
		return err
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		return err
	}
	child := exec.CommandContext(ctx, binary, args...)
	child.Cancel = func() error { return child.Process.Signal(syscall.SIGTERM) }
	child.WaitDelay = 5 * time.Second
	child.Stdout = out
	child.Stderr = stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		return err
	}
	if err = child.Start(); err != nil {
		return err
	}
	defer func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	}()
	start, err := birth(child.Process.Pid)
	if err != nil {
		return err
	}
	socket := filepath.Join(root, strconv.Itoa(child.Process.Pid)+".sock")
	// A crashed adapter may leave a socket for a PID that has since been reused.
	// Only remove the exact private socket when its recorded birth is different.
	if old, e := ReadRecord(root, child.Process.Pid); e == nil && old.Start != start {
		if st, e := os.Lstat(socket); e == nil && st.Mode()&os.ModeSocket != 0 {
			os.Remove(socket)
		}
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err = os.Chmod(socket, 0600); err != nil {
		return err
	}
	record := Record{child.Process.Pid, start, project, socket}
	p := filepath.Join(root, strconv.Itoa(child.Process.Pid)+".json")
	data, _ := json.Marshal(record)
	f, err := os.CreateTemp(root, ".owner-")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	_, err = f.Write(data)
	f.Close()
	if err != nil {
		return err
	}
	if err = os.Rename(temporary, p); err != nil {
		return err
	}
	defer os.Remove(p)
	var write sync.Mutex
	var requests sync.Mutex
	sent := map[string]Reply{}
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".claude")
	}
	native := filepath.Join(config, "sessions", strconv.Itoa(child.Process.Pid)+".json")
	check := func(session string) bool {
		if private(filepath.Dir(native), true) != nil {
			return false
		}
		st, e := os.Lstat(native)
		if e != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 || st.Mode().Perm()&0022 != 0 {
			return false
		}
		b, e := os.ReadFile(native)
		if e != nil {
			return false
		}
		var n struct {
			PID     int    `json:"pid"`
			Start   string `json:"procStart"`
			Session string `json:"sessionId"`
			CWD     string `json:"cwd"`
			Parked  string `json:"parkedJobId"`
		}
		if json.Unmarshal(b, &n) != nil {
			return false
		}
		cwd, e := filepath.EvalSymlinks(n.CWD)
		return e == nil && n.PID == record.PID && strings.Join(strings.Fields(n.Start), " ") == record.Start && n.Session == session && cwd == project && n.Parked == ""
	}
	go func() {
		r := bufio.NewReader(in)
		for {
			line, e := r.ReadBytes('\n')
			write.Lock()
			_, we := stdin.Write(line)
			write.Unlock()
			if e != nil || we != nil {
				write.Lock()
				stdin.Close()
				write.Unlock()
				return
			}
		}
	}()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(15 * time.Second))
				var input Input
				d := json.NewDecoder(io.LimitReader(c, MaxInput+1))
				d.DisallowUnknownFields()
				if d.Decode(&input) != nil || d.InputOffset() > MaxInput || !validInput(input) || !check(input.Session) {
					json.NewEncoder(c).Encode(Reply{Error: "invalid or stale original-session input"})
					return
				}
				requests.Lock()
				defer requests.Unlock()
				if reply, ok := sent[input.UUID]; ok {
					reply.Duplicate = true
					json.NewEncoder(c).Encode(reply)
					return
				}
				sent[input.UUID] = Reply{Error: "native write unconfirmed"} // Never retry an ambiguous partial pipe write.
				frame := map[string]any{"type": "user", "uuid": input.UUID, "session_id": input.Session, "message": map[string]any{"role": "user", "content": input.Content}, "parent_tool_use_id": nil}
				b, _ := json.Marshal(frame)
				b = append(b, '\n')
				write.Lock()
				n, e := stdin.Write(b)
				write.Unlock()
				if e != nil || n != len(b) {
					json.NewEncoder(c).Encode(Reply{Error: "native write unconfirmed"})
					return
				}
				sent[input.UUID] = Reply{Written: true}
				json.NewEncoder(c).Encode(sent[input.UUID])
			}()
		}
	}()
	if err = child.Wait(); err != nil {
		return fmt.Errorf("native Claude process exited: %w", err)
	}
	return nil
}
