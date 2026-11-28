//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

// A smooth dark dielectric floor under a uniform environment reflects more at grazing angles (Fresnel).
// Blinn-Phong ambient is flat; GGX ambient must brighten toward the horizon.
func TestGGXAmbientFresnelBrightensGrazingAngles(t *testing.T) {
	const size uint32 = 64
	render := func(model ShadingModel) (near, far [4]byte) {
		provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
		r, err := NewRenderer(provider)
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetShadingModel(model)
		r.SetSize(size, size)

		scene := NewScene()
		scene.SetBackground(Black)
		amb := NewNode()
		amb.SetUserData(NewAmbientLight(WithLightColor(White), WithLightIntensity(0.6)))
		scene.Add(amb)
		floor := NewMesh(NewPlaneGeometry(200, 200), NewStandardMaterial(WithColor(RGB(0.2, 0.2, 0.2)), WithRoughness(0.1)))
		scene.Add(floor.MeshNode())
		cam := NewPerspectiveCamera(60, 1, 0.1, 500)
		cam.CameraNode().SetPosition(Vec3{Y: 1.5})
		cam.CameraNode().LookAt(Vec3{Z: -10, Y: 0})
		px := renderSoftwareRGBA8(t, provider, r, scene, cam, size)
		// Bottom rows look steeply down at the floor; rows just under the horizon see it at grazing angles.
		return pixelAt(px, size, size/2, size-4), pixelAt(px, size, size/2, size/2+3)
	}
	nearB, farB := render(ShadingBlinnPhong)
	nearG, farG := render(ShadingGGX)
	t.Logf("blinn near=%v far=%v | ggx near=%v far=%v", nearB, farB, nearG, farG)
	if farB[0] != nearB[0] {
		// Blinn-Phong ambient is view independent: only the plane's own shading could differ.
		t.Logf("note: blinn near/far differ by %d", int(farB[0])-int(nearB[0]))
	}
	if int(farG[0]) < int(farB[0])+50 {
		t.Errorf("GGX should reflect far more at grazing angles: ggx far=%d blinn far=%d", farG[0], farB[0])
	}
	if int(nearG[0]) > int(nearB[0])+45 {
		t.Errorf("GGX at steep angles should stay close to Blinn-Phong: ggx=%d blinn=%d", nearG[0], nearB[0])
	}
}

// A surface facing down is lit by the ground below the horizon, not by the sky: raising the ground
// radiance must brighten it, and must leave an up-facing surface untouched. The quad is double-sided, so
// the camera side decides which way its normal points.
func TestGroundRadianceLightsDownFacingSurfaces(t *testing.T) {
	const size uint32 = 64
	render := func(camY, ground float32) [4]byte {
		provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
		r, err := NewRenderer(provider)
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetShadingModel(ShadingGGX)
		r.SetGroundRadiance(White, ground)
		r.SetSize(size, size)

		scene := NewScene()
		scene.SetBackground(Black)
		amb := NewNode()
		amb.SetUserData(NewAmbientLight(WithLightColor(White), WithLightIntensity(0.4)))
		scene.Add(amb)
		quad := NewMesh(NewPlaneGeometry(4, 4), NewStandardMaterial(WithColor(RGB(0.6, 0.6, 0.6)), WithRoughness(1), WithDoubleSided(true)))
		scene.Add(quad.MeshNode())
		cam := NewPerspectiveCamera(50, 1, 0.1, 50)
		cam.CameraNode().SetPosition(Vec3{Y: camY, Z: 0.01})
		cam.CameraNode().LookAt(Vec3{})
		return pixelAt(renderSoftwareRGBA8(t, provider, r, scene, cam, size), size, size/2, size/2)
	}
	up0, up1 := render(4, 0), render(4, 1.5)
	down0, down1 := render(-4, 0), render(-4, 1.5)
	t.Logf("up-facing ground off=%v on=%v | down-facing ground off=%v on=%v", up0, up1, down0, down1)
	if d := int(up1[0]) - int(up0[0]); d < -2 || d > 2 {
		t.Errorf("an up-facing surface must not depend on the ground radiance: off=%d on=%d", up0[0], up1[0])
	}
	if int(down1[0]) < int(down0[0])+40 {
		t.Errorf("a down-facing surface must brighten with the ground radiance: off=%d on=%d", down0[0], down1[0])
	}
}
