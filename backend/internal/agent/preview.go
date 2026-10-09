package agent

import (
	"bytes"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"agentdeck/internal/document"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// PreviewAttachment transforms already-authorized bytes. It never opens paths,
// executes document code or creates plaintext temporary/cache files.
func PreviewAttachment(name string, data []byte, line int) (Attachment, error) {
	if len(data) > 8<<20 {
		return Attachment{}, errors.New("附件读取失败或过大")
	}
	truncated := false
	mime := http.DetectContentType(data)
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case strings.HasPrefix(mime, "image/"):
	case ext == ".mp4" && len(data) >= 12 && string(data[4:8]) == "ftyp":
		mime = "video/mp4"
	case ext == ".webm" && bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		mime = "video/webm"
	case audioMIME(ext, data) != "":
		mime = audioMIME(ext, data)
	case (ext == ".docx" || ext == ".xlsx") && bytes.HasPrefix(data, []byte("PK\x03\x04")):
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		if ext == ".xlsx" {
			mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
	case ext == ".doc" || ext == ".xls":
		var e error
		data, e = document.Parse(data, ext, Redact)
		if errors.Is(e, document.ErrEncrypted) {
			return Attachment{}, errors.New("加密的 Office 文件需要先在工作站解密")
		}
		if e != nil || len(data) > 8<<20 {
			return Attachment{}, errors.New("文档无法预览，文件可能损坏或格式不支持")
		}
		mime = document.MIME
	default:
		text, e := decodeText(data, ext)
		if e != nil {
			return Attachment{}, e
		}
		data = []byte(Redact(text))
		if len(data) > 128<<10 {
			end := 128 << 10
			for end > 0 && !utf8.Valid(data[:end]) {
				end--
			}
			data = data[:end]
			truncated = true
		}
		mime = "text/plain; charset=utf-8"
	}
	return Attachment{Name: name, MIME: mime, Data: data, Line: line, Truncated: truncated}, nil
}
func audioMIME(ext string, b []byte) string {
	riff := len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WAVE"
	mpeg := bytes.HasPrefix(b, []byte("ID3")) || len(b) >= 4 && b[0] == 0xff && b[1]&0xe0 == 0xe0 && b[1]&6 != 0 && b[2]&0xf0 != 0xf0 && b[2]&0xf0 != 0
	switch {
	case ext == ".wav" && riff:
		return "audio/wav"
	case ext == ".mp3" && mpeg:
		return "audio/mpeg"
	case (ext == ".m4a" || ext == ".m4b") && len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "audio/mp4"
	case ext == ".aac" && len(b) >= 7 && b[0] == 0xff && b[1]&0xf6 == 0xf0:
		return "audio/aac"
	case ext == ".amr" && bytes.HasPrefix(b, []byte("#!AMR-WB\n")):
		return "audio/amr-wb"
	case ext == ".amr" && bytes.HasPrefix(b, []byte("#!AMR\n")):
		return "audio/amr"
	case ext == ".flac" && bytes.HasPrefix(b, []byte("fLaC")):
		return "audio/flac"
	case (ext == ".ogg" || ext == ".oga" || ext == ".opus") && bytes.HasPrefix(b, []byte("OggS")):
		return "audio/ogg"
	case (ext == ".aif" || ext == ".aiff") && len(b) >= 12 && string(b[:4]) == "FORM" && (string(b[8:12]) == "AIFF" || string(b[8:12]) == "AIFC"):
		return "audio/aiff"
	}
	return ""
}
func decodeText(data []byte, ext string) (string, error) {
	bad := errors.New("暂不支持此文件格式，请在工作站打开")
	var decoded []byte
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		if (len(data)-2)%2 != 0 {
			return "", bad
		}
		var e error
		decoded, e = unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
		if e != nil {
			return "", bad
		}
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		if (len(data)-2)%2 != 0 {
			return "", bad
		}
		var e error
		decoded, e = unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
		if e != nil {
			return "", bad
		}
	case utf8.Valid(data):
		decoded = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	default:
		// Legacy Chinese is only considered for explicit text extensions. An exact
		// roundtrip rejects replacement decoding and arbitrary binary fallbacks.
		known := "|.txt|.log|.csv|.tsv|.md|.json|.xml|.yaml|.yml|.ini|.conf|.cfg|.sql|"
		if !strings.Contains(known, "|"+ext+"|") {
			return "", bad
		}
		var e error
		decoded, e = simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if e != nil {
			return "", bad
		}
		back, e := simplifiedchinese.GB18030.NewEncoder().Bytes(decoded)
		if e != nil || !bytes.Equal(back, data) {
			return "", bad
		}
	}
	if !utf8.Valid(decoded) {
		return "", bad
	}
	for _, r := range string(decoded) {
		if r == utf8.RuneError || r < 32 && r != '\n' && r != '\r' && r != '\t' && r != '\f' || r >= 0x7f && r < 0xa0 {
			return "", bad
		}
	}
	return string(decoded), nil
}
