package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strconv"
	"strings"

	_ "github.com/chai2010/webp"
	"github.com/fogleman/gg"
)

const defaultFontPath = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"

type CardRendererOptions struct {
	Width    int
	Height   int
	FontPath string
}

type CardRenderer struct {
	width    int
	height   int
	fontPath string
}

type CardInput struct {
	Brand           string
	Level           int
	Question        string
	BaseColor       string
	FontPath        string
	BackgroundBytes []byte
	BorderRadius    float64
	GlassOpacity    float64
}

type RenderedCard struct {
	PNG    []byte
	Width  int
	Height int
}

func NewCardRenderer(options CardRendererOptions) *CardRenderer {
	width := options.Width
	if width <= 0 {
		width = 1170
	}
	height := options.Height
	if height <= 0 {
		height = 760
	}
	fontPath := options.FontPath
	if fontPath == "" {
		fontPath = defaultFontPath
	}
	return &CardRenderer{width: width, height: height, fontPath: fontPath}
}

func (r *CardRenderer) Render(input CardInput) (RenderedCard, error) {
	if strings.TrimSpace(input.Question) == "" {
		return RenderedCard{}, fmt.Errorf("question text is required")
	}
	brand := strings.TrimSpace(input.Brand)
	if brand == "" {
		brand = "WRNRS"
	}
	base, err := parseHexColor(input.BaseColor)
	if err != nil {
		base = color.RGBA{R: 217, G: 140, B: 159, A: 255}
	}
	fontPath := r.fontPath
	if strings.TrimSpace(input.FontPath) != "" {
		fontPath = input.FontPath
	}

	dc := gg.NewContext(r.width, r.height)
	if len(input.BackgroundBytes) > 0 {
		bgImg, _, err := image.Decode(bytes.NewReader(input.BackgroundBytes))
		if err == nil {
			dc.DrawImage(bgImg, 0, 0)
		} else {
			drawWarmBackground(dc, r.width, r.height, base)
		}
	} else {
		drawWarmBackground(dc, r.width, r.height, base)
	}

	margin := float64(r.width) * 0.08
	cardX := margin
	cardY := float64(r.height) * 0.14
	cardW := float64(r.width) - 2*margin
	cardH := float64(r.height) * 0.66

	borderRadius := input.BorderRadius
	if borderRadius <= 0 {
		borderRadius = 30
	}
	glassOpacity := input.GlassOpacity
	if glassOpacity <= 0 {
		glassOpacity = 0.62
	}

	dc.SetRGBA(1, 1, 1, glassOpacity)
	dc.DrawRoundedRectangle(cardX, cardY, cardW, cardH, borderRadius)
	dc.Fill()
	dc.SetColor(darken(base, 0.42))
	dc.SetLineWidth(4)
	dc.DrawRoundedRectangle(cardX, cardY, cardW, cardH, borderRadius)
	dc.Stroke()

	if err := dc.LoadFontFace(fontPath, 56); err != nil {
		return RenderedCard{}, fmt.Errorf("load brand font: %w", err)
	}
	dc.SetColor(darken(base, 0.62))
	dc.DrawStringAnchored(strings.ToUpper(brand), float64(r.width)/2, 72, 0.5, 0.5)

	if err := dc.LoadFontFace(fontPath, 50); err != nil {
		return RenderedCard{}, fmt.Errorf("load question font: %w", err)
	}
	dc.SetColor(darken(base, 0.72))
	dc.DrawStringWrapped(input.Question, cardX+80, cardY+cardH/2, 0, 0.5, cardW-160, 1.28, gg.AlignLeft)

	if err := dc.LoadFontFace(fontPath, 42); err != nil {
		return RenderedCard{}, fmt.Errorf("load level font: %w", err)
	}
	level := fmt.Sprintf("РІВЕНЬ %d", input.Level)
	if isASCII(brand) {
		level = fmt.Sprintf("LEVEL %d", input.Level)
	}
	dc.SetColor(darken(base, 0.66))
	dc.DrawStringAnchored(level, float64(r.width)/2, float64(r.height)-52, 0.5, 0.5)

	var out bytes.Buffer
	if err := dc.EncodePNG(&out); err != nil {
		return RenderedCard{}, fmt.Errorf("encode card png: %w", err)
	}
	return RenderedCard{PNG: out.Bytes(), Width: r.width, Height: r.height}, nil
}

func drawWarmBackground(dc *gg.Context, width, height int, base color.RGBA) {
	light := lighten(base, 0.72)
	for y := 0; y < height; y++ {
		t := float64(y) / math.Max(1, float64(height-1))
		c := mix(light, color.RGBA{R: 255, G: 247, B: 241, A: 255}, t)
		dc.SetColor(c)
		dc.DrawRectangle(0, float64(y), float64(width), 1)
		dc.Fill()
	}
	dc.SetRGBA255(int(base.R), int(base.G), int(base.B), 56)
	dc.DrawCircle(float64(width)*0.16, float64(height)*0.15, float64(width)*0.24)
	dc.Fill()
	dc.SetRGBA255(255, 255, 255, 72)
	dc.DrawCircle(float64(width)*0.83, float64(height)*0.82, float64(width)*0.30)
	dc.Fill()
}

func parseHexColor(value string) (color.RGBA, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return color.RGBA{}, fmt.Errorf("hex color must have 6 characters")
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{R: uint8(n >> 16), G: uint8((n >> 8) & 0xff), B: uint8(n & 0xff), A: 255}, nil
}

func lighten(c color.RGBA, amount float64) color.RGBA {
	return mix(c, color.RGBA{R: 255, G: 255, B: 255, A: 255}, amount)
}

func darken(c color.RGBA, amount float64) color.RGBA {
	return mix(c, color.RGBA{R: 20, G: 28, B: 44, A: 255}, amount)
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: uint8(float64(a.A)*(1-t) + float64(b.A)*t),
	}
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}
