package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fmp4audit/internal/fmp4"
	"fmp4audit/internal/gen"
	"fmp4audit/internal/server"
)

func postMultipart(t *testing.T, h http.Handler, parts [][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for i, data := range parts {
		name := "init"
		if i > 0 {
			name = fmt.Sprintf("media%d", i)
		}
		pw, err := w.CreateFormFile(name, fmt.Sprintf("part%d.bin", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/fmp4/audit", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func fixture() ([]byte, [][]byte) {
	init := gen.Init(gen.InitOpts{Timescale: 48000})
	media := [][]byte{
		gen.Media(gen.MediaOpts{Sequence: 1, BaseTime: 0, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 100}, {Dur: 1024, Size: 110}}}),
		gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 2048, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 105}}}),
	}
	return init, media
}

func TestEndpointContinuous(t *testing.T) {
	h := server.New()
	init, media := fixture()
	rec := postMultipart(t, h, append([][]byte{init}, media...))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		OK            bool   `json:"ok"`
		Timescale     uint32 `json:"timescale"`
		SegmentCount  int    `json:"segment_count"`
		TotalDuration uint64 `json:"total_duration"`
		Segments      []struct {
			Sequence uint32 `json:"sequence_number"`
			Start    uint64 `json:"start"`
			End      uint64 `json:"end"`
			Samples  uint32 `json:"samples"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Timescale != 48000 || body.SegmentCount != 2 || body.TotalDuration != 3072 {
		t.Fatalf("bad body: %s", rec.Body)
	}
	if body.Segments[1].Sequence != 2 || body.Segments[1].Start != 2048 ||
		body.Segments[1].End != 3072 || body.Segments[1].Samples != 1 {
		t.Fatalf("bad segment 1: %s", rec.Body)
	}
}

func TestEndpointGap(t *testing.T) {
	h := server.New()
	init, media := fixture()
	media[1] = gen.Media(gen.MediaOpts{Sequence: 2, BaseTime: 3000, TrunFields: true,
		Samples: []gen.Sample{{Dur: 1024, Size: 105}}})
	rec := postMultipart(t, h, append([][]byte{init}, media...))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Error struct {
			Code    string
			Segment int `json:"segment_index"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != fmp4.CodeGap || body.Error.Segment != 1 {
		t.Fatalf("bad error body: %s", rec.Body)
	}
}

func TestEndpointNotMultipart(t *testing.T) {
	h := server.New()
	req := httptest.NewRequest(http.MethodPost, "/api/fmp4/audit", strings.NewReader("x"))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "BAD_MULTIPART") {
		t.Fatalf("body: %s", rec.Body)
	}
}

func TestEndpointPartCount(t *testing.T) {
	h := server.New()
	init, media := fixture()

	// init only, no media segments
	rec := postMultipart(t, h, [][]byte{init})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "BAD_PART_COUNT") {
		t.Fatalf("init-only: %d %s", rec.Code, rec.Body)
	}

	// 33 media segments: one too many
	parts := [][]byte{init}
	for i := 0; i < 33; i++ {
		parts = append(parts, media[0])
	}
	rec = postMultipart(t, h, parts)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "BAD_PART_COUNT") {
		t.Fatalf("33 media: %d %s", rec.Code, rec.Body)
	}

	// 32 media segments would pass the count check but fail the timeline;
	// make them continuous so the request succeeds end to end.
	parts = [][]byte{init}
	for i := 0; i < 32; i++ {
		parts = append(parts, gen.Media(gen.MediaOpts{
			Sequence: uint32(i + 1), BaseTime: uint64(i) * 1024, TrunFields: true,
			Samples: []gen.Sample{{Dur: 1024, Size: 8}},
		}))
	}
	rec = postMultipart(t, h, parts)
	if rec.Code != http.StatusOK {
		t.Fatalf("32 media: %d %s", rec.Code, rec.Body)
	}
}

func TestEndpointTooLarge(t *testing.T) {
	h := server.New()
	init, media := fixture()
	big := append(media[0], make([]byte, fmp4.MaxTotalBytes)...)
	rec := postMultipart(t, h, [][]byte{init, big})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "TOO_LARGE") {
		t.Fatalf("body: %s", rec.Body)
	}
}

func TestEndpointEmptyPart(t *testing.T) {
	h := server.New()
	init, media := fixture()
	rec := postMultipart(t, h, [][]byte{init, {}, media[0]})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "EMPTY_PART") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestHealthz(t *testing.T) {
	h := server.New()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := server.New()
	req := httptest.NewRequest(http.MethodGet, "/api/fmp4/audit", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}
