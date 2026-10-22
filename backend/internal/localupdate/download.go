package localupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func validPublicKey(raw string) bool {
	b, err := base64.StdEncoding.DecodeString(raw)
	return err == nil && len(b) == ed25519.PublicKeySize
}
func HTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("insecure update redirect rejected")
		}
		return nil
	}}
}

func download(ctx context.Context, client *http.Client, dir string, r Release, progress func(int64)) error {
	target := binary(dir, r)
	if verifyFile(target, r) == nil {
		progress(r.Bytes)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	part := target + ".part"
	var offset int64
	if stat, err := os.Lstat(part); err == nil {
		if !stat.Mode().IsRegular() {
			return errors.New("invalid update staging file")
		}
		offset = stat.Size()
		if offset > r.Bytes {
			if err = os.Remove(part); err != nil {
				return err
			}
			offset = 0
		}
	}
	if offset < r.Bytes {
		req, err := http.NewRequestWithContext(ctx, "GET", r.URL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			offset = 0
		} else if resp.StatusCode == http.StatusPartialContent {
			if resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, r.Bytes-1, r.Bytes) {
				return errors.New("invalid update content range")
			}
		} else {
			return fmt.Errorf("update download HTTP %d", resp.StatusCode)
		}
		if resp.ContentLength != -1 && resp.ContentLength != r.Bytes-offset {
			return errors.New("update download size mismatch")
		}
		f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		if err = f.Truncate(offset); err != nil {
			return err
		}
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		written := offset
		buf := make([]byte, 128<<10)
		reader := io.LimitReader(resp.Body, r.Bytes-offset+1)
		for {
			n, re := reader.Read(buf)
			if n > 0 {
				if written+int64(n) > r.Bytes {
					return errors.New("oversized update download")
				}
				if _, err = f.Write(buf[:n]); err != nil {
					return err
				}
				written += int64(n)
				progress(written)
			}
			if re == io.EOF {
				break
			}
			if re != nil {
				return re
			}
		}
		if written != r.Bytes {
			return io.ErrUnexpectedEOF
		}
		if err = f.Sync(); err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
	}
	if err := verifyFile(part, r); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Chmod(part, 0700); err != nil {
		return err
	}
	return os.Rename(part, target)
}
