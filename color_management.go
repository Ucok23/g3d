package g3d

import "github.com/gogpu/gputypes"

// ColorManagement selects how the renderer treats color spaces.
type ColorManagement uint8

const (
	// ColorManagementOff is the default: colors, light colors, the background and texels are used as they are, and
	// the result is written as it is. Lighting then happens on gamma-encoded values.
	ColorManagementOff ColorManagement = iota
	// ColorManagementSRGB treats every color you give as sRGB (the colors you would write in CSS), does the lighting in
	// linear light, and encodes the result to sRGB when it is written. This is the physically sensible pipeline
	// of three.js (ColorManagement.enabled) and glTF.
	//
	// What is converted: material colors, light colors, the scene background, and the texels of a Texture
	// whose color space is ColorSpaceSRGB (the default; the GPU decodes and filters them in linear space).
	// The encoding at the end is done by the GPU when the target format is an sRGB format (RGBA8UnormSrgb,
	// BGRA8UnormSrgb), and in the shader otherwise.
	ColorManagementSRGB
)

// ColorSpace says how the texels of a Texture are encoded.
type ColorSpace uint8

const (
	// ColorSpaceSRGB is for color images (base color, emissive): the texels are sRGB-encoded.
	ColorSpaceSRGB ColorSpace = iota
	// ColorSpaceLinear is for data images (normal, roughness, occlusion): the texels are numbers, never decoded.
	ColorSpaceLinear
)

// Frame flags, shared with the shaders (FrameUniforms.flags).
const (
	frameFlagManaged         uint32 = 1 << 0 // colors are sRGB inputs: convert to linear for lighting
	frameFlagEncodeOutput    uint32 = 1 << 1 // the target is not an sRGB format: encode the result in the shader
	frameFlagTonemapReinhard uint32 = 1 << 2 // apply Reinhard tone mapping before sRGB encoding
	frameFlagTonemapACES     uint32 = 1 << 3 // apply ACES filmic tone mapping before sRGB encoding
	frameFlagNoEmissive      uint32 = 1 << 4 // skip emissive (multi-pass accumulation adds it once)
	frameFlagNoSpecular      uint32 = 1 << 5 // diffuse only (point-sampled sky lights would draw speckles)
)

// isSRGBFormat reports whether writing to a texture of this format encodes linear values to sRGB in hardware.
func isSRGBFormat(f gputypes.TextureFormat) bool {
	return f == gputypes.TextureFormatRGBA8UnormSrgb || f == gputypes.TextureFormatBGRA8UnormSrgb
}

// SetColorManagement selects the color pipeline. The default is ColorManagementOff, which keeps the
// behavior of earlier versions. Changing it frees the cached GPU textures, because the format of a color
// texture depends on it; they are uploaded again when next drawn.
func (r *Renderer) SetColorManagement(m ColorManagement) {
	if m == r.colorManagement {
		return
	}
	r.colorManagement = m
	r.releaseTextures()
}

// ColorManagement returns the selected color pipeline.
func (r *Renderer) ColorManagement() ColorManagement { return r.colorManagement }

// frameFlags returns the frame flags for the current color pipeline, tone mapping, and target format.
func (r *Renderer) frameFlags() uint32 {
	var flags uint32
	if r.colorManagement == ColorManagementSRGB {
		flags |= frameFlagManaged
		if !isSRGBFormat(r.surfaceFormat) {
			flags |= frameFlagEncodeOutput
		}
	}
	if !r.emissiveEnabled() {
		flags |= frameFlagNoEmissive
	}
	if !r.specularEnabled() {
		flags |= frameFlagNoSpecular
	}
	switch r.toneMapping {
	case ToneMappingReinhard:
		flags |= frameFlagTonemapReinhard
	case ToneMappingACES:
		flags |= frameFlagTonemapACES
	}
	return flags
}

// clearColor returns the value to clear the target with for a background color. The background is an sRGB
// input when color management is on; clearing an sRGB target takes linear values (the hardware encodes them).
func (r *Renderer) clearColor(bg Color) Color {
	if r.colorManagement == ColorManagementSRGB && isSRGBFormat(r.surfaceFormat) {
		return bg.ToLinear()
	}
	return bg
}

// lightColor returns a light color as the shader sums it: linear when color management is on.
func (r *Renderer) lightColor(c [3]float32) [3]float32 {
	if r.colorManagement != ColorManagementSRGB {
		return c
	}
	return [3]float32{SRGBToLinear(c[0]), SRGBToLinear(c[1]), SRGBToLinear(c[2])}
}

// textureFormat returns the GPU format of a texture: sRGB-decoding for color images when color management is on.
func (r *Renderer) textureFormat(cs ColorSpace) gputypes.TextureFormat {
	if r.colorManagement == ColorManagementSRGB && cs == ColorSpaceSRGB {
		return gputypes.TextureFormatRGBA8UnormSrgb
	}
	return gputypes.TextureFormatRGBA8Unorm
}

// SetEmissiveEnabled turns the emissive term of StandardMaterial on (default) or off. Multi-pass
// accumulation renders the scene many times and wants the emission in exactly one of the passes.
func (r *Renderer) SetEmissiveEnabled(enabled bool) { r.noEmissive = !enabled }

// SetSpecularEnabled turns the specular term of StandardMaterial on (default) or off (diffuse only).
func (r *Renderer) SetSpecularEnabled(enabled bool) { r.noSpecular = !enabled }

func (r *Renderer) emissiveEnabled() bool { return !r.noEmissive }
func (r *Renderer) specularEnabled() bool { return !r.noSpecular }
