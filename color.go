package g3d

import "math"

// Color represents an RGBA color with float32 components in [0,1] range.
// Used for material colors, light colors, and scene background.
type Color struct {
	R, G, B, A float32
}

// Named color constants. All have alpha = 1 (fully opaque).
var (
	White   = Color{1, 1, 1, 1}
	Black   = Color{0, 0, 0, 1}
	Red     = Color{1, 0, 0, 1}
	Green   = Color{0, 1, 0, 1}
	Blue    = Color{0, 0, 1, 1}
	Yellow  = Color{1, 1, 0, 1}
	Cyan    = Color{0, 1, 1, 1}
	Magenta = Color{1, 0, 1, 1}
	Gray    = Color{0.5, 0.5, 0.5, 1}
)

// RGB creates a Color from red, green, blue components with alpha = 1.
func RGB(r, g, b float32) Color {
	return Color{r, g, b, 1}
}

// RGBA creates a Color from red, green, blue, alpha components.
func RGBA(r, g, b, a float32) Color {
	return Color{r, g, b, a}
}

// Lerp linearly interpolates between c and other by t in [0,1].
func (c Color) Lerp(other Color, t float32) Color {
	return Color{
		R: c.R + (other.R-c.R)*t,
		G: c.G + (other.G-c.G)*t,
		B: c.B + (other.B-c.B)*t,
		A: c.A + (other.A-c.A)*t,
	}
}

// SRGBToLinear converts one sRGB-encoded component in [0,1] to linear light (the IEC 61966-2-1 transfer function).
func SRGBToLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow(float64(c+0.055)/1.055, 2.4))
}

// LinearToSRGB converts one linear-light component in [0,1] to its sRGB encoding.
func LinearToSRGB(c float32) float32 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return float32(1.055*math.Pow(float64(c), 1/2.4) - 0.055)
}

// ToLinear converts the color from sRGB to linear light; alpha is unchanged.
func (c Color) ToLinear() Color {
	return Color{R: SRGBToLinear(c.R), G: SRGBToLinear(c.G), B: SRGBToLinear(c.B), A: c.A}
}

// ToSRGB converts the color from linear light to sRGB; alpha is unchanged.
func (c Color) ToSRGB() Color {
	return Color{R: LinearToSRGB(c.R), G: LinearToSRGB(c.G), B: LinearToSRGB(c.B), A: c.A}
}
