package agent

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func testVoiceWAV() []byte {
	wav := make([]byte, 44+32000)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 32000)
	return wav
}

const workerFixture = `import sys,struct,json
count=0
while True:
 h=sys.stdin.buffer.read(4)
 if not h:break
 n=struct.unpack('>I',h)[0]
 a=sys.stdin.buffer.read(n)
 assert len(a)==n and a[:4]==b'RIFF'
 count+=1
 print(json.dumps({'text':str(count)}),flush=True)
`

func TestVoiceWorkerReuseAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{Context: ctx, VoiceCommand: []string{"python3", "-c", workerFixture, "--persistent"}}
	for _, want := range []string{"1", "2", "3"} {
		got, e := m.TranscribePreview(testVoiceWAV())
		if e != nil || got != want {
			t.Fatalf("worker not reused: %q %v", got, e)
		}
	}
	w := m.voiceWorker
	cancel()
	select {
	case <-w.done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker leaked after shutdown")
	}
}
func TestVoiceWorkerTimeoutAndRestart(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{Context: parent, VoiceCommand: []string{"python3", "-c", "import time;time.sleep(30)", "--persistent"}}
	ctx, deadline := context.WithTimeout(parent, 50*time.Millisecond)
	defer deadline()
	if _, e := m.workerTranscribe(ctx, testVoiceWAV()); e == nil {
		t.Fatal("timeout ignored")
	}
	if m.voiceWorker != nil {
		t.Fatal("expired worker retained")
	}
	m.VoiceCommand = []string{"python3", "-c", workerFixture, "--persistent"}
	got, e := m.Transcribe(testVoiceWAV())
	if e != nil || got != "1" {
		t.Fatalf("restart failed: %q %v", got, e)
	}
	cancel()
	<-m.voiceWorker.done
}
func TestVoicePreviewSilence(t *testing.T) {
	m := &Manager{VoiceCommand: []string{"python3", "-c", `import sys;sys.stdin.buffer.read();print('{"text":""}')`}}
	if text, e := m.TranscribePreview(testVoiceWAV()); e != nil || text != "" {
		t.Fatal("silence should be an empty preview", e)
	}
	if _, e := m.Transcribe(testVoiceWAV()); e == nil {
		t.Fatal("empty final accepted")
	}
}
