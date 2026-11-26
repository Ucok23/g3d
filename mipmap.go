package g3d

import "math/bits"

// mipLevelCount returns the number of levels of a full mip chain: floor(log2(max(w, h))) + 1.
func mipLevelCount(w, h int) int {
	m := w
	if h > m {
		m = h
	}
	return bits.Len(uint(m))
}

// srgbToLinearLUT decodes an 8-bit sRGB value to linear light.
var srgbToLinearLUT = func() (t [256]float32) {
	for i := range t {
		t[i] = SRGBToLinear(float32(i) / 255)
	}
	return t
}()

func encodeSRGB8(v float32) byte {
	e := LinearToSRGB(v)*255 + 0.5
	if e <= 0 {
		return 0
	}
	if e >= 255 {
		return 255
	}
	return byte(e)
}

func encodeUnorm8(v float32) byte {
	e := v*255 + 0.5
	if e <= 0 {
		return 0
	}
	if e >= 255 {
		return 255
	}
	return byte(e)
}

// generateMips builds the mip levels 1 and up of an RGBA8 image (straight alpha), each level half the size of the
// previous one (rounded down, at least 1) down to 1x1, with a 2x2 box filter.
//
// The color is averaged weighted by alpha, so a transparent texel does not bleed its color into its neighbors.
// With linear set the color channels are averaged in linear light (decoded from sRGB, re-encoded afterwards):
// use it for sRGB color textures when the GPU decodes them before filtering, so every level matches what the GPU
// itself would produce. Without it the stored values are averaged as they are.
//
// When a dimension of a level is odd, the last destination texel averages the last source texel with itself
// (the source is clamped to the edge), which is a small approximation.
func generateMips(base []byte, w, h int, linear bool) [][]byte {
	n := mipLevelCount(w, h)
	levels := make([][]byte, 0, n-1)
	src, sw, sh := base, w, h
	for l := 1; l < n; l++ {
		dw, dh := max(sw/2, 1), max(sh/2, 1)
		dst := make([]byte, dw*dh*4)
		for y := 0; y < dh; y++ {
			y0, y1 := min(2*y, sh-1), min(2*y+1, sh-1)
			for x := 0; x < dw; x++ {
				x0, x1 := min(2*x, sw-1), min(2*x+1, sw-1)
				downsample(dst[(y*dw+x)*4:], src, [4]int{(y0*sw + x0) * 4, (y0*sw + x1) * 4, (y1*sw + x0) * 4, (y1*sw + x1) * 4}, linear)
			}
		}
		levels = append(levels, dst)
		src, sw, sh = dst, dw, dh
	}
	return levels
}

// downsample averages four texels (at the given byte offsets of src) into out[0:4].
func downsample(out, src []byte, at [4]int, linear bool) {
	var r, g, b, a float32
	for _, o := range at {
		al := float32(src[o+3]) / 255
		cr, cg, cb := float32(src[o])/255, float32(src[o+1])/255, float32(src[o+2])/255
		if linear {
			cr, cg, cb = srgbToLinearLUT[src[o]], srgbToLinearLUT[src[o+1]], srgbToLinearLUT[src[o+2]]
		}
		r += cr * al
		g += cg * al
		b += cb * al
		a += al
	}
	if a > 0 { // un-premultiply: the color of the visible texels only
		r, g, b = r/a, g/a, b/a
	}
	enc := encodeUnorm8
	if linear {
		enc = encodeSRGB8
	}
	out[0], out[1], out[2], out[3] = enc(r), enc(g), enc(b), encodeUnorm8(a/4)
}
