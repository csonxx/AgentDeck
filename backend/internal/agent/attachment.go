package agent

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"agentdeck/internal/store"
)

// Destinations include CommonMark angle-wrapped paths and optional link titles.
var markdownDestination = regexp.MustCompile(`!?\[[^\]]*\]\(\s*(?:<([^>]+)>|([^\s)]+))(?:\s+"[^"]*")?\s*\)`)
var sourceLine = regexp.MustCompile(`:(\d+)(?::\d+)?$`)

func destinations(text string) []string {
	out := []string{}
	for _, m := range markdownDestination.FindAllStringSubmatch(text, -1) {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		out = append(out, v)
	}
	return out
}
func localTarget(href, cwd string) (string, int, error) {
	href = strings.TrimSpace(href)
	line := 0
	if m := sourceLine.FindStringSubmatch(href); m != nil {
		line, _ = strconv.Atoi(m[1])
		href = href[:len(href)-len(m[0])]
	}
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "" && u.Scheme != "file") || (u.Host != "" && u.Host != "localhost") {
		return "", 0, errors.New("unsupported attachment link")
	}
	p := u.Path
	if m := sourceLine.FindStringSubmatch(p); m != nil {
		line, _ = strconv.Atoi(m[1])
		p = p[:len(p)-len(m[0])]
	}
	if strings.HasPrefix(u.Fragment, "L") {
		if v, e := strconv.Atoi(strings.TrimPrefix(u.Fragment, "L")); e == nil {
			line = v
		}
	}
	if p == "" || strings.ContainsRune(p, 0) {
		return "", 0, errors.New("invalid attachment link")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p), line, nil
}
func (n *Normalizer) openAllowed(path string) (*os.File, error) {
	p, e := filepath.EvalSymlinks(path)
	if e != nil {
		return nil, errors.New("文件已不存在或无法读取")
	}
	for _, r := range n.MediaRoots {
		rr, e := filepath.EvalSymlinks(r)
		if e != nil {
			continue
		}
		rel, e := filepath.Rel(rr, p)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		root, e := os.OpenRoot(rr)
		if e != nil {
			continue
		}
		f, e := root.Open(rel)
		_ = root.Close()
		if e == nil {
			return f, nil
		}
	}
	return nil, errors.New("文件不在工作站允许的附件目录内")
}

type Attachment struct {
	Truncated bool   `json:"truncated,omitempty"`
	Name      string `json:"name"`
	MIME      string `json:"mime"`
	Data      []byte `json:"bytes"`
	Line      int    `json:"line,omitempty"`
}

// ReadAttachment only reads an exact link already present in this session, inside
// configured roots. Local paths never become public HTTP URLs or gateway files.
func (n *Normalizer) ReadAttachment(session, href string) (Attachment, error) {
	var ss store.Session
	if len(href) > 4096 || n.Store.Get("sessions", session, &ss) != nil {
		return Attachment{}, errors.New("会话附件不可用")
	}
	p, line, e := localTarget(href, ss.Project)
	if e != nil {
		return Attachment{}, errors.New("不支持此附件链接")
	}
	matched := false
	cursor := uint64(0)
	for {
		events, e := n.Store.Events(session, cursor, 100)
		if e != nil {
			return Attachment{}, e
		}
		if len(events) == 0 {
			break
		}
		for _, ev := range events {
			cursor = ev.Seq
			for _, ref := range destinations(ev.Text) {
				q, _, err := localTarget(ref, ss.Project)
				if err == nil && q == p {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			break
		}
	}
	if !matched {
		return Attachment{}, errors.New("此文件未在当前会话中引用")
	}
	base := strings.ToLower(filepath.Base(p))
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "storage.key" || base == "auth.json" || strings.HasSuffix(base, ".key") || strings.Contains(p, string(os.PathSeparator)+".ssh"+string(os.PathSeparator)) {
		return Attachment{}, errors.New("不提供凭据文件预览")
	}
	f, e := n.openAllowed(p)
	if e != nil {
		return Attachment{}, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return Attachment{}, errors.New("附件不是普通文件或超过 8 MiB")
	}
	data, e := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if e != nil || len(data) > 8<<20 {
		return Attachment{}, errors.New("附件读取失败或过大")
	}
	return PreviewAttachment(filepath.Base(p), data, line)
}

func (n *Normalizer) imageSource(src, cwd string) (string, error) {
	p, _, err := localTarget(src, cwd)
	if err != nil {
		return "", fmt.Errorf("unsupported image source")
	}
	return p, nil
}
