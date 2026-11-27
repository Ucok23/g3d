package g3d

// ToneMapping selects how high-dynamic-range linear radiance is mapped into [0..1]
// before sRGB output encoding in StandardMaterial.
type ToneMapping uint8

const (
	// ToneMappingNone is the default: values are clamped directly to [0..1],
	// keeping earlier behavior unchanged.
	ToneMappingNone ToneMapping = iota
	// ToneMappingReinhard applies c / (1 + c) highlight compression.
	ToneMappingReinhard
	// ToneMappingACES applies the Narkowicz (2015) ACES filmic tone curve,
	// preserving highlight roll-off and rich contrast.
	ToneMappingACES
)

// SetToneMapping selects the opt-in tone mapping operator for lit materials.
func (r *Renderer) SetToneMapping(m ToneMapping) {
	r.toneMapping = m
}

// ToneMapping returns the active tone mapping operator.
func (r *Renderer) ToneMapping() ToneMapping {
	return r.toneMapping
}

// SetExposure sets the linear exposure multiplier applied before tone mapping.
// Values <= 0 reset to the default exposure of 1.0.
func (r *Renderer) SetExposure(exposure float32) {
	if exposure <= 0 {
		r.exposure = 0
		return
	}
	r.exposure = exposure
}

// Exposure returns the active exposure multiplier (1.0 by default).
func (r *Renderer) Exposure() float32 {
	if r.exposure <= 0 {
		return 1.0
	}
	return r.exposure
}
