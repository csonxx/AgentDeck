package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"agentdeck/internal/console"
	"agentdeck/internal/server"
)

func runConsole(dir, addr string, noOpen bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		return errors.New("console must listen on 127.0.0.1")
	}
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	ui := console.New(server.AdminClient(dir), ln.Addr().String())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Handler: ui.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	// An owner-readable temporary launcher supports headless launch without
	// putting the capability in terminal output, server logs or URL query strings.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	launcher := filepath.Join(dir, fmt.Sprintf("console-open-%d.html", os.Getpid()))
	content := fmt.Sprintf("<!doctype html><meta charset=utf-8><meta name=referrer content=no-referrer><title>AgentDeck</title><a href=\"%s\">打开本机 AgentDeck 管理页面 / Open workstation controls</a>", ui.URL())
	f, err := os.OpenFile(launcher, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(launcher)
		return err
	}
	defer os.Remove(launcher)
	fmt.Printf("AgentDeck workstation console: %s\nOwner-only launcher: %s\n", strings.Split(ui.URL(), "#")[0], launcher)
	if !noOpen {
		var command *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			command = exec.Command("open", ui.URL())
		case "linux":
			command = exec.Command("xdg-open", ui.URL())
		case "windows":
			command = exec.Command("rundll32", "url.dll,FileProtocolHandler", ui.URL())
		}
		if command != nil {
			if err := command.Run(); err != nil {
				fmt.Println("Open the owner-only launcher above in your browser.")
			}
		}
	}
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
