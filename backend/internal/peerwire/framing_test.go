package peerwire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFragmentedFrameBoundariesAndMalformedOffsets(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 255, 48}, 200000)
	a := Assembler{}
	count := 0
	err := fragments(data, func(p []byte) error {
		count++
		out, e := a.Push(p)
		if e != nil {
			return e
		}
		if out != nil && !bytes.Equal(out, data) {
			t.Fatal("payload changed")
		}
		return nil
	})
	if err != nil || count < 2 || len(a.data) != 0 {
		t.Fatal("fragmentation", err)
	}
	bad := make([]byte, 9)
	binary.BigEndian.PutUint32(bad, MaxFrame+1)
	if _, err := a.Push(bad); err == nil {
		t.Fatal("oversized frame accepted")
	}
	binary.BigEndian.PutUint32(bad, 2)
	binary.BigEndian.PutUint32(bad[4:], 1)
	if _, err := a.Push(bad); err == nil {
		t.Fatal("missing start accepted")
	}
}
