package verification

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCheckImage_AcceptsRealPhotos(t *testing.T) {
	if ext, problem := checkImage(pngBytes(t, 300, 300), "ID"); ext != ".png" || problem != "" {
		t.Errorf("png: got (%q, %q)", ext, problem)
	}
	if ext, problem := checkImage(jpegBytes(t, 640, 480), "ID"); ext != ".jpg" || problem != "" {
		t.Errorf("jpeg: got (%q, %q)", ext, problem)
	}
}

func TestCheckImage_RejectsEverythingElse(t *testing.T) {
	cases := map[string][]byte{
		"empty":             nil,
		"plain text":        []byte("this is not an image"),
		"pdf":               []byte("%PDF-1.4 fake"),
		"png magic + junk":  append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0xAB}, 500)...),
		"jpeg magic + junk": append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 500)...),
		"too small":         pngBytes(t, 100, 100),
		"too many bytes":    make([]byte, MaxImageBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			ext, problem := checkImage(data, "selfie")
			if problem == "" || ext != "" {
				t.Fatalf("expected a refusal, got ext=%q problem=%q", ext, problem)
			}
			if !strings.Contains(problem, "selfie") {
				t.Errorf("the message should say which photo is wrong: %q", problem)
			}
		})
	}
}
