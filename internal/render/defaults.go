package render

import (
	"image"
	"image/color"
	"os"
	"path/filepath"

	"github.com/chai2010/webp"
	"github.com/fogleman/gg"
)

// EnsureBuiltInBackgrounds creates blush-gradient.webp and candle-glow.webp programmatically if missing.
func EnsureBuiltInBackgrounds(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	blushPath := filepath.Join(dir, "blush-gradient.webp")
	if _, err := os.Stat(blushPath); os.IsNotExist(err) {
		if err := generateBlushGradient(blushPath); err != nil {
			return err
		}
	}

	candlePath := filepath.Join(dir, "candle-glow.webp")
	if _, err := os.Stat(candlePath); os.IsNotExist(err) {
		if err := generateCandleGlow(candlePath); err != nil {
			return err
		}
	}

	return nil
}

func saveWebP(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return webp.Encode(f, img, &webp.Options{Quality: 85})
}

func generateBlushGradient(path string) error {
	dc := gg.NewContext(1170, 760)
	base := color.RGBA{R: 217, G: 140, B: 159, A: 255} // Rose pink base
	drawWarmBackground(dc, 1170, 760, base)
	return saveWebP(dc.Image(), path)
}

func generateCandleGlow(path string) error {
	dc := gg.NewContext(1170, 760)
	grad := gg.NewLinearGradient(0, 0, 1170, 760)
	grad.AddColorStop(0, color.RGBA{R: 44, G: 15, B: 24, A: 255})    // Deep dark plum
	grad.AddColorStop(0.5, color.RGBA{R: 120, G: 35, B: 45, A: 255}) // Warm burgundy
	grad.AddColorStop(1, color.RGBA{R: 212, G: 110, B: 50, A: 255})  // Candle yellow/orange
	dc.SetFillStyle(grad)
	dc.DrawRectangle(0, 0, 1170, 760)
	dc.Fill()

	// Soft golden candle glow highlight at the bottom
	dc.SetRGBA255(253, 184, 39, 45)
	dc.DrawCircle(585, 760, 360)
	dc.Fill()

	return saveWebP(dc.Image(), path)
}
