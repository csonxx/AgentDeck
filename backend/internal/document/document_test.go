package document

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func identity(s string) string { return s }
func TestRealWord97UnicodeAndParagraphs(t *testing.T) {
	raw, e := Parse(fixture(t, "word97.doc"), ".doc", identity)
	if e != nil {
		t.Fatal(e)
	}
	var p Preview
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	text := strings.Join(p.Lines, "\n")
	if !strings.Contains(text, "Legacy Word preview") || !strings.Contains(text, "中文正文与表格") || !strings.Contains(text, "Sample\t42") {
		t.Fatalf("unexpected synthetic DOC output: %q", text)
	}
}
func TestRealBIFF8SheetsUnicodeSparseNumbersAndContinue(t *testing.T) {
	raw, e := Parse(fixture(t, "book97.xls"), ".xls", identity)
	if e != nil {
		t.Fatal(e)
	}
	var p Preview
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	if len(p.Sheets) != 2 || p.Sheets[0].Name != "预算" {
		t.Fatal("sheets")
	}
	s := p.Sheets[0]
	if s.Rows[0][0] != "项目" || s.Rows[0][2] != "金额" || s.Rows[1][2] != "42.5" || s.Rows[2][1] != "TRUE" {
		t.Fatalf("synthetic cells: %v", s.Rows[:3])
	}
	if len(s.Rows[4][0]) < 4096 || !strings.HasSuffix(s.Rows[4][0], "…") {
		t.Fatal("CONTINUE/truncated SST")
	}
	if p.Sheets[1].RowNumbers[0] != 12 || p.Sheets[1].Rows[0][1] != "1234" {
		t.Fatal("sparse row")
	}
	if s.Rows[3][2] != "" {
		t.Fatal("uncached formula must stay blank, never evaluate")
	}
}
func TestOfficeRejectsMalformedAndPasswordProtected(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("not office"), make([]byte, 512), bytes.Repeat([]byte{0xff}, 1024)} {
		if _, e := Parse(b, ".doc", identity); e == nil {
			t.Fatal("invalid DOC accepted")
		}
	}
	ss, e := streams(fixture(t, "word97.doc"))
	if e != nil {
		t.Fatal(e)
	}
	ss["WordDocument"][11] |= 1
	if _, e = word(ss); e != ErrEncrypted {
		t.Fatal("password DOC", e)
	}
	bad := fixture(t, "book97.xls")
	for i := 44; i < 48; i++ {
		bad[i] = 0xff
	}
	if _, e = Parse(bad, ".xls", identity); e == nil {
		t.Fatal("unbounded CFB header accepted")
	}
}
func TestSegmentedUnicodeCompressionChangesAndRichRuns(t *testing.T) {
	s := segments{parts: [][]byte{{3, 0, 0, 65}, {1, 0x2d, 0x4e, 0x87, 0x65}}}
	v, e := s.text(false)
	if e != nil || v != "A中文" {
		t.Fatal(v, e)
	}
	s = segments{parts: [][]byte{{1, 0, 8, 1, 0, 65, 0}, {0, 0, 0}}}
	v, e = s.text(false)
	if e != nil || v != "A" {
		t.Fatal(v, e)
	}
	if _, e = (&segments{parts: [][]byte{{2, 0, 1, 65}, {1, 0}}}).text(false); e == nil {
		t.Fatal("split UTF16 unit accepted")
	}
}
func FuzzOffice(f *testing.F) {
	for _, name := range []string{"word97.doc", "book97.xls"} {
		b, _ := os.ReadFile("testdata/" + name)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		for _, ext := range []string{".doc", ".xls"} {
			_, _ = Parse(b, ext, identity)
		}
	})
}
