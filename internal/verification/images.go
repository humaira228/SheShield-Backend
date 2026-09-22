package verification

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoders image.DecodeConfig uses
	_ "image/png"
)

const (
	// MaxImageBytes is the largest single photo we accept.
	MaxImageBytes = 5 << 20 // 5 MB

	minDimension = 200   // px; rejects icons, blanks and thumbnails
	maxDimension = 12000 // px; rejects decompression-bomb style images
)

// checkImage verifies data really is a JPEG or PNG of a sensible size, by
// decoding its header -- not by trusting a filename or a Content-Type sent by
// the client. It returns the file extension to store it under, or a
// user-facing reason why it was refused.
func checkImage(data []byte, label string) (ext, problem string) {
	if len(data) == 0 {
		return "", fmt.Sprintf("The %s photo is missing.", label)
	}
	if len(data) > MaxImageBytes {
		return "", fmt.Sprintf("The %s photo is too large (5 MB max).", label)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Sprintf("The %s photo must be a JPEG or PNG image.", label)
	}
	if cfg.Width < minDimension || cfg.Height < minDimension {
		return "", fmt.Sprintf("The %s photo is too small. Please retake it closer to the camera.", label)
	}
	if cfg.Width > maxDimension || cfg.Height > maxDimension {
		return "", fmt.Sprintf("The %s photo is too large in size. Please use a normal camera photo.", label)
	}

	switch format {
	case "jpeg":
		return ".jpg", ""
	case "png":
		return ".png", ""
	}
	return "", fmt.Sprintf("The %s photo must be a JPEG or PNG image.", label)
}
