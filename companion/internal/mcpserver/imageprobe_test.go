package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readProbeCode reads the digits back out of a probe image by sampling the
// centre of every glyph cell, the way a reader who knows the layout would.
// It is what proves the picture shows the code rather than a blank.
func readProbeCode(t *testing.T, img image.Image) string {
	t.Helper()
	scale, x0, y0, _ := probeLayout(img.Bounds().Dx())
	var code strings.Builder
	for i := 0; i < probeCodeDigits; i++ {
		left := x0 + i*(probeGlyphCols+probeGlyphGap)*scale
		var seen [probeGlyphRows]string
		for row := range seen {
			var line strings.Builder
			for col := 0; col < probeGlyphCols; col++ {
				r, _, _, _ := img.At(left+col*scale+scale/2, y0+row*scale+scale/2).RGBA()
				if r>>8 < probeDarkThreshold {
					line.WriteByte('#')
				} else {
					line.WriteByte(' ')
				}
			}
			seen[row] = line.String()
		}
		digit := -1
		for d, g := range probeGlyphs {
			if g == seen {
				digit = d
			}
		}
		if digit < 0 {
			t.Fatalf("digit %d is no glyph: %q", i, seen)
		}
		code.WriteByte(byte('0' + digit))
	}
	return code.String()
}

func TestProbeGlyphsAreDistinct(t *testing.T) {
	for a := range probeGlyphs {
		for b := a + 1; b < len(probeGlyphs); b++ {
			if probeGlyphs[a] == probeGlyphs[b] {
				t.Fatalf("glyphs %d and %d are the same", a, b)
			}
		}
		for _, line := range probeGlyphs[a] {
			if len(line) != probeGlyphCols {
				t.Fatalf("glyph %d has a line of %d columns", a, len(line))
			}
		}
	}
}

// TestImageProbeShowsTheCodeOnlyInThePicture pins the property the whole
// measurement rests on: the code is drawn, readable, and absent from every
// text the platform could hand the model instead of the image.
func TestImageProbeShowsTheCodeOnlyInThePicture(t *testing.T) {
	for _, tc := range []struct {
		width int
		noise bool
	}{{0, false}, {minProbeWidth, false}, {1280, true}} {
		ts := &toolset{}
		res, out, err := ts.imageProbe(context.Background(), nil, imageProbeInput{Width: tc.width, Noise: tc.noise})
		if err != nil {
			t.Fatal(err)
		}
		code := ts.probeCodes.code
		if len(code) != probeCodeDigits {
			t.Fatalf("issued code %q", code)
		}
		var img *mcp.ImageContent
		for _, c := range res.Content {
			if ic, ok := c.(*mcp.ImageContent); ok {
				img = ic
			}
		}
		if img == nil || img.MIMEType != "image/png" || out.ImageBytes != len(img.Data) {
			t.Fatalf("image block: %+v, out %+v", img, out)
		}
		decoded, err := png.Decode(bytes.NewReader(img.Data))
		if err != nil {
			t.Fatal(err)
		}
		wantWidth := tc.width
		if wantWidth == 0 {
			wantWidth = defaultProbeWidth
		}
		if b := decoded.Bounds(); b.Dx() != wantWidth || b.Dy() != wantWidth/2 || out.Width != wantWidth || out.Height != wantWidth/2 {
			t.Fatalf("size %v, out %+v", b, out)
		}
		if got := readProbeCode(t, decoded); got != code {
			t.Fatalf("image reads %q, code is %q", got, code)
		}

		res.Content = res.Content[:1]
		text, _ := json.Marshal(res.Content)
		structuredOut, _ := json.Marshal(out)
		if strings.Contains(string(text), code) || strings.Contains(string(structuredOut), code) {
			t.Fatalf("code %s leaks as text: %s %s", code, text, structuredOut)
		}
	}
}

func TestImageProbeNoiseDefeatsCompression(t *testing.T) {
	ts := &toolset{}
	_, plain, err := ts.imageProbe(context.Background(), nil, imageProbeInput{Width: 1024})
	if err != nil {
		t.Fatal(err)
	}
	_, noisy, err := ts.imageProbe(context.Background(), nil, imageProbeInput{Width: 1024, Noise: true})
	if err != nil {
		t.Fatal(err)
	}
	if noisy.ImageBytes < 10*plain.ImageBytes {
		t.Fatalf("noise %d bytes, plain %d: the background compresses away", noisy.ImageBytes, plain.ImageBytes)
	}
}

func TestImageProbeJPEGAndResourceShapes(t *testing.T) {
	ts := &toolset{}
	res, out, err := ts.imageProbe(context.Background(), nil, imageProbeInput{Format: "jpeg", As: "resource", Width: 800})
	if err != nil {
		t.Fatal(err)
	}
	er, ok := res.Content[1].(*mcp.EmbeddedResource)
	if !ok || er.Resource.MIMEType != "image/jpeg" || er.Resource.URI != "fylane://probe/image.jpeg" || out.As != "resource" {
		t.Fatalf("resource block %+v, out %+v", res.Content[1], out)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(er.Resource.Blob))
	if err != nil {
		t.Fatal(err)
	}
	if b := decoded.Bounds(); b.Dx() != 800 || b.Dy() != 400 {
		t.Fatalf("jpeg size %v", b)
	}
}

// TestImageProbeCheckSpendsTheCode: a mismatch reveals the code only
// because it is gone; a second answer finds nothing to match.
func TestImageProbeCheckSpendsTheCode(t *testing.T) {
	ts := &toolset{}
	ctx := context.Background()
	if _, out, _ := ts.imageProbe(ctx, nil, imageProbeInput{Answer: "123456"}); out.Result != "no_image" {
		t.Fatalf("answer before any image: %+v", out)
	}

	if _, _, err := ts.imageProbe(ctx, nil, imageProbeInput{}); err != nil {
		t.Fatal(err)
	}
	code := ts.probeCodes.code
	spaced := code[:3] + " " + code[3:]
	if _, out, _ := ts.imageProbe(ctx, nil, imageProbeInput{Answer: spaced}); out.Result != "match" || out.Expected != code {
		t.Fatalf("right answer %q: %+v", spaced, out)
	}
	if _, out, _ := ts.imageProbe(ctx, nil, imageProbeInput{Answer: code}); out.Result != "no_image" {
		t.Fatalf("second answer: %+v", out)
	}

	if _, _, err := ts.imageProbe(ctx, nil, imageProbeInput{}); err != nil {
		t.Fatal(err)
	}
	code = ts.probeCodes.code
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if _, out, _ := ts.imageProbe(ctx, nil, imageProbeInput{Answer: wrong}); out.Result != "mismatch" || out.Expected != code {
		t.Fatalf("wrong answer: %+v", out)
	}
	if _, out, _ := ts.imageProbe(ctx, nil, imageProbeInput{Answer: code}); out.Result != "no_image" {
		t.Fatalf("retry after mismatch: %+v", out)
	}
}

func TestImageProbeOverTheWire(t *testing.T) {
	session := startProbeSession(t)
	res := callTool(t, session, "image_probe", map[string]any{"width": 320})
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("image_probe result: %+v", res)
	}
	img, ok := res.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("second block is %T", res.Content[1])
	}
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatal(err)
	}
	code := readProbeCode(t, decoded)
	var out imageProbeOutput
	structured(t, callTool(t, session, "image_probe", map[string]any{"answer": code}), &out)
	if out.Result != "match" {
		t.Fatalf("answer read from the wire image: %+v", out)
	}

	for _, args := range []map[string]any{
		{"format": "gif"}, {"as": "file"}, {"width": minProbeWidth - 1}, {"width": maxProbeWidth + 1},
	} {
		if res := callTool(t, session, "image_probe", args); !res.IsError {
			t.Errorf("image_probe(%v): expected error result", args)
		}
	}
}
