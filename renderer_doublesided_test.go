//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"testing"

	"github.com/gogpu/gputypes"
)

const doubleSidedFormat = gputypes.TextureFormatRGBA8Unorm

// A quad facing +Z seen from behind (the camera is at -Z), lit by a light on the camera's side. A double-sided
// material shades its back face with the normal turned around, so the light that reaches the viewer lights it
// fully; with the normal left as it is, the face would look unlit.
func TestBackFaceOfADoubleSidedMaterialIsLitFromTheViewersSide(t *testing.T) {
	const size uint32 = 64
	render := func(mat Material) [4]byte {
		provider := newSoftwareProvider(t, doubleSidedFormat)
		renderer, err := NewRenderer(provider)
		if err != nil {
			t.Fatal(err)
		}
		defer renderer.Release()
		renderer.SetSize(size, size)
		scene := NewScene()
		scene.SetBackground(Black)
		sun := NewDirectionalLight(WithLightColor(White), WithLightIntensity(1))
		sun.LightNode().SetPosition(Vec3{Z: -5}) // on the camera's side, shining toward +Z
		sun.LightNode().LookAt(Vec3{})
		scene.Add(sun.LightNode())
		scene.Add(NewMesh(backFacingQuad(), mat).MeshNode())
		camera := NewPerspectiveCamera(60, 1, 0.1, 100)
		camera.CameraNode().SetPosition(Vec3{Z: -3})
		camera.CameraNode().LookAt(Vec3{})
		px := renderSoftwareRGBA8(t, provider, renderer, scene, camera, size)
		i := (32*size + 32) * 4 // the middle pixel
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	two := render(NewStandardMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithRoughness(1), WithDoubleSided(true)))
	if two[0] < 120 || two[0] > 136 {
		t.Errorf("the back face of a double-sided material = %v, want about 128 (gray 0.5 fully lit)", two)
	}
}

// backFacingQuad is a 2x2 quad in the XY plane whose front (counter-clockwise, normal +Z) faces away from a camera at -Z.
func backFacingQuad() *BufferGeometry {
	v := []float32{
		-1, -1, 0, 0, 0, 1, 0, 1,
		1, -1, 0, 0, 0, 1, 1, 1,
		1, 1, 0, 0, 0, 1, 1, 0,
		-1, 1, 0, 0, 0, 1, 0, 0,
	}
	return NewBufferGeometry(v, []uint32{0, 1, 2, 0, 2, 3})
}
