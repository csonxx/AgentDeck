package main

import (
	"agentdeck/internal/server"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"unicode/utf8"
)

func loadVoiceCommand(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("voice command file must be a private regular file (0600)")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var args []string
	if len(data) > 16<<10 || json.Unmarshal(data, &args) != nil || len(args) == 0 || len(args) > 32 || !filepath.IsAbs(args[0]) {
		return nil, errors.New("voice command must be a JSON array with an absolute executable")
	}
	for _, a := range args {
		if len(a) > 4096 {
			return nil, errors.New("voice command argument too long")
		}
	}
	info, e = os.Stat(args[0])
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("voice executable unavailable")
	}
	return args, nil
}
func readInputFile(dir, session, upload, format string, offset, limit int) error {
	if session == "" || upload == "" || offset < 0 || limit < 0 || limit > 8<<20 {
		return errors.New("input-file requires --session, --upload and valid --offset/--limit")
	}
	if format != "text" && format != "raw" && format != "base64" {
		return errors.New("--format must be text, raw or base64")
	}
	if limit == 0 {
		if format == "text" {
			limit = 64 << 10
		} else {
			limit = 8 << 20
		}
	}
	client := server.AdminClient(dir)
	buffer := bytes.Buffer{}
	for buffer.Len() < limit {
		n := limit - buffer.Len()
		if n > 64<<10 {
			n = 64 << 10
		}
		raw, _ := json.Marshal(map[string]any{"session": session, "upload": upload, "offset": offset, "limit": n})
		req, _ := http.NewRequest("POST", "http://unix/input-read", bytes.NewReader(raw))
		resp, e := client.Do(req)
		if e != nil {
			return errors.New("owner attachment socket unavailable")
		}
		var result struct {
			Bytes        string `json:"bytes"`
			Offset, Size int
		}
		e = json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&result)
		resp.Body.Close()
		if resp.StatusCode != 200 || e != nil {
			return errors.New("attachment unavailable or outside this session")
		}
		b, e := base64.StdEncoding.DecodeString(result.Bytes)
		if e != nil || result.Offset != offset+len(b) || len(b) > n {
			return errors.New("invalid attachment response")
		}
		buffer.Write(b)
		offset = result.Offset
		if offset >= result.Size {
			break
		}
		if len(b) == 0 {
			return errors.New("empty attachment response")
		}
	}
	data := buffer.Bytes()
	if format == "text" && !utf8.Valid(data) {
		return errors.New("not a complete UTF-8 range; use --format raw/base64 or adjust --offset/--limit")
	}
	if format == "base64" {
		_, e := io.WriteString(os.Stdout, base64.StdEncoding.EncodeToString(data))
		return e
	}
	_, e := os.Stdout.Write(data)
	return e
}
