package agent

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"agentdeck/internal/store"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestTextEncodingsAndRedaction(t *testing.T) {
	text := "中文 text\nAPI_KEY=hidden\nend"
	gb, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	le, be := []byte{0xff, 0xfe}, []byte{0xfe, 0xff}
	for _, u := range utf16.Encode([]rune(text)) {
		le = binary.LittleEndian.AppendUint16(le, u)
		be = binary.BigEndian.AppendUint16(be, u)
	}
	for _, data := range [][]byte{[]byte(text), append([]byte{0xef, 0xbb, 0xbf}, []byte(text)...), le, be, gb} {
		out, e := PreviewAttachment("notes.txt", data, 0)
		if e != nil || string(out.Data) != "中文 text\n[REDACTED]\nend" || !strings.HasPrefix(out.MIME, "text/") {
			t.Fatal("text decoding", e)
		}
	}
	large := bytes.Repeat([]byte("line\n"), 300000)
	if out, e := PreviewAttachment("large.log", large, 100); e != nil || !out.Truncated || len(out.Data) > 128<<10 {
		t.Fatal("bounded large text preview", e)
	}
	for _, data := range [][]byte{{0xff, 0xfe, 65}, []byte{65, 0, 66}, []byte{0x81}} {
		if _, e := PreviewAttachment("bad.txt", data, 0); e == nil {
			t.Fatal("malformed or binary text accepted")
		}
	}
}
func TestAudioSignaturesAndSessionAuthorization(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	n.MediaRoots = []string{root}
	n.Store.Put("sessions", "s", store.Session{ID: "s", Project: root})
	for _, tc := range []struct {
		name, mime string
		data       []byte
	}{
		{"clip.wav", "audio/wav", []byte("RIFF0000WAVEfmt ")},
		{"clip.mp3", "audio/mpeg", []byte("ID3\x04\x00\x00\x00\x00\x00\x00")},
		{"clip.m4a", "audio/mp4", []byte("\x00\x00\x00\x18ftypM4A ")},
		{"clip.flac", "audio/flac", []byte("fLaC0000")},
		{"clip.ogg", "audio/ogg", []byte("OggS0000")},
		{"clip.opus", "audio/ogg", []byte("OggS0000")},
		{"clip.aac", "audio/aac", []byte{0xff, 0xf1, 0x50, 0x80, 0, 0, 0}},
	} {
		path := filepath.Join(root, tc.name)
		os.WriteFile(path, tc.data, 0600)
		if _, e := n.ReadAttachment("s", path); e == nil {
			t.Fatal("unlinked audio accepted")
		}
		n.Store.Append(store.Event{ID: tc.name, Item: tc.name, Session: "s", Kind: "message", Text: "![Audio](" + path + ")", Mode: "snapshot"})
		a, e := n.ReadAttachment("s", path)
		if e != nil || a.MIME != tc.mime || !bytes.Equal(a.Data, tc.data) {
			t.Fatal("audio scope/signature", tc.name, e)
		}
		if _, e = n.ReadAttachment("other", path); e == nil {
			t.Fatal("cross-chat audio accepted")
		}
	}
	if _, e := PreviewAttachment("fake.wav", []byte{0, 1, 2, 3}, 0); e == nil {
		t.Fatal("forged audio accepted")
	}
}
func TestLegacyDocumentPreviewRedactsAndPreservesOriginal(t *testing.T) {
	for _, name := range []string{"word97.doc", "book97.xls"} {
		b, e := os.ReadFile("../document/testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		original := append([]byte{}, b...)
		out, e := PreviewAttachment(name, b, 0)
		if e != nil || out.MIME != "application/vnd.agentdeck.office+json" || len(out.Data) == 0 || !bytes.Equal(b, original) {
			t.Fatal("legacy preview", e)
		}
	}
}
