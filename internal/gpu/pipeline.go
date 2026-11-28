package gpu

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// PipelineKey identifies a unique render pipeline configuration. Pipelines with
// the same key share a single GPU pipeline object.
//
// The key captures all state that affects pipeline compilation:
//   - ShaderID: which shader module ("basic" or "standard")
//   - SurfaceFormat: the color target format (e.g., BGRA8Unorm)
//   - DepthFormat: the depth attachment format (e.g., Depth24Plus)
//   - DoubleSided: whether back-face culling is disabled
type PipelineKey struct {
	ShaderID      string
	SurfaceFormat gputypes.TextureFormat
	DepthFormat   gputypes.TextureFormat
	DoubleSided   bool
	SampleCount   uint32
}

// PipelineBundle holds a compiled render pipeline and its associated layouts.
// All resources in the bundle are released together via the PipelineCache.
type PipelineBundle struct {
	Pipeline       *wgpu.RenderPipeline
	FrameLayout    *wgpu.BindGroupLayout // group 0 — per-frame uniforms
	ObjectLayout   *wgpu.BindGroupLayout // group 1 — per-object + per-material uniforms
	PipelineLayout *wgpu.PipelineLayout
}

// PipelineCache creates and caches render pipelines keyed by PipelineKey.
// Pipelines are created on first request and reused for all subsequent frames.
type PipelineCache struct {
	device  *wgpu.Device
	shaders *ShaderCache
	cache   map[PipelineKey]*PipelineBundle
}

// NewPipelineCache creates a pipeline cache backed by the given device and shaders.
func NewPipelineCache(device *wgpu.Device, shaders *ShaderCache) *PipelineCache {
	return &PipelineCache{
		device:  device,
		shaders: shaders,
		cache:   make(map[PipelineKey]*PipelineBundle),
	}
}

// Get returns the cached pipeline bundle for the given key, creating it on first
// access. Returns an error if pipeline creation fails.
func (c *PipelineCache) Get(key PipelineKey) (*PipelineBundle, error) {
	if bundle, ok := c.cache[key]; ok {
		return bundle, nil
	}

	bundle, err := c.createPipeline(key)
	if err != nil {
		return nil, err
	}
	c.cache[key] = bundle
	return bundle, nil
}

// createPipeline compiles the render pipeline for the given key.
// Follows the gg/internal/gpu/convex_renderer.go:250-322 pattern.
func (c *PipelineCache) createPipeline(key PipelineKey) (*PipelineBundle, error) {
	shader := c.shaders.Get(key.ShaderID)
	if shader == nil {
		return nil, fmt.Errorf("g3d: unknown shader %q", key.ShaderID)
	}

	// Determine material uniform size from shader ID.
	// basic.wgsl MaterialUniforms = 16 bytes (vec4 color).
	// standard.wgsl MaterialUniforms = 64 bytes (vec4 color + 4 scalars + vec4 emissive/occ + vec4 normalScale).
	materialUniformSize := uint64(16)
	if key.ShaderID == ShaderStandard {
		materialUniformSize = 64
	}

	// Group 0: per-frame uniforms (FrameUniforms, 336 bytes).
	// StandardMaterial also binds the directional shadow depth map (binding 1) and comparison sampler (binding 2).
	frameEntries := []gputypes.BindGroupLayoutEntry{
		{
			Binding:    0,
			Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
			Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: FrameUniformsSize},
		},
	}
	if key.ShaderID == ShaderStandard {
		frameEntries = append(frameEntries,
			gputypes.BindGroupLayoutEntry{
				Binding:    1,
				Visibility: gputypes.ShaderStageFragment,
				Texture: &gputypes.TextureBindingLayout{
					SampleType:    gputypes.TextureSampleTypeDepth,
					ViewDimension: gputypes.TextureViewDimension2D,
				},
			},
			gputypes.BindGroupLayoutEntry{
				Binding:    2,
				Visibility: gputypes.ShaderStageFragment,
				Sampler:    &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeComparison},
			},
		)
	}
	frameLayout, err := c.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "g3d_frame_layout_" + key.ShaderID,
		Entries: frameEntries,
	})
	if err != nil {
		return nil, fmt.Errorf("g3d: create frame bind group layout: %w", err)
	}

	// Group 1: per-object uniforms (ObjectUniforms binding 0, MaterialUniforms binding 1),
	// the color map (binding 2) and its sampler (binding 3). StandardMaterial also binds
	// metallic-roughness (4,5), occlusion (6,7), and emissive (8,9) maps + samplers.
	objectEntries := []gputypes.BindGroupLayoutEntry{
		{
			Binding:    0,
			Visibility: gputypes.ShaderStageVertex,
			Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: ObjectUniformsSize},
		},
		{
			Binding:    1,
			Visibility: gputypes.ShaderStageFragment,
			Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: materialUniformSize},
		},
		{
			// The color map. A material without one gets a 1x1 white texture, so every draw binds a texture.
			Binding:    2,
			Visibility: gputypes.ShaderStageFragment,
			Texture: &gputypes.TextureBindingLayout{
				SampleType:    gputypes.TextureSampleTypeFloat,
				ViewDimension: gputypes.TextureViewDimension2D,
			},
		},
		{
			Binding:    3,
			Visibility: gputypes.ShaderStageFragment,
			Sampler:    &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering},
		},
	}
	if key.ShaderID == ShaderStandard {
		for _, pair := range [][2]uint32{{4, 5}, {6, 7}, {8, 9}, {10, 11}} {
			objectEntries = append(objectEntries,
				gputypes.BindGroupLayoutEntry{
					Binding:    pair[0],
					Visibility: gputypes.ShaderStageFragment,
					Texture: &gputypes.TextureBindingLayout{
						SampleType:    gputypes.TextureSampleTypeFloat,
						ViewDimension: gputypes.TextureViewDimension2D,
					},
				},
				gputypes.BindGroupLayoutEntry{
					Binding:    pair[1],
					Visibility: gputypes.ShaderStageFragment,
					Sampler:    &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering},
				},
			)
		}
	}
	objectLayout, err := c.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "g3d_object_layout_" + key.ShaderID,
		Entries: objectEntries,
	})
	if err != nil {
		frameLayout.Release()
		return nil, fmt.Errorf("g3d: create object bind group layout: %w", err)
	}

	pipeLayout, err := c.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "g3d_pipe_layout_" + key.ShaderID,
		BindGroupLayouts: []*wgpu.BindGroupLayout{frameLayout, objectLayout},
	})
	if err != nil {
		objectLayout.Release()
		frameLayout.Release()
		return nil, fmt.Errorf("g3d: create pipeline layout: %w", err)
	}

	pipeline, err := c.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "g3d_pipeline_" + key.ShaderID,
		Layout: pipeLayout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: shaderEntryVS,
			Buffers:    StandardVertexLayout(),
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: shaderEntryFS,
			Targets: []gputypes.ColorTargetState{
				{
					Format:    key.SurfaceFormat,
					WriteMask: gputypes.ColorWriteMaskAll,
				},
			},
		},
		Primitive:    DefaultPrimitive(key.DoubleSided),
		DepthStencil: OpaqueDepthStencil(key.DepthFormat),
		Multisample:  Multisample(key.SampleCount),
	})
	if err != nil {
		pipeLayout.Release()
		objectLayout.Release()
		frameLayout.Release()
		return nil, fmt.Errorf("g3d: create render pipeline %q: %w", key.ShaderID, err)
	}

	return &PipelineBundle{
		Pipeline:       pipeline,
		FrameLayout:    frameLayout,
		ObjectLayout:   objectLayout,
		PipelineLayout: pipeLayout,
	}, nil
}

// ShadowPipeline returns the cached depth-only shadow render pipeline for the given culling mode.
func (c *PipelineCache) ShadowPipeline(doubleSided bool) (*PipelineBundle, error) {
	key := PipelineKey{
		ShaderID:      ShaderShadow,
		SurfaceFormat: gputypes.TextureFormatUndefined,
		DepthFormat:   gputypes.TextureFormatDepth32Float,
		DoubleSided:   doubleSided,
		SampleCount:   1,
	}
	if bundle, ok := c.cache[key]; ok {
		return bundle, nil
	}
	shader := c.shaders.Get(ShaderShadow)
	if shader == nil {
		return nil, fmt.Errorf("g3d: shadow shader not compiled")
	}
	frameLayout, err := c.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "g3d_shadow_frame_layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: gputypes.ShaderStageVertex,
				Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: ShadowFrameUniformsSize},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("g3d: create shadow frame layout: %w", err)
	}
	objectLayout, err := c.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "g3d_shadow_object_layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: gputypes.ShaderStageVertex,
				Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, MinBindingSize: ObjectUniformsSize},
			},
		},
	})
	if err != nil {
		frameLayout.Release()
		return nil, fmt.Errorf("g3d: create shadow object layout: %w", err)
	}
	pipeLayout, err := c.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "g3d_shadow_pipe_layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{frameLayout, objectLayout},
	})
	if err != nil {
		objectLayout.Release()
		frameLayout.Release()
		return nil, fmt.Errorf("g3d: create shadow pipeline layout: %w", err)
	}
	ds := OpaqueDepthStencil(gputypes.TextureFormatDepth32Float)
	ds.DepthBias = 2
	ds.DepthBiasSlopeScale = 2.0
	pipeline, err := c.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "g3d_shadow_pipeline",
		Layout: pipeLayout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: shaderEntryVS,
			Buffers:    StandardVertexLayout(),
		},
		Primitive:    DefaultPrimitive(doubleSided),
		DepthStencil: ds,
		Multisample:  NoMultisample(),
	})
	if err != nil {
		pipeLayout.Release()
		objectLayout.Release()
		frameLayout.Release()
		return nil, fmt.Errorf("g3d: create shadow pipeline: %w", err)
	}
	bundle := &PipelineBundle{
		Pipeline:       pipeline,
		FrameLayout:    frameLayout,
		ObjectLayout:   objectLayout,
		PipelineLayout: pipeLayout,
	}
	c.cache[key] = bundle
	return bundle, nil
}

// Close releases all cached pipelines and layouts in reverse creation order.
func (c *PipelineCache) Close() {
	for key, bundle := range c.cache {
		bundle.Pipeline.Release()
		bundle.PipelineLayout.Release()
		bundle.ObjectLayout.Release()
		bundle.FrameLayout.Release()
		delete(c.cache, key)
	}
}
