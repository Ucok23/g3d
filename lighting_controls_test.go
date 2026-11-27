//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

// lightingControlScene is a facing quad lit by one head-on sun, with a glossy emissive material.
func lightingControlScene() (*Scene, *PerspectiveCamera) {
	scene := NewScene()
	scene.SetBackground(Black)
	sun := NewDirectionalLight(WithLightColor(White), WithLightIntensity(0.6))
	sun.LightNode().SetPosition(Vec3{})
	sun.LightNode().LookAt(Vec3{Z: -1})
	scene.Add(sun.LightNode())
	mesh := NewMesh(NewPlaneGeometry(4, 4), NewStandardMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithRoughness(0.1), WithEmissive(RGB(0.4, 0.1, 0.1))))
	mesh.MeshNode().SetRotation(Euler{X: Radians(90)})
	scene.Add(mesh.MeshNode())
	cam := NewPerspectiveCamera(50, 1, 0.1, 50)
	cam.CameraNode().SetPosition(Vec3{Z: 4})
	cam.CameraNode().LookAt(Vec3{})
	return scene, cam
}

func TestEmissiveAndSpecularSwitches(t *testing.T) {
	const size uint32 = 64
	render := func(emissive, specular bool) [4]byte {
		provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
		r, err := NewRenderer(provider)
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetEmissiveEnabled(emissive)
		r.SetSpecularEnabled(specular)
		r.SetSize(size, size)
		scene, cam := lightingControlScene()
		return pixelAt(renderSoftwareRGBA8(t, provider, r, scene, cam, size), size, size/2, size/2)
	}
	full := render(true, true)
	noEm := render(false, true)
	noSpec := render(true, false)
	none := render(false, false)
	t.Logf("full=%v noEmissive=%v noSpecular=%v neither=%v", full, noEm, noSpec, none)
	if noEm[0] >= full[0] {
		t.Errorf("disabling emissive should darken red: full=%d noEmissive=%d", full[0], noEm[0])
	}
	if noSpec[0] >= full[0] && noSpec[1] >= full[1] {
		t.Errorf("disabling specular should darken a glossy surface: full=%v noSpecular=%v", full, noSpec)
	}
	if none[0] >= noEm[0] && none[0] >= noSpec[0] {
		t.Errorf("disabling both should be darkest: %v", none)
	}
}
