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

// textureUpload describes what to put on the GPU for one texture.
type textureUpload struct {
	label        string
	format       gputypes.TextureFormat // RGBA8Unorm (read as stored) or RGBA8UnormSrgb (decoded to linear by the GPU, filtered in linear space)
	width        int
	height       int
	levels       [][]byte // RGBA8 pixels of every mip level, the full size first
	wrapU, wrapV TextureWrap
	mag, min     TextureFilter
	mipFilter    TextureFilter
	lodMin       float32
	lodMax       float32 // 0: no upper limit
}

// uploadTexture creates the GPU texture and sampler and writes every mip level.
func uploadTexture(device *wgpu.Device, queue *wgpu.Queue, d *textureUpload) (*gpuTexture, error) {
	size := wgpu.Extent3D{Width: uint32(d.width), Height: uint32(d.height), DepthOrArrayLayers: 1} //nolint:gosec // G115: Texture sizes are limited to 8192
	tex, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         d.label,
		Size:          size,
		MipLevelCount: uint32(len(d.levels)), //nolint:gosec // G115: at most 14 levels
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        d.format,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, fmt.Errorf("create texture: %w", err)
	}
	g := &gpuTexture{tex: tex}
	lw, lh := d.width, d.height
	for level, pixels := range d.levels {
		ext := wgpu.Extent3D{Width: uint32(lw), Height: uint32(lh), DepthOrArrayLayers: 1} //nolint:gosec // G115: Texture sizes are limited to 8192
		err = queue.WriteTexture(
			&wgpu.ImageCopyTexture{Texture: tex, MipLevel: uint32(level)},
			pixels,
			&wgpu.ImageDataLayout{BytesPerRow: uint32(lw) * 4, RowsPerImage: uint32(lh)}, //nolint:gosec // G115: Texture sizes are limited to 8192
			&ext,
		)
		if err != nil {
			g.release()
			return nil, fmt.Errorf("write texture level %d: %w", level, err)
		}
		lw, lh = max(lw/2, 1), max(lh/2, 1)
	}
	if g.view, err = device.CreateTextureView(tex, nil); err != nil {
		g.release()
		return nil, fmt.Errorf("create texture view: %w", err)
	}
	aniso := uint16(1)
	if len(d.levels) > 1 && d.mag == FilterLinear && d.min == FilterLinear && d.mipFilter == FilterLinear {
		aniso = 16
	}
	samplerDesc := &wgpu.SamplerDescriptor{
		Label:        d.label + "_sampler",
		AddressModeU: addressMode(d.wrapU),
		AddressModeV: addressMode(d.wrapV),
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    filterMode(d.mag),
		MinFilter:    filterMode(d.min),
		MipmapFilter: filterMode(d.mipFilter),
		LodMinClamp:  d.lodMin,
		LodMaxClamp:  d.lodMax,
		Anisotropy:   aniso,
	}
	g.sampler, err = device.CreateSampler(samplerDesc)
	if err != nil && aniso > 8 {
		samplerDesc.Anisotropy = 8
		g.sampler, err = device.CreateSampler(samplerDesc)
	}
	if err != nil && aniso > 1 {
		samplerDesc.Anisotropy = 1
		g.sampler, err = device.CreateSampler(samplerDesc)
	}
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
	levels := [][]byte{t.pixels}
	if !t.noMipmaps {
		// With color management the GPU decodes an sRGB texture before it filters, so the levels are averaged in
		// linear light to match; otherwise the stored values are averaged as they are.
		linear := r.colorManagement == ColorManagementSRGB && t.colorSpace == ColorSpaceSRGB
		levels = append(levels, generateMips(t.pixels, t.width, t.height, linear)...)
	}
	g, err := uploadTexture(device, queue, &textureUpload{
		label: "g3d_texture", format: r.textureFormat(t.colorSpace), width: t.width, height: t.height, levels: levels,
		wrapU: t.wrapU, wrapV: t.wrapV, mag: t.mag, min: t.min, mipFilter: t.mipFilter, lodMin: t.lodMin, lodMax: t.lodMax,
	})
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
	g, err := uploadTexture(device, queue, &textureUpload{
		label: "g3d_white", format: gputypes.TextureFormatRGBA8Unorm, width: 1, height: 1, levels: [][]byte{{255, 255, 255, 255}},
		wrapU: WrapClamp, wrapV: WrapClamp, mag: FilterLinear, min: FilterLinear, mipFilter: FilterNearest,
	})
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

// flatNormalTexture returns the 1x1 flat tangent-space normal (128,128,255,255 = (0,0,1))
// bound for materials without a normal map.
func (r *Renderer) flatNormalTexture(device *wgpu.Device, queue *wgpu.Queue) (*gpuTexture, error) {
	if r.defaultNormalTexture != nil {
		return r.defaultNormalTexture, nil
	}
	g, err := uploadTexture(device, queue, &textureUpload{
		label: "g3d_flat_normal", format: gputypes.TextureFormatRGBA8Unorm, width: 1, height: 1, levels: [][]byte{{128, 128, 255, 255}},
		wrapU: WrapClamp, wrapV: WrapClamp, mag: FilterLinear, min: FilterLinear, mipFilter: FilterNearest,
	})
	if err != nil {
		return nil, err
	}
	r.defaultNormalTexture = g
	return g, nil
}

// pbrMaterialTextures resolves metallic-roughness, occlusion, emissive, and normal GPU textures for mat,
// falling back to 1x1 defaults (white or flat normal) for any unset slot.
func (r *Renderer) pbrMaterialTextures(device *wgpu.Device, queue *wgpu.Queue, mat Material) (mr, occ, emissive, norm *gpuTexture, err error) {
	white, err := r.whiteTexture(device, queue)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	flatNorm, err := r.flatNormalTexture(device, queue)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	mr, occ, emissive, norm = white, white, white, flatNorm
	pm, ok := mat.(pbrMapped)
	if !ok {
		return mr, occ, emissive, norm, nil
	}
	if t := pm.MetallicRoughnessMap(); t != nil {
		if mr, err = r.ensureTexture(device, queue, t); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("metallic-roughness map: %w", err)
		}
	}
	if t := pm.OcclusionMap(); t != nil {
		if occ, err = r.ensureTexture(device, queue, t); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("occlusion map: %w", err)
		}
	}
	if t := pm.EmissiveMap(); t != nil {
		if emissive, err = r.ensureTexture(device, queue, t); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("emissive map: %w", err)
		}
	}
	if t := pm.NormalMap(); t != nil {
		if norm, err = r.ensureTexture(device, queue, t); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("normal map: %w", err)
		}
	}
	return mr, occ, emissive, norm, nil
}

// releaseTextures frees every cached GPU texture.
func (r *Renderer) releaseTextures() {
	for _, g := range r.textures {
		g.release()
	}
	r.textures = nil
	r.defaultTexture.release()
	r.defaultTexture = nil
	r.defaultNormalTexture.release()
	r.defaultNormalTexture = nil
}
