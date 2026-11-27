package g3d

import (
	"encoding/binary"
	"math"
)

// StandardMaterial is a PBR material following the GLTF 2.0 metallic-roughness model.
// Phase 1 uses Blinn-Phong approximation; full PBR in Phase 2.
//
// The GPU uniform layout is 32 bytes:
//
//	offset  0: color RGBA        [4]float32  (16 bytes)
//	offset 16: metallic          float32     (4 bytes)
//	offset 20: roughness         float32     (4 bytes)
//	offset 24: alpha_cutoff      float32     (4 bytes)
//	offset 28: _padding          float32     (4 bytes)
//	                                         Total: 32 bytes
type StandardMaterial struct {
	color                Color
	metallic             float32
	roughness            float32
	emissive             Color
	opacity              float32
	alphaMode            AlphaMode
	alphaCutoff          float32
	doubleSided          bool
	wireframe            bool
	colorMap             *Texture
	metallicRoughnessMap *Texture
	occlusionMap         *Texture
	occlusionStrength    float32
	emissiveMap          *Texture
}

// NewStandardMaterial creates a PBR material with the given options.
// Defaults: color=White, metallic=0, roughness=0.5, emissive=Black,
// opacity=1, alphaMode=Opaque, alphaCutoff=0.5, occlusionStrength=1.
func NewStandardMaterial(opts ...MaterialOption) *StandardMaterial {
	m := &StandardMaterial{
		color:             White,
		metallic:          0,
		roughness:         0.5,
		emissive:          Black,
		opacity:           1,
		alphaMode:         AlphaModeOpaque,
		alphaCutoff:       0.5,
		occlusionStrength: 1,
	}
	for _, opt := range opts {
		opt.applyStandard(m)
	}
	return m
}

// ShaderID returns "standard", identifying the PBR shader pipeline.
func (m *StandardMaterial) ShaderID() string { return shaderStandard }

// RenderBucket returns the appropriate bucket based on AlphaMode:
//   - AlphaModeOpaque, AlphaModeMask -> RenderBucketOpaque
//   - AlphaModeBlend -> RenderBucketTransparent
func (m *StandardMaterial) RenderBucket() RenderBucket {
	if m.alphaMode == AlphaModeBlend {
		return RenderBucketTransparent
	}
	return RenderBucketOpaque
}

// DoubleSided reports whether back-face culling is disabled.
func (m *StandardMaterial) DoubleSided() bool { return m.doubleSided }

// Map returns the color map multiplied with the base color, or nil.
func (m *StandardMaterial) Map() *Texture { return m.colorMap }

// MetallicRoughnessMap returns the metallic-roughness texture (G=roughness, B=metallic), or nil.
func (m *StandardMaterial) MetallicRoughnessMap() *Texture { return m.metallicRoughnessMap }

// OcclusionMap returns the ambient occlusion texture (R channel), or nil.
func (m *StandardMaterial) OcclusionMap() *Texture { return m.occlusionMap }

// OcclusionStrength returns the ambient occlusion strength in [0, 1].
func (m *StandardMaterial) OcclusionStrength() float32 { return m.occlusionStrength }

// EmissiveMap returns the emissive texture, or nil.
func (m *StandardMaterial) EmissiveMap() *Texture { return m.emissiveMap }

// Wireframe reports whether the material renders as wireframe.
func (m *StandardMaterial) Wireframe() bool { return m.wireframe }

// Color returns the base color.
func (m *StandardMaterial) Color() Color { return m.color }

// Metallic returns the metallic factor.
func (m *StandardMaterial) Metallic() float32 { return m.metallic }

// Roughness returns the roughness factor.
func (m *StandardMaterial) Roughness() float32 { return m.roughness }

// Emissive returns the emissive color.
func (m *StandardMaterial) Emissive() Color { return m.emissive }

// Opacity returns the opacity value.
func (m *StandardMaterial) Opacity() float32 { return m.opacity }

// AlphaMode returns the alpha handling mode.
func (m *StandardMaterial) AlphaMode() AlphaMode { return m.alphaMode }

// AlphaCutoff returns the alpha test threshold for AlphaModeMask.
func (m *StandardMaterial) AlphaCutoff() float32 { return m.alphaCutoff }

// UniformData returns 48 bytes encoding the material properties for GPU upload.
// The alpha component of color is pre-multiplied with opacity.
//
// Layout (matches WGSL MaterialUniforms struct):
//
//	offset  0: R             float32
//	offset  4: G             float32
//	offset  8: B             float32
//	offset 12: A             float32 (color.A * opacity)
//	offset 16: metallic      float32
//	offset 20: roughness     float32
//	offset 24: alpha_cutoff  float32
//	offset 28: double_sided  float32 (1 when the material is double-sided, else 0)
//	offset 32: emissive RGB  [3]float32 + pad (16 bytes)
func (m *StandardMaterial) UniformData() []byte {
	var buf [48]byte
	// Color RGBA (vec4<f32>, 16 bytes)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(m.color.R))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(m.color.G))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(m.color.B))
	binary.LittleEndian.PutUint32(buf[12:16], math.Float32bits(m.color.A*m.opacity))
	// Scalar properties (3 x float32 + double_sided)
	binary.LittleEndian.PutUint32(buf[16:20], math.Float32bits(m.metallic))
	binary.LittleEndian.PutUint32(buf[20:24], math.Float32bits(m.roughness))
	binary.LittleEndian.PutUint32(buf[24:28], math.Float32bits(m.alphaCutoff))
	if m.doubleSided {
		// the shader turns the normal of a back face toward the viewer
		binary.LittleEndian.PutUint32(buf[28:32], math.Float32bits(1))
	}
	// Emissive RGB + occlusion_strength (vec4<f32>, 16 bytes)
	binary.LittleEndian.PutUint32(buf[32:36], math.Float32bits(m.emissive.R))
	binary.LittleEndian.PutUint32(buf[36:40], math.Float32bits(m.emissive.G))
	binary.LittleEndian.PutUint32(buf[40:44], math.Float32bits(m.emissive.B))
	binary.LittleEndian.PutUint32(buf[44:48], math.Float32bits(m.occlusionStrength))
	return buf[:]
}
