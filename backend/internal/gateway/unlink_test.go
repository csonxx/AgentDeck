package gateway

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/server"
	"agentdeck/internal/store"
)

func TestOwnerUnlinkClosesTunnelRevokesOnlyItsRoutesAndKeepsHistory(t *testing.T) {
	f := setupPrefix(t, "/agentdeck")
	db, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	link, err := f.g.AddLocal("tenant-a", "Test workstation")
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Put("relayconfig", "gateway", link)
	_ = db.Put("relayconfig", "name", "old display name")
	_ = db.Put("relaypending", "old", map[string]any{"grant": "old"})
	_ = db.Put("invites", "old", map[string]any{"test": true})
	_, _, err = db.Append(store.Event{ID: "keep-history", Session: "history", Agent: "codex", Role: "user", Kind: "message", Text: "fixture history", Time: time.Now().UTC().Format(time.RFC3339), Source: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Put("devices", "keep-device", store.Device{ID: "keep-device", Root: secure.Random(32)})
	s := server.New(db, "", f.g.URL, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.Association.Wait() }()
	if err := s.EnableRelay(ctx); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return s.Association.Status().Online })
	if s.Association.Status().Name != "Test workstation" {
		t.Fatal("display name did not come from authenticated gateway")
	}
	invite, err := s.Invite()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Association.Unlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := s.Association.Status()
	if state.Linked || state.Online || state.State != "unlinked" || state.URL != "" {
		t.Fatal("unlink left a linked status")
	}
	var saved relay.Link
	if !os.IsNotExist(db.Get("relayconfig", "gateway", &saved)) {
		t.Fatal("local enrollment remained")
	}
	var node Node
	if !os.IsNotExist(f.g.Store.Get("gwnodes", link.Local, &node)) {
		t.Fatal("remote identity remained")
	}
	var grant relay.Grant
	if !os.IsNotExist(f.g.Store.Get("gwgrants", invite.Relay.Grant, &grant)) {
		t.Fatal("old phone route remained")
	}
	for _, bucket := range []string{"relaypending", "invites"} {
		rows, err := db.Values(bucket)
		if err != nil || len(rows) != 0 {
			t.Fatal("unfinished pairing remained")
		}
	}
	var device store.Device
	if db.Get("devices", "keep-device", &device) != nil || len(device.Root) != 32 {
		t.Fatal("phone roots/history were erased")
	}
	rows, err := db.Values("events")
	if err != nil || len(rows) != 1 {
		t.Fatal("conversation history lost")
	}
	if f.g.Store.Get("gwnodes", f.link.Local, &node) != nil || !f.client.Online() {
		t.Fatal("another workstation affected")
	}
	old, err := relay.NewClient(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Unlink(context.Background()); err != nil {
		t.Fatal("retry after lost receipt should be idempotent")
	}
	restarted := server.New(db, "", f.g.URL, time.Second)
	if err := restarted.EnableRelay(ctx); err != nil {
		t.Fatal(err)
	}
	if restarted.Association.Status().Linked {
		t.Fatal("restart resurrected revoked enrollment")
	}
	if err := s.Association.Begin(f.g.URL, "reconnect test"); err != nil {
		t.Fatal("new enrollment was blocked after unlink")
	}
}

func TestUnlinkAuthenticationAndUnsupportedGatewayDoNotClearIdentity(t *testing.T) {
	f := setupPrefix(t, "/agentdeck")
	// A caller cannot nominate another workstation, even with its own valid proof.
	key, _ := secure.B64.DecodeString(f.link.Key)
	request(t, f.http.Client(), f.g.URL, "/v2/local/unlink", "Local "+f.link.Local, key, "local/"+f.link.Local, map[string]string{"local": "another-workstation"}, 400)
	var node Node
	if f.g.Store.Get("gwnodes", f.link.Local, &node) != nil {
		t.Fatal("body target was accepted")
	}
	request(t, f.http.Client(), f.g.URL, "/v2/local/unlink", "Local "+f.link.Local, secure.Random(32), "local/"+f.link.Local, nil, 401)
	if f.g.Store.Get("gwnodes", f.link.Local, &node) != nil {
		t.Fatal("wrong key revoked workstation")
	}
	if !strings.Contains(f.client.Status().ConnectedAt, "T") {
		t.Fatal("fixture tunnel missing")
	}
}
