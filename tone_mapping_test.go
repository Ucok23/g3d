//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

func TestStandardMaterialEmissiveGlowsWithoutLights(t *testing.T) {
	const size uint32 = 64
	provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
	renderer, err := NewRenderer(provider)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer renderer.Release()
	renderer.SetColorManagement(ColorManagementSRGB)
	renderer.SetSize(size, size)

	scene := NewScene()
	scene.SetBackground(Black)
	// No lights at all: only the emissive color should light the quad.
	mat := NewStandardMaterial(WithColor(Black), WithEmissive(RGB(0.8, 0.4, 0.2)))
	scene.Add(NewMesh(facingQuad(1), mat).MeshNode())

	cam := NewPerspectiveCamera(60, 1, 0.1, 100)
	cam.CameraNode().SetPosition(Vec3{Z: 3})
	px := renderSoftwareRGBA8(t, provider, renderer, scene, cam, size)
	got := pixelAt(px, size, 32, 32)
	// Emissive sRGB (0.8, 0.4, 0.2) -> linear -> sRGB should round-trip to ~(204, 102, 51).
	if !isColor(got, 204, 102, 51) {
		t.Fatalf("emissive pixel = %v, want ~(204, 102, 51)", got)
	}
}

func TestToneMappingCompressesBrightHighlightsAndRespectsExposure(t *testing.T) {
	const size uint32 = 64
	renderBright := func(tm ToneMapping, exposure float32) [4]byte {
		provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
		r, err := NewRenderer(provider)
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetToneMapping(tm)
		r.SetExposure(exposure)
		r.SetSize(size, size)

		scene := NewScene()
		scene.SetBackground(Black)
		amb := NewNode()
		amb.SetUserData(NewAmbientLight(WithLightColor(White), WithLightIntensity(2.0)))
		scene.Add(amb)
		scene.Add(NewMesh(facingQuad(1), NewStandardMaterial(WithColor(White))).MeshNode())

		cam := NewPerspectiveCamera(60, 1, 0.1, 100)
		cam.CameraNode().SetPosition(Vec3{Z: 3})
		px := renderSoftwareRGBA8(t, provider, r, scene, cam, size)
		return pixelAt(px, size, 32, 32)
	}

	none := renderBright(ToneMappingNone, 1.0)
	reinhard := renderBright(ToneMappingReinhard, 1.0)
	aces := renderBright(ToneMappingACES, 1.0)
	acesDark := renderBright(ToneMappingACES, 0.25)

	if none[0] < 250 {
		t.Fatalf("ToneMappingNone at intensity=2.0 got %v, want clipped white (>=250)", none)
	}
	if reinhard[0] <= 180 || reinhard[0] >= 245 {
		t.Errorf("ToneMappingReinhard at intensity=2.0 got %v, want compressed in (180..245)", reinhard)
	}
	if aces[0] <= 200 || aces[0] >= 254 {
		t.Errorf("ToneMappingACES at intensity=2.0 got %v, want compressed filmic roll-off in (200..254)", aces)
	}
	if acesDark[0] >= aces[0]-25 {
		t.Errorf("exposure 0.25 (%v) should be noticeably darker than exposure 1.0 (%v)", acesDark, aces)
	}
}
