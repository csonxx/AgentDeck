package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Validate the exact format emitted by the app before passing bytes to a decoder.
func ValidateRecording(data []byte) error {
	if len(data) < 46 || len(data) > 2<<20 || string(data[:4]) != "RIFF" || int(binary.LittleEndian.Uint32(data[4:8])) != len(data)-8 || string(data[8:12]) != "WAVE" || string(data[12:16]) != "fmt " || binary.LittleEndian.Uint32(data[16:20]) != 16 || binary.LittleEndian.Uint16(data[20:22]) != 1 || binary.LittleEndian.Uint16(data[22:24]) != 1 || binary.LittleEndian.Uint32(data[24:28]) != 16000 || binary.LittleEndian.Uint32(data[28:32]) != 32000 || binary.LittleEndian.Uint16(data[32:34]) != 2 || binary.LittleEndian.Uint16(data[34:36]) != 16 || string(data[36:40]) != "data" || int(binary.LittleEndian.Uint32(data[40:44])) != len(data)-44 || len(data)%2 != 0 || len(data)-44 > 60*32000 {
		return errors.New("录音需为最长 60 秒的单声道 16kHz PCM WAV")
	}
	return nil
}

type limitedOutput struct {
	bytes.Buffer
	exceeded bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > 64<<10 {
		w.exceeded = true
		return 0, errors.New("语音输出过大")
	}
	return w.Buffer.Write(p)
}
func (m *Manager) Transcribe(data []byte) (string, error) {
	return m.transcribe(data, false)
}
func (m *Manager) TranscribePreview(data []byte) (string, error) {
	return m.transcribe(data, true)
}

// Warm only the configured persistent worker, using generated silence.
// No microphone input or coding-agent request is involved.
func (m *Manager) WarmVoice() error {
	persistent := false
	for _, arg := range m.VoiceCommand {
		if arg == "--persistent" {
			persistent = true
		}
	}
	if !persistent {
		return nil
	}
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
	_, e := m.TranscribePreview(wav)
	return e
}
func (m *Manager) transcribe(data []byte, preview bool) (string, error) {
	if e := ValidateRecording(data); e != nil {
		return "", e
	}
	if len(m.VoiceCommand) == 0 {
		return "", errors.New("工作站尚未启用本机语音转写")
	}
	if !m.voiceMu.TryLock() {
		return "", errors.New("语音服务正在转写，请稍后重试")
	}
	defer m.voiceMu.Unlock()
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	var output []byte
	persistent := false
	for _, arg := range m.VoiceCommand {
		if arg == "--persistent" {
			persistent = true
		}
	}
	if persistent {
		var e error
		output, e = m.workerTranscribe(ctx, data)
		if e != nil {
			return "", errors.New("本机语音转写失败，请重试")
		}
	} else {
		cmd := exec.CommandContext(ctx, m.VoiceCommand[0], m.VoiceCommand[1:]...)
		cmd.Stdin = bytes.NewReader(data)
		cmd.Stderr = io.Discard
		var out limitedOutput
		cmd.Stdout = &out
		if e := cmd.Run(); e != nil || out.exceeded {
			return "", errors.New("本机语音转写失败，请检查工作站语音服务后重试")
		}
		output = out.Bytes()
	}
	var result struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if json.Unmarshal(output, &result) != nil || result.Error != "" || !utf8.ValidString(result.Text) || len(result.Text) > 32<<10 {
		return "", errors.New("语音服务返回无效结果")
	}
	text := strings.TrimSpace(result.Text)
	if text == "" && !preview {
		return "", errors.New("没有识别到文字，请靠近麦克风重试")
	}
	return text, nil
}

// Owner-configured worker: bounded length-prefixed WAV stdin, one JSON line
// per request. The model stays in RAM; the server context owns its lifetime.
type voiceWorker struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	done chan struct{}
}

func (m *Manager) workerTranscribe(ctx context.Context, data []byte) ([]byte, error) {
	w := m.voiceWorker
	if w != nil {
		select {
		case <-w.done:
			w = nil
			m.voiceWorker = nil
		default:
		}
	}
	if w == nil {
		parent := m.Context
		if parent == nil {
			parent = context.Background()
		}
		cmd := exec.CommandContext(parent, m.VoiceCommand[0], m.VoiceCommand[1:]...)
		in, e := cmd.StdinPipe()
		if e != nil {
			return nil, e
		}
		out, e := cmd.StdoutPipe()
		if e != nil {
			in.Close()
			return nil, e
		}
		cmd.Stderr = io.Discard
		if e = cmd.Start(); e != nil {
			in.Close()
			out.Close()
			return nil, e
		}
		w = &voiceWorker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10), done: make(chan struct{})}
		m.voiceWorker = w
		go func() { _ = cmd.Wait(); close(w.done) }()
	}
	type reply struct {
		data []byte
		err  error
	}
	ch := make(chan reply, 1)
	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(len(data)))
		_, e := io.Copy(w.in, io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(data)))
		if e != nil {
			ch <- reply{err: e}
			return
		}
		line, e := w.out.ReadSlice('\n')
		ch <- reply{data: append([]byte(nil), line...), err: e}
	}()
	select {
	case r := <-ch:
		if r.err == nil {
			return r.data, nil
		}
		_ = w.cmd.Process.Kill()
		_ = w.in.Close()
		<-w.done
		m.voiceWorker = nil
		return nil, r.err
	case <-ctx.Done():
		_ = w.cmd.Process.Kill()
		_ = w.in.Close()
		<-w.done
		<-ch // No reader/writer from an expired request may reach the next one.
		m.voiceWorker = nil
		return nil, ctx.Err()
	}
}
