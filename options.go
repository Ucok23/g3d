package g3d

// MaterialOption configures material properties via functional options.
// Options that apply to both BasicMaterial and StandardMaterial implement
// both apply methods; standard-only options are no-ops on BasicMaterial.
type MaterialOption interface {
	applyBasic(m *BasicMaterial)
	applyStandard(m *StandardMaterial)
}

// --- Shared options (apply to both Basic and Standard) ---

type colorOption struct{ c Color }

func (o colorOption) applyBasic(m *BasicMaterial)       { m.color = o.c }
func (o colorOption) applyStandard(m *StandardMaterial) { m.color = o.c }

// WithColor sets the base color. Applies to both BasicMaterial and StandardMaterial.
func WithColor(c Color) MaterialOption { return colorOption{c} }

type opacityOption struct{ v float32 }

func (o opacityOption) applyBasic(m *BasicMaterial)       { m.opacity = o.v }
func (o opacityOption) applyStandard(m *StandardMaterial) { m.opacity = o.v }

// WithOpacity sets the opacity in [0,1]. Applies to both BasicMaterial and StandardMaterial.
// For BasicMaterial, opacity < 1 moves the material to the transparent render bucket.
func WithOpacity(v float32) MaterialOption { return opacityOption{v} }

type doubleSidedOption struct{ v bool }

func (o doubleSidedOption) applyBasic(m *BasicMaterial)       { m.doubleSided = o.v }
func (o doubleSidedOption) applyStandard(m *StandardMaterial) { m.doubleSided = o.v }

// WithDoubleSided disables back-face culling when true. Applies to both materials.
func WithDoubleSided(v bool) MaterialOption { return doubleSidedOption{v} }

type wireframeOption struct{ v bool }

func (o wireframeOption) applyBasic(m *BasicMaterial)       { m.wireframe = o.v }
func (o wireframeOption) applyStandard(m *StandardMaterial) { m.wireframe = o.v }

// WithWireframe enables wireframe rendering when true. Applies to both materials.
func WithWireframe(v bool) MaterialOption { return wireframeOption{v} }

// --- Standard-only options (no-op on BasicMaterial) ---

type metallicOption struct{ v float32 }

func (o metallicOption) applyBasic(*BasicMaterial)         {}
func (o metallicOption) applyStandard(m *StandardMaterial) { m.metallic = o.v }

// WithMetallic sets the metallic factor in [0,1]. 0 = dielectric, 1 = metal.
// Only affects StandardMaterial; ignored by BasicMaterial.
func WithMetallic(v float32) MaterialOption { return metallicOption{v} }

type roughnessOption struct{ v float32 }

func (o roughnessOption) applyBasic(*BasicMaterial)         {}
func (o roughnessOption) applyStandard(m *StandardMaterial) { m.roughness = o.v }

// WithRoughness sets the roughness factor in [0,1]. 0 = mirror, 1 = matte.
// Only affects StandardMaterial; ignored by BasicMaterial.
func WithRoughness(v float32) MaterialOption { return roughnessOption{v} }

type emissiveOption struct{ c Color }

func (o emissiveOption) applyBasic(*BasicMaterial)         {}
func (o emissiveOption) applyStandard(m *StandardMaterial) { m.emissive = o.c }

// WithEmissive sets the emissive color. Objects emit this color regardless of lighting.
// Only affects StandardMaterial; ignored by BasicMaterial.
func WithEmissive(c Color) MaterialOption { return emissiveOption{c} }

type alphaModeOption struct{ v AlphaMode }

func (o alphaModeOption) applyBasic(*BasicMaterial)         {}
func (o alphaModeOption) applyStandard(m *StandardMaterial) { m.alphaMode = o.v }

// WithAlphaMode sets the alpha handling mode (Opaque, Mask, or Blend).
// Only affects StandardMaterial; ignored by BasicMaterial.
func WithAlphaMode(mode AlphaMode) MaterialOption { return alphaModeOption{mode} }

type alphaCutoffOption struct{ v float32 }

func (o alphaCutoffOption) applyBasic(*BasicMaterial)         {}
func (o alphaCutoffOption) applyStandard(m *StandardMaterial) { m.alphaCutoff = o.v }

// WithAlphaCutoff sets the alpha test threshold for AlphaModeMask.
// Fragments with alpha below this value are discarded.
// Only affects StandardMaterial; ignored by BasicMaterial.
func WithAlphaCutoff(cutoff float32) MaterialOption { return alphaCutoffOption{cutoff} }

type mapOption struct{ t *Texture }

func (o mapOption) applyBasic(m *BasicMaterial)       { m.colorMap = o.t }
func (o mapOption) applyStandard(m *StandardMaterial) { m.colorMap = o.t }

// WithMap sets the color map: a Texture whose texels are multiplied with the color (and its alpha with
// the opacity). Applies to both BasicMaterial and StandardMaterial. A nil texture means no map.
func WithMap(t *Texture) MaterialOption { return mapOption{t} }

// mapped is implemented by materials that carry a color map; the renderer binds it to the draw.
type mapped interface {
	Map() *Texture
}

type metallicRoughnessMapOption struct{ t *Texture }

func (o metallicRoughnessMapOption) applyBasic(*BasicMaterial) {}
func (o metallicRoughnessMapOption) applyStandard(m *StandardMaterial) {
	m.metallicRoughnessMap = o.t
}

// WithMetallicRoughnessMap sets the metallic-roughness texture (glTF packing:
// G = roughness, B = metallic, multiplied with the scalar factors). Use
// WithColorSpace(ColorSpaceLinear) when creating the texture.
func WithMetallicRoughnessMap(t *Texture) MaterialOption { return metallicRoughnessMapOption{t} }

type occlusionMapOption struct{ t *Texture }

func (o occlusionMapOption) applyBasic(*BasicMaterial)         {}
func (o occlusionMapOption) applyStandard(m *StandardMaterial) { m.occlusionMap = o.t }

// WithOcclusionMap sets the ambient occlusion texture (glTF packing: R channel
// scales ambient light, 1 = unoccluded, 0 = fully occluded). Use ColorSpaceLinear.
func WithOcclusionMap(t *Texture) MaterialOption { return occlusionMapOption{t} }

type occlusionStrengthOption struct{ v float32 }

func (o occlusionStrengthOption) applyBasic(*BasicMaterial)         {}
func (o occlusionStrengthOption) applyStandard(m *StandardMaterial) { m.occlusionStrength = o.v }

// WithOcclusionStrength sets how strongly the occlusion map darkens ambient light
// in [0, 1] (default 1.0, per glTF 2.0: ao = 1 + strength * (occ - 1)).
func WithOcclusionStrength(v float32) MaterialOption { return occlusionStrengthOption{v} }

type emissiveMapOption struct{ t *Texture }

func (o emissiveMapOption) applyBasic(*BasicMaterial)         {}
func (o emissiveMapOption) applyStandard(m *StandardMaterial) { m.emissiveMap = o.t }

// WithEmissiveMap sets the emissive color texture (sRGB), multiplied with the
// material's emissive color.
func WithEmissiveMap(t *Texture) MaterialOption { return emissiveMapOption{t} }

type normalMapOption struct{ t *Texture }

func (o normalMapOption) applyBasic(*BasicMaterial)         {}
func (o normalMapOption) applyStandard(m *StandardMaterial) { m.normalMap = o.t }

// WithNormalMap sets the tangent-space normal map texture (OpenGL/glTF convention:
// +R right, +G up, +B out). Create the texture with WithColorSpace(ColorSpaceLinear).
func WithNormalMap(t *Texture) MaterialOption { return normalMapOption{t} }

type normalScaleOption struct{ v float32 }

func (o normalScaleOption) applyBasic(*BasicMaterial)         {}
func (o normalScaleOption) applyStandard(m *StandardMaterial) { m.normalScale = o.v }

// WithNormalScale sets the XY perturbation strength of the normal map (default 1.0).
func WithNormalScale(v float32) MaterialOption { return normalScaleOption{v} }

// pbrMapped is implemented by StandardMaterial to expose auxiliary PBR textures.
type pbrMapped interface {
	MetallicRoughnessMap() *Texture
	OcclusionMap() *Texture
	EmissiveMap() *Texture
	NormalMap() *Texture
}
