//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

func TestNormalMapPerturbsLightingInTangentSpace(t *testing.T) {
	const size uint32 = 64
	renderWithNormal := func(nr, ng, nb byte, scale float32, lightDir Vec3) [4]byte {
		tex, err := NewTexture(2, 2, []byte{
			nr, ng, nb, 255, nr, ng, nb, 255,
			nr, ng, nb, 255, nr, ng, nb, 255,
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
		sun := NewDirectionalLight(WithLightColor(White), WithLightIntensity(1))
		sun.LightNode().SetPosition(Vec3{})
		sun.LightNode().LookAt(lightDir)
		scene.Add(sun.LightNode())
		mat := NewStandardMaterial(WithColor(White), WithRoughness(1), WithNormalMap(tex), WithNormalScale(scale))
		scene.Add(NewMesh(facingQuad(1), mat).MeshNode())

		cam := NewPerspectiveCamera(60, 1, 0.1, 100)
		cam.CameraNode().SetPosition(Vec3{Z: 3})
		px := renderSoftwareRGBA8(t, provider, r, scene, cam, size)
		return pixelAt(px, size, size/2, size/2)
	}

	// Grazing light shining from the RIGHT toward (-1, 0, -0.1):
	// A flat normal (0,0,1) barely catches it; a right-tilted normal (+R = 230, 128, 190) faces it brightly,
	// while a left-tilted normal (-R = 25, 128, 190) faces away and goes dark!
	lightFromRight := Vec3{X: -1, Y: 0, Z: -0.15}.Normalize()
	flatR := renderWithNormal(128, 128, 255, 1.0, lightFromRight)
	tiltRight := renderWithNormal(235, 128, 180, 1.0, lightFromRight)
	tiltLeft := renderWithNormal(20, 128, 180, 1.0, lightFromRight)
	scaleZero := renderWithNormal(235, 128, 180, 0.0, lightFromRight)

	if int(tiltRight[0]) <= int(flatR[0])+40 {
		t.Errorf("right-tilted normal (%v) should be much brighter than flat (%v) under right light", tiltRight, flatR)
	}
	if int(tiltLeft[0]) >= int(flatR[0])-30 {
		t.Errorf("left-tilted normal (%v) should be much darker than flat (%v) under right light", tiltLeft, flatR)
	}
	if !isColor(scaleZero, flatR[0], flatR[1], flatR[2]) {
		t.Errorf("normalScale=0 (%v) should match flat normal (%v)", scaleZero, flatR)
	}

	// Grazing light shining from ABOVE toward (0, -1, -0.15):
	// OpenGL/glTF convention: +G (235) tilts normal UP -> bright; -G (20) tilts normal DOWN -> dark.
	lightFromTop := Vec3{X: 0, Y: -1, Z: -0.15}.Normalize()
	flatT := renderWithNormal(128, 128, 255, 1.0, lightFromTop)
	tiltUp := renderWithNormal(128, 235, 180, 1.0, lightFromTop)
	tiltDown := renderWithNormal(128, 20, 180, 1.0, lightFromTop)
	if int(tiltUp[0]) <= int(flatT[0])+40 {
		t.Errorf("up-tilted (+G) normal (%v) should be much brighter than flat (%v) under top light", tiltUp, flatT)
	}
	if int(tiltDown[0]) >= int(flatT[0])-30 {
		t.Errorf("down-tilted (-G) normal (%v) should be much darker than flat (%v) under top light", tiltDown, flatT)
	}
}
