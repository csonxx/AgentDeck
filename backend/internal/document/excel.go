package document

import (
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
)

type record struct {
	id     int
	data   []byte
	offset int
}

func records(b []byte, off int) ([]record, error) {
	out := []record{}
	for off < len(b) {
		if len(b)-off < 4 {
			return nil, ErrFormat
		}
		id := int(le.Uint16(b[off:]))
		n := int(le.Uint16(b[off+2:]))
		p := off
		off += 4
		if n > len(b)-off {
			return nil, ErrFormat
		}
		out = append(out, record{id, b[off : off+n], p})
		off += n
		if id == 0x0a {
			return out, nil
		}
		if len(out) > 200000 {
			return nil, ErrFormat
		}
	}
	return nil, ErrFormat
}

// Segmented BIFF strings preserve CONTINUE boundaries. Only a continuation
// inside character data introduces a new compression flag; headers/runs do not.
type segments struct {
	parts     [][]byte
	part, pos int
}

func (s *segments) byte() (byte, error) {
	for s.part < len(s.parts) && s.pos == len(s.parts[s.part]) {
		s.part++
		s.pos = 0
	}
	if s.part >= len(s.parts) {
		return 0, ErrFormat
	}
	b := s.parts[s.part][s.pos]
	s.pos++
	return b, nil
}
func (s *segments) uint(n int) (int, error) {
	v := 0
	for i := 0; i < n; i++ {
		b, e := s.byte()
		if e != nil {
			return 0, e
		}
		v |= int(b) << (8 * i)
	}
	return v, nil
}
func (s *segments) skip(n int) error {
	if n < 0 || n > 8<<20 {
		return ErrFormat
	}
	for i := 0; i < n; i++ {
		if _, e := s.byte(); e != nil {
			return e
		}
	}
	return nil
}
func (s *segments) text(short bool) (string, error) {
	width := 2
	if short {
		width = 1
	}
	n, e := s.uint(width)
	if e != nil {
		return "", e
	}
	flags, e := s.byte()
	if e != nil || flags & ^byte(0x0d) != 0 {
		return "", ErrFormat
	}
	runs, ext := 0, 0
	if flags&8 != 0 {
		runs, e = s.uint(2)
		if e != nil {
			return "", e
		}
	}
	if flags&4 != 0 {
		ext, e = s.uint(4)
		if e != nil {
			return "", e
		}
	}
	units := make([]uint16, 0, min(n, 4096))
	wide := flags&1 != 0
	for i := 0; i < n; i++ {
		// A boundary between strings has no flag; a boundary mid-string does.
		if s.part < len(s.parts) && s.pos == len(s.parts[s.part]) {
			s.part++
			s.pos = 0
			if s.part >= len(s.parts) {
				return "", ErrFormat
			}
			f, e := s.byte()
			if e != nil || f > 1 {
				return "", ErrFormat
			}
			wide = f == 1
		}
		a, e := s.byte()
		if e != nil {
			return "", e
		}
		v := uint16(a)
		if wide {
			// BIFF never splits a UTF-16 code unit across CONTINUE records.
			if s.part >= len(s.parts) || s.pos == len(s.parts[s.part]) {
				return "", ErrFormat
			}
			b, e := s.byte()
			if e != nil {
				return "", e
			}
			v |= uint16(b) << 8
		}
		if i < 4096 {
			units = append(units, v)
		}
	}
	if e = s.skip(runs * 4); e != nil {
		return "", e
	}
	if e = s.skip(ext); e != nil {
		return "", e
	}
	text := string(utf16.Decode(units))
	if n > 4096 {
		text += "…"
	}
	return text, nil
}
func stringParts(rs []record, i, start int) (*segments, int, error) {
	if start > len(rs[i].data) {
		return nil, i, ErrFormat
	}
	parts := [][]byte{rs[i].data[start:]}
	j := i + 1
	for j < len(rs) && rs[j].id == 0x3c {
		parts = append(parts, rs[j].data)
		j++
	}
	return &segments{parts: parts}, j - 1, nil
}
func number(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "#NUM!"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
func rk(v uint32) string {
	var n float64
	if v&2 != 0 {
		n = float64(int32(v) >> 2)
	} else {
		n = math.Float64frombits(uint64(v & ^uint32(3)) << 32)
	}
	if v&1 != 0 {
		n /= 100
	}
	return number(n)
}
func excel(b []byte) (Preview, error) {
	p := Preview{Kind: "excel", Sheets: []Sheet{}}
	rs, e := records(b, 0)
	if e != nil || len(rs) == 0 || rs[0].id != 0x809 || len(rs[0].data) < 4 || le.Uint16(rs[0].data) != 0x600 || le.Uint16(rs[0].data[2:]) != 5 {
		return p, ErrFormat
	}
	type bound struct {
		name string
		off  int
	}
	sheets := []bound{}
	sst := []string{}
	textBytes := 0
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r.id {
		case 0x2f:
			return p, ErrEncrypted
		case 0x85:
			if len(r.data) < 8 {
				return p, ErrFormat
			}
			if r.data[5] != 0 {
				continue
			}
			if len(sheets) >= 50 {
				p.Truncated = true
				continue
			}
			s := &segments{parts: [][]byte{r.data[6:]}}
			name, e := s.text(true)
			if e != nil {
				return p, e
			}
			sheets = append(sheets, bound{name, int(le.Uint32(r.data))})
		case 0xfc:
			if len(r.data) < 8 {
				return p, ErrFormat
			}
			unique := int(le.Uint32(r.data[4:]))
			if unique > 100000 {
				return p, ErrFormat
			}
			s, last, e := stringParts(rs, i, 8)
			if e != nil {
				return p, e
			}
			i = last
			for j := 0; j < unique; j++ {
				v, e := s.text(false)
				if e != nil {
					return p, e
				}
				textBytes += len(v)
				if textBytes > 4<<20 {
					return p, ErrFormat
				}
				sst = append(sst, v)
			}
		}
	}
	gridBudget := 200000
	valueBudget := 4 << 20
	cellBudget := 50000
	for _, sh := range sheets {
		rs, e := records(b, sh.off)
		if e != nil || len(rs) == 0 || rs[0].id != 0x809 || len(rs[0].data) < 4 || le.Uint16(rs[0].data[2:]) != 0x10 {
			return p, ErrFormat
		}
		sheet := Sheet{Name: sh.name, Rows: [][]string{}, RowNumbers: []int{}}
		cells := map[int]map[int]string{}
		pendingRow, pendingCol := -1, -1
		put := func(row, col int, value string) {
			if col >= 100 || cellBudget <= 0 || valueBudget < len(value) {
				sheet.Truncated = true
				return
			}
			if _, ok := cells[row]; !ok {
				if len(cells) >= 2000 {
					sheet.Truncated = true
					return
				}
				cells[row] = map[int]string{}
			}
			valueBudget -= len(value)
			cellBudget--
			cells[row][col] = value
		}
		for i := 0; i < len(rs); i++ {
			r := rs[i]
			d := r.data
			if r.id == 0x2f {
				return p, ErrEncrypted
			}
			if r.id == 0x207 {
				if pendingRow >= 0 {
					s, last, e := stringParts(rs, i, 0)
					if e != nil {
						return p, e
					}
					v, e := s.text(false)
					if e != nil {
						return p, e
					}
					i = last
					put(pendingRow, pendingCol, v)
					pendingRow = -1
				}
				continue
			}
			switch r.id {
			case 0xfd, 0x203, 0x27e, 0xbd, 0x205, 0x204, 0x6:
			default:
				continue
			}
			if len(d) < 6 {
				return p, ErrFormat
			}
			row, col := int(le.Uint16(d)), int(le.Uint16(d[2:]))
			v := ""
			switch r.id {
			case 0xfd:
				if len(d) < 10 {
					return p, ErrFormat
				}
				index := int(le.Uint32(d[6:]))
				if index >= len(sst) {
					return p, ErrFormat
				}
				v = sst[index]
			case 0x203:
				if len(d) < 14 {
					return p, ErrFormat
				}
				v = number(math.Float64frombits(le.Uint64(d[6:])))
			case 0x27e:
				if len(d) < 10 {
					return p, ErrFormat
				}
				v = rk(le.Uint32(d[6:]))
			case 0xbd:
				if len(d) < 12 || (len(d)-6)%6 != 0 {
					return p, ErrFormat
				}
				count := (len(d) - 6) / 6
				if int(le.Uint16(d[len(d)-2:])) != col+count-1 {
					return p, ErrFormat
				}
				for j := 0; j < count; j++ {
					put(row, col+j, rk(le.Uint32(d[6+j*6:])))
				}
				continue
			case 0x205:
				if len(d) < 8 {
					return p, ErrFormat
				}
				if d[7] != 0 {
					v = errorValue(d[6])
				} else if d[6] != 0 {
					v = "TRUE"
				} else {
					v = "FALSE"
				}
			case 0x204:
				s, last, e := stringParts(rs, i, 6)
				if e != nil {
					return p, e
				}
				v, e = s.text(false)
				if e != nil {
					return p, e
				}
				i = last
			case 0x6:
				if len(d) < 22 {
					return p, ErrFormat
				}
				if d[12] == 0xff && d[13] == 0xff {
					switch d[6] {
					case 0:
						pendingRow, pendingCol = row, col
						continue
					case 1:
						if d[8] != 0 {
							v = "TRUE"
						} else {
							v = "FALSE"
						}
					case 2:
						v = errorValue(d[8])
					case 3:
						v = ""
					default:
						return p, ErrFormat
					}
				} else {
					v = number(math.Float64frombits(le.Uint64(d[6:])))
				}
			}
			put(row, col, v)
		}
		rows := make([]int, 0, len(cells))
		for row := range cells {
			rows = append(rows, row)
		}
		sort.Ints(rows)
		for _, row := range rows {
			maxCol := -1
			for col := range cells[row] {
				maxCol = max(maxCol, col)
			}
			if gridBudget < maxCol+1 {
				sheet.Truncated = true
				break
			}
			gridBudget -= maxCol + 1
			values := make([]string, maxCol+1)
			for col, v := range cells[row] {
				values[col] = v
			}
			sheet.Columns = max(sheet.Columns, maxCol+1)
			sheet.Rows = append(sheet.Rows, values)
			sheet.RowNumbers = append(sheet.RowNumbers, row+1)
		}
		p.Sheets = append(p.Sheets, sheet)
	}
	if len(p.Sheets) == 0 {
		return p, ErrFormat
	}
	return p, nil
}
func errorValue(v byte) string {
	m := map[byte]string{0: "#NULL!", 7: "#DIV/0!", 15: "#VALUE!", 23: "#REF!", 29: "#NAME?", 36: "#NUM!", 42: "#N/A"}
	if s := m[v]; s != "" {
		return s
	}
	return "#ERROR!"
}
