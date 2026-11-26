package g3d

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
)

// TextureWrap says what a sampler reads outside the 0..1 range of the texture coordinates.
type TextureWrap uint8

const (
	// WrapClamp repeats the edge texel (the WebGPU default).
	WrapClamp TextureWrap = iota
	// WrapRepeat tiles the texture, like wallpaper (the glTF default).
	WrapRepeat
	// WrapMirror tiles the texture and flips every other tile.
	WrapMirror
)

// TextureFilter says how a sampler blends texels when the texture is magnified or minified.
type TextureFilter uint8

const (
	// FilterLinear blends the four nearest texels (smooth).
	FilterLinear TextureFilter = iota
	// FilterNearest takes the nearest texel (blocky, good for pixel art and for tests).
	FilterNearest
)

// maxTextureSize is the largest width or height of a Texture, the WebGPU default limit.
const maxTextureSize = 8192

// Texture is an image that materials paint onto a mesh through its texture coordinates (UV).
//
// The pixels are 8-bit RGBA with straight (not premultiplied) alpha, row by row, the top row first:
// texture coordinate v = 0 is the top row of the image (the WebGPU and glTF convention).
//
// A Texture only holds data. The Renderer uploads it to the GPU the first time a material that uses it
// is drawn, and keeps it until the Renderer is released. Do not change the pixels after that.
//
// With ColorManagementOff (the default) the renderer samples the texels as they are. With ColorManagementSRGB
// it decodes them to linear light according to the color space of the texture (ColorSpaceSRGB by default).
type Texture struct {
	width, height int
	pixels        []byte
	wrapU, wrapV  TextureWrap
	mag, min      TextureFilter
	colorSpace    ColorSpace
}

// TextureOption configures a Texture.
type TextureOption func(*Texture)

// WithWrap sets how the texture is read outside 0..1 along u and v. The default is WrapClamp on both.
func WithWrap(u, v TextureWrap) TextureOption {
	return func(t *Texture) { t.wrapU, t.wrapV = u, v }
}

// WithFilter sets the filter used when the texture is magnified (mag) and minified (min).
// The default is FilterLinear for both.
func WithFilter(mag, minification TextureFilter) TextureOption {
	return func(t *Texture) { t.mag, t.min = mag, minification }
}

// WithColorSpace sets how the texels are encoded: ColorSpaceSRGB (the default) for color images, ColorSpaceLinear
// for data images such as normal or roughness maps. It matters only when the renderer has color management on.
func WithColorSpace(cs ColorSpace) TextureOption {
	return func(t *Texture) { t.colorSpace = cs }
}

// NewTexture creates a Texture from width x height RGBA pixels (4 bytes each, straight alpha, top row
// first). The pixels are copied.
func NewTexture(width, height int, rgba []byte, opts ...TextureOption) (*Texture, error) {
	if width < 1 || height < 1 || width > maxTextureSize || height > maxTextureSize {
		return nil, fmt.Errorf("g3d: texture size %dx%d is outside 1..%d", width, height, maxTextureSize)
	}
	if want := width * height * 4; len(rgba) != want {
		return nil, fmt.Errorf("g3d: a %dx%d texture needs %d bytes of RGBA, got %d", width, height, want, len(rgba))
	}
	t := &Texture{width: width, height: height, pixels: append([]byte(nil), rgba...)}
	for _, o := range opts {
		o(t)
	}
	return t, nil
}

// NewTextureFromImage creates a Texture from any image.Image (PNG, JPEG, ...), converting it to
// non-premultiplied RGBA.
func NewTextureFromImage(img image.Image, opts ...TextureOption) (*Texture, error) {
	if img == nil {
		return nil, errors.New("g3d: nil image")
	}
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 || b.Dx() > maxTextureSize || b.Dy() > maxTextureSize {
		return nil, fmt.Errorf("g3d: image size %dx%d is outside 1..%d", b.Dx(), b.Dy(), maxTextureSize)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return NewTexture(b.Dx(), b.Dy(), dst.Pix, opts...)
}

// Width returns the width in pixels.
func (t *Texture) Width() int { return t.width }

// Height returns the height in pixels.
func (t *Texture) Height() int { return t.height }

// Pixels returns the RGBA pixels. The slice is shared with the Texture: treat it as read-only.
func (t *Texture) Pixels() []byte { return t.pixels }

// Wrap returns the address modes along u and v.
func (t *Texture) Wrap() (u, v TextureWrap) { return t.wrapU, t.wrapV }

// ColorSpace returns how the texels are encoded.
func (t *Texture) ColorSpace() ColorSpace { return t.colorSpace }

// Filter returns the magnification and minification filters.
func (t *Texture) Filter() (mag, minification TextureFilter) { return t.mag, t.min }
