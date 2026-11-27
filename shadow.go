package g3d

import (
	"fmt"
	"math"

	"github.com/gogpu/g3d/internal/gpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

const (
	defaultShadowMapSize    uint32  = 2048
	minShadowMapSize        uint32  = 256
	maxShadowMapSize        uint32  = 4096
	defaultShadowBias       float32 = 0.0012
	defaultShadowNormalBias float32 = 0.03
)

type shadowState struct {
	enabled    bool
	mapSize    uint32
	bias       float32
	normalBias float32
	softness   float32 // tan of the light's angular radius; 0 = fixed 3x3 PCF

	texture      *wgpu.Texture
	view         *wgpu.TextureView
	allocatedDim uint32

	dummyTex     *wgpu.Texture
	dummyView    *wgpu.TextureView
	dummyCleared bool

	sampler *wgpu.Sampler

	frameUniformBuf   *wgpu.Buffer
	frameBindGroups   []*wgpu.BindGroup
	objectUniformBufs []*wgpu.Buffer
	objectBindGroups  []*wgpu.BindGroup
	pendingBindGroups []*wgpu.BindGroup
}

// SetShadowsEnabled turns directional shadow mapping on or off (default: false).
// When enabled, the first visible DirectionalLight casts soft PCF shadows onto StandardMaterial surfaces.
func (r *Renderer) SetShadowsEnabled(enabled bool) {
	r.shadows.enabled = enabled
}

// ShadowsEnabled reports whether directional shadow mapping is enabled.
func (r *Renderer) ShadowsEnabled() bool {
	return r.shadows.enabled
}

// SetShadowMapSize sets the square resolution (in texels) of the directional shadow map.
// Clamped to [256..4096]; defaults to 2048.
func (r *Renderer) SetShadowMapSize(size uint32) {
	if size < minShadowMapSize {
		size = minShadowMapSize
	}
	if size > maxShadowMapSize {
		size = maxShadowMapSize
	}
	r.shadows.mapSize = size
}

// ShadowMapSize returns the configured shadow map resolution in texels (default 2048).
func (r *Renderer) ShadowMapSize() uint32 {
	if r.shadows.mapSize == 0 {
		return defaultShadowMapSize
	}
	return r.shadows.mapSize
}

// SetShadowSoftness enables contact-hardening (PCSS-style) soft shadows for the directional light.
// softness is tan(angular radius of the light): the penumbra grows by 2*softness meters per meter
// between receiver and occluder (the real sun is ~0.0047; ~0.015 is a pleasant architectural look).
// 0 (default) keeps the fixed 3x3 PCF kernel, which is also what the software backend uses.
// Values are clamped to [0..0.1].
func (r *Renderer) SetShadowSoftness(softness float32) {
	if softness < 0 {
		softness = 0
	}
	if softness > 0.1 {
		softness = 0.1
	}
	r.shadows.softness = softness
}

// ShadowSoftness returns the configured light softness (0 = fixed 3x3 PCF).
func (r *Renderer) ShadowSoftness() float32 { return r.shadows.softness }

// SetShadowBias sets the depth comparison bias used to prevent shadow acne (default 0.0012).
func (r *Renderer) SetShadowBias(bias float32) {
	r.shadows.bias = bias
}

// SetShadowNormalBias sets the world-space normal offset (in meters) used to prevent shadow acne
// on sloped surfaces (default 0.03).
func (r *Renderer) SetShadowNormalBias(bias float32) {
	r.shadows.normalBias = bias
}

func (r *Renderer) shadowBias() float32 {
	if r.shadows.bias <= 0 {
		return defaultShadowBias
	}
	return r.shadows.bias
}

func (r *Renderer) shadowNormalBias() float32 {
	if r.shadows.normalBias <= 0 {
		return defaultShadowNormalBias
	}
	return r.shadows.normalBias
}

func (r *Renderer) ensureShadowSampler(device *wgpu.Device) (*wgpu.Sampler, error) {
	if r.shadows.sampler != nil {
		return r.shadows.sampler, nil
	}
	smp, err := device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "g3d_shadow_sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeNearest,
		Compare:      gputypes.CompareFunctionLessEqual,
	})
	if err != nil {
		return nil, fmt.Errorf("g3d: create shadow sampler: %w", err)
	}
	r.shadows.sampler = smp
	return smp, nil
}

func (r *Renderer) ensureDummyShadowTexture(device *wgpu.Device, encoder *wgpu.CommandEncoder) (*wgpu.TextureView, error) {
	if r.shadows.dummyView == nil {
		tex, err := device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         "g3d_shadow_dummy",
			Size:          wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   1,
			Dimension:     gputypes.TextureDimension2D,
			Format:        gputypes.TextureFormatDepth32Float,
			Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding,
		})
		if err != nil {
			return nil, fmt.Errorf("g3d: create dummy shadow texture: %w", err)
		}
		view, err := device.CreateTextureView(tex, nil)
		if err != nil {
			tex.Release()
			return nil, fmt.Errorf("g3d: create dummy shadow view: %w", err)
		}
		r.shadows.dummyTex = tex
		r.shadows.dummyView = view
		r.shadows.dummyCleared = false
	}
	if !r.shadows.dummyCleared && encoder != nil {
		rp, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
			Label: "g3d_shadow_dummy_clear",
			DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
				View:            r.shadows.dummyView,
				DepthLoadOp:     gputypes.LoadOpClear,
				DepthStoreOp:    gputypes.StoreOpStore,
				DepthClearValue: 1.0,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("g3d: clear dummy shadow texture: %w", err)
		}
		if err := rp.End(); err != nil {
			return nil, err
		}
		r.shadows.dummyCleared = true
	}
	return r.shadows.dummyView, nil
}

func (r *Renderer) ensureShadowMap(device *wgpu.Device) (*wgpu.TextureView, uint32, error) {
	dim := r.ShadowMapSize()
	if r.shadows.view != nil && r.shadows.allocatedDim == dim {
		return r.shadows.view, dim, nil
	}
	if r.shadows.view != nil {
		r.shadows.view.Release()
		r.shadows.view = nil
	}
	if r.shadows.texture != nil {
		r.shadows.texture.Release()
		r.shadows.texture = nil
	}
	tex, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "g3d_shadow_map",
		Size:          wgpu.Extent3D{Width: dim, Height: dim, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatDepth32Float,
		Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("g3d: create shadow map (%dx%d): %w", dim, dim, err)
	}
	view, err := device.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return nil, 0, fmt.Errorf("g3d: create shadow map view: %w", err)
	}
	r.shadows.texture = tex
	r.shadows.view = view
	r.shadows.allocatedDim = dim
	return view, dim, nil
}

func (r *Renderer) ensureShadowFrameBuffer(device *wgpu.Device) error {
	if r.shadows.frameUniformBuf != nil {
		return nil
	}
	buf, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "g3d_shadow_frame_uniforms",
		Size:  gpu.ShadowFrameUniformsSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return err
	}
	r.shadows.frameUniformBuf = buf
	return nil
}

func (r *Renderer) ensureShadowObjectPool(device *wgpu.Device, idx int) error {
	for idx >= len(r.shadows.objectUniformBufs) {
		n := len(r.shadows.objectUniformBufs)
		buf, err := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: fmt.Sprintf("g3d_shadow_obj_%d", n),
			Size:  gpu.ObjectUniformsSize,
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return err
		}
		r.shadows.objectUniformBufs = append(r.shadows.objectUniformBufs, buf)
	}
	return nil
}

// recordShadowPass renders all visible opaque meshes into the directional shadow map
// and populates frameData.LightViewProjection + frameData.ShadowParams.
// Returns the TextureView to bind as @group(0) @binding(1) in the forward pass.
func (r *Renderer) recordShadowPass(
	encoder *wgpu.CommandEncoder,
	scene *Scene,
	frameData *gpu.FrameUniformsData,
) (*wgpu.TextureView, *wgpu.Sampler, error) {
	device := r.gpuState.Device()
	queue := r.gpuState.Queue()

	sampler, err := r.ensureShadowSampler(device)
	if err != nil {
		return nil, nil, err
	}

	if !r.shadows.enabled {
		frameData.ShadowParams = [4]float32{}
		frameData.ShadowSoft = [4]float32{}
		dummy, err := r.ensureDummyShadowTexture(device, encoder)
		return dummy, sampler, err
	}

	// Find the first directional light in frameData.Lights.
	dirIdx := -1
	var lightDir Vec3
	for i := uint32(0); i < frameData.LightCount; i++ {
		if frameData.Lights[i].LightType == uint32(LightKindDirectional) && frameData.Lights[i].Intensity > 0 {
			dirIdx = int(i)
			d := frameData.Lights[i].Direction
			lightDir = Vec3{X: d[0], Y: d[1], Z: d[2]}.Normalize()
			break
		}
	}
	if dirIdx < 0 {
		frameData.ShadowParams = [4]float32{}
		frameData.ShadowSoft = [4]float32{}
		dummy, err := r.ensureDummyShadowTexture(device, encoder)
		return dummy, sampler, err
	}

	// Collect all visible opaque shadow casters and their union world AABB.
	var casters []*Mesh
	var sceneBox AABB
	hasBox := false
	scene.TraverseVisible(func(node *Node) {
		mesh := meshFromNode(node)
		if mesh == nil || mesh.geometry == nil || mesh.material == nil {
			return
		}
		if mesh.material.RenderBucket() != RenderBucketOpaque {
			return
		}
		casters = append(casters, mesh)
		wb := mesh.WorldBoundingBox()
		if !hasBox {
			sceneBox = wb
			hasBox = true
		} else {
			sceneBox = sceneBox.Merge(wb)
		}
	})
	if len(casters) == 0 || !hasBox {
		frameData.ShadowParams = [4]float32{}
		frameData.ShadowSoft = [4]float32{}
		dummy, err := r.ensureDummyShadowTexture(device, encoder)
		return dummy, sampler, err
	}

	shadowView, dim, err := r.ensureShadowMap(device)
	if err != nil {
		return nil, nil, err
	}
	if err := r.ensureShadowFrameBuffer(device); err != nil {
		return nil, nil, fmt.Errorf("g3d: shadow frame buffer: %w", err)
	}

	lightVP, worldWidth, worldHeight, depthRange := computeDirectionalShadowMatrix(sceneBox, lightDir, dim)
	if err := queue.WriteBuffer(r.shadows.frameUniformBuf, 0, gpu.PackMat4([16]float32(lightVP))); err != nil {
		return nil, nil, fmt.Errorf("g3d: write shadow frame uniforms: %w", err)
	}

	frameData.LightViewProjection = [16]float32(lightVP)
	frameData.ShadowParams = [4]float32{
		r.shadowBias(),
		r.shadowNormalBias(),
		1.0 / float32(dim),
		float32(dirIdx + 1),
	}
	frameData.ShadowSoft = [4]float32{worldWidth, depthRange, r.shadows.softness, worldHeight}

	rp, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		Label: "g3d_shadow",
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            shadowView,
			DepthLoadOp:     gputypes.LoadOpClear,
			DepthStoreOp:    gputypes.StoreOpStore,
			DepthClearValue: 1.0,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("g3d: begin shadow pass: %w", err)
	}

	var curDS bool
	pipeSet := false
	var curBundle *gpu.PipelineBundle

	for idx, mesh := range casters {
		ds := mesh.material.DoubleSided()
		if !pipeSet || ds != curDS {
			bundle, err := r.gpuState.Pipelines().ShadowPipeline(ds)
			if err != nil {
				_ = rp.End()
				return nil, nil, err
			}
			curBundle = bundle
			curDS = ds
			pipeSet = true
			rp.SetPipeline(bundle.Pipeline)

			fbg, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Label:  "g3d_shadow_frame_bg",
				Layout: bundle.FrameLayout,
				Entries: []wgpu.BindGroupEntry{
					{Binding: 0, Buffer: r.shadows.frameUniformBuf, Size: gpu.ShadowFrameUniformsSize},
				},
			})
			if err != nil {
				_ = rp.End()
				return nil, nil, fmt.Errorf("g3d: shadow frame bind group: %w", err)
			}
			r.shadows.frameBindGroups = append(r.shadows.frameBindGroups, fbg)
			rp.SetBindGroup(0, fbg, nil)
		}

		if err := r.ensureShadowObjectPool(device, idx); err != nil {
			_ = rp.End()
			return nil, nil, err
		}
		worldMat := mesh.node.WorldMatrix()
		objBytes := gpu.PackObjectUniforms(&gpu.ObjectUniformsData{
			Model: [16]float32(worldMat),
		})
		if err := queue.WriteBuffer(r.shadows.objectUniformBufs[idx], 0, objBytes); err != nil {
			_ = rp.End()
			return nil, nil, err
		}
		if idx < len(r.shadows.objectBindGroups) && r.shadows.objectBindGroups[idx] != nil {
			r.shadows.pendingBindGroups = append(r.shadows.pendingBindGroups, r.shadows.objectBindGroups[idx])
			r.shadows.objectBindGroups[idx] = nil
		}
		objBG, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  "g3d_shadow_obj_bg",
			Layout: curBundle.ObjectLayout,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: r.shadows.objectUniformBufs[idx], Size: gpu.ObjectUniformsSize},
			},
		})
		if err != nil {
			_ = rp.End()
			return nil, nil, err
		}
		for len(r.shadows.objectBindGroups) <= idx {
			r.shadows.objectBindGroups = append(r.shadows.objectBindGroups, nil)
		}
		r.shadows.objectBindGroups[idx] = objBG
		rp.SetBindGroup(1, objBG, nil)

		geom := mesh.geometry
		vertBuf, err := r.ensureVertexBuffer(device, geom)
		if err != nil {
			_ = rp.End()
			return nil, nil, err
		}
		if len(geom.Indices()) == 0 {
			vc := geom.VertexCount()
			if vc > 0 && uint64(vc) <= math.MaxUint32 {
				rp.SetVertexBuffer(0, vertBuf, 0)
				rp.Draw(gputypes.DrawArgs{VertexCount: uint32(vc), InstanceCount: 1})
			}
			continue
		}
		idxBuf, idxCount, err := r.ensureIndexBuffer(device, geom)
		if err != nil {
			_ = rp.End()
			return nil, nil, err
		}
		rp.SetVertexBuffer(0, vertBuf, 0)
		rp.SetIndexBuffer(idxBuf, gputypes.IndexFormatUint32, 0)
		rp.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: idxCount, InstanceCount: 1})
	}

	if err := rp.End(); err != nil {
		return nil, nil, fmt.Errorf("g3d: end shadow pass: %w", err)
	}
	return shadowView, sampler, nil
}

// computeDirectionalShadowMatrix fits a tight texel-snapped WebGPU orthographic projection around sceneBox.
func computeDirectionalShadowMatrix(box AABB, lightDir Vec3, mapSize uint32) (Mat4, float32, float32, float32) {
	center := box.Center()
	size := box.Size()
	radius := size.Length() * 0.5
	if radius < 1.0 {
		radius = 1.0
	}
	dir := lightDir.Normalize()
	up := Vec3{0, 1, 0}
	if absFloat32(dir.Y) > 0.95 {
		up = Vec3{0, 0, 1}
	}
	eye := center.Sub(dir.Scale(radius * 2.5))
	view := Mat4LookAt(eye, center, up)

	// Transform all 8 AABB corners into light view space.
	minX, minY, minZ := float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY, maxZ := float32(-math.MaxFloat32), float32(-math.MaxFloat32), float32(-math.MaxFloat32)
	xs := [2]float32{box.Min.X, box.Max.X}
	ys := [2]float32{box.Min.Y, box.Max.Y}
	zs := [2]float32{box.Min.Z, box.Max.Z}
	for _, x := range xs {
		for _, y := range ys {
			for _, z := range zs {
				p := view.MulVec4(Vec4{X: x, Y: y, Z: z, W: 1})
				if p.X < minX {
					minX = p.X
				}
				if p.X > maxX {
					maxX = p.X
				}
				if p.Y < minY {
					minY = p.Y
				}
				if p.Y > maxY {
					maxY = p.Y
				}
				// In right-handed view space, distance in front of the light is -p.Z.
				dist := -p.Z
				if dist < minZ {
					minZ = dist
				}
				if dist > maxZ {
					maxZ = dist
				}
			}
		}
	}

	// Pad XY slightly and snap to texel increments to prevent edge shimmer.
	padXY := max(maxX-minX, maxY-minY) * 0.06
	if padXY < 0.25 {
		padXY = 0.25
	}
	minX -= padXY
	maxX += padXY
	minY -= padXY
	maxY += padXY
	if mapSize > 0 {
		w := maxX - minX
		h := maxY - minY
		tx := w / float32(mapSize)
		ty := h / float32(mapSize)
		if tx > 1e-6 && ty > 1e-6 {
			minX = float32(math.Floor(float64(minX/tx))) * tx
			maxX = minX + w
			minY = float32(math.Floor(float64(minY/ty))) * ty
			maxY = minY + h
		}
	}
	near := minZ - radius*0.25
	if near < 0.05 {
		near = 0.05
	}
	far := maxZ + radius*0.25
	if far <= near+0.1 {
		far = near + 1.0
	}
	proj := Mat4Ortho(minX, maxX, minY, maxY, near, far)
	return proj.Mul(view), maxX - minX, maxY - minY, far - near
}

func (r *Renderer) releaseShadowPendingBindGroups() {
	for _, bg := range r.shadows.pendingBindGroups {
		bg.Release()
	}
	r.shadows.pendingBindGroups = r.shadows.pendingBindGroups[:0]
	for _, bg := range r.shadows.frameBindGroups {
		bg.Release()
	}
	r.shadows.frameBindGroups = r.shadows.frameBindGroups[:0]
}

func (r *Renderer) releaseShadows() {
	for _, bg := range r.shadows.pendingBindGroups {
		bg.Release()
	}
	r.shadows.pendingBindGroups = nil
	for _, bg := range r.shadows.frameBindGroups {
		bg.Release()
	}
	r.shadows.frameBindGroups = nil
	for _, bg := range r.shadows.objectBindGroups {
		if bg != nil {
			bg.Release()
		}
	}
	r.shadows.objectBindGroups = nil

	if r.shadows.frameUniformBuf != nil {
		r.shadows.frameUniformBuf.Release()
		r.shadows.frameUniformBuf = nil
	}
	for _, buf := range r.shadows.objectUniformBufs {
		buf.Release()
	}
	r.shadows.objectUniformBufs = nil

	if r.shadows.view != nil {
		r.shadows.view.Release()
		r.shadows.view = nil
	}
	if r.shadows.texture != nil {
		r.shadows.texture.Release()
		r.shadows.texture = nil
	}
	r.shadows.allocatedDim = 0
	if r.shadows.dummyView != nil {
		r.shadows.dummyView.Release()
		r.shadows.dummyView = nil
	}
	if r.shadows.dummyTex != nil {
		r.shadows.dummyTex.Release()
		r.shadows.dummyTex = nil
	}
	r.shadows.dummyCleared = false
	if r.shadows.sampler != nil {
		r.shadows.sampler.Release()
		r.shadows.sampler = nil
	}
}
