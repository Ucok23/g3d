package g3d

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// gpuTexture is the GPU side of a Texture: the texture, a view of it and the sampler that reads it.
type gpuTexture struct {
	tex     *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler
}

func (g *gpuTexture) release() {
	if g == nil {
		return
	}
	if g.sampler != nil {
		g.sampler.Release()
	}
	if g.view != nil {
		g.view.Release()
	}
	if g.tex != nil {
		g.tex.Release()
	}
}

func addressMode(w TextureWrap) gputypes.AddressMode {
	switch w {
	case WrapRepeat:
		return gputypes.AddressModeRepeat
	case WrapMirror:
		return gputypes.AddressModeMirrorRepeat
	default:
		return gputypes.AddressModeClampToEdge
	}
}

func filterMode(f TextureFilter) gputypes.FilterMode {
	if f == FilterNearest {
		return gputypes.FilterModeNearest
	}
	return gputypes.FilterModeLinear
}

// uploadTexture creates the GPU texture and sampler for t and writes its pixels.
// format is RGBA8Unorm (read as stored) or RGBA8UnormSrgb (decoded to linear by the GPU, and filtered in linear space).
func uploadTexture(device *wgpu.Device, queue *wgpu.Queue, label string, format gputypes.TextureFormat, w, h int, pixels []byte, wrapU, wrapV TextureWrap, mag, minification TextureFilter) (*gpuTexture, error) {
	size := wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1} //nolint:gosec // G115: Texture sizes are limited to 8192
	tex, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         label,
		Size:          size,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        format,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, fmt.Errorf("create texture: %w", err)
	}
	g := &gpuTexture{tex: tex}
	err = queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: tex},
		pixels,
		&wgpu.ImageDataLayout{BytesPerRow: uint32(w) * 4, RowsPerImage: uint32(h)}, //nolint:gosec // G115: Texture sizes are limited to 8192
		&size,
	)
	if err != nil {
		g.release()
		return nil, fmt.Errorf("write texture: %w", err)
	}
	if g.view, err = device.CreateTextureView(tex, nil); err != nil {
		g.release()
		return nil, fmt.Errorf("create texture view: %w", err)
	}
	g.sampler, err = device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        label + "_sampler",
		AddressModeU: addressMode(wrapU),
		AddressModeV: addressMode(wrapV),
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    filterMode(mag),
		MinFilter:    filterMode(minification),
		MipmapFilter: gputypes.FilterModeNearest, // one mip level for now
	})
	if err != nil {
		g.release()
		return nil, fmt.Errorf("create sampler: %w", err)
	}
	return g, nil
}

// ensureTexture returns the cached GPU texture for t, uploading it on first use.
func (r *Renderer) ensureTexture(device *wgpu.Device, queue *wgpu.Queue, t *Texture) (*gpuTexture, error) {
	if g, ok := r.textures[t]; ok {
		return g, nil
	}
	g, err := uploadTexture(device, queue, "g3d_texture", r.textureFormat(t.colorSpace), t.width, t.height, t.pixels, t.wrapU, t.wrapV, t.mag, t.min)
	if err != nil {
		return nil, err
	}
	if r.textures == nil {
		r.textures = make(map[*Texture]*gpuTexture)
	}
	r.textures[t] = g
	return g, nil
}

// whiteTexture returns the 1x1 white texture bound for materials without a color map, so that
// multiplying by the texel changes nothing.
func (r *Renderer) whiteTexture(device *wgpu.Device, queue *wgpu.Queue) (*gpuTexture, error) {
	if r.defaultTexture != nil {
		return r.defaultTexture, nil
	}
	g, err := uploadTexture(device, queue, "g3d_white", gputypes.TextureFormatRGBA8Unorm, 1, 1, []byte{255, 255, 255, 255}, WrapClamp, WrapClamp, FilterLinear, FilterLinear)
	if err != nil {
		return nil, err
	}
	r.defaultTexture = g
	return g, nil
}

// materialTexture returns the GPU texture a draw of mat binds: its color map, or the white texture.
func (r *Renderer) materialTexture(device *wgpu.Device, queue *wgpu.Queue, mat Material) (*gpuTexture, error) {
	if m, ok := mat.(mapped); ok {
		if t := m.Map(); t != nil {
			return r.ensureTexture(device, queue, t)
		}
	}
	return r.whiteTexture(device, queue)
}

// releaseTextures frees every cached GPU texture.
func (r *Renderer) releaseTextures() {
	for _, g := range r.textures {
		g.release()
	}
	r.textures = nil
	r.defaultTexture.release()
	r.defaultTexture = nil
}
