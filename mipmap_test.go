package g3d

import "testing"

func TestMipLevelCount(t *testing.T) {
	for _, c := range []struct{ w, h, want int }{
		{1, 1, 1}, {2, 1, 2}, {2, 2, 2}, {3, 3, 2}, {4, 4, 3}, {5, 3, 3}, {64, 64, 7}, {256, 16, 9}, {1000, 1, 10}, {8192, 8192, 14},
	} {
		if got := mipLevelCount(c.w, c.h); got != c.want {
			t.Errorf("mipLevelCount(%d, %d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

func TestMipChainSizes(t *testing.T) {
	for _, c := range []struct {
		w, h  int
		sizes [][2]int // the levels after the base
	}{
		{64, 64, [][2]int{{32, 32}, {16, 16}, {8, 8}, {4, 4}, {2, 2}, {1, 1}}},
		{5, 3, [][2]int{{2, 1}, {1, 1}}},
		{4, 1, [][2]int{{2, 1}, {1, 1}}},
		{1, 1, nil},
	} {
		levels := generateMips(make([]byte, c.w*c.h*4), c.w, c.h, false)
		if len(levels) != len(c.sizes) {
			t.Errorf("%dx%d: %d extra levels, want %d", c.w, c.h, len(levels), len(c.sizes))
			continue
		}
		for i, l := range levels {
			if want := c.sizes[i][0] * c.sizes[i][1] * 4; len(l) != want {
				t.Errorf("%dx%d level %d has %d bytes, want %d (%dx%d)", c.w, c.h, i+1, len(l), want, c.sizes[i][0], c.sizes[i][1])
			}
		}
	}
}

// a one-pixel black and white checkerboard of n x n
func checkerPixels(n int) []byte {
	pix := make([]byte, n*n*4)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := byte(0)
			if (x+y)%2 == 0 {
				v = 255
			}
			i := (y*n + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = v, v, v, 255
		}
	}
	return pix
}

func TestMipOfACheckerIsGray(t *testing.T) {
	levels := generateMips(checkerPixels(8), 8, 8, false)
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	// every 2x2 block of the checker holds two black and two white texels: the average is mid-gray, at every level
	for i, l := range levels {
		for p := 0; p < len(l); p += 4 {
			if l[p] < 127 || l[p] > 128 || l[p+1] != l[p] || l[p+3] != 255 {
				t.Fatalf("level %d texel %d = %v, want (128,128,128,255)", i+1, p/4, l[p:p+4])
			}
		}
	}
}

// Averaged in linear light (what an sRGB texture needs), the mid-gray of black and white is 188, not 128.
func TestMipAveragedInLinearLight(t *testing.T) {
	l := generateMips(checkerPixels(2), 2, 2, true)[0]
	if l[0] < 187 || l[0] > 189 {
		t.Errorf("linear average of black and white = %d, want about 188", l[0])
	}
	r := generateMips(checkerPixels(2), 2, 2, false)[0]
	if r[0] < 127 || r[0] > 128 {
		t.Errorf("raw average of black and white = %d, want about 128", r[0])
	}
}

func TestMipDoesNotBleedColorFromTransparentTexels(t *testing.T) {
	// a 2x1 image: a transparent red texel next to an opaque blue one. The visible color of the average is blue.
	base := []byte{255, 0, 0, 0, 0, 0, 255, 255}
	l := generateMips(base, 2, 1, false)[0]
	if l[0] != 0 || l[1] != 0 || l[2] != 255 {
		t.Errorf("color = %v, want pure blue (the transparent red must not count)", l[:3])
	}
	if l[3] < 127 || l[3] > 128 {
		t.Errorf("alpha = %d, want about 128 (the average of 0 and 255)", l[3])
	}
	// fully transparent stays transparent, without a color from dividing by zero
	z := generateMips([]byte{9, 9, 9, 0, 9, 9, 9, 0}, 2, 1, false)[0]
	if z[3] != 0 {
		t.Errorf("an all transparent texel pair has alpha %d, want 0", z[3])
	}
}

func TestMipOfAnOddSizeClampsToTheEdge(t *testing.T) {
	// 3x1: black, white, white. Level 1 is 1x1 and averages the first two texels (the third is dropped by the size rounding)
	base := []byte{0, 0, 0, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	l := generateMips(base, 3, 1, false)
	if len(l) != 1 || len(l[0]) != 4 {
		t.Fatalf("levels = %d", len(l))
	}
	if l[0][0] < 127 || l[0][0] > 128 {
		t.Errorf("average = %d, want about 128", l[0][0])
	}
}

func TestMipOptionsAndDefaults(t *testing.T) {
	d, _ := NewTexture(2, 2, make([]byte, 16))
	if !d.Mipmaps() {
		t.Error("mipmaps are on by default")
	}
	if f := d.MipmapFilter(); f != FilterLinear {
		t.Errorf("default mipmap filter = %v, want linear (trilinear)", f)
	}
	if lo, hi := d.LODRange(); lo != 0 || hi != 0 {
		t.Errorf("default LOD range = %v..%v, want 0..0 (no limit)", lo, hi)
	}
	o, _ := NewTexture(2, 2, make([]byte, 16), WithMipmaps(false), WithMipmapFilter(FilterNearest), WithLODRange(1, 3))
	if o.Mipmaps() || o.MipmapFilter() != FilterNearest {
		t.Error("the options were not applied")
	}
	if lo, hi := o.LODRange(); lo != 1 || hi != 3 {
		t.Errorf("LOD range = %v..%v, want 1..3", lo, hi)
	}
}
