package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/chai2010/webp"
	xdraw "golang.org/x/image/draw"
)

const DefaultMaxUploadBytes = 10 << 20

type UploadOptions struct {
	UserID       int64
	MaxBytes     int64
	TargetWidth  int
	TargetHeight int
	Quality      float32
}

type ProcessedBackground struct {
	ObjectKey string
	MIMEType  string
	Bytes     []byte
	Width     int
	Height    int
	SizeBytes int
}

func ProcessUploadedBackground(r io.Reader, options UploadOptions) (ProcessedBackground, error) {
	if options.UserID == 0 {
		return ProcessedBackground{}, errors.New("user id is required")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = DefaultMaxUploadBytes
	}
	if options.TargetWidth <= 0 {
		options.TargetWidth = 1170
	}
	if options.TargetHeight <= 0 {
		options.TargetHeight = 760
	}
	if options.Quality <= 0 {
		options.Quality = 82
	}

	raw, err := io.ReadAll(io.LimitReader(r, options.MaxBytes+1))
	if err != nil {
		return ProcessedBackground{}, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(raw)) > options.MaxBytes {
		return ProcessedBackground{}, fmt.Errorf("upload exceeds %d bytes", options.MaxBytes)
	}

	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return ProcessedBackground{}, fmt.Errorf("decode upload image: %w", err)
	}
	processed := resizeCenterCrop(source, options.TargetWidth, options.TargetHeight)

	var out bytes.Buffer
	if err := webp.Encode(&out, processed, &webp.Options{Quality: options.Quality}); err != nil {
		return ProcessedBackground{}, fmt.Errorf("encode webp: %w", err)
	}
	sum := sha256.Sum256(out.Bytes())
	objectKey := fmt.Sprintf("user-backgrounds/%d/%s.webp", options.UserID, hex.EncodeToString(sum[:16]))
	return ProcessedBackground{
		ObjectKey: objectKey,
		MIMEType:  "image/webp",
		Bytes:     out.Bytes(),
		Width:     options.TargetWidth,
		Height:    options.TargetHeight,
		SizeBytes: out.Len(),
	}, nil
}

func resizeCenterCrop(source image.Image, targetWidth, targetHeight int) *image.RGBA {
	bounds := source.Bounds()
	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	targetRatio := float64(targetWidth) / float64(targetHeight)
	sourceRatio := float64(sourceWidth) / float64(sourceHeight)

	crop := bounds
	if sourceRatio > targetRatio {
		newWidth := int(float64(sourceHeight) * targetRatio)
		x0 := bounds.Min.X + (sourceWidth-newWidth)/2
		crop = image.Rect(x0, bounds.Min.Y, x0+newWidth, bounds.Max.Y)
	} else if sourceRatio < targetRatio {
		newHeight := int(float64(sourceWidth) / targetRatio)
		y0 := bounds.Min.Y + (sourceHeight-newHeight)/2
		crop = image.Rect(bounds.Min.X, y0, bounds.Max.X, y0+newHeight)
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), source, crop, xdraw.Over, nil)
	return dst
}
