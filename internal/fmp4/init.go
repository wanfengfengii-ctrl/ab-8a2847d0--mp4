package fmp4

import "fmt"

// Init holds the track parameters recovered from an init segment.
type Init struct {
	TrackID   uint32
	Timescale uint32
	Trex      Trex
}

// Trex holds the trex default sample parameters for the track.
type Trex struct {
	DefaultSampleDuration uint32
	DefaultSampleSize     uint32
	DefaultSampleFlags    uint32
}

// ParseInit validates an init segment and extracts the single audio track's
// parameters. It requires: ftyp first, exactly one moov, no moof/mdat,
// exactly one trak with a 'soun' handler, and an mvex with a matching trex.
func ParseInit(data []byte) (*Init, *Error) {
	top, err := parseBoxes(data, 0, len(data))
	if err != nil {
		return nil, boxErr(-1, "init: %v", err)
	}
	if len(top) == 0 || top[0].typ != "ftyp" {
		return nil, boxErr(-1, "init: first box must be ftyp")
	}
	if findBox(top, "moof") != nil || findBox(top, "mdat") != nil {
		return nil, boxErr(-1, "init: contains moof/mdat (a media segment was submitted as init)")
	}
	moovs := filterBoxes(top, "moov")
	if len(moovs) == 0 {
		return nil, boxErr(-1, "init: missing moov")
	}
	if len(moovs) > 1 {
		return nil, boxErr(-1, "init: multiple moov boxes")
	}
	mc, err := children(data, moovs[0])
	if err != nil {
		return nil, boxErr(-1, "init: moov: %v", err)
	}
	traks := filterBoxes(mc, "trak")
	if len(traks) != 1 {
		return nil, &Error{Code: CodeNotAudio, Segment: -1,
			Msg: fmt.Sprintf("init: expected exactly one track, found %d", len(traks))}
	}
	mvexs := filterBoxes(mc, "mvex")
	if len(mvexs) == 0 {
		return nil, &Error{Code: CodeNoMvex, Segment: -1,
			Msg: "init: missing mvex (not a fragmented MP4)"}
	}
	if len(mvexs) > 1 {
		return nil, boxErr(-1, "init: multiple mvex boxes")
	}
	trackID, timescale, aerr := parseTrak(data, traks[0])
	if aerr != nil {
		return nil, aerr
	}
	trex, aerr := parseMvex(data, mvexs[0], trackID)
	if aerr != nil {
		return nil, aerr
	}
	return &Init{TrackID: trackID, Timescale: timescale, Trex: *trex}, nil
}

// parseTrak extracts the track ID and media timescale, and verifies the
// track is an audio track with a usable sample description.
func parseTrak(data []byte, trak box) (trackID, timescale uint32, rerr *Error) {
	tc, err := children(data, trak)
	if err != nil {
		return 0, 0, boxErr(-1, "init: trak: %v", err)
	}
	tkhd := findBox(tc, "tkhd")
	if tkhd == nil {
		return 0, 0, boxErr(-1, "init: trak missing tkhd")
	}
	p := tkhd.payload(data)
	if len(p) < 4 {
		return 0, 0, boxErr(-1, "init: tkhd truncated")
	}
	var idOff int
	switch p[0] { // version
	case 0:
		idOff = 12
	case 1:
		idOff = 20
	default:
		return 0, 0, boxErr(-1, "init: unsupported tkhd version %d", p[0])
	}
	if len(p) < idOff+4 {
		return 0, 0, boxErr(-1, "init: tkhd truncated")
	}
	trackID = be32(p[idOff:])

	mdia := findBox(tc, "mdia")
	if mdia == nil {
		return 0, 0, boxErr(-1, "init: trak missing mdia")
	}
	dc, err := children(data, *mdia)
	if err != nil {
		return 0, 0, boxErr(-1, "init: mdia: %v", err)
	}
	mdhd := findBox(dc, "mdhd")
	if mdhd == nil {
		return 0, 0, boxErr(-1, "init: mdia missing mdhd")
	}
	mp := mdhd.payload(data)
	if len(mp) < 4 {
		return 0, 0, boxErr(-1, "init: mdhd truncated")
	}
	var tsOff int
	switch mp[0] { // version
	case 0:
		tsOff = 12
	case 1:
		tsOff = 20
	default:
		return 0, 0, boxErr(-1, "init: unsupported mdhd version %d", mp[0])
	}
	if len(mp) < tsOff+4 {
		return 0, 0, boxErr(-1, "init: mdhd truncated")
	}
	timescale = be32(mp[tsOff:])
	if timescale == 0 {
		return 0, 0, boxErr(-1, "init: mdhd timescale is zero")
	}

	hdlr := findBox(dc, "hdlr")
	if hdlr == nil {
		return 0, 0, boxErr(-1, "init: mdia missing hdlr")
	}
	hp := hdlr.payload(data)
	if len(hp) < 12 {
		return 0, 0, boxErr(-1, "init: hdlr truncated")
	}
	if handler := string(hp[8:12]); handler != "soun" {
		return 0, 0, &Error{Code: CodeNotAudio, Segment: -1,
			Msg: fmt.Sprintf("init: track handler is %q, want 'soun' (audio)", handler)}
	}

	// The track must carry at least one sample entry to be decodable.
	minf := findBox(dc, "minf")
	if minf == nil {
		return 0, 0, boxErr(-1, "init: mdia missing minf")
	}
	ic, err := children(data, *minf)
	if err != nil {
		return 0, 0, boxErr(-1, "init: minf: %v", err)
	}
	stbl := findBox(ic, "stbl")
	if stbl == nil {
		return 0, 0, boxErr(-1, "init: minf missing stbl")
	}
	sc, err := children(data, *stbl)
	if err != nil {
		return 0, 0, boxErr(-1, "init: stbl: %v", err)
	}
	stsd := findBox(sc, "stsd")
	if stsd == nil {
		return 0, 0, boxErr(-1, "init: stbl missing stsd")
	}
	sp := stsd.payload(data)
	if len(sp) < 8 {
		return 0, 0, boxErr(-1, "init: stsd truncated")
	}
	if be32(sp[4:]) == 0 {
		return 0, 0, boxErr(-1, "init: stsd has no sample entries")
	}
	return trackID, timescale, nil
}

// parseMvex finds the trex matching trackID and returns its defaults.
func parseMvex(data []byte, mvex box, trackID uint32) (*Trex, *Error) {
	xc, err := children(data, mvex)
	if err != nil {
		return nil, boxErr(-1, "init: mvex: %v", err)
	}
	for _, b := range xc {
		if b.typ != "trex" {
			continue
		}
		p := b.payload(data)
		if len(p) < 24 {
			return nil, boxErr(-1, "init: trex truncated")
		}
		if be32(p[4:]) != trackID {
			continue
		}
		return &Trex{
			DefaultSampleDuration: be32(p[12:]),
			DefaultSampleSize:     be32(p[16:]),
			DefaultSampleFlags:    be32(p[20:]),
		}, nil
	}
	return nil, &Error{Code: CodeNoTrex, Segment: -1,
		Msg: fmt.Sprintf("init: mvex has no trex for track %d", trackID)}
}
