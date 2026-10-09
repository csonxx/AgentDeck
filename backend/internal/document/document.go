// Package document provides bounded, content-only Office binary previews.
// Formats follow Microsoft MS-DOC and MS-XLS. Macros and formulas are never run.
package document

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/charmap"
)

const MIME = "application/vnd.agentdeck.office+json"

var ErrFormat = errors.New("invalid or unsupported Office document")
var ErrEncrypted = errors.New("password-protected Office document")
var le = binary.LittleEndian

const maxText = 1 << 20

type Preview struct {
	Kind      string   `json:"kind"`
	Lines     []string `json:"lines,omitempty"`
	Sheets    []Sheet  `json:"sheets,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}
type Sheet struct {
	Name       string     `json:"name"`
	Rows       [][]string `json:"rows"`
	Columns    int        `json:"columns"`
	RowNumbers []int      `json:"rowNumbers"`
	Truncated  bool       `json:"truncated"`
}

// Cap all CFB reads, including internal directory and FAT traversal. Header
// counts cannot cause allocations larger than the physical file's sectors.
type boundedReader struct {
	*bytes.Reader
	calls int
}

func (r *boundedReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	if r.calls > 200000 {
		return 0, ErrFormat
	}
	return r.Reader.ReadAt(p, off)
}
func streams(data []byte) (map[string][]byte, error) {
	if len(data) < 512 || len(data) > 8<<20 {
		return nil, ErrFormat
	}
	sectorShift := le.Uint16(data[30:32])
	if sectorShift != 9 && sectorShift != 12 {
		return nil, ErrFormat
	}
	sectors := uint32(len(data) >> sectorShift)
	for _, off := range []int{40, 44, 64, 72} {
		if le.Uint32(data[off:off+4]) > sectors {
			return nil, ErrFormat
		}
	}
	r, e := mscfb.New(&boundedReader{Reader: bytes.NewReader(data)})
	if e != nil || len(r.File) > 8192 {
		return nil, ErrFormat
	}
	out := map[string][]byte{}
	for _, f := range r.File {
		if len(f.Path) != 0 {
			continue
		}
		switch f.Name {
		case "WordDocument", "0Table", "1Table", "Workbook", "Book":
		default:
			continue
		}
		if f.Size < 0 || f.Size > int64(len(data)) {
			return nil, ErrFormat
		}
		b, e := io.ReadAll(io.LimitReader(f, f.Size+1))
		if e != nil || int64(len(b)) != f.Size {
			return nil, ErrFormat
		}
		out[f.Name] = b
	}
	return out, nil
}
func Parse(data []byte, ext string, redact func(string) string) (out []byte, err error) {
	// Invalid offsets in a damaged binary must not terminate the server.
	defer func() {
		if recover() != nil {
			out = nil
			err = ErrFormat
		}
	}()
	ss, e := streams(data)
	if e != nil {
		return nil, e
	}
	var p Preview
	switch ext {
	case ".doc":
		p, e = word(ss)
	case ".xls":
		b := ss["Workbook"]
		if b == nil {
			b = ss["Book"]
		}
		p, e = excel(b)
	default:
		e = ErrFormat
	}
	if e != nil {
		return nil, e
	}
	for i := range p.Lines {
		p.Lines[i] = redact(p.Lines[i])
	}
	for i := range p.Sheets {
		p.Sheets[i].Name = redact(p.Sheets[i].Name)
		for j := range p.Sheets[i].Rows {
			for k, v := range p.Sheets[i].Rows[j] {
				p.Sheets[i].Rows[j][k] = redact(v)
			}
		}
	}
	return json.Marshal(p)
}
func u16(b []byte, p int) (int, error) {
	if p < 0 || p > len(b)-2 {
		return 0, ErrFormat
	}
	return int(le.Uint16(b[p:])), nil
}
func u32(b []byte, p int) (int, error) {
	if p < 0 || p > len(b)-4 {
		return 0, ErrFormat
	}
	return int(le.Uint32(b[p:])), nil
}
func span(b []byte, p, n int) ([]byte, error) {
	if p < 0 || n < 0 || p > len(b)-n {
		return nil, ErrFormat
	}
	return b[p : p+n], nil
}
func word(ss map[string][]byte) (Preview, error) {
	p := Preview{Kind: "word", Lines: []string{}}
	b := ss["WordDocument"]
	if len(b) < 34 || le.Uint16(b) != 0xa5ec || le.Uint16(b[2:]) < 0xc1 {
		return p, ErrFormat
	}
	flags := le.Uint16(b[10:])
	if flags&0x8100 != 0 {
		return p, ErrEncrypted
	}
	table := ss["0Table"]
	if flags&0x200 != 0 {
		table = ss["1Table"]
	}
	csw, e := u16(b, 32)
	if e != nil {
		return p, e
	}
	off := 34 + csw*2
	cslw, e := u16(b, off)
	if e != nil || cslw < 4 {
		return p, ErrFormat
	}
	chars, e := u32(b, off+2+12)
	if e != nil {
		return p, e
	}
	off += 2 + cslw*4
	count, e := u16(b, off)
	if e != nil || count < 34 {
		return p, ErrFormat
	}
	off += 2 + 33*8
	fc, e := u32(b, off)
	if e != nil {
		return p, e
	}
	size, e := u32(b, off+4)
	if e != nil {
		return p, e
	}
	clx, e := span(table, fc, size)
	if e != nil {
		return p, e
	}
	for len(clx) > 0 && clx[0] == 1 {
		n, e := u16(clx, 1)
		if e != nil || n > len(clx)-3 {
			return p, ErrFormat
		}
		clx = clx[3+n:]
	}
	if len(clx) < 5 || clx[0] != 2 {
		return p, ErrFormat
	}
	n, e := u32(clx, 1)
	if e != nil || n < 4 || (n-4)%12 != 0 {
		return p, ErrFormat
	}
	plc, e := span(clx, 5, n)
	if e != nil {
		return p, e
	}
	pieces := (n - 4) / 12
	units := make([]uint16, 0, min(chars, 65536))
	truncated := false
	previous := 0
	for i := 0; i < pieces; i++ {
		a, _ := u32(plc, i*4)
		z, _ := u32(plc, (i+1)*4)
		if a < previous || z < a {
			return p, ErrFormat
		}
		previous = z
		if a >= chars {
			break
		}
		z = min(z, chars)
		raw, _ := u32(plc, (pieces+1)*4+i*8+2)
		compressed := raw&0x40000000 != 0
		pos := raw & 0x3fffffff
		length := z - a
		width := 2
		if compressed {
			pos /= 2
			width = 1
		}
		// Validate even the truncated tail before choosing a bounded preview.
		source, e := span(b, pos, length*width)
		if e != nil {
			return p, e
		}
		take := min(length, 500000-len(units))
		if take < length {
			truncated = true
		}
		if compressed {
			text, e := charmap.Windows1252.NewDecoder().Bytes(source[:take])
			if e != nil {
				return p, ErrFormat
			}
			units = append(units, utf16.Encode([]rune(string(text)))...)
		} else {
			for j := 0; j < take; j++ {
				units = append(units, le.Uint16(source[j*2:]))
			}
		}
		if truncated {
			break
		}
	}
	// Exclude field instructions; retain display results, including nested fields.
	var out strings.Builder
	fields := []bool{}
	for _, r := range utf16.Decode(units) {
		if r == 0x13 {
			fields = append(fields, false)
			continue
		}
		if r == 0x14 {
			if len(fields) > 0 {
				fields[len(fields)-1] = true
			}
			continue
		}
		if r == 0x15 {
			if len(fields) > 0 {
				fields = fields[:len(fields)-1]
			}
			continue
		}
		hidden := false
		for _, visible := range fields {
			if !visible {
				hidden = true
				break
			}
		}
		if hidden {
			continue
		}
		switch r {
		case '\r', 0x0b, 0x0c:
			r = '\n'
		case 7:
			r = '\t'
		}
		if r < 32 && r != '\n' && r != '\t' {
			continue
		}
		if out.Len()+4 > maxText {
			truncated = true
			break
		}
		out.WriteRune(r)
	}
	p.Lines = strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	p.Truncated = truncated
	if len(p.Lines) > 10000 {
		p.Lines = p.Lines[:10000]
		p.Truncated = true
	}
	return p, nil
}
