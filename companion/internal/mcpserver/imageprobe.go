package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/big"
	mrand "math/rand/v2"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// image_probe settles the question page_snapshot (D38) stands on: whether a
// platform hands an image returned by a tool to the model, or only shows the
// user that one arrived. The image carries a code that never appears as
// text, so a model can only pass the check by having seen the picture.

const (
	probeCodeDigits     = 6
	defaultProbeWidth   = 640
	minProbeWidth       = 160
	maxProbeWidth       = 2400
	probeJPEGQuality    = 80
	probeGlyphCols      = 5
	probeGlyphRows      = 7
	probeGlyphGap       = 2
	probeMarginCols     = 2
	probeDarkThreshold  = 100
	probeNoiseFloorGray = 170
)

// probeGlyphs is a 5×7 bitmap face for the ten digits. A drawn face rather
// than a font file keeps the probe free of a font dependency, and a coarse
// one survives the downscaling a platform may apply before the model sees it.
var probeGlyphs = [10][probeGlyphRows]string{
	{" ### ", "#   #", "#  ##", "# # #", "##  #", "#   #", " ### "},
	{"  #  ", " ##  ", "  #  ", "  #  ", "  #  ", "  #  ", " ### "},
	{" ### ", "#   #", "    #", "   # ", "  #  ", " #   ", "#####"},
	{"#####", "   # ", "  #  ", "   # ", "    #", "#   #", " ### "},
	{"   # ", "  ## ", " # # ", "#  # ", "#####", "   # ", "   # "},
	{"#####", "#    ", "#### ", "    #", "    #", "#   #", " ### "},
	{"  ## ", " #   ", "#    ", "#### ", "#   #", "#   #", " ### "},
	{"#####", "    #", "   # ", "  #  ", " #   ", " #   ", " #   "},
	{" ### ", "#   #", "#   #", " ### ", "#   #", "#   #", " ### "},
	{" ### ", "#   #", "#   #", " ####", "    #", "   # ", " ##  "},
}

type imageProbeInput struct {
	Answer string `json:"answer,omitempty" jsonschema:"Leave empty to receive an image. To check the image you received last, pass the digits you read in it."`
	Format string `json:"format,omitempty" jsonschema:"png (default) or jpeg."`
	Width  int    `json:"width,omitempty" jsonschema:"Image width in pixels, 160-2400 (default 640). The height is half the width."`
	Noise  bool   `json:"noise,omitempty" jsonschema:"Fill the background with random noise so the image does not compress, to measure the largest image the platform accepts."`
	As     string `json:"as,omitempty" jsonschema:"image (default) returns an image content block; resource returns the same bytes as an embedded resource."`
}

type imageProbeOutput struct {
	Format     string `json:"format,omitempty"`
	As         string `json:"as,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	ImageBytes int    `json:"image_bytes,omitempty"`
	// Result is "match", "mismatch" or "no_image" for a check.
	Result string `json:"result,omitempty"`
	// Expected is revealed only once a check has used the code up.
	Expected string `json:"expected,omitempty"`
	Note     string `json:"note"`
}

// probeCodes holds the code of the last image issued to one platform. A
// check spends it, so a model cannot learn the answer from a mismatch and
// then pass on a second try.
type probeCodes struct {
	mu   sync.Mutex
	code string
}

func (p *probeCodes) issue(code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code = code
}

func (p *probeCodes) check(answer string) imageProbeOutput {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" {
		return imageProbeOutput{Result: "no_image", Note: "No image is waiting for an answer; call image_probe without answer first."}
	}
	expected := p.code
	p.code = ""
	var got strings.Builder
	for _, r := range answer {
		if r >= '0' && r <= '9' {
			got.WriteRune(r)
		}
	}
	if got.String() == expected {
		return imageProbeOutput{Result: "match", Expected: expected, Note: "The digits match: the image reached the model."}
	}
	return imageProbeOutput{Result: "mismatch", Expected: expected, Note: "The digits do not match: the model did not read the image."}
}

func (t *toolset) imageProbe(ctx context.Context, _ *mcp.CallToolRequest, in imageProbeInput) (*mcp.CallToolResult, imageProbeOutput, error) {
	var zero imageProbeOutput
	if ctx.Err() != nil {
		return nil, zero, ctx.Err()
	}
	if in.Answer != "" {
		return nil, t.probeCodes.check(in.Answer), nil
	}

	format := in.Format
	if format == "" {
		format = "png"
	}
	if format != "png" && format != "jpeg" {
		return nil, zero, errors.New("format must be png or jpeg")
	}
	as := in.As
	if as == "" {
		as = "image"
	}
	if as != "image" && as != "resource" {
		return nil, zero, errors.New("as must be image or resource")
	}
	width := in.Width
	if width == 0 {
		width = defaultProbeWidth
	}
	if width < minProbeWidth || width > maxProbeWidth {
		return nil, zero, fmt.Errorf("width must be between %d and %d", minProbeWidth, maxProbeWidth)
	}

	code, err := newProbeCode()
	if err != nil {
		return nil, zero, err
	}
	img := probeImage(code, width, in.Noise)
	var buf bytes.Buffer
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: probeJPEGQuality})
	}
	if err != nil {
		return nil, zero, fmt.Errorf("encode probe image: %w", err)
	}
	t.probeCodes.issue(code)

	mime := "image/" + format
	var block mcp.Content = &mcp.ImageContent{Data: buf.Bytes(), MIMEType: mime}
	if as == "resource" {
		block = &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
			URI: "fylane://probe/image." + format, MIMEType: mime, Blob: buf.Bytes(),
		}}
	}
	out := imageProbeOutput{
		Format:     format,
		As:         as,
		Width:      width,
		Height:     img.Bounds().Dy(),
		ImageBytes: buf.Len(),
		Note:       "The image shows six digits. Read them and call image_probe again with answer set to those digits.",
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.Note}, block}}, out, nil
}

func newProbeCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("probe code: %w", err)
	}
	return fmt.Sprintf("%0*d", probeCodeDigits, n.Int64()), nil
}

// probeLayout places the digits: the scale of one glyph cell in pixels and
// the top-left corner of the first digit, centred in a width × width/2 image.
func probeLayout(width int) (scale, x0, y0, height int) {
	textCols := probeCodeDigits*probeGlyphCols + (probeCodeDigits-1)*probeGlyphGap
	scale = width / (textCols + 2*probeMarginCols)
	height = width / 2
	x0 = (width - textCols*scale) / 2
	y0 = (height - probeGlyphRows*scale) / 2
	return scale, x0, y0, height
}

// probeImage draws code in black on white, or on light noise when noise is
// set. The noise stays above probeNoiseFloorGray so the digits keep their
// contrast however incompressible the background is.
func probeImage(code string, width int, noise bool) *image.Gray {
	scale, x0, y0, height := probeLayout(width)
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = 0xff
		if noise {
			img.Pix[i] = uint8(probeNoiseFloorGray + mrand.IntN(256-probeNoiseFloorGray))
		}
	}
	for i, r := range code {
		glyph := probeGlyphs[r-'0']
		left := x0 + i*(probeGlyphCols+probeGlyphGap)*scale
		for row, line := range glyph {
			for col, ch := range line {
				if ch != '#' {
					continue
				}
				for y := y0 + row*scale; y < y0+(row+1)*scale; y++ {
					for x := left + col*scale; x < left+(col+1)*scale; x++ {
						img.SetGray(x, y, color.Gray{Y: 0})
					}
				}
			}
		}
	}
	return img
}
