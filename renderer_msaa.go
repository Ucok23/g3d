package g3d

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// effectiveSampleCount returns the active MSAA sample count (1 when unset).
func (r *Renderer) effectiveSampleCount() uint32 {
	if r.sampleCount <= 1 {
		return 1
	}
	return r.sampleCount
}

// SampleCount returns the configured MSAA sample count (1 = disabled, 4 = 4x MSAA).
func (r *Renderer) SampleCount() uint32 {
	return r.effectiveSampleCount()
}

// SetSampleCount configures multisample anti-aliasing (MSAA) for the renderer.
// WebGPU guarantees support for sample counts 1 (disabled, default) and 4 (4x MSAA).
// When sample count is 4, the renderer draws into an internal multisampled color
// and depth buffer and resolves into the single-sample target view passed to Render.
//
// If the underlying device does not support multisampled textures (for example
// wgpu's software backend), SetSampleCount restores the previous sample count
// and returns an error.
func (r *Renderer) SetSampleCount(count uint32) error {
	if count != 1 && count != 4 {
		return fmt.Errorf("g3d: unsupported MSAA sample count %d (supported: 1, 4)", count)
	}
	if count == r.effectiveSampleCount() {
		return nil
	}
	prev := r.effectiveSampleCount()
	r.sampleCount = count
	if r.width > 0 && r.height > 0 && r.gpuState.IsReady() {
		if err := r.recreateMSAATexture(); err != nil {
			r.sampleCount = prev
			_ = r.recreateMSAATexture()
			return err
		}
		r.recreateDepthTexture()
		if r.depthView == nil {
			r.sampleCount = prev
			_ = r.recreateMSAATexture()
			r.recreateDepthTexture()
			return fmt.Errorf("g3d: device does not support %dx MSAA depth attachment", count)
		}
	} else if count > 1 && r.gpuState.IsReady() {
		// Probe 1x1 multisampled texture creation so unsupported backends fail eagerly.
		probe, err := r.gpuState.Device().CreateTexture(&wgpu.TextureDescriptor{
			Label:         "g3d_msaa_probe",
			Size:          wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   count,
			Dimension:     gputypes.TextureDimension2D,
			Format:        r.surfaceFormat,
			Usage:         gputypes.TextureUsageRenderAttachment,
		})
		if err != nil {
			r.sampleCount = prev
			return fmt.Errorf("g3d: device does not support %dx MSAA: %w", count, err)
		}
		probe.Release()
	}
	return nil
}

func (r *Renderer) recreateMSAATexture() error {
	r.releaseMSAATexture()

	count := r.effectiveSampleCount()
	if count <= 1 || r.width == 0 || r.height == 0 || !r.gpuState.IsReady() {
		return nil
	}
	device := r.gpuState.Device()
	tex, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "g3d_msaa_color",
		Size: wgpu.Extent3D{
			Width:              r.width,
			Height:             r.height,
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   count,
		Dimension:     gputypes.TextureDimension2D,
		Format:        r.surfaceFormat,
		Usage:         gputypes.TextureUsageRenderAttachment,
	})
	if err != nil {
		return fmt.Errorf("g3d: create %dx MSAA color texture: %w", count, err)
	}
	view, err := device.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return fmt.Errorf("g3d: create %dx MSAA color view: %w", count, err)
	}
	r.msaaTexture = tex
	r.msaaView = view
	return nil
}

func (r *Renderer) releaseMSAATexture() {
	if r.msaaView != nil {
		r.msaaView.Release()
		r.msaaView = nil
	}
	if r.msaaTexture != nil {
		r.msaaTexture.Release()
		r.msaaTexture = nil
	}
}
