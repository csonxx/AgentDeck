package server

import (
	"agentdeck/internal/agent"
	"agentdeck/internal/store"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadIsolationRetryEncryptionAndOwnerRead(t *testing.T) {
	s, ts := setupServer(t)
	state := t.TempDir()
	storage, e := store.New(state)
	if e != nil {
		t.Fatal(e)
	}
	defer storage.Close()
	s.Store = storage
	project := t.TempDir()
	s.Manager = &agent.Manager{Store: s.Store, Roots: []string{project}}
	session := "fixture-session"
	if e := s.Store.Put("sessions", session, store.Session{ID: session, Project: project, Agent: "codex"}); e != nil {
		t.Fatal(e)
	}
	data := bytes.Repeat([]byte("PRIVATE_UPLOAD_ONLY"), 4000)
	r, e := s.beginUpload("phone-a", Request{Session: session, Name: "report.txt", Size: len(data), Kind: "file"})
	if e != nil {
		t.Fatal(e)
	}
	chunk := Request{Session: session, Upload: r.ID, Bytes: base64.StdEncoding.EncodeToString(data[:uploadChunkSize])}
	if _, e = s.chunkUpload("phone-b", chunk); e == nil {
		t.Fatal("other peer uploaded")
	}
	if _, e = s.chunkUpload("phone-a", chunk); e != nil {
		t.Fatal(e)
	}
	if _, e = s.chunkUpload("phone-a", chunk); e != nil {
		t.Fatal("same chunk retry failed")
	}
	conflict := chunk
	conflict.Bytes = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), uploadChunkSize))
	if _, e = s.chunkUpload("phone-a", conflict); e == nil {
		t.Fatal("conflict accepted")
	}
	chunk.Offset = uploadChunkSize
	chunk.Bytes = base64.StdEncoding.EncodeToString(data[uploadChunkSize:])
	if _, e = s.chunkUpload("phone-a", chunk); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.readUpload("phone-b", r.ID, session); e == nil {
		t.Fatal("other peer read")
	}
	if _, _, e = s.readUpload("phone-a", r.ID, "wrong-session"); e == nil {
		t.Fatal("other session read")
	}
	if e = s.markUploadsUsed("phone-a", session, []string{r.ID}); e != nil {
		t.Fatal(e)
	}
	if e = s.dropUpload("phone-a", r.ID, session); e == nil {
		t.Fatal("deleted sent file")
	}
	for _, name := range []string{"../escape", "sub/file", "bad\nname"} {
		if _, e = s.beginUpload("phone-a", Request{Session: session, Name: name, Kind: "file", Size: 1}); e == nil {
			t.Fatal("unsafe name")
		}
	}
	// The read command exists on the owner socket only, not the public listener.
	post(t, ts, "/input-read", map[string]any{}, 404)
	dir, e := os.MkdirTemp("/tmp", "agentdeck-owner-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Admin(ctx, dir)
	for n := 0; n < 100; n++ {
		if _, e = os.Stat(filepath.Join(dir, "control.sock")); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := json.Marshal(Request{Session: session, Upload: r.ID, Offset: 65000, Limit: 1000})
	resp, e := AdminClient(dir).Post("http://unix/input-read", "application/json", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("owner read failed")
	}
	result := map[string]any{}
	if e = json.NewDecoder(resp.Body).Decode(&result); e != nil {
		t.Fatal(e)
	}
	part, _ := base64.StdEncoding.DecodeString(result["bytes"].(string))
	if !bytes.Equal(part, data[65000:66000]) {
		t.Fatal("range across chunks differs")
	}
	// Inspect the actual Bolt file, never trust an encrypted API roundtrip alone.
	// Store directories are known only to the fixture; encrypted values contain no plaintext.
	ciphertext, e := os.ReadFile(filepath.Join(state, "vault.db"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ciphertext, []byte("PRIVATE_UPLOAD_ONLY")) || bytes.Contains(ciphertext, []byte(base64.StdEncoding.EncodeToString(data[:uploadChunkSize]))) {
		t.Fatal("plaintext persisted")
	}
	rows, e := s.Store.Values("uploadchunks")
	if e != nil || len(rows) != 2 {
		t.Fatal("chunk storage")
	}

}
func TestEncryptedChunkRPC(t *testing.T) {
	s, ts := setupServer(t)
	project := t.TempDir()
	s.Manager = &agent.Manager{Store: s.Store, Roots: []string{project}}
	session := "chat"
	s.Store.Put("sessions", session, store.Session{ID: session, Project: project, Agent: "codex"})
	p := pairPhone(t, s, ts)
	p.login(t, ts)
	p.connect(t, ts, s)
	p.send(t, Request{ID: "b", Method: "upload_begin", Session: session, Name: "file.txt", Kind: "file", Size: 5})
	result := p.read(t, "result")["result"].(map[string]any)
	id := result["upload"].(string)
	p.send(t, Request{ID: "c", Method: "upload_chunk", Session: session, Upload: id, Bytes: base64.StdEncoding.EncodeToString([]byte("hello"))})
	p.read(t, "result")
	p.send(t, Request{ID: "r", Method: "upload_read", Session: session, Upload: id})
	result = p.read(t, "result")["result"].(map[string]any)
	data, _ := base64.StdEncoding.DecodeString(result["bytes"].(string))
	if string(data) != "hello" {
		t.Fatal("encrypted wire bytes differ")
	}
	other := pairPhone(t, s, ts)
	other.login(t, ts)
	other.connect(t, ts, s)
	other.send(t, Request{ID: "r", Method: "upload_read", Session: session, Upload: id})
	other.read(t, "error")
	image, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==")
	uploaded, e := s.beginUpload(p.device, Request{Session: session, Name: "pixel.png", MIME: "image/png", Kind: "file", Size: len(image)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.chunkUpload(p.device, Request{Session: session, Upload: uploaded.ID, Bytes: base64.StdEncoding.EncodeToString(image)}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.inputFiles(p.device, session, []string{uploaded.ID}); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(image)
	media := hex.EncodeToString(hash[:])
	s.Store.Put("media", media, store.Media{MIME: "image/png", Data: image})
	p.send(t, Request{ID: "m", Method: "media", Media: media})
	p.read(t, "media")
	other.send(t, Request{ID: "m", Method: "media", Media: media})
	other.read(t, "error")

}

func TestEncryptedLegacyAndTextPreviewsKeepOriginalOwnerScoped(t *testing.T) {
	s, ts := setupServer(t)
	project := t.TempDir()
	s.Manager = &agent.Manager{Store: s.Store, Roots: []string{project}}
	s.Store.Put("sessions", "preview", store.Session{ID: "preview", Project: project, Agent: "codex"})
	a := pairPhone(t, s, ts)
	a.login(t, ts)
	a.connect(t, ts, s)
	b := pairPhone(t, s, ts)
	b.login(t, ts)
	b.connect(t, ts, s)
	for _, name := range []string{"word97.doc", "book97.xls", "notes.txt"} {
		var raw []byte
		var e error
		if name == "notes.txt" {
			raw = []byte{0xff, 0xfe, 'O', 0, 'K', 0}
		} else {
			raw, e = os.ReadFile("../document/testdata/" + name)
			if e != nil {
				t.Fatal(e)
			}
		}
		r, e := s.beginUpload(a.device, Request{Session: "preview", Name: name, Kind: "file", Size: len(raw)})
		if e != nil {
			t.Fatal(e)
		}
		for off := 0; off < len(raw); off += uploadChunkSize {
			_, e = s.chunkUpload(a.device, Request{Session: "preview", Upload: r.ID, Offset: off, Bytes: base64.StdEncoding.EncodeToString(raw[off:min(off+uploadChunkSize, len(raw))])})
			if e != nil {
				t.Fatal(e)
			}
		}
		a.send(t, Request{ID: "preview", Method: "upload_preview", Session: "preview", Upload: r.ID})
		result := a.read(t, "result")["result"].(map[string]any)
		data, e := base64.StdEncoding.DecodeString(result["bytes"].(string))
		if e != nil {
			t.Fatal(e)
		}
		if name == "notes.txt" {
			if string(data) != "OK" {
				t.Fatal("UTF16 wire preview")
			}
		} else if result["mime"] != "application/vnd.agentdeck.office+json" || !json.Valid(data) {
			t.Fatal("Office wire preview")
		}
		b.send(t, Request{ID: "denied", Method: "upload_preview", Session: "preview", Upload: r.ID})
		b.read(t, "error")
		a.send(t, Request{ID: "denied", Method: "upload_preview", Session: "other", Upload: r.ID})
		a.read(t, "error")
		_, original, e := s.readUpload(a.device, r.ID, "preview")
		if e != nil || !bytes.Equal(original, raw) {
			t.Fatal("preview replaced agent's original file")
		}
	}
}
