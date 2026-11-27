//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"context"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/vulkan"
)

// shadowTestScene places a floating occluder cube above a white ground plane under a straight-down sun.
// Looking straight down from above-side: the ground directly under the cube (projected by the sun) is
// shadowed while the adjacent sunlit ground stays bright.
func shadowTestScene() (*Scene, *PerspectiveCamera) {
	scene := NewScene()
	scene.SetBackground(Black)

	amb := NewNode()
	amb.SetUserData(NewAmbientLight(WithLightColor(White), WithLightIntensity(0.2)))
	scene.Add(amb)

	// Sun shining diagonally down (+X, -Y) so the cube at (0, 1.5, 0) casts a shadow onto the floor at (+1.5, 0, 0).
	sun := NewDirectionalLight(WithLightColor(White), WithLightIntensity(0.8))
	sun.LightNode().SetPosition(Vec3{})
	sun.LightNode().LookAt(Vec3{X: 1, Y: -1, Z: 0})
	scene.Add(sun.LightNode())

	floor := NewMesh(NewPlaneGeometry(10, 10), NewStandardMaterial(WithColor(White), WithRoughness(1)))
	scene.Add(floor.MeshNode())

	caster := NewMesh(NewBoxGeometry(1.4, 1.4, 1.4), NewStandardMaterial(WithColor(RGB(0.8, 0.2, 0.2)), WithRoughness(1)))
	caster.MeshNode().SetPosition(Vec3{X: 0, Y: 1.5, Z: 0})
	scene.Add(caster.MeshNode())

	cam := NewPerspectiveCamera(50, 1, 0.1, 100)
	cam.CameraNode().SetPosition(Vec3{X: 0, Y: 7.5, Z: 4.0})
	cam.CameraNode().LookAt(Vec3{X: 0.8, Y: 0, Z: 0})
	return scene, cam
}

func countDarkenedPixels(unshadowed, shadowed []byte) (count, maxDrop int, dropX, dropY uint32, size uint32) {
	n := len(unshadowed) / 4
	// infer square size
	for s := uint32(1); int(s*s) <= n; s++ {
		if int(s*s) == n {
			size = s
			break
		}
	}
	for y := uint32(0); y < size; y++ {
		for x := uint32(0); x < size; x++ {
			a := pixelAt(unshadowed, size, x, y)
			b := pixelAt(shadowed, size, x, y)
			drop := int(a[0]) - int(b[0])
			if drop > 30 {
				count++
			}
			if drop > maxDrop {
				maxDrop = drop
				dropX, dropY = x, y
			}
		}
	}
	return
}

func TestDirectionalShadowCastsOnSoftwareBackend(t *testing.T) {
	const size uint32 = 64
	renderScene := func(shadows bool) []byte {
		provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
		r, err := NewRenderer(provider)
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetShadowsEnabled(shadows)
		r.SetShadowMapSize(256)
		r.SetSize(size, size)

		scene, cam := shadowTestScene()
		return renderSoftwareRGBA8(t, provider, r, scene, cam, size)
	}

	unshadowed := renderScene(false)
	shadowed := renderScene(true)

	darkCount, maxDrop, dx, dy, _ := countDarkenedPixels(unshadowed, shadowed)
	t.Logf("software shadow: darkened(>30)=%d pixels, maxDrop=%d at (%d,%d) unshadowed=%v shadowed=%v",
		darkCount, maxDrop, dx, dy, pixelAt(unshadowed, size, dx, dy), pixelAt(shadowed, size, dx, dy))
	if darkCount < 30 || maxDrop < 60 {
		t.Fatalf("expected clear shadow footprint on floor, got darkCount=%d maxDrop=%d", darkCount, maxDrop)
	}
}

func TestDirectionalShadowCastsOnVulkanGPU(t *testing.T) {
	inst, err := wgpu.CreateInstance(&wgpu.InstanceDescriptor{Backends: gputypes.BackendsVulkan})
	if err != nil {
		t.Skipf("no Vulkan instance: %v", err)
	}
	defer inst.Release()
	adapter, err := inst.RequestAdapter(&wgpu.RequestAdapterOptions{PowerPreference: gputypes.PowerPreferenceHighPerformance})
	if err != nil || adapter.Info().DeviceType == gputypes.DeviceTypeCPU {
		if adapter != nil {
			adapter.Release()
		}
		t.Skip("no hardware Vulkan GPU available")
	}
	defer adapter.Release()
	device, err := adapter.RequestDevice(nil)
	if err != nil {
		t.Fatalf("RequestDevice: %v", err)
	}
	defer device.Release()

	const size uint32 = 64
	renderGPU := func(shadows bool) []byte {
		r, err := NewRendererFromDevice(device, device.Queue(), gputypes.TextureFormatRGBA8Unorm)
		if err != nil {
			t.Fatalf("NewRendererFromDevice: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetShadowsEnabled(shadows)
		r.SetShadowMapSize(512)
		r.SetSize(size, size)

		scene, cam := shadowTestScene()
		target, err := device.CreateTexture(&wgpu.TextureDescriptor{
			Label: "sh_target", Size: wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
			MipLevelCount: 1, SampleCount: 1, Dimension: gputypes.TextureDimension2D,
			Format: gputypes.TextureFormatRGBA8Unorm, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc,
		})
		if err != nil {
			t.Fatalf("CreateTexture: %v", err)
		}
		defer target.Release()
		view, err := device.CreateTextureView(target, nil)
		if err != nil {
			t.Fatalf("CreateTextureView: %v", err)
		}
		defer view.Release()
		if err := r.Render(scene, cam, view); err != nil {
			t.Fatalf("Render(shadows=%v): %v", shadows, err)
		}
		bytesPerRow := ((size*4 + 255) / 256) * 256
		bufSize := uint64(bytesPerRow) * uint64(size)
		staging, _ := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "sh_rb", Size: bufSize, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		})
		defer staging.Release()
		enc, _ := device.CreateCommandEncoder(nil)
		enc.CopyTextureToBuffer(target, staging, []wgpu.BufferTextureCopy{{
			BufferLayout: wgpu.ImageDataLayout{BytesPerRow: bytesPerRow, RowsPerImage: size},
			TextureBase:  wgpu.ImageCopyTexture{Texture: target},
			Size:         wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
		}})
		cmd, _ := enc.Finish()
		_, _ = device.Queue().Submit(cmd)
		if err := staging.Map(context.Background(), wgpu.MapModeRead, 0, bufSize); err != nil {
			t.Fatalf("Map: %v", err)
		}
		rng, _ := staging.MappedRange(0, bufSize)
		raw := rng.Bytes()
		out := make([]byte, size*size*4)
		for y := uint32(0); y < size; y++ {
			copy(out[y*size*4:(y+1)*size*4], raw[y*bytesPerRow:y*bytesPerRow+size*4])
		}
		_ = staging.Unmap()
		return out
	}

	unshadowed := renderGPU(false)
	shadowed := renderGPU(true)
	darkCount, maxDrop, dx, dy, _ := countDarkenedPixels(unshadowed, shadowed)
	t.Logf("vulkan shadow: darkened(>30)=%d pixels, maxDrop=%d at (%d,%d) unshadowed=%v shadowed=%v",
		darkCount, maxDrop, dx, dy, pixelAt(unshadowed, size, dx, dy), pixelAt(shadowed, size, dx, dy))
	if darkCount < 30 || maxDrop < 60 {
		t.Fatalf("Vulkan expected clear shadow footprint on floor, got darkCount=%d maxDrop=%d", darkCount, maxDrop)
	}
}

func TestSoftShadowOnSoftwareBackend(t *testing.T) {
	const size uint32 = 64
	renderScene := func(shadows bool, softness float32) []byte {
		provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
		r, err := NewRenderer(provider)
		if err != nil {
			t.Fatalf("NewRenderer: %v", err)
		}
		defer r.Release()
		r.SetColorManagement(ColorManagementSRGB)
		r.SetShadowsEnabled(shadows)
		r.SetShadowMapSize(1024)
		r.SetShadowSoftness(softness)
		r.SetSize(size, size)

		scene, cam := shadowTestScene()
		return renderSoftwareRGBA8(t, provider, r, scene, cam, size)
	}
	partial := func(unshadowed, img []byte) int {
		n := 0
		for y := uint32(0); y < size; y++ {
			for x := uint32(0); x < size; x++ {
				u := pixelAt(unshadowed, size, x, y)
				if u[0] != u[1] || u[1] != u[2] || u[0] < 150 {
					continue // floor only: skip the red caster and the background
				}
				drop := int(u[0]) - int(pixelAt(img, size, x, y)[0])
				if drop > 8 && drop < 55 {
					n++
				}
			}
		}
		return n
	}

	unshadowed := renderScene(false, 0)
	hard := renderScene(true, 0)
	soft := renderScene(true, 0.05)

	darkCount, maxDrop, _, _, _ := countDarkenedPixels(unshadowed, soft)
	if darkCount < 25 || maxDrop < 50 {
		t.Fatalf("soft shadow lost its footprint: darkCount=%d maxDrop=%d", darkCount, maxDrop)
	}
	ph, ps := partial(unshadowed, hard), partial(unshadowed, soft)
	t.Logf("partially shadowed pixels: hard=%d soft=%d", ph, ps)
	if ps <= ph {
		t.Fatalf("soft shadow should have a wider penumbra than 3x3 PCF: soft=%d hard=%d", ps, ph)
	}
}
