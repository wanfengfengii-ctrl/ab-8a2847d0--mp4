package fmp4

import (
	"encoding/binary"
	"fmt"
)

// box is a parsed ISO BMFF box header with absolute offsets into the file.
type box struct {
	typ  string
	pos  int // offset of the box header
	size int // total size including header
	hdr  int // header size (8 or 16)
}

// payload returns the box contents (everything after the header).
func (b box) payload(data []byte) []byte { return data[b.pos+b.hdr : b.pos+b.size] }

// parseBoxes parses the box sequence in data[base:base+size].
func parseBoxes(data []byte, base, size int) ([]box, error) {
	if base < 0 || size < 0 || base+size > len(data) {
		return nil, fmt.Errorf("invalid box range")
	}
	var out []box
	off, end := base, base+size
	for off < end {
		if end-off < 8 {
			return nil, fmt.Errorf("truncated box header at offset %d", off)
		}
		size32 := binary.BigEndian.Uint32(data[off:])
		typ := string(data[off+4 : off+8])
		hdr := 8
		var sz uint64
		switch size32 {
		case 1: // 64-bit largesize
			if end-off < 16 {
				return nil, fmt.Errorf("truncated largesize box %q at offset %d", typ, off)
			}
			sz = binary.BigEndian.Uint64(data[off+8:])
			hdr = 16
		case 0: // box extends to end of enclosing data
			sz = uint64(end - off)
		default:
			sz = uint64(size32)
		}
		if sz < uint64(hdr) {
			return nil, fmt.Errorf("box %q at offset %d has invalid size %d", typ, off, sz)
		}
		if sz > uint64(end-off) {
			return nil, fmt.Errorf("box %q at offset %d exceeds enclosing data", typ, off)
		}
		out = append(out, box{typ: typ, pos: off, size: int(sz), hdr: hdr})
		off += int(sz)
	}
	return out, nil
}

// children parses the boxes nested directly inside b.
func children(data []byte, b box) ([]box, error) {
	return parseBoxes(data, b.pos+b.hdr, b.size-b.hdr)
}

func findBox(bs []box, typ string) *box {
	for i := range bs {
		if bs[i].typ == typ {
			return &bs[i]
		}
	}
	return nil
}

func filterBoxes(bs []box, typ string) []box {
	var out []box
	for _, b := range bs {
		if b.typ == typ {
			out = append(out, b)
		}
	}
	return out
}

func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
func be64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }
