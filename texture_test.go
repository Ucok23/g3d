package g3d

import (
	"image"
	"image/color"
	"testing"
)

func TestNewTextureValidatesItsInput(t *testing.T) {
	pix := make([]byte, 2*2*4)
	if _, err := NewTexture(2, 2, pix); err != nil {
		t.Fatalf("a valid texture: %v", err)
	}
	for name, c := range map[string]struct {
		w, h int
		pix  []byte
	}{
		"zero width":     {0, 2, pix},
		"zero height":    {2, 0, pix},
		"negative":       {-1, 2, pix},
		"too large":      {maxTextureSize + 1, 1, pix},
		"too few bytes":  {2, 2, pix[:15]},
		"too many bytes": {2, 2, make([]byte, 2*2*4+1)},
	} {
		if _, err := NewTexture(c.w, c.h, c.pix); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestNewTextureCopiesThePixelsAndAppliesOptions(t *testing.T) {
	pix := []byte{1, 2, 3, 4}
	tex, err := NewTexture(1, 1, pix, WithWrap(WrapRepeat, WrapMirror), WithFilter(FilterNearest, FilterLinear))
	if err != nil {
		t.Fatal(err)
	}
	pix[0] = 99
	if tex.Pixels()[0] != 1 {
		t.Error("the texture should keep its own copy of the pixels")
	}
	if u, v := tex.Wrap(); u != WrapRepeat || v != WrapMirror {
		t.Errorf("wrap = %v %v", u, v)
	}
	if mag, mn := tex.Filter(); mag != FilterNearest || mn != FilterLinear {
		t.Errorf("filter = %v %v", mag, mn)
	}
	if tex.Width() != 1 || tex.Height() != 1 {
		t.Errorf("size = %dx%d", tex.Width(), tex.Height())
	}
	// defaults: clamp and linear
	d, _ := NewTexture(1, 1, pix)
	if u, v := d.Wrap(); u != WrapClamp || v != WrapClamp {
		t.Errorf("default wrap = %v %v", u, v)
	}
	if mag, mn := d.Filter(); mag != FilterLinear || mn != FilterLinear {
		t.Errorf("default filter = %v %v", mag, mn)
	}
}

func TestNewTextureFromImageConvertsToStraightRGBA(t *testing.T) {
	img := image.NewRGBA(image.Rect(10, 10, 12, 11))       // an offset 2x1 image, premultiplied alpha
	img.Set(10, 10, color.RGBA{R: 50, G: 0, B: 0, A: 100}) // premultiplied: the straight color is (127, 0, 0, 100)
	img.Set(11, 10, color.RGBA{R: 0, G: 200, B: 0, A: 255})
	tex, err := NewTextureFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	if tex.Width() != 2 || tex.Height() != 1 {
		t.Fatalf("size = %dx%d", tex.Width(), tex.Height())
	}
	p := tex.Pixels()
	if p[0] < 126 || p[0] > 128 || p[1] != 0 || p[3] != 100 {
		t.Errorf("pixel 0 = %v, want about (127,0,0,100): straight alpha", p[:4])
	}
	if p[4] != 0 || p[5] != 200 || p[7] != 255 {
		t.Errorf("pixel 1 = %v, want (0,200,0,255)", p[4:8])
	}
	if _, err := NewTextureFromImage(nil); err == nil {
		t.Error("a nil image should be refused")
	}
}

func TestWithMapSetsTheColorMapOnBothMaterials(t *testing.T) {
	tex, _ := NewTexture(1, 1, []byte{255, 255, 255, 255})
	if NewBasicMaterial(WithMap(tex)).Map() != tex || NewStandardMaterial(WithMap(tex)).Map() != tex {
		t.Error("WithMap should set the map on both materials")
	}
	if NewBasicMaterial().Map() != nil || NewStandardMaterial(WithMap(nil)).Map() != nil {
		t.Error("a material has no map unless one is set")
	}
	var _ mapped = NewBasicMaterial()
	var _ mapped = NewStandardMaterial()
}
