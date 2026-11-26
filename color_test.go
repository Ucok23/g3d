package g3d

import (
	"math"
	"testing"
)

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
