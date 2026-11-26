//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"math"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// renderManaged renders a scene on the software backend into a target of the given format with the given
// color management, and returns the 64x64 RGBA pixels.
func renderManaged(t *testing.T, format gputypes.TextureFormat, m ColorManagement, scene *Scene) []byte {
	t.Helper()
	const size uint32 = 64
	provider := newSoftwareProvider(t, format)
	renderer, err := NewRenderer(provider)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer renderer.Release()
	renderer.SetColorManagement(m)
	renderer.SetSize(size, size)

	target, err := provider.device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "managed-target", Size: wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
		MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format,
		Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	defer target.Release()
	view, err := provider.device.CreateTextureView(target, nil)
	if err != nil {
		t.Fatalf("CreateTextureView: %v", err)
	}
	defer view.Release()
	camera := NewPerspectiveCamera(60, 1, 0.1, 100)
	camera.CameraNode().SetPosition(Vec3{Z: 3})
	if err := renderer.Render(scene, camera, view); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return readbackRGBA8(t, provider.device, target, size)
}

var (
	unorm = gputypes.TextureFormatRGBA8Unorm
	srgb  = gputypes.TextureFormatRGBA8UnormSrgb
)

func near8(px [4]byte, want byte, tol int) bool {
	for i := 0; i < 3; i++ {
		if math.Abs(float64(int(px[i])-int(want))) > float64(tol) {
			return false
		}
	}
	return true
}

func quadScene(mat Material, bg Color) *Scene {
	s := NewScene()
	s.SetBackground(bg)
	s.Add(NewMesh(facingQuad(1), mat).MeshNode())
	return s
}

// withSun adds a white directional light that lights the +Z facing quad at 60 degrees from its normal (N.L = 0.5).
func withSun(s *Scene) *Scene {
	sun := NewDirectionalLight(WithLightColor(White), WithLightIntensity(1))
	sun.LightNode().SetPosition(Vec3{Y: 0.866 * 5, Z: 0.5 * 5})
	sun.LightNode().LookAt(Vec3{})
	s.Add(sun.LightNode())
	return s
}

func TestColorManagementIsOffByDefault(t *testing.T) {
	provider := newSoftwareProvider(t, unorm)
	r, err := NewRenderer(provider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if r.ColorManagement() != ColorManagementOff || r.frameFlags() != 0 {
		t.Errorf("default = %v flags %d, want off and 0", r.ColorManagement(), r.frameFlags())
	}
	r.SetColorManagement(ColorManagementSRGB)
	if r.frameFlags() != frameFlagManaged|frameFlagEncodeOutput {
		t.Errorf("managed on a unorm target: flags = %d, want managed + encode", r.frameFlags())
	}
	r.surfaceFormat = srgb
	if r.frameFlags() != frameFlagManaged {
		t.Errorf("managed on an sRGB target: flags = %d, want managed only (the hardware encodes)", r.frameFlags())
	}
}

// A color you write is what you see when it is not lit, with or without color management and on either kind of target.
func TestUnlitColorKeepsItsValueUnderColorManagement(t *testing.T) {
	gray := func() *Scene { return quadScene(NewBasicMaterial(WithColor(RGB(0.5, 0.5, 0.5))), Black) }
	for name, c := range map[string]struct {
		format gputypes.TextureFormat
		m      ColorManagement
	}{
		"off, unorm target":     {unorm, ColorManagementOff},
		"managed, unorm target": {unorm, ColorManagementSRGB},
		"managed, sRGB target":  {srgb, ColorManagementSRGB},
	} {
		if got := pixelAt(renderManaged(t, c.format, c.m, gray()), 64, 32, 32); !near8(got, 128, 3) {
			t.Errorf("%s: gray 0.5 = %v, want about 128", name, got)
		}
	}
}

// The point of color management: light adds up in linear space. A gray lit at half strength is 64 when the
// math is done on encoded values and 92 when it is done in linear light and encoded afterwards.
func TestLightingHappensInLinearSpaceWhenManaged(t *testing.T) {
	lit := func() *Scene {
		return withSun(quadScene(NewStandardMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithRoughness(1)), Black))
	}
	if got := pixelAt(renderManaged(t, unorm, ColorManagementOff, lit()), 64, 32, 32); !near8(got, 64, 4) {
		t.Errorf("off: lit gray = %v, want about 64", got)
	}
	if got := pixelAt(renderManaged(t, unorm, ColorManagementSRGB, lit()), 64, 32, 32); !near8(got, 92, 4) {
		t.Errorf("managed, unorm target (encoded in the shader): lit gray = %v, want about 92", got)
	}
	if got := pixelAt(renderManaged(t, srgb, ColorManagementSRGB, lit()), 64, 32, 32); !near8(got, 92, 4) {
		t.Errorf("managed, sRGB target (encoded by the hardware): lit gray = %v, want about 92 (not 0.36 encoded twice)", got)
	}
}

// An sRGB color texture is decoded to linear light before it is filtered. Halfway between a gray of 128 and white:
//   - decoded, then filtered, then encoded (correct):  about 205
//   - never decoded (the texels taken as linear):       about 224
//   - filtered as encoded values, then decoded:         about 192
//
// Black and white cannot tell these apart (they are fixed points of the decoding), so the ramp starts at gray.
func TestSRGBTextureIsDecodedBeforeItIsFiltered(t *testing.T) {
	tex, err := NewTexture(2, 1, []byte{128, 128, 128, 255, 255, 255, 255, 255}, WithFilter(FilterLinear, FilterLinear))
	if err != nil {
		t.Fatal(err)
	}
	scene := func() *Scene { return quadScene(NewBasicMaterial(WithMap(tex)), Black) }
	want := byte(LinearToSRGB((SRGBToLinear(128.0/255)+1)/2)*255 + 0.5)
	if want < 203 || want > 207 {
		t.Fatalf("the reference value moved: %d", want)
	}
	if got := pixelAt(renderManaged(t, unorm, ColorManagementSRGB, scene()), 64, 32, 32); !near8(got, want, 5) {
		t.Errorf("managed: halfway between gray 128 and white = %v, want about %d", got, want)
	}
	// without color management the texels are blended as they are: (128 + 255) / 2
	if got := pixelAt(renderManaged(t, unorm, ColorManagementOff, scene()), 64, 32, 32); !near8(got, 192, 6) {
		t.Errorf("off: halfway between gray 128 and white = %v, want about 192", got)
	}
}

// A texture of data (ColorSpaceLinear) is never decoded: 128 means 0.502 of the light, which encodes to 188.
func TestLinearColorSpaceTextureIsNotDecoded(t *testing.T) {
	mk := func(opts ...TextureOption) *Texture {
		tex, err := NewTexture(1, 1, []byte{128, 128, 128, 255}, opts...)
		if err != nil {
			t.Fatal(err)
		}
		return tex
	}
	srgbTex := quadScene(NewBasicMaterial(WithMap(mk())), Black)
	linearTex := quadScene(NewBasicMaterial(WithMap(mk(WithColorSpace(ColorSpaceLinear)))), Black)
	if got := pixelAt(renderManaged(t, unorm, ColorManagementSRGB, srgbTex), 64, 32, 32); !near8(got, 128, 3) {
		t.Errorf("an sRGB texel of 128 shows as %v, want 128", got)
	}
	if got := pixelAt(renderManaged(t, unorm, ColorManagementSRGB, linearTex), 64, 32, 32); !near8(got, 188, 4) {
		t.Errorf("a linear texel of 128 shows as %v, want about 188", got)
	}
}

func TestBackgroundKeepsItsValueUnderColorManagement(t *testing.T) {
	for name, c := range map[string]struct {
		format gputypes.TextureFormat
		m      ColorManagement
	}{
		"off, unorm target":     {unorm, ColorManagementOff},
		"managed, unorm target": {unorm, ColorManagementSRGB},
		"managed, sRGB target":  {srgb, ColorManagementSRGB},
	} {
		empty := NewScene()
		empty.SetBackground(RGB(0.5, 0.5, 0.5))
		if got := pixelAt(renderManaged(t, c.format, c.m, empty), 64, 32, 32); !near8(got, 128, 3) {
			t.Errorf("%s: background 0.5 = %v, want about 128", name, got)
		}
	}
}

func TestChangingColorManagementReloadsTheTextures(t *testing.T) {
	provider := newSoftwareProvider(t, unorm)
	r, err := NewRenderer(provider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	r.SetSize(64, 64)
	tex, _ := NewTexture(1, 1, []byte{255, 255, 255, 255})
	device, queue := r.gpuState.Device(), r.gpuState.Queue()
	if _, err := r.ensureTexture(device, queue, tex); err != nil {
		t.Fatal(err)
	}
	if len(r.textures) != 1 {
		t.Fatalf("cached textures = %d, want 1", len(r.textures))
	}
	r.SetColorManagement(ColorManagementSRGB)
	if len(r.textures) != 0 || r.defaultTexture != nil {
		t.Error("changing the color pipeline should free the cached textures")
	}
	if _, err := r.ensureTexture(device, queue, tex); err != nil {
		t.Fatalf("uploading again in the new format: %v", err)
	}
	r.SetColorManagement(ColorManagementSRGB) // no change: nothing is freed
	if len(r.textures) != 1 {
		t.Error("setting the same mode again should keep the textures")
	}
}
