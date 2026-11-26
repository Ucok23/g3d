package g3d

import (
	"math"
	"testing"
)

func TestRGB(t *testing.T) {
	c := RGB(0.5, 0.6, 0.7)
	if c.R != 0.5 || c.G != 0.6 || c.B != 0.7 || c.A != 1.0 {
		t.Errorf("RGB(0.5, 0.6, 0.7) = %+v, want R=0.5 G=0.6 B=0.7 A=1.0", c)
	}
}

func TestRGBA(t *testing.T) {
	c := RGBA(0.1, 0.2, 0.3, 0.4)
	if c.R != 0.1 || c.G != 0.2 || c.B != 0.3 || c.A != 0.4 {
		t.Errorf("RGBA(0.1, 0.2, 0.3, 0.4) = %+v, want R=0.1 G=0.2 B=0.3 A=0.4", c)
	}
}

func TestColorConstants(t *testing.T) {
	tests := []struct {
		name       string
		color      Color
		r, g, b, a float32
	}{
		{"White", White, 1, 1, 1, 1},
		{"Black", Black, 0, 0, 0, 1},
		{"Red", Red, 1, 0, 0, 1},
		{"Green", Green, 0, 1, 0, 1},
		{"Blue", Blue, 0, 0, 1, 1},
		{"Yellow", Yellow, 1, 1, 0, 1},
		{"Cyan", Cyan, 0, 1, 1, 1},
		{"Magenta", Magenta, 1, 0, 1, 1},
		{"Gray", Gray, 0.5, 0.5, 0.5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.color.R != tt.r || tt.color.G != tt.g || tt.color.B != tt.b || tt.color.A != tt.a {
				t.Errorf("%s = %+v, want {R:%v G:%v B:%v A:%v}", tt.name, tt.color, tt.r, tt.g, tt.b, tt.a)
			}
		})
	}
}

func TestColorLerp(t *testing.T) {
	tests := []struct {
		name string
		a, b Color
		t    float32
		want Color
	}{
		{"t=0", Black, White, 0, Black},
		{"t=1", Black, White, 1, White},
		{"t=0.5", Black, White, 0.5, Color{0.5, 0.5, 0.5, 1}},
		{"alpha lerp", RGBA(0, 0, 0, 0), RGBA(1, 1, 1, 1), 0.25, RGBA(0.25, 0.25, 0.25, 0.25)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Lerp(tt.b, tt.t)
			if !approxEqualColor(got, tt.want, 1e-6) {
				t.Errorf("Color.Lerp() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func approxEqualColor(a, b Color, eps float32) bool {
	return approxEqual(a.R, b.R, eps) && approxEqual(a.G, b.G, eps) &&
		approxEqual(a.B, b.B, eps) && approxEqual(a.A, b.A, eps)
}

func TestSRGBConversionsMatchTheStandard(t *testing.T) {
	// reference values of the IEC 61966-2-1 transfer function
	for _, c := range []struct{ srgb, linear float32 }{
		{0, 0}, {1, 1}, {0.5, 0.21404}, {0.04045, 0.0031308}, {0.2, 0.033105}, {0.8, 0.603827},
	} {
		if got := SRGBToLinear(c.srgb); math.Abs(float64(got-c.linear)) > 1e-4 {
			t.Errorf("SRGBToLinear(%v) = %v, want %v", c.srgb, got, c.linear)
		}
		if got := LinearToSRGB(c.linear); math.Abs(float64(got-c.srgb)) > 1e-4 {
			t.Errorf("LinearToSRGB(%v) = %v, want %v", c.linear, got, c.srgb)
		}
	}
}

func TestSRGBConversionsRoundTrip(t *testing.T) {
	for i := 0; i <= 255; i++ {
		c := float32(i) / 255
		if got := LinearToSRGB(SRGBToLinear(c)); math.Abs(float64(got-c)) > 1e-5 {
			t.Fatalf("round trip of %v = %v", c, got)
		}
	}
}

func TestColorToLinearAndToSRGBKeepAlpha(t *testing.T) {
	c := RGBA(0.5, 0.5, 0.5, 0.25)
	l := c.ToLinear()
	if l.A != 0.25 || math.Abs(float64(l.R-0.21404)) > 1e-4 {
		t.Errorf("ToLinear = %+v", l)
	}
	if back := l.ToSRGB(); math.Abs(float64(back.G-0.5)) > 1e-5 || back.A != 0.25 {
		t.Errorf("ToSRGB(ToLinear) = %+v", back)
	}
}
