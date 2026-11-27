//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

func TestOcclusionMapModulatesAmbientLight(t *testing.T) {
	// 2x1 linear texture: left pixel R=0 (fully occluded), right pixel R=255 (unoccluded).
	occ, err := NewTexture(2, 1, []byte{
		0, 255, 255, 255,
		255, 255, 255, 255,
	}, WithColorSpace(ColorSpaceLinear), WithFilter(FilterNearest, FilterNearest), WithMipmaps(false))
	if err != nil {
		t.Fatal(err)
	}
	// Full strength (1.0): left half ambient = 0, right half ambient = 255.
	pxFull := renderQuad(t, facingQuad(1), NewStandardMaterial(WithColor(White), WithOcclusionMap(occ), WithOcclusionStrength(1)), true)
	if left := pixelAt(pxFull, 64, 22, 32); !isColor(left, 0, 0, 0) {
		t.Errorf("occluded left pixel (strength=1) = %v, want black", left)
	}
	if right := pixelAt(pxFull, 64, 42, 32); !isColor(right, 255, 255, 255) {
		t.Errorf("unoccluded right pixel (strength=1) = %v, want white", right)
	}
	// Half strength (0.5): left half ambient = 0.5 -> ~128.
	pxHalf := renderQuad(t, facingQuad(1), NewStandardMaterial(WithColor(White), WithOcclusionMap(occ), WithOcclusionStrength(0.5)), true)
	if left := pixelAt(pxHalf, 64, 22, 32); !isColor(left, 128, 128, 128) {
		t.Errorf("occluded left pixel (strength=0.5) = %v, want ~(128,128,128)", left)
	}
}

func TestEmissiveMapModulatesEmissionInDarkness(t *testing.T) {
	// 2x1 sRGB texture: left red, right green.
	emTex, err := NewTexture(2, 1, []byte{
		255, 0, 0, 255,
		0, 255, 0, 255,
	}, WithFilter(FilterNearest, FilterNearest), WithMipmaps(false))
	if err != nil {
		t.Fatal(err)
	}
	// No lights at all; white emissive * emTex shows red on left, green on right.
	mat := NewStandardMaterial(WithColor(Black), WithEmissive(White), WithEmissiveMap(emTex))
	px := renderQuad(t, facingQuad(1), mat, false)
	if left := pixelAt(px, 64, 22, 32); !isColor(left, 255, 0, 0) {
		t.Errorf("emissive left = %v, want red", left)
	}
	if right := pixelAt(px, 64, 42, 32); !isColor(right, 0, 255, 0) {
		t.Errorf("emissive right = %v, want green", right)
	}
}

func TestMetallicRoughnessMapControlsSpecularAndDiffuse(t *testing.T) {
	const size uint32 = 64
	// 2x1 linear MR texture (G=roughness, B=metallic):
	// left: G=255 (rough=1.0), B=0 (dielectric) -> full diffuse, zero specular.
	// right: G=0 (rough=0.0), B=255 (metal=1.0) -> attenuated diffuse, strong tinted specular.
	mrTex, err := NewTexture(2, 1, []byte{
		0, 255, 0, 255,
		0, 0, 255, 255,
	}, WithColorSpace(ColorSpaceLinear), WithFilter(FilterNearest, FilterNearest), WithMipmaps(false))
	if err != nil {
		t.Fatal(err)
	}

	provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
	r, err := NewRenderer(provider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	r.SetColorManagement(ColorManagementSRGB)
	r.SetSize(size, size)

	scene := NewScene()
	scene.SetBackground(Black)
	// Directional light tilted 45 deg so H != N -> zero specular highlight, isolating diffuse metalness attenuation.
	sun := NewDirectionalLight(WithLightColor(White), WithLightIntensity(1))
	sun.LightNode().SetRotation(Euler{Y: Radians(45)})
	scene.Add(sun.LightNode())
	mat := NewStandardMaterial(WithColor(RGB(0.8, 0.4, 0.2)), WithMetallic(1), WithRoughness(1), WithMetallicRoughnessMap(mrTex))
	scene.Add(NewMesh(facingQuad(1), mat).MeshNode())

	cam := NewPerspectiveCamera(60, 1, 0.1, 100)
	cam.CameraNode().SetPosition(Vec3{Z: 3})
	px := renderSoftwareRGBA8(t, provider, r, scene, cam, size)

	left := pixelAt(px, size, 22, 32)  // rough dielectric: clearly lit
	right := pixelAt(px, size, 42, 32) // smooth metal off-specular angle: dark diffuse
	if left[0] < 100 {
		t.Errorf("dielectric left pixel = %v, want lit red channel >= 100", left)
	}
	if right[0] > 40 {
		t.Errorf("metal off-specular right pixel = %v, want dark diffuse <= 40", right)
	}
}
