package localipc

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestOwnerTransportAndDuplicateListener(t *testing.T) {
	dir, err := os.MkdirTemp("", "ad-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err = PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "owner") })}
	go srv.Serve(ln)
	defer srv.Close()
	if second, err := Listen(dir, "test"); err == nil {
		second.Close()
		t.Fatal("duplicate owner listener allowed")
	}
	client := Client(dir, "test")
	defer client.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://owner/", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal("original socket was displaced", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if string(b) != "owner" {
		t.Fatal("wrong owner response")
	}
}
