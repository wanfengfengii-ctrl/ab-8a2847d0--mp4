// Package server exposes the fMP4 audit HTTP API.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"fmp4audit/internal/fmp4"
)

// Request-level error codes (audit-level codes live in package fmp4).
const (
	codeBadMultipart = "BAD_MULTIPART"
	codeBadPartCount = "BAD_PART_COUNT"
	codeEmptyPart    = "EMPTY_PART"
	codeTooLarge     = "TOO_LARGE"
)

// multipartOverhead allows room for boundaries and headers on top of the
// 16 MiB payload limit enforced on the combined part contents.
const multipartOverhead = 1 << 20

// New returns the API handler with the audit and health routes.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/fmp4/audit", auditHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

type segBody struct {
	Index    int    `json:"index"`
	Sequence uint32 `json:"sequence_number"`
	Start    uint64 `json:"start"`
	End      uint64 `json:"end"`
	Samples  uint32 `json:"samples"`
	Duration uint64 `json:"duration"`
}

type okBody struct {
	OK            bool      `json:"ok"`
	Timescale     uint32    `json:"timescale"`
	TrackID       uint32    `json:"track_id"`
	SegmentCount  int       `json:"segment_count"`
	TotalDuration uint64    `json:"total_duration"`
	Segments      []segBody `json:"segments"`
}

type errBody struct {
	OK    bool `json:"ok"`
	Error struct {
		Code    string `json:"code"`
		Segment int    `json:"segment_index"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code string, seg int, msg string) {
	var b errBody
	b.Error.Code = code
	b.Error.Segment = seg
	b.Error.Message = msg
	writeJSON(w, status, &b)
}

// auditHandler accepts one init segment followed by 1..32 media segments as
// ordered multipart parts, audits the decode timeline and reports per-segment
// results or the first validation failure.
func auditHandler(w http.ResponseWriter, r *http.Request) {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		fail(w, http.StatusBadRequest, codeBadMultipart, -1,
			"Content-Type must be multipart/form-data with a boundary")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, fmp4.MaxTotalBytes+multipartOverhead)
	mr := multipart.NewReader(r.Body, params["boundary"])

	var initData []byte
	var media [][]byte
	total := 0
	partIdx := -1
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if strings.Contains(err.Error(), "request body too large") {
				fail(w, http.StatusRequestEntityTooLarge, codeTooLarge, -1,
					fmt.Sprintf("request exceeds the %d MiB limit", fmp4.MaxTotalBytes>>20))
			} else {
				fail(w, http.StatusBadRequest, codeBadMultipart, -1,
					"malformed multipart body: "+err.Error())
			}
			return
		}
		partIdx++
		segIdx := partIdx - 1 // media segment index; -1 for the init part
		data, err := io.ReadAll(io.LimitReader(part, int64(fmp4.MaxTotalBytes-total)+1))
		part.Close()
		if err != nil {
			fail(w, http.StatusBadRequest, codeBadMultipart, segIdx,
				"failed reading multipart part: "+err.Error())
			return
		}
		total += len(data)
		if total > fmp4.MaxTotalBytes {
			fail(w, http.StatusRequestEntityTooLarge, codeTooLarge, segIdx,
				fmt.Sprintf("combined payload exceeds %d bytes", fmp4.MaxTotalBytes))
			return
		}
		if len(data) == 0 {
			fail(w, http.StatusBadRequest, codeEmptyPart, segIdx,
				"multipart parts must not be empty")
			return
		}
		if partIdx == 0 {
			initData = data
			continue
		}
		if len(media) >= fmp4.MaxMediaSegments {
			fail(w, http.StatusBadRequest, codeBadPartCount, -1,
				fmt.Sprintf("at most %d media segments per request", fmp4.MaxMediaSegments))
			return
		}
		media = append(media, data)
	}
	if initData == nil || len(media) == 0 {
		fail(w, http.StatusBadRequest, codeBadPartCount, -1,
			fmt.Sprintf("expected one init segment plus 1..%d media segments, got %d parts",
				fmp4.MaxMediaSegments, partIdx+1))
		return
	}

	res, aerr := fmp4.Audit(initData, media)
	if aerr != nil {
		fail(w, http.StatusUnprocessableEntity, aerr.Code, aerr.Segment, aerr.Msg)
		return
	}
	out := okBody{
		OK:            true,
		Timescale:     res.Timescale,
		TrackID:       res.TrackID,
		SegmentCount:  len(res.Segments),
		TotalDuration: res.TotalDuration,
	}
	for i, s := range res.Segments {
		out.Segments = append(out.Segments, segBody{
			Index: i, Sequence: s.Sequence, Start: s.Start,
			End: s.End, Samples: s.Samples, Duration: s.Duration,
		})
	}
	writeJSON(w, http.StatusOK, &out)
}
