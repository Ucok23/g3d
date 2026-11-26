//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

// checker2x2 is a 2x2 texture: row 0 is red, green; row 1 is blue, white. The top row of the image is v = 0.
func checker2x2(t *testing.T, opts ...TextureOption) *Texture {
	t.Helper()
	tex, err := NewTexture(2, 2, []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 255,
	}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return tex
}

func pixelAt(pixels []byte, size, x, y uint32) [4]byte {
	i := (y*size + x) * 4
	return [4]byte{pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]}
}

func isColor(px [4]byte, r, g, b byte) bool {
	d := func(a, b byte) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(px[0], r) <= 6 && d(px[1], g) <= 6 && d(px[2], b) <= 6
}

// facingQuad is a 2x2 quad in the XY plane facing the camera, with texture coordinates from 0 to maxUV in
// both directions (v = 0 is the top), so maxUV 2 makes the texture cover it twice in each direction.
// (NewPlaneGeometry lies in the XZ plane, so it cannot be used for this.)
func facingQuad(maxUV float32) *BufferGeometry {
	// position (3), normal (3), uv (2)
	v := []float32{
		-1, -1, 0, 0, 0, 1, 0, maxUV,
		1, -1, 0, 0, 0, 1, maxUV, maxUV,
		1, 1, 0, 0, 0, 1, maxUV, 0,
		-1, 1, 0, 0, 0, 1, 0, 0,
	}
	return NewBufferGeometry(v, []uint32{0, 1, 2, 0, 2, 3})
}

// renderQuad renders geometry with a material in front of a perspective camera 3 units away, on the
// software backend, and returns the 64x64 pixels. A quad of 2x2 covers 0.21..0.79 of the image.
func renderQuad(t *testing.T, geom Geometry, mat Material, lit bool) []byte {
	t.Helper()
	const size uint32 = 64
	provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
	renderer, err := NewRenderer(provider)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer renderer.Release()
	renderer.SetSize(size, size)

	scene := NewScene()
	scene.SetBackground(Black)
	if lit {
		amb := NewNode()
		amb.SetUserData(NewAmbientLight(WithLightColor(White), WithLightIntensity(1)))
		scene.Add(amb)
	}
	scene.Add(NewMesh(geom, mat).MeshNode())
	camera := NewPerspectiveCamera(60, 1, 0.1, 100)
	camera.CameraNode().SetPosition(Vec3{Z: 3})
	return renderSoftwareRGBA8(t, provider, renderer, scene, camera, size)
}

// TestTexturedPlaneShowsTheTexelsOnTheSoftwareBackend is the first proof that sampling works on the
// backend the server uses: each quadrant of the plane shows its own texel, unlit and unfiltered.
func TestTexturedPlaneShowsTheTexelsOnTheSoftwareBackend(t *testing.T) {
	tex := checker2x2(t, WithFilter(FilterNearest, FilterNearest))
	px := renderQuad(t, facingQuad(1), NewBasicMaterial(WithMap(tex)), false)
	for _, c := range []struct {
		name    string
		x, y    uint32
		r, g, b byte
	}{
		{"top left is the red texel", 22, 22, 255, 0, 0},
		{"top right is the green texel", 42, 22, 0, 255, 0},
		{"bottom left is the blue texel", 22, 42, 0, 0, 255},
		{"bottom right is the white texel", 42, 42, 255, 255, 255},
	} {
		if got := pixelAt(px, 64, c.x, c.y); !isColor(got, c.r, c.g, c.b) {
			t.Errorf("%s: pixel (%d,%d) = %v", c.name, c.x, c.y, got)
		}
	}
	if bg := pixelAt(px, 64, 2, 2); !isColor(bg, 0, 0, 0) {
		t.Errorf("outside the plane = %v, want the black background", bg)
	}
}

func TestColorMultipliesTheTexel(t *testing.T) {
	tex := checker2x2(t, WithFilter(FilterNearest, FilterNearest))
	// a half-gray material color halves every texel: white becomes (128,128,128), red (128,0,0)
	px := renderQuad(t, facingQuad(1), NewBasicMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithMap(tex)), false)
	if got := pixelAt(px, 64, 42, 42); !isColor(got, 128, 128, 128) {
		t.Errorf("white texel x gray color = %v, want about (128,128,128)", got)
	}
	if got := pixelAt(px, 64, 22, 22); !isColor(got, 128, 0, 0) {
		t.Errorf("red texel x gray color = %v, want about (128,0,0)", got)
	}
}

func TestStandardMaterialPaintsTheMapUnderLight(t *testing.T) {
	tex := checker2x2(t, WithFilter(FilterNearest, FilterNearest))
	// full white ambient light: the lit color is the base color times the texel
	px := renderQuad(t, facingQuad(1), NewStandardMaterial(WithMap(tex)), true)
	if got := pixelAt(px, 64, 42, 22); !isColor(got, 0, 255, 0) {
		t.Errorf("green texel under ambient light = %v", got)
	}
	if got := pixelAt(px, 64, 22, 42); !isColor(got, 0, 0, 255) {
		t.Errorf("blue texel under ambient light = %v", got)
	}
}

func TestMaterialWithoutAMapIsUnchangedByTheWhiteTexture(t *testing.T) {
	px := renderQuad(t, facingQuad(1), NewBasicMaterial(WithColor(RGB(0.2, 0.6, 0.4))), false)
	if got := pixelAt(px, 64, 32, 32); !isColor(got, 51, 153, 102) {
		t.Errorf("a plain color through the white texture = %v, want about (51,153,102)", got)
	}
}

func TestWrapModeDecidesWhatIsReadPastOne(t *testing.T) {
	// the pixel at 62.5% across and 12.5% down the quad has u = 1.25, v = 0.25
	repeat := renderQuad(t, facingQuad(2), NewBasicMaterial(WithMap(checker2x2(t, WithWrap(WrapRepeat, WrapRepeat), WithFilter(FilterNearest, FilterNearest)))), false)
	if got := pixelAt(repeat, 64, 37, 18); !isColor(got, 255, 0, 0) {
		t.Errorf("repeat at u=1.25 = %v, want the red texel (column 0 again)", got)
	}
	clamp := renderQuad(t, facingQuad(2), NewBasicMaterial(WithMap(checker2x2(t, WithWrap(WrapClamp, WrapClamp), WithFilter(FilterNearest, FilterNearest)))), false)
	if got := pixelAt(clamp, 64, 37, 18); !isColor(got, 0, 255, 0) {
		t.Errorf("clamp at u=1.25 = %v, want the green texel (the last column, held)", got)
	}
}

func TestLinearFilteringBlendsTexels(t *testing.T) {
	// a 2x1 texture, black and white: at the middle of the plane the two texels meet, and linear filtering
	// gives gray where nearest gives one of them
	tex, err := NewTexture(2, 1, []byte{0, 0, 0, 255, 255, 255, 255, 255}, WithFilter(FilterLinear, FilterLinear))
	if err != nil {
		t.Fatal(err)
	}
	px := renderQuad(t, facingQuad(1), NewBasicMaterial(WithMap(tex)), false)
	got := pixelAt(px, 64, 32, 32)
	if got[0] < 40 || got[0] > 215 {
		t.Errorf("linear filtering at the seam = %v, want a gray between black and white", got)
	}
}

// An 8x8 one-pixel checker drawn with its sampler locked to one mip level shows that level. This checks the whole
// chain on the backend the server uses: the levels are built, uploaded one by one, and the sampler reads the one it is told to.
func TestSamplerReadsTheMipLevelItIsLockedTo(t *testing.T) {
	px := func(opts ...TextureOption) [4]byte {
		tex, err := NewTexture(8, 8, checkerPixels(8), append([]TextureOption{WithFilter(FilterNearest, FilterNearest), WithMipmapFilter(FilterNearest)}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		return pixelAt(renderQuad(t, facingQuad(1), NewBasicMaterial(WithMap(tex)), false), 64, 22, 22)
	}
	// level 0: a texel of the checker, black or white
	if got := px(WithLODRange(0, 0.1)); got[0] != 0 && got[0] != 255 {
		t.Errorf("level 0 = %v, want a black or white texel", got)
	}
	// level 3 is 1x1 and holds the average: mid-gray (unmanaged, so the values are averaged as stored)
	if got := px(WithLODRange(3, 3)); !isColor(got, 128, 128, 128) {
		t.Errorf("level 3 = %v, want mid-gray", got)
	}
	// without a chain there is only level 0, whatever the sampler asks for
	if got := px(WithMipmaps(false), WithLODRange(3, 3)); got[0] != 0 && got[0] != 255 {
		t.Errorf("no mipmaps, locked to level 3 = %v, want the base level (a black or white texel)", got)
	}
}

// With color management the GPU decodes an sRGB texture before filtering, so the chain is built in linear light:
// the 1x1 level of a black and white checker reads 188 after the round trip through decoding and encoding.
func TestMipChainIsBuiltInLinearLightWhenManaged(t *testing.T) {
	tex, err := NewTexture(8, 8, checkerPixels(8), WithMipmapFilter(FilterNearest), WithLODRange(3, 3))
	if err != nil {
		t.Fatal(err)
	}
	scene := quadScene(NewBasicMaterial(WithMap(tex)), Black)
	got := pixelAt(renderManaged(t, unorm, ColorManagementSRGB, scene), 64, 22, 22)
	if !isColor(got, 188, 188, 188) {
		t.Errorf("managed, level 3 of a black and white checker = %v, want about 188", got)
	}
}

// The software backend chooses the mip level from the screen-space derivatives of the texture coordinates (computed
// between the lanes of a 2x2 quad of fragments), although the doc comment of wgpu's software package still says that
// implicit-LOD sampling uses the base level. The server render relies on it, so it is pinned here: a one-pixel checker
// minified about 14 to 1 reads as an even mid-gray with mipmaps and as moire without them.
func TestSoftwareBackendPicksTheMipLevelFromDerivatives(t *testing.T) {
	spread := func(opts ...TextureOption) int {
		tex, err := NewTexture(64, 64, checkerPixels(64), append([]TextureOption{WithWrap(WrapRepeat, WrapRepeat)}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		px := renderQuad(t, facingQuad(8), NewBasicMaterial(WithMap(tex)), false) // uv 0..8 over about 37 pixels
		lo, hi := 255, 0
		for x := uint32(24); x < 40; x++ {
			v := int(pixelAt(px, 64, x, 32)[0])
			lo, hi = min(lo, v), max(hi, v)
		}
		return hi - lo
	}
	if s := spread(); s > 4 {
		t.Errorf("with mipmaps the minified checker varies by %d, want an even gray (at most 4)", s)
	}
	if s := spread(WithMipmaps(false)); s < 12 {
		t.Errorf("without mipmaps the minified checker varies by only %d, want visible moire (at least 12)", s)
	}
}
