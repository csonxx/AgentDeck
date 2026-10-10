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
	"runtime"
	"syscall"
	"time"

	"agentdeck/internal/console"
	"agentdeck/internal/server"
)

type consoleService struct {
	URL    string
	server *http.Server
	done   chan error
}

func startConsole(dir, addr string) (*consoleService, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		return nil, errors.New("console must listen on 127.0.0.1")
	}
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return nil, fmt.Errorf("workstation console cannot listen on %s: %w; use --console-listen 127.0.0.1:PORT to select another port", addr, err)
	}
	ui := console.New(server.AdminClient(dir), ln.Addr().String())
	srv := &http.Server{Handler: ui.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	c := &consoleService{URL: ui.URL(), server: srv, done: make(chan error, 1)}
	go func() { c.done <- srv.Serve(ln) }()
	return c, nil
}

func runConsole(dir, addr string, noOpen bool) error {
	c, err := startConsole(dir, addr)
	if err != nil {
		return err
	}
	defer c.server.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("AgentDeck workstation console: %s\n", c.URL)
	if !noOpen {
		var command *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			command = exec.Command("open", c.URL)
		case "linux":
			command = exec.Command("xdg-open", c.URL)
		case "windows":
			command = exec.Command("rundll32", "url.dll,FileProtocolHandler", c.URL)
		}
		if command != nil {
			if err := command.Run(); err != nil {
				fmt.Println("Open the address above in your browser.")
			}
		}
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-c.done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
