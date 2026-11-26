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
	// camera and light at -Z: the quad (normal +Z) is seen from behind
	two := renderDoubleSided(t, -3, NewStandardMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithRoughness(1), WithDoubleSided(true)))
	if two[0] < 120 || two[0] > 136 {
		t.Errorf("the back face of a double-sided material = %v, want about 128 (gray 0.5 fully lit)", two)
	}
}

// The front face of a double-sided material is shaded as before, and a back face is not lit by a light that is
// on the other side of the surface from the viewer.
func TestDoubleSidedMaterialKeepsItsFrontFaceAndTheLightSide(t *testing.T) {
	// camera and light at +Z, the front side of the quad
	if front := renderDoubleSided(t, 3, NewStandardMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithRoughness(1), WithDoubleSided(true))); front[0] < 120 || front[0] > 136 {
		t.Errorf("the front face of a double-sided material = %v, want about 128", front)
	}
	// the single-sided control gives the same front face
	if single := renderDoubleSided(t, 3, NewStandardMaterial(WithColor(RGB(0.5, 0.5, 0.5)), WithRoughness(1))); single[0] < 120 || single[0] > 136 {
		t.Errorf("the front face of a single-sided material = %v, want about 128", single)
	}
}

// renderDoubleSided renders the quad (normal +Z) from a camera at z = camZ, with a light on the camera's side, and
// returns the middle pixel.
func renderDoubleSided(t *testing.T, camZ float32, mat Material) [4]byte {
	t.Helper()
	const size uint32 = 64
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
	sun.LightNode().SetPosition(Vec3{Z: camZ * 5 / 3}) // on the camera's side, shining toward the quad
	sun.LightNode().LookAt(Vec3{})
	scene.Add(sun.LightNode())
	scene.Add(NewMesh(backFacingQuad(), mat).MeshNode())
	camera := NewPerspectiveCamera(60, 1, 0.1, 100)
	camera.CameraNode().SetPosition(Vec3{Z: camZ})
	camera.CameraNode().LookAt(Vec3{})
	px := renderSoftwareRGBA8(t, provider, renderer, scene, camera, size)
	i := (32*size + 32) * 4 // the middle pixel
	return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
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
