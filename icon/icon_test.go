package icon

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var limits = Limits{MaxSize: 256 << 10, MinSide: 16, MaxSide: 512}

func pngOf(t *testing.T, w, h int) []byte {
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))))
	return b.Bytes()
}

func jpegOf(t *testing.T, w, h int) []byte {
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil))
	return b.Bytes()
}

// webp1x1 is a lossless WebP of one pixel.
func webp1x1(t *testing.T) []byte {
	b, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	require.NoError(t, err)
	return b
}

func TestCheck_Raster(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		data    []byte
		limits  Limits
		want    *Image
		wantErr error
	}{
		{name: "png", file: "logo.PNG", data: pngOf(t, 64, 32), want: &Image{ContentType: "image/png", Width: 64, Height: 32}},
		{name: "jpeg as .jpg", file: "logo.jpg", data: jpegOf(t, 16, 16), want: &Image{ContentType: "image/jpeg", Width: 16, Height: 16}},
		{name: "jpeg as .jpeg", file: "logo.jpeg", data: jpegOf(t, 512, 512), want: &Image{ContentType: "image/jpeg", Width: 512, Height: 512}},
		{name: "webp", file: "logo.webp", data: webp1x1(t), limits: Limits{MaxSize: 1024, MinSide: 1, MaxSide: 8},
			want: &Image{ContentType: "image/webp", Width: 1, Height: 1}},
		{name: "too small", file: "logo.png", data: pngOf(t, 15, 64), wantErr: ErrDimension},
		{name: "too wide", file: "logo.png", data: pngOf(t, 513, 64), wantErr: ErrDimension},
		{name: "webp too small", file: "logo.webp", data: webp1x1(t), wantErr: ErrDimension},
		{name: "png named jpg", file: "logo.jpg", data: pngOf(t, 32, 32), wantErr: ErrFormat},
		{name: "png named svg", file: "logo.svg", data: pngOf(t, 32, 32), wantErr: ErrFormat},
		{name: "too heavy", file: "logo.png", data: pngOf(t, 32, 32), limits: Limits{MaxSize: 10, MinSide: 1, MaxSide: 64}, wantErr: ErrTooBig},
		{name: "not an image", file: "logo.png", data: []byte("hello"), wantErr: ErrFormat},
		{name: "gif", file: "logo.gif", data: []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), wantErr: ErrFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := tt.limits
			if l == (Limits{}) {
				l = limits
			}
			got, err := Check(tt.file, tt.data, l)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheck_SVG(t *testing.T) {
	tests := []struct {
		name    string
		svg     string
		wantErr error
	}{
		{name: "plain", svg: `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24z" fill="#0a7"/></svg>`},
		{name: "local reference and embedded image", svg: `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><defs><linearGradient id="g"/></defs>` +
			`<use xlink:href="#g"/><image href="data:image/png;base64,AAAA"/><style>.a{fill:url(#g)}</style></svg>`},
		{name: "script", svg: `<svg><script>alert(1)</script></svg>`, wantErr: ErrUnsafe},
		{name: "SCRIPT upper case", svg: `<svg><SCRIPT>alert(1)</SCRIPT></svg>`, wantErr: ErrUnsafe},
		{name: "event handler", svg: `<svg onload="alert(1)"/>`, wantErr: ErrUnsafe},
		{name: "foreignObject", svg: `<svg><foreignObject><div/></foreignObject></svg>`, wantErr: ErrUnsafe},
		{name: "external link", svg: `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="https://evil.example/x.png"/></svg>`, wantErr: ErrUnsafe},
		{name: "javascript url", svg: `<svg><a href="javascript:alert(1)"><path/></a></svg>`, wantErr: ErrUnsafe},
		{name: "external style", svg: `<svg><style>@import "https://evil.example/a.css";</style></svg>`, wantErr: ErrUnsafe},
		{name: "external url in attribute", svg: `<svg><rect style="fill:url(https://evil.example/#p)"/></svg>`, wantErr: ErrUnsafe},
		{name: "DTD", svg: `<!DOCTYPE svg [<!ENTITY a "aaaa">]><svg>&a;</svg>`, wantErr: ErrUnsafe},
		{name: "other root", svg: `<html><svg/></html>`, wantErr: ErrFormat},
		{name: "not XML", svg: `<svg><path></svg>`, wantErr: ErrFormat},
		{name: "empty", svg: ``, wantErr: ErrFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Check("logo.svg", []byte(tt.svg), limits)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, &Image{ContentType: ContentTypeSVG}, got)
		})
	}

	_, err := Check("logo.txt", []byte(`<svg/>`), limits)
	assert.ErrorIs(t, err, ErrFormat, "an SVG must be named .svg")
	_, err = Check("logo.svg", bytes.Repeat([]byte(" "), 300<<10), limits)
	assert.ErrorIs(t, err, ErrTooBig)
}

func TestExtensions(t *testing.T) {
	assert.Equal(t, []string{".jpeg", ".jpg", ".png", ".svg", ".webp"}, Extensions())
}
