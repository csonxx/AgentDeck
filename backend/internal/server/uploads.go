package server

import (
	"agentdeck/internal/agent"
	"agentdeck/internal/store"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const uploadChunkSize = 64 << 10
const uploadMaxSize = 8 << 20

type uploadRecord struct {
	ID, Owner, Session, Name, MIME, Kind string
	Size, Offset                         int
	Used                                 bool
	Expires                              time.Time
}

func (s *Server) uploadSession(id string) error {
	if s.Manager == nil {
		return errors.New("工作站未启用附件输入")
	}
	_, e := s.Manager.CheckSession(id)
	return e
}
func (s *Server) pruneUploadsLocked() {
	rows, e := s.Store.Values("uploads")
	if e != nil {
		return
	}
	for _, row := range rows {
		var r uploadRecord
		if json.Unmarshal(row, &r) == nil && !r.Used && time.Now().After(r.Expires) {
			s.deleteUploadLocked(r)
		}
	}
}
func (s *Server) deleteUploadLocked(r uploadRecord) {
	for off := 0; off < r.Offset; off += uploadChunkSize {
		_ = s.Store.Delete("uploadchunks", fmt.Sprintf("%s/%d", r.ID, off))
	}
	_ = s.Store.Delete("uploads", r.ID)
}
func (s *Server) beginUpload(owner string, req Request) (uploadRecord, error) {
	if e := s.uploadSession(req.Session); e != nil {
		return uploadRecord{}, e
	}
	if req.Size <= 0 || req.Size > uploadMaxSize || !utf8.ValidString(req.Name) || len(req.Name) > 240 || strings.TrimSpace(req.Name) == "" || strings.ContainsAny(req.Name, "/\\\x00\r\n") || filepath.Base(req.Name) != req.Name || req.Name == "." || req.Name == ".." {
		return uploadRecord{}, errors.New("附件名称或大小无效（最多 8 MiB）")
	}
	if req.Kind != "file" && req.Kind != "voice" {
		return uploadRecord{}, errors.New("无效附件类型")
	}
	if req.Kind == "voice" && req.Size > 2<<20 {
		return uploadRecord{}, errors.New("录音最长 60 秒")
	}
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	s.pruneUploadsLocked()
	rows, e := s.Store.Values("uploads")
	if e != nil {
		return uploadRecord{}, e
	}
	count, total := 0, 0
	for _, row := range rows {
		var r uploadRecord
		if json.Unmarshal(row, &r) == nil && r.Owner == owner && !r.Used {
			count++
			total += r.Size
		}
	}
	if count >= 12 || total+req.Size > 32<<20 {
		return uploadRecord{}, errors.New("待发送附件过多，请移除附件后重试")
	}
	r := uploadRecord{ID: agent.UUID(), Owner: owner, Session: req.Session, Name: req.Name, MIME: req.MIME, Kind: req.Kind, Size: req.Size, Expires: time.Now().Add(time.Hour)}
	if len(r.MIME) > 100 {
		return r, errors.New("无效文件类型")
	}
	return r, s.Store.Put("uploads", r.ID, r)
}
func (s *Server) ownedUploadLocked(owner, id, session string) (uploadRecord, error) {
	var r uploadRecord
	if !uploadID(id) || s.Store.Get("uploads", id, &r) != nil || r.Owner != owner || !r.Used && time.Now().After(r.Expires) {
		return r, errors.New("附件已失效，请重新添加")
	}
	if r.Session != session {
		var original store.Session
		if s.Store.Get("sessions", r.Session, &original) != nil {
			return r, errors.New("附件与会话不匹配")
		}
		current := session
		matched := false
		for n := 0; n < 64; n++ {
			var child store.Session
			if s.Store.Get("sessions", current, &child) != nil || child.Agent != original.Agent || child.Project != original.Project {
				break
			}
			if child.Parent == r.Session {
				matched = true
				break
			}
			if child.Parent == "" || child.Parent == current {
				break
			}
			current = child.Parent
		}
		if !matched {
			return r, errors.New("附件与会话不匹配")
		}
	}

	if e := s.uploadSession(session); e != nil {
		return r, e
	}
	return r, nil
}
func uploadID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'f' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
func (s *Server) chunkUpload(owner string, req Request) (map[string]any, error) {
	raw, e := base64.StdEncoding.DecodeString(req.Bytes)
	if e != nil || len(raw) == 0 || len(raw) > uploadChunkSize || req.Offset < 0 || req.Offset%uploadChunkSize != 0 {
		return nil, errors.New("无效附件分块")
	}
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	r, e := s.ownedUploadLocked(owner, req.Upload, req.Session)
	if e != nil {
		return nil, e
	}
	if r.Used || req.Offset+len(raw) > r.Size || len(raw) != uploadChunkSize && req.Offset+len(raw) != r.Size {
		return nil, errors.New("附件分块大小无效")
	}
	key := fmt.Sprintf("%s/%d", r.ID, req.Offset)
	if req.Offset < r.Offset {
		var previous []byte
		if s.Store.Get("uploadchunks", key, &previous) != nil || sha256.Sum256(previous) != sha256.Sum256(raw) {
			return nil, errors.New("附件分块冲突")
		}
		return map[string]any{"offset": r.Offset}, nil
	}
	if req.Offset != r.Offset {
		return nil, errors.New("附件分块顺序错误")
	}
	if e = s.Store.Put("uploadchunks", key, raw); e != nil {
		return nil, e
	}
	r.Offset += len(raw)
	if e = s.Store.Put("uploads", r.ID, r); e != nil {
		return nil, e
	}
	return map[string]any{"offset": r.Offset}, nil
}
func (s *Server) readUpload(owner, id, session string) (uploadRecord, []byte, error) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	r, e := s.ownedUploadLocked(owner, id, session)
	if e != nil {
		return r, nil, e
	}
	if r.Offset != r.Size {
		return r, nil, errors.New("附件尚未上传完成")
	}
	data := make([]byte, 0, r.Size)
	for off := 0; off < r.Size; off += uploadChunkSize {
		var raw []byte
		if e = s.Store.Get("uploadchunks", fmt.Sprintf("%s/%d", r.ID, off), &raw); e != nil {
			return r, nil, e
		}
		data = append(data, raw...)
	}
	if len(data) != r.Size {
		return r, nil, errors.New("附件大小不匹配")
	}
	if r.Kind == "file" {
		detected := http.DetectContentType(data)
		if strings.HasPrefix(detected, "image/") {
			c, _, e := image.DecodeConfig(bytes.NewReader(data))
			if e != nil || c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > 24_000_000 {
				return r, nil, errors.New("图片格式无效或尺寸过大，请压缩后重试")
			}
			r.MIME = detected
		} else if strings.HasPrefix(r.MIME, "image/") {
			return r, nil, errors.New("图片只支持 JPEG、PNG 和 GIF，请转换后重试")
		} else {
			r.MIME = detected
		}
	}
	if e = s.Store.Put("uploads", r.ID, r); e != nil {
		return r, nil, e
	}
	return r, data, nil
}
func (s *Server) dropUpload(owner, id, session string) error {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	r, e := s.ownedUploadLocked(owner, id, session)
	if e != nil {
		return e
	}
	if r.Used {
		return errors.New("已发送附件保留在会话中")
	}
	s.deleteUploadLocked(r)
	return nil
}
func (s *Server) inputFiles(owner, session string, ids []string) ([]agent.InputAttachment, error) {
	if len(ids) > 6 {
		return nil, errors.New("一次最多 6 个附件")
	}
	files := []agent.InputAttachment{}
	seen := map[string]bool{}
	total, images := 0, 0
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("重复附件")
		}
		seen[id] = true
		r, raw, e := s.readUpload(owner, id, session)
		if e != nil {
			return nil, e
		}
		if r.Kind != "file" {
			return nil, errors.New("录音需先转为文字")
		}
		total += len(raw)
		if total > 24<<20 {
			return nil, errors.New("附件总计不能超过 24 MiB")
		}
		if strings.HasPrefix(r.MIME, "image/") {
			if e = s.bindImageOwner(owner, raw); e != nil {
				return nil, e
			}
		}
		if strings.HasPrefix(r.MIME, "image/") {
			images += len(raw)
			if images > 8<<20 {
				return nil, errors.New("一次发送的图片总计不能超过 8 MiB")
			}
		}
		files = append(files, agent.InputAttachment{ID: r.ID, Name: r.Name, MIME: r.MIME, Data: raw})
	}
	return files, nil
}
func (s *Server) markUploadsUsed(owner, session string, ids []string) error {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	for _, id := range ids {
		r, e := s.ownedUploadLocked(owner, id, session)
		if e != nil {
			return e
		}
		r.Used = true
		r.Expires = time.Time{}
		if e = s.Store.Put("uploads", id, r); e != nil {
			return e
		}
	}
	return nil
}

func (s *Server) inputRequest(owner string, req Request) (any, error) {
	if s.Manager == nil {
		return nil, errors.New("工作站未启用会话输入")
	}
	switch req.Method {
	case "goal_list":
		return s.Manager.Goals()
	case "usage":
		return s.Manager.AccountUsage(req.Agent)
	case "session_settings":
		return s.Manager.SessionSettings(req.Session)
	case "models":
		choices, e := s.Manager.Models(req.Agent, req.Session)
		if e != nil {
			return nil, e
		}
		caps, e := s.Manager.InputCapabilities(req.Agent, req.Session, true)
		if e != nil {
			return nil, e
		}
		caps["models"] = choices

		return caps, nil
	case "input_capabilities":
		return s.Manager.InputCapabilities(req.Agent, req.Session, req.Kind != "voice")
	case "upload_begin":
		r, e := s.beginUpload(owner, req)
		return map[string]any{"upload": r.ID, "offset": r.Offset}, e
	case "upload_chunk":
		return s.chunkUpload(owner, req)
	case "upload_drop":
		return nil, s.dropUpload(owner, req.Upload, req.Session)
	case "upload_preview":
		r, data, e := s.readUpload(owner, req.Upload, req.Session)
		if e != nil {
			return nil, e
		}
		preview, e := agent.PreviewAttachment(r.Name, data, 0)
		if e != nil {
			return nil, e
		}
		return preview, nil
	case "upload_read":
		limit := req.Limit
		if limit == 0 {
			limit = uploadChunkSize
		}
		r, data, end, e := s.rangeUpload(owner, req.Upload, req.Session, req.Offset, limit)
		if e != nil {
			return nil, e
		}
		return map[string]any{"name": r.Name, "mime": r.MIME, "size": r.Size, "offset": end, "bytes": base64.StdEncoding.EncodeToString(data)}, nil
	case "transcribe", "transcribe_preview":
		r, b, e := s.readUpload(owner, req.Upload, req.Session)
		if e != nil {
			return nil, e
		}
		if r.Kind != "voice" {
			return nil, errors.New("需要录音附件")
		}
		defer s.dropUpload(owner, req.Upload, req.Session)
		var text string
		if req.Method == "transcribe_preview" {
			text, e = s.Manager.TranscribePreview(b)
		} else {
			text, e = s.Manager.Transcribe(b)
		}
		return map[string]string{"text": text}, e
	}
	return nil, errors.New("不支持的输入操作")
}

// This handler is registered ONLY on the owner Unix socket. It exposes no keys,
// shell execution or filesystem paths. File IDs remain scoped to their chat.
func (s *Server) adminInputRead(w http.ResponseWriter, r *http.Request) {
	var req Request
	if decode(w, r, &req) != nil || !uploadID(req.Upload) || req.Offset < 0 || req.Limit < 1 || req.Limit > uploadChunkSize {
		fail(w, 400)
		return
	}
	var record uploadRecord
	if s.Store.Get("uploads", req.Upload, &record) != nil || record.Kind != "file" {
		fail(w, 404)
		return
	}
	meta, data, end, e := s.rangeUpload(record.Owner, req.Upload, req.Session, req.Offset, req.Limit)
	if e != nil {
		fail(w, 404)
		return
	}
	write(w, map[string]any{"name": meta.Name, "mime": meta.MIME, "size": meta.Size, "offset": end, "bytes": base64.StdEncoding.EncodeToString(data)})
}

func (s *Server) rangeUpload(owner, id, session string, offset, limit int) (uploadRecord, []byte, int, error) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	r, e := s.ownedUploadLocked(owner, id, session)
	if e != nil {
		return r, nil, 0, e
	}
	if r.Offset != r.Size || offset < 0 || offset > r.Size || limit < 1 || limit > downloadChunkSize {
		return r, nil, 0, errors.New("无效附件读取范围")
	}
	end := offset + limit
	if end > r.Size {
		end = r.Size
	}
	data := make([]byte, 0, end-offset)
	for start := offset; start < end; {
		chunkOffset := (start / uploadChunkSize) * uploadChunkSize
		var chunk []byte
		if e = s.Store.Get("uploadchunks", fmt.Sprintf("%s/%d", id, chunkOffset), &chunk); e != nil {
			return r, nil, 0, e
		}
		lo := start - chunkOffset
		hi := end - chunkOffset
		if hi > len(chunk) {
			hi = len(chunk)
		}
		if lo < 0 || hi <= lo {
			return r, nil, 0, errors.New("附件大小不匹配")
		}
		data = append(data, chunk[lo:hi]...)
		start = chunkOffset + hi
	}
	return r, data, end, nil
}

// CLI echoes normalize native images into content-addressed media records. Keep
// the upload's device restriction on this secondary retrieval path as well.
func (s *Server) bindImageOwner(owner string, raw []byte) error {
	h := sha256.Sum256(raw)
	id := hex.EncodeToString(h[:])
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	owners := map[string]bool{}
	e := s.Store.Get("mediaowners", id, &owners)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	owners[owner] = true
	return s.Store.Put("mediaowners", id, owners)
}
func (s *Server) mayReadMedia(owner, id string) bool {
	owners := map[string]bool{}
	e := s.Store.Get("mediaowners", id, &owners)
	if errors.Is(e, os.ErrNotExist) {
		return true
	}
	return e == nil && owners[owner]
}
