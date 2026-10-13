package agent

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
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
	Size      int64  `json:"size"`
	Download  bool   `json:"download_required,omitempty"`
	Name      string `json:"name"`
	MIME      string `json:"mime"`
	Data      []byte `json:"bytes"`
	Line      int    `json:"line,omitempty"`
}

// OpenAttachment authorizes an exact session link and opens it inside configured
// roots. Both bounded previews and streaming downloads use this same check.
func (n *Normalizer) OpenAttachment(session, href string) (*os.File, int, error) {
	var ss store.Session
	if len(href) > 4096 || n.Store.Get("sessions", session, &ss) != nil {
		return nil, 0, errors.New("会话附件不可用")
	}
	p, line, e := localTarget(href, ss.Project)
	if e != nil {
		return nil, 0, errors.New("不支持此附件链接")
	}
	matched := false
	cursor := uint64(0)
	for {
		events, e := n.Store.Events(session, cursor, 100)
		if e != nil {
			return nil, 0, e
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
		return nil, 0, errors.New("此文件未在当前会话中引用")
	}
	// Apply credential exclusions to the resolved target as well as its link name.
	resolved, resolveErr := filepath.EvalSymlinks(p)
	if resolveErr != nil {
		return nil, 0, errors.New("文件已不存在或无法读取")
	}
	for _, target := range []string{p, resolved} {
		base := strings.ToLower(filepath.Base(target))
		if base == ".env" || strings.HasPrefix(base, ".env.") || base == "storage.key" || base == "auth.json" || strings.HasSuffix(base, ".key") || strings.Contains(target, string(os.PathSeparator)+".ssh"+string(os.PathSeparator)) {
			return nil, 0, errors.New("不提供凭据文件预览")
		}
	}

	f, e := n.openAllowed(resolved)
	if e != nil {
		return nil, 0, e
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, 0, errors.New("附件不是普通文件")
	}
	return f, line, nil
}

// AttachmentInfo reads only a signature, independent of file size. Original
// bytes are read on demand by an encrypted connection-scoped download.
func AttachmentInfo(f *os.File, line int) (Attachment, error) {
	info, e := f.Stat()
	if e != nil {
		return Attachment{}, errors.New("文件已不存在或无法读取")
	}
	prefix := make([]byte, 512)
	count, e := f.ReadAt(prefix, 0)
	if e != nil && e != io.EOF {
		return Attachment{}, errors.New("附件读取失败")
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name())))
	if contentType == "" {
		contentType = http.DetectContentType(prefix[:count])
	}
	return Attachment{Name: info.Name(), MIME: contentType, Size: info.Size(), Line: line, Download: true}, nil
}

func (n *Normalizer) ReadAttachment(session, href string) (Attachment, error) {
	f, line, e := n.OpenAttachment(session, href)
	if e != nil {
		return Attachment{}, e
	}
	defer f.Close()
	info, e := AttachmentInfo(f, line)
	if e != nil || info.Size > 8<<20 {
		return info, e
	}
	data, e := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if e != nil {
		return Attachment{}, errors.New("附件读取失败")
	}
	if len(data) > 8<<20 {
		return AttachmentInfo(f, line)
	}
	preview, e := PreviewAttachment(info.Name, data, line)
	// Unsupported/large/locked documents remain downloadable as originals.
	if e != nil {
		return info, nil
	}
	preview.Size = info.Size
	return preview, nil
}

func (n *Normalizer) imageSource(src, cwd string) (string, error) {
	p, _, err := localTarget(src, cwd)
	if err != nil {
		return "", fmt.Errorf("unsupported image source")
	}
	return p, nil
}
