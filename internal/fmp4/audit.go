package fmp4

import "fmt"

// Result is a successful audit of one init segment plus its media segments.
type Result struct {
	TrackID       uint32
	Timescale     uint32
	Segments      []Segment
	TotalDuration uint64 // sum of all segment durations, in timescale ticks
}

// Audit validates the init segment and every media segment in submission
// order: track consistency, increasing sequence numbers, exact mdat payload
// coverage and a perfectly contiguous decode timeline.
func Audit(initData []byte, media [][]byte) (*Result, *Error) {
	init, aerr := ParseInit(initData)
	if aerr != nil {
		return nil, aerr
	}
	res := &Result{TrackID: init.TrackID, Timescale: init.Timescale}
	var prevEnd uint64
	var prevSeq uint32
	for i, m := range media {
		seg, aerr := ParseSegment(m, i, init)
		if aerr != nil {
			return nil, aerr
		}
		if i > 0 {
			if seg.Sequence <= prevSeq {
				return nil, &Error{Code: CodeSequence, Segment: i, Msg: fmt.Sprintf(
					"media: sequence number %d does not follow %d", seg.Sequence, prevSeq)}
			}
			if seg.Start != prevEnd {
				code, what, delta := CodeGap, "gap", prevEnd-seg.Start
				if seg.Start < prevEnd {
					code, what, delta = CodeOverlap, "overlap", prevEnd-seg.Start
				} else {
					delta = seg.Start - prevEnd
				}
				return nil, &Error{Code: code, Segment: i, Msg: fmt.Sprintf(
					"media: decode start %d but previous segment ends at %d (%s of %d ticks)",
					seg.Start, prevEnd, what, delta)}
			}
		}
		prevEnd, prevSeq = seg.End, seg.Sequence
		res.Segments = append(res.Segments, *seg)
		res.TotalDuration += seg.Duration
	}
	return res, nil
}
