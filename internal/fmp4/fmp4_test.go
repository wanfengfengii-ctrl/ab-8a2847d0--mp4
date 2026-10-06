package fmp4_test

import (
	"encoding/binary"
	"testing"

	"fmp4audit/internal/fmp4"
	"fmp4audit/internal/gen"
)

func u32b(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func u64b(v uint64) []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, v); return b }
func cat(bs ...[]byte) []byte {
	var o []byte
	for _, b := range bs {
		o = append(o, b...)
	}
	return o
}

// continuous returns an init segment and three media segments with decode
// intervals [0,2048) [2048,3072) [3072,6144) at a 48000 Hz timescale.
func continuous() ([]byte, [][]byte) {
	init := gen.Init(gen.InitOpts{Timescale: 48000})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 100}, {Dur: 1024, Size: 110}}}),
		gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 2048, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 105}}}),
		gen.Media(gen.MediaOpts{Sequence: 3, BaseTime: 3072, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 120}, {Dur: 1024, Size: 121}, {Dur: 1024, Size: 122}}}),
	}
	return init, media
}

func wantErr(t *testing.T, init []byte, media [][]byte, code string, seg int) {
	t.Helper()
	res, err := fmp4.Audit(init, media)
	if err == nil {
		t.Fatalf("expected %s at segment %d, got success: %+v", code, seg, res)
	}
	if err.Code != code || err.Segment != seg {
		t.Fatalf("expected %s at segment %d, got %v", code, seg, err)
	}
}

func TestAuditContinuous(t *testing.T) {
	init, media := continuous()
	res, err := fmp4.Audit(init, media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Timescale != 48000 || res.TrackID != 1 {
		t.Fatalf("timescale/track: got %d/%d", res.Timescale, res.TrackID)
	}
	want := []fmp4.Segment{
		{Index: 0, Sequence: 1, Start: 0, End: 2048, Samples: 2, Duration: 2048},
		{Index: 1, Sequence: 2, Start: 2048, End: 3072, Samples: 1, Duration: 1024},
		{Index: 2, Sequence: 3, Start: 3072, End: 6144, Samples: 3, Duration: 3072},
	}
	if len(res.Segments) != len(want) {
		t.Fatalf("segment count: got %d want %d", len(res.Segments), len(want))
	}
	for i, w := range want {
		if res.Segments[i] != w {
			t.Errorf("segment %d: got %+v want %+v", i, res.Segments[i], w)
		}
	}
	if res.TotalDuration != 6144 {
		t.Errorf("total duration: got %d want 6144", res.TotalDuration)
	}
}

func TestAuditGap(t *testing.T) {
	init, media := continuous()
	media[1] = gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 2048 + 10, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 105}}})
	wantErr(t, init, media, fmp4.CodeGap, 1)
}

func TestAuditOverlap(t *testing.T) {
	init, media := continuous()
	media[1] = gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 2048 - 10, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 105}}})
	wantErr(t, init, media, fmp4.CodeOverlap, 1)
}

func TestAuditGapOnThirdSegment(t *testing.T) {
	init, media := continuous()
	media[2] = gen.Media(gen.MediaOpts{Sequence: 3, BaseTime: 3072 + 1, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 120}, {Dur: 1024, Size: 121}, {Dur: 1024, Size: 122}}})
	wantErr(t, init, media, fmp4.CodeGap, 2)
}

func TestAuditSequenceNotIncreasing(t *testing.T) {
	init, media := continuous()
	media[1] = gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 2048, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 105}}})
	wantErr(t, init, media, fmp4.CodeSequence, 1)

	media[1] = gen.Media(gen.MediaOpts{Sequence: 5, BaseTime: 2048, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 105}}})
	media[2] = gen.Media(gen.MediaOpts{Sequence: 4, BaseTime: 3072, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 120}, {Dur: 1024, Size: 121}, {Dur: 1024, Size: 122}}})
	wantErr(t, init, media, fmp4.CodeSequence, 2)
}

func TestAuditPayloadMismatch(t *testing.T) {
	init, media := continuous()
	media[0] = gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrunFields: true, MdatExtra: 3,
		Samples: []gen.Sample{{Dur: 1024, Size: 100}, {Dur: 1024, Size: 110}}})
	wantErr(t, init, media, fmp4.CodePayload, 0)

	media[0] = gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrunFields: true, MdatCut: 3,
		Samples: []gen.Sample{{Dur: 1024, Size: 100}, {Dur: 1024, Size: 110}}})
	wantErr(t, init, media, fmp4.CodePayload, 0)
}

func TestInheritFromTfhd(t *testing.T) {
	init := gen.Init(gen.InitOpts{})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TfhdDefaults: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}, {Dur: 1024, Size: 64}}}),
		gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 2048, TfhdDefaults: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	res, err := fmp4.Audit(init, media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalDuration != 3072 || res.Segments[1].End != 3072 {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestInheritFromTrex(t *testing.T) {
	init := gen.Init(gen.InitOpts{TrexDuration: 1024, TrexSize: 64})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 7, BaseTime: 500,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}, {Dur: 1024, Size: 64}}}),
		gen.Media(gen.MediaOpts{Sequence: 8, BaseTime: 500 + 2048,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	res, err := fmp4.Audit(init, media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Segments[0].Start != 500 || res.Segments[1].End != 500+3072 {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestParamInheritMissing(t *testing.T) {
	// No trun fields, no tfhd defaults, trex defaults are zero: unresolvable.
	init := gen.Init(gen.InitOpts{})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	wantErr(t, init, media, fmp4.CodeParamInherit, 0)

	// Duration resolvable via tfhd but size is not.
	init = gen.Init(gen.InitOpts{TrexSize: 64})
	media = [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	wantErr(t, init, media, fmp4.CodeParamInherit, 0)
}

func TestTrackMismatch(t *testing.T) {
	init := gen.Init(gen.InitOpts{TrackID: 1})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrackID: 2, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	wantErr(t, init, media, fmp4.CodeTrackMismatch, 0)
}

func TestNotAudio(t *testing.T) {
	init := gen.Init(gen.InitOpts{Handler: "vide"})
	_, media := continuous()
	wantErr(t, init, media[:1], fmp4.CodeNotAudio, -1)
}

func TestTwoTracks(t *testing.T) {
	init := gen.Init(gen.InitOpts{ExtraTrack: true})
	_, media := continuous()
	wantErr(t, init, media[:1], fmp4.CodeNotAudio, -1)
}

func TestNoMvex(t *testing.T) {
	init := gen.Init(gen.InitOpts{OmitMvex: true})
	_, media := continuous()
	wantErr(t, init, media[:1], fmp4.CodeNoMvex, -1)
}

func TestNoTfdt(t *testing.T) {
	init := gen.Init(gen.InitOpts{})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, OmitTfdt: true, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	wantErr(t, init, media, fmp4.CodeNoTfdt, 0)
}

func TestBoxStructure(t *testing.T) {
	init, media := continuous()

	// Truncated init.
	wantErr(t, init[:len(init)-5], media[:1], fmp4.CodeBoxStructure, -1)

	// Media segment without mdat.
	noMdat := gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrunFields: true, OmitMdat: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 64}}})
	wantErr(t, init, [][]byte{noMdat}, fmp4.CodeBoxStructure, 0)

	// Init segment submitted as media.
	wantErr(t, init, [][]byte{init}, fmp4.CodeBoxStructure, 0)

	// Media segment submitted as init.
	wantErr(t, media[0], media[1:2], fmp4.CodeBoxStructure, -1)

	// Garbage trailing bytes inside a trun: craft a trun with 4 extra bytes.
	bad := gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 64}}})
	bad = append(bad, 0) // trailing byte past the last box
	wantErr(t, init, [][]byte{bad}, fmp4.CodeBoxStructure, 0)
}

func TestTfdtVersion1(t *testing.T) {
	init := gen.Init(gen.InitOpts{})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 1 << 33, TfdtVersion: 1, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
		gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: (1 << 33) + 1024, TfdtVersion: 1, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	res, err := fmp4.Audit(init, media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Segments[0].Start != 1<<33 || res.Segments[1].End != (1<<33)+2048 {
		t.Fatalf("bad 64-bit timeline: %+v", res.Segments)
	}
}

func TestStypAndZeroSamples(t *testing.T) {
	init := gen.Init(gen.InitOpts{})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, WithStyp: true, TrunFields: true,
			Samples: nil}), // zero samples, empty mdat
		gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 0, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 64}}}),
	}
	res, err := fmp4.Audit(init, media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Segments[0].Duration != 0 || res.Segments[1].Start != 0 {
		t.Fatalf("bad result: %+v", res.Segments)
	}
}

func TestLargesizeBox(t *testing.T) {
	init, media := continuous()
	// Re-encode the mdat of segment 0 with a 64-bit largesize header.
	seg := media[0]
	// Find "mdat" fourcc and rebuild: simplest is regenerating via gen is not
	// possible with largesize, so patch manually.
	idx := -1
	for i := 0; i+4 < len(seg); i++ {
		if string(seg[i:i+4]) == "mdat" {
			idx = i - 4 // start of size field
			break
		}
	}
	if idx < 0 {
		t.Fatal("mdat not found")
	}
	payload := seg[idx+8:]
	large := make([]byte, 0, 16+len(payload))
	large = append(large, 0, 0, 0, 1) // size = 1 => largesize
	large = append(large, []byte("mdat")...)
	sz := uint64(16 + len(payload))
	large = append(large, byte(sz>>56), byte(sz>>48), byte(sz>>40), byte(sz>>32),
		byte(sz>>24), byte(sz>>16), byte(sz>>8), byte(sz))
	large = append(large, payload...)
	patched := append(append([]byte{}, seg[:idx]...), large...)
	res, err := fmp4.Audit(init, [][]byte{patched})
	if err != nil {
		t.Fatalf("largesize mdat rejected: %v", err)
	}
	if res.Segments[0].Samples != 2 {
		t.Fatalf("bad result: %+v", res.Segments[0])
	}
}

// Real-world fragments commonly carry tfhd base-data-offset and trun
// data-offset; the parser must skip them while still applying tfhd defaults.
func TestDataOffsetFields(t *testing.T) {
	init := gen.Init(gen.InitOpts{})
	tfhd := gen.FullBox("tfhd", 0, 0x1|0x8|0x10,
		cat(u32b(1), u64b(0), u32b(1024), u32b(5)))
	tfdt := gen.FullBox("tfdt", 0, 0, u32b(0))
	trun := gen.FullBox("trun", 0, 0x1, cat(u32b(2), u32b(100))) // data_offset present
	traf := gen.Box("traf", cat(tfhd, tfdt, trun))
	moof := gen.Box("moof", cat(gen.FullBox("mfhd", 0, 0, u32b(1)), traf))
	seg := cat(moof, gen.Box("mdat", make([]byte, 10))) // 2 samples x 5 bytes

	res, err := fmp4.Audit(init, [][]byte{seg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Segments[0].Samples != 2 || res.Segments[0].Duration != 2048 {
		t.Fatalf("bad result: %+v", res.Segments[0])
	}
}

// Multiple trafs in one moof must chain contiguously inside the segment.
func TestMultiTraf(t *testing.T) {
	init := gen.Init(gen.InitOpts{})
	traf := func(base uint32) []byte {
		return gen.Box("traf", cat(
			gen.FullBox("tfhd", 0, 0x8|0x10, cat(u32b(1), u32b(1024), u32b(5))),
			gen.FullBox("tfdt", 0, 0, u32b(base)),
			gen.FullBox("trun", 0, 0, u32b(1)),
		))
	}
	moof := func(trafs ...[]byte) []byte {
		return gen.Box("moof", cat(append([][]byte{gen.FullBox("mfhd", 0, 0, u32b(1))}, trafs...)...))
	}
	mdat := gen.Box("mdat", make([]byte, 10))

	res, err := fmp4.Audit(init, [][]byte{cat(moof(traf(0), traf(1024)), mdat)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Segments[0].Samples != 2 || res.Segments[0].End != 2048 {
		t.Fatalf("bad result: %+v", res.Segments[0])
	}

	// Second traf starts 512 ticks late: gap inside the segment.
	wantErr(t, init, [][]byte{cat(moof(traf(0), traf(1536)), mdat)}, fmp4.CodeGap, 0)
}
