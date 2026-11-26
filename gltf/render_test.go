//go:build !rust && !(js && wasm) && !android

package gltf

import (
	"context"
	"testing"

	"github.com/gogpu/g3d"
	"github.com/gogpu/gpucontext"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/gogpu/wgpu/wgputest"
)

// softwareProvider hands wgpu's software device to the renderer, the way the g3d tests do.
type softwareProvider struct {
	device *wgpu.Device
	format gputypes.TextureFormat
}

func (p *softwareProvider) Device() gpucontext.Device { return wgpu.DeviceToHandle(p.device) }
func (p *softwareProvider) Queue() gpucontext.Queue   { return wgpu.QueueToHandle(p.device.Queue()) }
func (p *softwareProvider) SurfaceFormat() gputypes.TextureFormat {
	return p.format
}
func (p *softwareProvider) Adapter() gpucontext.Adapter { return gpucontext.Adapter{} }
func (p *softwareProvider) AdapterInfo() gpucontext.AdapterInfo {
	return gpucontext.AdapterInfo{Name: "wgputest-software", Type: gpucontext.AdapterTypeSoftware}
}
func (p *softwareProvider) Features() gputypes.Features { return 0 }
func (p *softwareProvider) DownlevelCapabilities() gputypes.DownlevelCapabilities {
	return gputypes.DownlevelCapabilities{}
}

// render draws the model in front of a camera on the software backend into a 64x64 target and returns the pixels.
func render(t *testing.T, m *Model, mgmt g3d.ColorManagement) []byte {
	t.Helper()
	const size uint32 = 64
	device, _, cleanup, err := wgputest.NewSoftwareDevice()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	provider := &softwareProvider{device: device, format: gputypes.TextureFormatRGBA8Unorm}
	renderer, err := g3d.NewRenderer(provider)
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Release()
	renderer.SetColorManagement(mgmt)
	renderer.SetSize(size, size)

	scene := g3d.NewScene()
	scene.SetBackground(g3d.Black)
	scene.Add(&m.Root.Node)
	camera := g3d.NewPerspectiveCamera(30, 1, 0.1, 100)
	// the triangle spans (0,0)..(1,1) in the XY plane: look at its middle
	camera.CameraNode().SetPosition(g3d.Vec3{X: 0.35, Y: 0.35, Z: 2})
	camera.CameraNode().LookAt(g3d.Vec3{X: 0.35, Y: 0.35})

	target, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "t", Size: wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1,
		Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA8Unorm,
		Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Release()
	view, err := device.CreateTextureView(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Release()
	if err := renderer.Render(scene, camera, view); err != nil {
		t.Fatal(err)
	}

	bytesPerRow := size * 4
	total := uint64(bytesPerRow) * uint64(size)
	staging, err := device.CreateBuffer(&wgpu.BufferDescriptor{Label: "rb", Size: total, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		t.Fatal(err)
	}
	defer staging.Release()
	enc, err := device.CreateCommandEncoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	enc.CopyTextureToBuffer(target, staging, []wgpu.BufferTextureCopy{{
		BufferLayout: wgpu.ImageDataLayout{BytesPerRow: bytesPerRow, RowsPerImage: size},
		TextureBase:  wgpu.ImageCopyTexture{Texture: target},
		Size:         wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
	}})
	cmd, err := enc.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.Queue().Submit(cmd); err != nil {
		t.Fatal(err)
	}
	if err := staging.Map(context.Background(), wgpu.MapModeRead, 0, total); err != nil {
		t.Fatal(err)
	}
	rng, err := staging.MappedRange(0, total)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), rng.Bytes()...)
}

func px(pixels []byte, x, y int) [3]byte {
	i := (y*64 + x) * 4
	return [3]byte{pixels[i], pixels[i+1], pixels[i+2]}
}

func isNear(got [3]byte, r, g, b int) bool {
	d := func(a byte, b int) int { return max(int(a)-b, b-int(a)) }
	return d(got[0], r) < 40 && d(got[1], g) < 40 && d(got[2], b) < 40
}

// The whole path: a PNG inside a GLB, decoded by the loader, uploaded by the renderer, sampled by the shader.
// The triangle has uvs (0,0) (1,0) (0,1) and the texture has red, green on the top row and blue, white below,
// so near the (0,0) corner the triangle is red, near (1,0) green and near (0,1) blue.
func TestAGLBWithATextureRendersItsTexels(t *testing.T) {
	a := texAsset{texture: `{"source":0,"sampler":0}`, samplers: `[{"magFilter":9728,"minFilter":9728,"wrapS":33071,"wrapT":33071}]`,
		material: `{"extensions":{"KHR_materials_unlit":{}},"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}}`}
	m := mustParse(t, a.glb(t))
	pixels := render(t, m, g3d.ColorManagementOff)
	// the camera looks at (0.35, 0.35); the triangle's corners (0,0) (1,0) (0,1) are at the screen positions below
	corner := map[string]struct {
		x, y    int
		r, g, b int
	}{
		"red texel near (0,0)":   {28, 38, 255, 0, 0},
		"green texel near (1,0)": {50, 38, 0, 255, 0},
		"blue texel near (0,1)":  {28, 18, 0, 0, 255},
	}
	for name, c := range corner {
		if got := px(pixels, c.x, c.y); !isNear(got, c.r, c.g, c.b) {
			t.Errorf("%s: pixel (%d,%d) = %v", name, c.x, c.y, got)
		}
	}
	if px(pixels, 2, 2) != [3]byte{0, 0, 0} {
		t.Errorf("outside the triangle = %v, want the black background", px(pixels, 2, 2))
	}
}
