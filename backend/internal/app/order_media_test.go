package app

import (
	"bytes"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestRepairPhotoOrientationAndMetadata(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 4)), nil); err != nil {
		t.Fatal(err)
	}
	// A minimal EXIF directory with orientation 6 (90 degrees clockwise).
	exif := []byte("Exif\x00\x00II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00")
	jpg := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	jpg = append(jpg, encoded.Bytes()[2:]...)
	output, kind, err := normalizePhoto(jpg)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "image/jpeg" || bytes.Contains(output, []byte("Exif\x00\x00")) {
		t.Fatal("expected JPEG without source EXIF")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(output))
	if err != nil || config.Width != 4 || config.Height != 8 {
		t.Fatalf("expected rotated dimensions 4×8, got %+v (%v)", config, err)
	}
}

func TestMultipartDraftConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields [][2]string
		valid  bool
	}{
		{"manual", nil, true},
		{"draft", [][2]string{{"draft_message_id", "42"}, {"draft_version", "3"}}, true},
		{"missing version", [][2]string{{"draft_message_id", "42"}}, false},
		{"zero version", [][2]string{{"draft_message_id", "42"}, {"draft_version", "0"}}, false},
		{"duplicate reference", [][2]string{{"draft_message_id", "42"}, {"draft_message_id", "43"}, {"draft_version", "3"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			w := multipart.NewWriter(&body)
			for _, field := range tc.fields {
				if err := w.WriteField(field[0], field[1]); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/work-orders", &body)
			req.Header.Set("Content-Type", w.FormDataContentType())
			var ref draftConfirmation
			_, _, err := readOrderMedia(httptest.NewRecorder(), req, &ref)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.name == "draft" && (ref.MessageID != 42 || ref.Version != 3) {
				t.Fatalf("lost confirmation fields: %+v", ref)
			}
		})
	}
}
