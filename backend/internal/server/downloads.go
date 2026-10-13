package server

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"agentdeck/internal/agent"
	"agentdeck/internal/secure"
)

const downloadChunkSize = 512 << 10

// Downloads belong to one authenticated encrypted stream, never the gateway or
// a public URL. No plaintext cache or whole-file buffer is created.
type download struct {
	file    *os.File
	info    os.FileInfo
	last    time.Time
	offset  int64
	version string
}
type downloads map[string]*download

func (d downloads) close(id string) {
	if v := d[id]; v != nil {
		_ = v.file.Close()
		delete(d, id)
	}
}
func (d downloads) prune(all bool) {
	for id, v := range d {
		if all || time.Since(v.last) > 10*time.Minute {
			d.close(id)
		}
	}
}
func (d downloads) request(n *agent.Normalizer, r Request) (any, error) {
	d.prune(false)
	if r.Method == "attachment_close" {
		d.close(r.Download)
		return true, nil
	}
	if r.Method == "attachment_open" {
		if n == nil {
			return nil, errors.New("工作站未启用附件读取")
		}
		if len(d) >= 4 {
			return nil, errors.New("同时下载的附件过多")
		}
		f, line, err := n.OpenAttachment(r.Session, r.Href)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, errors.New("文件已不存在或无法读取")
		}
		meta, err := agent.AttachmentInfo(f, line)
		if err != nil {
			f.Close()
			return nil, err
		}
		fingerprint := sha256.New()
		fmt.Fprintf(fingerprint, "%s/%d/%d", info.Name(), info.Size(), info.ModTime().UnixNano())
		sample := make([]byte, 64<<10)
		for _, offset := range []int64{0, max(int64(0), info.Size()-int64(len(sample)))} {
			count, readErr := f.ReadAt(sample, offset)
			if readErr != nil && readErr != io.EOF {
				f.Close()
				return nil, errors.New("附件读取失败")
			}
			fingerprint.Write(sample[:count])
		}
		version := fmt.Sprintf("%x", fingerprint.Sum(nil))
		if r.Version != "" && r.Version != version {
			f.Close()
			return nil, errors.New("附件已变化，请重新下载")
		}
		if r.Offset < 0 || int64(r.Offset) > info.Size() {
			f.Close()
			return nil, errors.New("无效附件下载位置")
		}
		id := secure.B64.EncodeToString(secure.Random(24))
		d[id] = &download{file: f, info: info, last: time.Now(), version: version}
		return map[string]any{"download": id, "name": meta.Name, "mime": meta.MIME, "size": meta.Size, "version": version, "chunk_size": downloadChunkSize}, nil
	}
	v := d[r.Download]
	if v == nil {
		return nil, errors.New("附件下载已失效，请重试")
	}
	if r.Offset < 0 || int64(r.Offset) > v.info.Size() {
		return nil, errors.New("无效附件下载位置")
	}
	info, err := v.file.Stat()
	if err != nil || info.Size() != v.info.Size() || !info.ModTime().Equal(v.info.ModTime()) {
		d.close(r.Download)
		return nil, errors.New("附件已变化，请重新下载")
	}
	remaining := v.info.Size() - int64(r.Offset)
	count := int64(downloadChunkSize)
	if remaining < count {
		count = remaining
	}
	b := make([]byte, int(count))
	readCount, err := v.file.ReadAt(b, int64(r.Offset))
	if readCount != len(b) || (err != nil && err != io.EOF) {
		d.close(r.Download)
		return nil, errors.New("附件读取失败")
	}
	after, statErr := v.file.Stat()
	if statErr != nil || after.Size() != v.info.Size() || !after.ModTime().Equal(v.info.ModTime()) {
		d.close(r.Download)
		return nil, errors.New("附件已变化，请重新下载")
	}
	v.offset = int64(r.Offset) + count
	v.last = time.Now()
	return map[string]any{"bytes": b, "offset": v.offset, "size": v.info.Size()}, nil
}
