package gpu

import (
	"encoding/binary"
	"math"
)

// Uniform buffer sizes must match WGSL struct layouts byte-for-byte.
const (
	// FrameUniformsSize is the byte size of the FrameUniforms struct (320 bytes).
	FrameUniformsSize = 320

	// ShadowFrameUniformsSize is the byte size of ShadowFrameUniforms (64 bytes, 1 mat4x4).
	ShadowFrameUniformsSize = 64

	// ObjectUniformsSize is the byte size of the ObjectUniforms struct (128 bytes).
	ObjectUniformsSize = 128

	// MaxLights is the maximum number of lights per frame (matches WGSL array size).
	MaxLights = 4
)

// LightData is the GPU-side representation of a single light source.
// Layout matches the WGSL LightData struct (32 bytes, 16-byte aligned).
//
//	offset  0: Direction [3]float32 (12 bytes)
//	offset 12: LightType uint32     (4 bytes)
//	offset 16: Color     [3]float32 (12 bytes)
//	offset 28: Intensity float32    (4 bytes)
type LightData struct {
	Direction [3]float32
	LightType uint32
	Color     [3]float32
	Intensity float32
}

// FrameUniformsData holds per-frame data for GPU upload. Layout matches the
// WGSL FrameUniforms struct (320 bytes total).
//
//	offset   0: ViewProjection      [16]float32 (64 bytes)
//	offset  64: CameraPosition      [4]float32  (16 bytes — vec4, NOT vec3!)
//	offset  80: Lights              [4]LightData (128 bytes)
//	offset 208: LightCount          uint32       (4 bytes)
//	offset 212: Flags               uint32       (4 bytes, see the frame flags of the renderer)
//	offset 216: Exposure            float32      (4 bytes, tone-mapping exposure multiplier)
//	offset 220: Pad                 uint32       (4 bytes)
//	offset 224: LightViewProjection [16]float32  (64 bytes, directional shadow matrix)
//	offset 288: ShadowParams        [4]float32   (16 bytes: depthBias, normalBias, texelSize, shadowLightIdx+1)
//	offset 304: ShadowSoft          [4]float32   (16 bytes: mapWorldWidth, depthRangeWorld, softness tan(angular radius), mapWorldHeight)
type FrameUniformsData struct {
	ViewProjection      [16]float32
	CameraPosition      [4]float32
	Lights              [MaxLights]LightData
	LightCount          uint32
	Flags               uint32
	Exposure            float32
	Pad                 uint32
	LightViewProjection [16]float32
	ShadowParams        [4]float32
	ShadowSoft          [4]float32
}

// ObjectUniformsData holds per-object data for GPU upload. Layout matches
// the WGSL ObjectUniforms struct (128 bytes total).
//
//	offset  0: Model       [16]float32 (64 bytes)
//	offset 64: NormalModel  [16]float32 (64 bytes)
type ObjectUniformsData struct {
	Model       [16]float32
	NormalModel [16]float32
}

// PackFrameUniforms encodes FrameUniformsData as FrameUniformsSize bytes in little-endian
// format for GPU upload.
func PackFrameUniforms(data *FrameUniformsData) []byte {
	buf := make([]byte, FrameUniformsSize)
	off := 0

	// ViewProjection: 64 bytes (16 x float32)
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.ViewProjection[i]))
		off += 4
	}

	// CameraPosition: 16 bytes (4 x float32, vec4)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.CameraPosition[i]))
		off += 4
	}

	// Lights: 128 bytes (4 x 32 bytes)
	for i := 0; i < MaxLights; i++ {
		light := &data.Lights[i]
		// Direction: 12 bytes
		for j := 0; j < 3; j++ {
			binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(light.Direction[j]))
			off += 4
		}
		// LightType: 4 bytes
		binary.LittleEndian.PutUint32(buf[off:off+4], light.LightType)
		off += 4
		// Color: 12 bytes
		for j := 0; j < 3; j++ {
			binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(light.Color[j]))
			off += 4
		}
		// Intensity: 4 bytes
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(light.Intensity))
		off += 4
	}

	// LightCount: 4 bytes
	binary.LittleEndian.PutUint32(buf[off:off+4], data.LightCount)

	binary.LittleEndian.PutUint32(buf[212:216], data.Flags)
	binary.LittleEndian.PutUint32(buf[216:220], math.Float32bits(data.Exposure))
	// Pad at [220:224] is zeroed.

	// LightViewProjection: 64 bytes at [224:288]
	off = 224
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.LightViewProjection[i]))
		off += 4
	}
	// ShadowParams: 16 bytes at [288:304]
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.ShadowParams[i]))
		off += 4
	}
	// ShadowSoft: 16 bytes at [304:320]
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.ShadowSoft[i]))
		off += 4
	}

	return buf
}

// PackMat4 encodes a single 4x4 matrix as 64 little-endian bytes.
func PackMat4(m [16]float32) []byte {
	buf := make([]byte, ShadowFrameUniformsSize)
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], math.Float32bits(m[i]))
	}
	return buf
}

// PackObjectUniforms encodes ObjectUniformsData as 128 bytes in little-endian
// format for GPU upload.
func PackObjectUniforms(data *ObjectUniformsData) []byte {
	buf := make([]byte, ObjectUniformsSize)
	off := 0

	// Model: 64 bytes
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.Model[i]))
		off += 4
	}

	// NormalModel: 64 bytes
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(data.NormalModel[i]))
		off += 4
	}

	return buf
}
