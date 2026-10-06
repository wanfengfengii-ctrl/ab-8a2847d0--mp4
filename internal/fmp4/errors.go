// Package fmp4 parses and audits fragmented MP4 (ISO BMFF) audio streams:
// it recovers the decode timeline of every media segment from tfhd/tfdt/trun
// and rejects tracks with gaps, overlaps, broken parameter inheritance or
// payload mismatches.
package fmp4

import "fmt"

// Stable machine-readable error codes returned by Audit and the HTTP API.
const (
	CodeBoxStructure  = "BOX_STRUCTURE"           // malformed or unexpected box layout
	CodeNotAudio      = "NOT_SINGLE_AUDIO_TRACK"  // not exactly one audio track
	CodeNoMvex        = "NO_MVEX"                 // init segment lacks mvex (not fragmented)
	CodeNoTrex        = "NO_TREX"                 // mvex has no trex for the track
	CodeTrackMismatch = "TRACK_MISMATCH"          // tfhd track_ID differs from init track
	CodeSequence      = "SEQUENCE_NOT_INCREASING" // mfhd sequence numbers not increasing
	CodeNoTfdt        = "NO_TFDT"                 // traf lacks tfdt baseMediaDecodeTime
	CodeParamInherit  = "PARAM_INHERIT"           // sample duration/size not resolvable
	CodePayload       = "PAYLOAD_MISMATCH"        // sample sizes do not cover mdat exactly
	CodeGap           = "TIMELINE_GAP"            // decode timeline hole between segments
	CodeOverlap       = "TIMELINE_OVERLAP"        // decode timeline overlap between segments
)

// MaxTotalBytes is the combined init+media payload limit (16 MiB).
const MaxTotalBytes = 16 << 20

// MaxMediaSegments is the maximum number of media segments per audit request.
const MaxMediaSegments = 32

// Error is a stable, machine-readable audit failure. Segment is the media
// segment index (0-based, in submission order); it is -1 when the init
// segment or the request as a whole is at fault.
type Error struct {
	Code    string
	Segment int
	Msg     string
}

func (e *Error) Error() string {
	where := "init"
	if e.Segment >= 0 {
		where = fmt.Sprintf("segment %d", e.Segment)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, where, e.Msg)
}

func boxErr(seg int, format string, args ...any) *Error {
	return &Error{Code: CodeBoxStructure, Segment: seg, Msg: fmt.Sprintf(format, args...)}
}
