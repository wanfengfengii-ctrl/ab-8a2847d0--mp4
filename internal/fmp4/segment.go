package fmp4

import "fmt"

// Segment describes one audited media segment on the decode timeline.
type Segment struct {
	Index    int    // 0-based position in submission order
	Sequence uint32 // mfhd sequence_number
	Start    uint64 // first decode tick (tfdt baseMediaDecodeTime)
	End      uint64 // one past the last decode tick (Start + Duration)
	Samples  uint32 // total sample count across all truns
	Duration uint64 // total sample durations in track timescale ticks
}

// tfhdInfo carries the per-fragment defaults declared by tfhd.
type tfhdInfo struct {
	trackID uint32
	hasDur  bool
	defDur  uint32
	hasSize bool
	defSize uint32
}

// ParseSegment validates one media segment against the init parameters and
// recovers its decode interval, sample count and payload extent.
func ParseSegment(data []byte, idx int, init *Init) (*Segment, *Error) {
	top, err := parseBoxes(data, 0, len(data))
	if err != nil {
		return nil, boxErr(idx, "media: %v", err)
	}
	if findBox(top, "moov") != nil {
		return nil, boxErr(idx, "media: contains moov (an init segment was submitted as media)")
	}
	moofs := filterBoxes(top, "moof")
	mdats := filterBoxes(top, "mdat")
	if len(moofs) == 0 {
		return nil, boxErr(idx, "media: missing moof")
	}
	if len(moofs) > 1 {
		return nil, boxErr(idx, "media: expected exactly one moof, found %d", len(moofs))
	}
	if len(mdats) == 0 {
		return nil, boxErr(idx, "media: missing mdat")
	}
	if len(mdats) > 1 {
		return nil, boxErr(idx, "media: expected exactly one mdat, found %d", len(mdats))
	}
	mdatLen := mdats[0].size - mdats[0].hdr

	mc, err := children(data, moofs[0])
	if err != nil {
		return nil, boxErr(idx, "media: moof: %v", err)
	}
	mfhd := findBox(mc, "mfhd")
	if mfhd == nil {
		return nil, boxErr(idx, "media: moof missing mfhd")
	}
	mp := mfhd.payload(data)
	if len(mp) < 8 {
		return nil, boxErr(idx, "media: mfhd truncated")
	}
	seq := be32(mp[4:])

	trafs := filterBoxes(mc, "traf")
	if len(trafs) == 0 {
		return nil, boxErr(idx, "media: moof missing traf")
	}

	seg := &Segment{Index: idx, Sequence: seq}
	var totalSize, running uint64
	for ti, traf := range trafs {
		samples, start, dur, sizeSum, aerr := parseTraf(data, traf, idx, init)
		if aerr != nil {
			return nil, aerr
		}
		if ti == 0 {
			seg.Start = start
		} else if start != running {
			code := CodeGap
			if start < running {
				code = CodeOverlap
			}
			return nil, &Error{Code: code, Segment: idx, Msg: fmt.Sprintf(
				"media: traf %d decode start %d does not continue previous traf end %d",
				ti, start, running)}
		}
		running = start + dur
		seg.Samples += samples
		seg.Duration += dur
		totalSize += sizeSum
	}
	seg.End = seg.Start + seg.Duration
	if totalSize != uint64(mdatLen) {
		return nil, &Error{Code: CodePayload, Segment: idx, Msg: fmt.Sprintf(
			"media: samples occupy %d bytes but mdat holds %d", totalSize, mdatLen)}
	}
	return seg, nil
}

// parseTraf resolves every sample of one traf and returns its sample count,
// decode start, total duration and total byte size.
func parseTraf(data []byte, traf box, segIdx int, init *Init) (samples uint32, start, dur, sizeSum uint64, rerr *Error) {
	tc, err := children(data, traf)
	if err != nil {
		return 0, 0, 0, 0, boxErr(segIdx, "media: traf: %v", err)
	}
	tfhdb := findBox(tc, "tfhd")
	if tfhdb == nil {
		return 0, 0, 0, 0, boxErr(segIdx, "media: traf missing tfhd")
	}
	tfhd, aerr := parseTfhd(tfhdb.payload(data), segIdx)
	if aerr != nil {
		return 0, 0, 0, 0, aerr
	}
	if tfhd.trackID != init.TrackID {
		return 0, 0, 0, 0, &Error{Code: CodeTrackMismatch, Segment: segIdx, Msg: fmt.Sprintf(
			"media: tfhd track_ID %d does not match init track_ID %d", tfhd.trackID, init.TrackID)}
	}
	tfdtb := findBox(tc, "tfdt")
	if tfdtb == nil {
		return 0, 0, 0, 0, &Error{Code: CodeNoTfdt, Segment: segIdx,
			Msg: "media: traf missing tfdt (baseMediaDecodeTime)"}
	}
	start, aerr = parseTfdt(tfdtb.payload(data), segIdx)
	if aerr != nil {
		return 0, 0, 0, 0, aerr
	}
	truns := filterBoxes(tc, "trun")
	if len(truns) == 0 {
		return 0, 0, 0, 0, boxErr(segIdx, "media: traf has no trun")
	}
	for _, trun := range truns {
		n, d, z, aerr := parseTrun(trun.payload(data), segIdx, tfhd, init.Trex)
		if aerr != nil {
			return 0, 0, 0, 0, aerr
		}
		samples += n
		dur += d
		sizeSum += z
	}
	return samples, start, dur, sizeSum, nil
}

// parseTfhd decodes tfhd, honouring the optional-field flag order.
func parseTfhd(p []byte, seg int) (*tfhdInfo, *Error) {
	if len(p) < 8 {
		return nil, boxErr(seg, "media: tfhd truncated")
	}
	flags := uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])
	t := &tfhdInfo{trackID: be32(p[4:])}
	off := 8
	need := func(n int, what string) *Error {
		if len(p)-off < n {
			return boxErr(seg, "media: tfhd truncated reading %s", what)
		}
		return nil
	}
	if flags&0x000001 != 0 { // base-data-offset-present
		if e := need(8, "base_data_offset"); e != nil {
			return nil, e
		}
		off += 8
	}
	if flags&0x000002 != 0 { // sample-description-index-present
		if e := need(4, "sample_description_index"); e != nil {
			return nil, e
		}
		off += 4
	}
	if flags&0x000008 != 0 { // default-sample-duration-present
		if e := need(4, "default_sample_duration"); e != nil {
			return nil, e
		}
		t.hasDur, t.defDur = true, be32(p[off:])
		off += 4
	}
	if flags&0x000010 != 0 { // default-sample-size-present
		if e := need(4, "default_sample_size"); e != nil {
			return nil, e
		}
		t.hasSize, t.defSize = true, be32(p[off:])
		off += 4
	}
	if flags&0x000020 != 0 { // default-sample-flags-present
		if e := need(4, "default_sample_flags"); e != nil {
			return nil, e
		}
		off += 4
	}
	if len(p) != off {
		return nil, boxErr(seg, "media: tfhd has %d trailing bytes", len(p)-off)
	}
	return t, nil
}

// parseTfdt decodes the baseMediaDecodeTime (version 0: 32-bit, version 1: 64-bit).
func parseTfdt(p []byte, seg int) (uint64, *Error) {
	if len(p) < 4 {
		return 0, boxErr(seg, "media: tfdt truncated")
	}
	switch p[0] { // version
	case 0:
		if len(p) < 8 {
			return 0, boxErr(seg, "media: tfdt v0 truncated")
		}
		return uint64(be32(p[4:])), nil
	case 1:
		if len(p) < 12 {
			return 0, boxErr(seg, "media: tfdt v1 truncated")
		}
		return be64(p[4:]), nil
	default:
		return 0, boxErr(seg, "media: unsupported tfdt version %d", p[0])
	}
}

// parseTrun decodes one trun and resolves every sample's duration and size
// through the inheritance chain trun -> tfhd -> trex.
func parseTrun(p []byte, seg int, tfhd *tfhdInfo, trex Trex) (samples uint32, dur, size uint64, rerr *Error) {
	if len(p) < 8 {
		return 0, 0, 0, boxErr(seg, "media: trun truncated")
	}
	flags := uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])
	count := be32(p[4:])
	off := 8
	if flags&0x001 != 0 { // data-offset-present
		if len(p)-off < 4 {
			return 0, 0, 0, boxErr(seg, "media: trun truncated reading data_offset")
		}
		off += 4
	}
	if flags&0x004 != 0 { // first-sample-flags-present
		if len(p)-off < 4 {
			return 0, 0, 0, boxErr(seg, "media: trun truncated reading first_sample_flags")
		}
		off += 4
	}
	rec := 0
	if flags&0x100 != 0 {
		rec += 4 // sample_duration
	}
	if flags&0x200 != 0 {
		rec += 4 // sample_size
	}
	if flags&0x400 != 0 {
		rec += 4 // sample_flags
	}
	if flags&0x800 != 0 {
		rec += 4 // sample_composition_time_offset
	}
	need := uint64(count) * uint64(rec)
	if uint64(len(p)-off) < need {
		return 0, 0, 0, boxErr(seg,
			"media: trun declares %d samples (%d bytes) but only %d bytes remain",
			count, need, len(p)-off)
	}
	if uint64(len(p)-off) > need {
		return 0, 0, 0, boxErr(seg,
			"media: trun has %d trailing bytes after %d samples", uint64(len(p)-off)-need, count)
	}
	hasDur := flags&0x100 != 0
	hasSize := flags&0x200 != 0
	for i := uint32(0); i < count; i++ {
		var d, s uint32
		var dok, sok bool
		if hasDur {
			d, dok = be32(p[off:]), true
			off += 4
		}
		if hasSize {
			s, sok = be32(p[off:]), true
			off += 4
		}
		if flags&0x400 != 0 {
			off += 4
		}
		if flags&0x800 != 0 {
			off += 4
		}
		rd, ok := resolveSample(d, dok, tfhd.hasDur, tfhd.defDur, trex.DefaultSampleDuration)
		if !ok {
			return 0, 0, 0, &Error{Code: CodeParamInherit, Segment: seg, Msg: fmt.Sprintf(
				"media: sample %d has no duration in trun, tfhd or trex", i)}
		}
		rs, ok := resolveSample(s, sok, tfhd.hasSize, tfhd.defSize, trex.DefaultSampleSize)
		if !ok {
			return 0, 0, 0, &Error{Code: CodeParamInherit, Segment: seg, Msg: fmt.Sprintf(
				"media: sample %d has no size in trun, tfhd or trex", i)}
		}
		dur += uint64(rd)
		size += uint64(rs)
	}
	return count, dur, size, nil
}

// resolveSample applies the inheritance chain: per-sample trun field, then
// tfhd default, then trex default (a zero trex value means "no default").
func resolveSample(v uint32, present, tfhdOK bool, tfhdVal, trexVal uint32) (uint32, bool) {
	switch {
	case present:
		return v, true
	case tfhdOK:
		return tfhdVal, true
	case trexVal != 0:
		return trexVal, true
	}
	return 0, false
}
