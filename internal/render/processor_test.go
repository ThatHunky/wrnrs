package render_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"wrnrs/internal/render"
)

func TestProcessUploadedBackgroundEncodesWebPDerivative(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < src.Bounds().Dy(); y++ {
		for x := 0; x < src.Bounds().Dx(); x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 180, A: 255})
		}
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}

	processed, err := render.ProcessUploadedBackground(bytes.NewReader(input.Bytes()), render.UploadOptions{
		UserID:       1001,
		MaxBytes:     10 << 20,
		TargetWidth:  1170,
		TargetHeight: 760,
		Quality:      82,
	})
	if err != nil {
		t.Fatalf("ProcessUploadedBackground returned error: %v", err)
	}
	if processed.MIMEType != "image/webp" {
		t.Fatalf("MIMEType = %q, want image/webp", processed.MIMEType)
	}
	if !strings.HasSuffix(processed.ObjectKey, ".webp") {
		t.Fatalf("ObjectKey = %q, want .webp suffix", processed.ObjectKey)
	}
	if !bytes.HasPrefix(processed.Bytes, []byte("RIFF")) || string(processed.Bytes[8:12]) != "WEBP" {
		t.Fatalf("processed bytes do not have RIFF WEBP header")
	}
	if processed.Width != 1170 || processed.Height != 760 {
		t.Fatalf("dimensions = %dx%d, want 1170x760", processed.Width, processed.Height)
	}
}

func TestCardRendererOutputsPNGImage(t *testing.T) {
	renderer := render.NewCardRenderer(render.CardRendererOptions{
		Width:    1170,
		Height:   760,
		FontPath: "../../assets/fonts/Nunito/static/Nunito-Bold.ttf",
	})

	card, err := renderer.Render(render.CardInput{
		Brand:     "між нами.",
		Level:     2,
		Question:  "Як ти відчуваєш, що тебе люблять?",
		BaseColor: "#d98c9f",
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !bytes.HasPrefix(card.PNG, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("rendered card does not have PNG header")
	}
	if card.Width != 1170 || card.Height != 760 {
		t.Fatalf("card dimensions = %dx%d, want 1170x760", card.Width, card.Height)
	}
}

func TestCardRendererAcceptsPerCardFontOverride(t *testing.T) {
	renderer := render.NewCardRenderer(render.CardRendererOptions{
		Width:    1170,
		Height:   760,
		FontPath: "../../assets/fonts/Nunito/static/Nunito-Bold.ttf",
	})

	card, err := renderer.Render(render.CardInput{
		Brand:     "WRNRS",
		Level:     1,
		Question:  "What makes you feel close to me?",
		BaseColor: "#8f3f5f",
		FontPath:  "../../assets/fonts/Google_Sans/static/GoogleSans-Regular.ttf",
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !bytes.HasPrefix(card.PNG, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("rendered card does not have PNG header")
	}
}
