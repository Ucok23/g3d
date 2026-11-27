package gltf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder: glTF images are PNG or JPEG
	_ "image/png"  // registers the PNG decoder
	"math"

	"github.com/gogpu/g3d"
)

// Limits on the images of one asset, so a hostile file cannot make the loader decode gigabytes. An image over
// the limits is skipped with a warning; the rest of the asset still loads.
const (
	maxImagePixels = 1 << 26 // 8192 x 8192, the largest texture g3d accepts
	maxTotalPixels = 1 << 27 // all the images of one asset together
)

// sampler constants of glTF (they are OpenGL enums).
const (
	filterNearest              = 9728
	filterLinear               = 9729
	filterNearestMipmapNearest = 9984
	filterLinearMipmapNearest  = 9985
	filterNearestMipmapLinear  = 9986
	filterLinearMipmapLinear   = 9987
	wrapClampToEdge            = 33071
	wrapMirroredRepeat         = 33648
	wrapRepeat                 = 10497
)

// uvSpec says which texture coordinates a primitive's vertices carry for its base color texture, and how they are
// transformed (KHR_texture_transform is baked into the vertices, since g3d's shaders take no texture matrix).
// The zero value is TEXCOORD_0 untouched.
type uvSpec struct {
	set int
	has bool
	m   [6]float32 // u' = m0*u + m1*v + m2, v' = m3*u + m4*v + m5
}

// texKey distinguishes sRGB color textures (baseColor, emissive) from linear data textures
// (metallicRoughness, occlusion, normal), even when two slots share one glTF texture index.
type texKey struct {
	index  int
	linear bool
}

// mapSlot is what a material's texture slot resolves to.
type mapSlot struct {
	tex *g3d.Texture
	uv  uvSpec
}

// baseColor resolves the base color texture of material mi once (nil when it has none or cannot be used).
func (l *loader) baseColor(mi *int) *mapSlot {
	if mi == nil || *mi < 0 || *mi >= len(l.doc.Materials) {
		return nil
	}
	if s, ok := l.slots[*mi]; ok {
		return s
	}
	var slot *mapSlot
	if m := &l.doc.Materials[*mi]; m.PBR != nil && len(m.PBR.BaseColorTexture) > 0 && string(m.PBR.BaseColorTexture) != "null" {
		slot = l.resolveTextureInfo(m.PBR.BaseColorTexture, "baseColorTexture", false)
	}
	l.slots[*mi] = slot
	return slot
}

func (l *loader) resolveTextureInfo(raw json.RawMessage, what string, linear bool) *mapSlot {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var info textureInfo
	if err := json.Unmarshal(raw, &info); err != nil || info.Index == nil {
		l.warnf("%s is not readable and is ignored", what)
		return nil
	}
	uv := uvSpec{set: info.TexCoord}
	if ext, ok := info.Extensions["KHR_texture_transform"]; ok {
		var tt textureTransform
		if err := json.Unmarshal(ext, &tt); err != nil {
			l.warnf("KHR_texture_transform of %s is not readable and is ignored", what)
		} else {
			uv = transformSpec(&tt, info.TexCoord)
		}
	}
	tex := l.texture(*info.Index, linear)
	if tex == nil {
		return nil
	}
	return &mapSlot{tex: tex, uv: uv}
}

func (l *loader) resolveOcclusionInfo(raw json.RawMessage) (*g3d.Texture, float32) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, 1
	}
	var info struct {
		textureInfo
		Strength *float64 `json:"strength"`
	}
	if err := json.Unmarshal(raw, &info); err != nil || info.Index == nil {
		l.warnf("occlusionTexture is not readable and is ignored")
		return nil, 1
	}
	strength := float32(1)
	if info.Strength != nil {
		strength = float32(*info.Strength)
	}
	return l.texture(*info.Index, true), strength
}

func (l *loader) resolveNormalInfo(raw json.RawMessage) (*g3d.Texture, float32) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, 1
	}
	var info struct {
		textureInfo
		Scale *float64 `json:"scale"`
	}
	if err := json.Unmarshal(raw, &info); err != nil || info.Index == nil {
		l.warnf("normalTexture is not readable and is ignored")
		return nil, 1
	}
	scale := float32(1)
	if info.Scale != nil {
		scale = float32(*info.Scale)
	}
	return l.texture(*info.Index, true), scale
}

// transformSpec builds the matrix of KHR_texture_transform: translation * rotation * scale applied to (u, v).
func transformSpec(tt *textureTransform, texCoord int) uvSpec {
	off, sc := [2]float64{0, 0}, [2]float64{1, 1}
	if len(tt.Offset) == 2 {
		copy(off[:], tt.Offset)
	}
	if len(tt.Scale) == 2 {
		copy(sc[:], tt.Scale)
	}
	set := texCoord
	if tt.TexCoord != nil {
		set = *tt.TexCoord
	}
	c, s := math.Cos(tt.Rotation), math.Sin(tt.Rotation)
	// scale first, then rotate counter-clockwise, then translate
	m := [6]float32{
		float32(c * sc[0]), float32(-s * sc[1]), float32(off[0]),
		float32(s * sc[0]), float32(c * sc[1]), float32(off[1]),
	}
	identity := [6]float32{1, 0, 0, 0, 1, 0}
	return uvSpec{set: set, has: m != identity, m: m}
}

// texture builds (and caches) the g3d.Texture of textures[i]: its image, decoded, with the options of its sampler
// and the requested color space (linear for data maps, sRGB for color maps).
func (l *loader) texture(i int, linear bool) *g3d.Texture {
	k := texKey{index: i, linear: linear}
	if t, ok := l.texs[k]; ok {
		return t
	}
	t := l.buildTexture(i, linear)
	l.texs[k] = t
	return t
}

func (l *loader) buildTexture(i int, linear bool) *g3d.Texture {
	if i < 0 || i >= len(l.doc.Textures) {
		l.warnf("a material refers to texture %d, which does not exist", i)
		return nil
	}
	tx := &l.doc.Textures[i]
	if tx.Source == nil {
		l.warnf("texture %d has no image source (images in extension formats such as WebP or KTX2 are not supported)", i)
		return nil
	}
	img, err := l.decodeImage(*tx.Source)
	if err != nil {
		l.warnf("texture %d is ignored: %v", i, err)
		return nil
	}
	var smp *sampler
	if tx.Sampler != nil && *tx.Sampler >= 0 && *tx.Sampler < len(l.doc.Samplers) {
		smp = &l.doc.Samplers[*tx.Sampler]
	}
	opts := samplerOptions(smp)
	if linear {
		opts = append(opts, g3d.WithColorSpace(g3d.ColorSpaceLinear))
	}
	t, err := g3d.NewTextureFromImage(img, opts...)
	if err != nil {
		l.warnf("texture %d is ignored: %v", i, err)
		return nil
	}
	return t
}

// samplerOptions maps a glTF sampler to texture options. Without a sampler the specification says repeat wrapping
// and implementation-chosen filtering: here linear with a full mip chain (trilinear).
func samplerOptions(s *sampler) []g3d.TextureOption {
	wrapU, wrapV := g3d.WrapRepeat, g3d.WrapRepeat
	mag, mn, mipFilter, mips := g3d.FilterLinear, g3d.FilterLinear, g3d.FilterLinear, true
	if s != nil {
		wrapU, wrapV = wrapMode(s.WrapS), wrapMode(s.WrapT)
		if s.MagFilter != nil && *s.MagFilter == filterNearest {
			mag = g3d.FilterNearest
		}
		if s.MinFilter != nil {
			switch *s.MinFilter {
			case filterNearest:
				mn, mips = g3d.FilterNearest, false
			case filterLinear:
				mips = false
			case filterNearestMipmapNearest:
				mn, mipFilter = g3d.FilterNearest, g3d.FilterNearest
			case filterLinearMipmapNearest:
				mipFilter = g3d.FilterNearest
			case filterNearestMipmapLinear:
				mn = g3d.FilterNearest
			}
		}
	}
	return []g3d.TextureOption{g3d.WithWrap(wrapU, wrapV), g3d.WithFilter(mag, mn), g3d.WithMipmaps(mips), g3d.WithMipmapFilter(mipFilter)}
}

func wrapMode(code *int) g3d.TextureWrap {
	if code == nil {
		return g3d.WrapRepeat
	}
	switch *code {
	case wrapClampToEdge:
		return g3d.WrapClamp
	case wrapMirroredRepeat:
		return g3d.WrapMirror
	}
	return g3d.WrapRepeat
}

// decodeImage decodes images[i] (PNG or JPEG) after checking its size, and counts it against the asset's pixel budget.
func (l *loader) decodeImage(i int) (image.Image, error) {
	if img, ok := l.images[i]; ok {
		return img, nil
	}
	if i < 0 || i >= len(l.doc.Images) {
		return nil, fmt.Errorf("image %d does not exist", i)
	}
	ref := &l.doc.Images[i]
	var data []byte
	var err error
	switch {
	case ref.BufferView != nil:
		var bv *bufferView
		if data, bv, err = l.view(*ref.BufferView); err == nil && bv.ByteStride != 0 {
			err = fmt.Errorf("the buffer view of image %d has a byteStride", i)
		}
	case ref.URI != nil:
		data, err = l.readURI(*ref.URI)
	default:
		err = fmt.Errorf("image %d has neither a uri nor a bufferView", i)
	}
	if err != nil {
		return nil, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image %d is not a PNG or JPEG: %w", i, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("image %d is %dx%d, over the %d pixel limit", i, cfg.Width, cfg.Height, maxImagePixels)
	}
	if l.pixels += int64(cfg.Width) * int64(cfg.Height); l.pixels > maxTotalPixels {
		return nil, fmt.Errorf("the images of the asset are over the %d pixel limit together", maxTotalPixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image %d (%s) cannot be decoded: %w", i, format, err)
	}
	l.images[i] = img
	return img, nil
}
