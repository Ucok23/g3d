//go:build !rust && !(js && wasm) && !android

package g3d

import (
	"context"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/vulkan"
)

func TestSetSampleCountValidationAndSoftwareFallback(t *testing.T) {
	provider := newSoftwareProvider(t, gputypes.TextureFormatRGBA8Unorm)
	renderer, err := NewRenderer(provider)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer renderer.Release()
	renderer.SetSize(64, 64)

	if got := renderer.SampleCount(); got != 1 {
		t.Fatalf("default SampleCount = %d, want 1", got)
	}
	for _, bad := range []uint32{0, 2, 3, 5, 16} {
		if err := renderer.SetSampleCount(bad); err == nil {
			t.Errorf("SetSampleCount(%d) succeeded, want error", bad)
		}
	}
	// Software backend does not support SampleCount > 1; SetSampleCount(4) must fail and restore 1.
	if err := renderer.SetSampleCount(4); err == nil {
		t.Fatalf("SetSampleCount(4) on software backend succeeded, want error")
	}
	if got := renderer.SampleCount(); got != 1 {
		t.Fatalf("after failed SetSampleCount(4), SampleCount = %d, want 1", got)
	}
	// Renderer must still render cleanly at 1x after the rollback.
	scene := NewScene()
	scene.SetBackground(Black)
	scene.Add(NewMesh(NewBoxGeometry(1, 1, 1), NewBasicMaterial(WithColor(White))).MeshNode())
	cam := NewPerspectiveCamera(60, 1, 0.1, 100)
	cam.CameraNode().SetPosition(Vec3{Z: 3})
	px := renderSoftwareRGBA8(t, provider, renderer, scene, cam, 64)
	if got := pixelAt(px, 64, 32, 32); !isColor(got, 255, 255, 255) {
		t.Fatalf("center after MSAA rollback = %v, want white", got)
	}
}

// TestMSAA4xSmoothsDiagonalEdgeOnVulkan verifies 4x MSAA resolve on a hardware Vulkan GPU when available.
func TestMSAA4xSmoothsDiagonalEdgeOnVulkan(t *testing.T) {
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
	renderWithSamples := func(samples uint32) []byte {
		r, err := NewRendererFromDevice(device, device.Queue(), gputypes.TextureFormatRGBA8Unorm)
		if err != nil {
			t.Fatalf("NewRendererFromDevice: %v", err)
		}
		defer r.Release()
		r.SetSize(size, size)
		if err := r.SetSampleCount(samples); err != nil {
			if samples == 8 {
				return nil // 8x is adapter-dependent; callers fall back to 4x
			}
			t.Fatalf("SetSampleCount(%d): %v", samples, err)
		}
		scene := NewScene()
		scene.SetBackground(Black)
		mesh := NewMesh(facingQuad(1), NewBasicMaterial(WithColor(White)))
		mesh.MeshNode().SetRotation(Euler{Z: Radians(27)})
		scene.Add(mesh.MeshNode())
		cam := NewPerspectiveCamera(60, 1, 0.1, 100)
		cam.CameraNode().SetPosition(Vec3{Z: 3})

		target, err := device.CreateTexture(&wgpu.TextureDescriptor{
			Label: "msaa_target", Size: wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
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
			t.Fatalf("Render(%dx): %v", samples, err)
		}
		bufSize := uint64(size) * uint64(size) * 4
		staging, err := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "msaa_rb", Size: bufSize, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			t.Fatalf("CreateBuffer: %v", err)
		}
		defer staging.Release()
		enc, _ := device.CreateCommandEncoder(nil)
		enc.CopyTextureToBuffer(target, staging, []wgpu.BufferTextureCopy{{
			BufferLayout: wgpu.ImageDataLayout{BytesPerRow: size * 4, RowsPerImage: size},
			TextureBase:  wgpu.ImageCopyTexture{Texture: target},
			Size:         wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
		}})
		cmd, _ := enc.Finish()
		_, _ = device.Queue().Submit(cmd)
		if err := staging.Map(context.Background(), wgpu.MapModeRead, 0, bufSize); err != nil {
			t.Fatalf("Map: %v", err)
		}
		rng, _ := staging.MappedRange(0, bufSize)
		out := append([]byte(nil), rng.Bytes()...)
		_ = staging.Unmap()
		return out
	}

	px1 := renderWithSamples(1)
	px4 := renderWithSamples(4)
	px8 := renderWithSamples(8)

	countBlended := func(px []byte) int {
		n := 0
		for i := 0; i < len(px); i += 4 {
			if px[i] > 10 && px[i] < 245 {
				n++
			}
		}
		return n
	}
	b1, b4 := countBlended(px1), countBlended(px4)
	if b1 != 0 {
		t.Errorf("1x render had %d intermediate edge pixels, want 0", b1)
	}
	if b4 < 20 {
		t.Errorf("4x MSAA render had only %d intermediate edge pixels, want >= 20", b4)
	}
	if px8 == nil {
		t.Log("8x MSAA not supported by this adapter (callers fall back to 4x)")
	} else if b8 := countBlended(px8); b8 < 20 {
		t.Errorf("8x MSAA render had only %d intermediate edge pixels, want >= 20", b8)
	} else {
		t.Logf("8x MSAA supported: %d intermediate edge pixels (4x: %d)", b8, b4)
	}
}
