// Package peerwire upgrades an existing authenticated encrypted stream to a
// WebRTC data channel. The WebSocket remains its authorization lifeline.
package peerwire

import (
	"encoding/binary"
	"errors"
)

const FragmentSize = 16 << 10
const MaxFrame = 32 << 20

// Each reliable, ordered SCTP message contains total length, byte offset and a
// bounded payload. Large history/image records never become giant SCTP messages.
type Assembler struct {
	Limit int
	data  []byte
	total int
}

func (a *Assembler) Push(p []byte) ([]byte, error) {
	if len(p) < 9 || len(p) > FragmentSize {
		return nil, errors.New("invalid peer fragment")
	}
	total, offset := int(binary.BigEndian.Uint32(p)), int(binary.BigEndian.Uint32(p[4:]))
	limit := a.Limit
	if limit == 0 {
		limit = MaxFrame
	}
	if total < 1 || total > limit || offset != len(a.data) || offset+len(p)-8 > total || (offset != 0 && total != a.total) {
		return nil, errors.New("invalid peer fragment sequence")
	}
	if offset == 0 {
		a.total = total
	}
	a.data = append(a.data, p[8:]...)
	if len(a.data) != total {
		return nil, nil
	}
	result := a.data
	a.data, a.total = nil, 0
	return result, nil
}

func fragments(data []byte, send func([]byte) error) error {
	if len(data) == 0 || len(data) > MaxFrame {
		return errors.New("peer frame too large")
	}
	for offset := 0; offset < len(data); {
		end := min(len(data), offset+FragmentSize-8)
		p := make([]byte, 8+end-offset)
		binary.BigEndian.PutUint32(p, uint32(len(data)))
		binary.BigEndian.PutUint32(p[4:], uint32(offset))
		copy(p[8:], data[offset:end])
		if err := send(p); err != nil {
			return err
		}
		offset = end
	}
	return nil
}
