package gltf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	glbMagic     = 0x46546C67 // "glTF"
	glbChunkJSON = 0x4E4F534A // "JSON"
	glbChunkBIN  = 0x004E4942 // "BIN\0"
)

// splitGLB reads the GLB container (GLB version 2, section "GLB File Format Specification"):
// a 12-byte header (magic, version, total length), then chunks of a length, a type and the data.
// The first chunk is the JSON; the second, if any, is the BIN chunk of buffer 0. Other chunk
// types are skipped. All fields are little-endian.
func splitGLB(data []byte) (jsonChunk, bin []byte, err error) {
	if len(data) < 12 {
		return nil, nil, errors.New("gltf: not a GLB: shorter than the 12-byte header")
	}
	if binary.LittleEndian.Uint32(data[0:]) != glbMagic {
		return nil, nil, errors.New("gltf: not a GLB: the magic bytes are not \"glTF\"")
	}
	if v := binary.LittleEndian.Uint32(data[4:]); v != 2 {
		return nil, nil, fmt.Errorf("gltf: GLB version %d is not supported (only 2)", v)
	}
	total := binary.LittleEndian.Uint32(data[8:])
	if int64(total) > int64(len(data)) {
		return nil, nil, fmt.Errorf("gltf: the GLB header says %d bytes but the data has %d", total, len(data))
	}
	data = data[:total]
	pos, n := 12, 0
	for pos+8 <= len(data) {
		clen := int64(binary.LittleEndian.Uint32(data[pos:]))
		ctype := binary.LittleEndian.Uint32(data[pos+4:])
		start := int64(pos) + 8
		if clen%4 != 0 {
			return nil, nil, fmt.Errorf("gltf: GLB chunk %d has a length (%d) that is not a multiple of 4", n, clen)
		}
		if start+clen > int64(len(data)) {
			return nil, nil, fmt.Errorf("gltf: GLB chunk %d runs past the end of the data", n)
		}
		chunk := data[start : start+clen]
		switch {
		case n == 0:
			if ctype != glbChunkJSON {
				return nil, nil, errors.New("gltf: the first GLB chunk must be the JSON chunk")
			}
			jsonChunk = chunk
		case ctype == glbChunkBIN && bin == nil:
			bin = chunk
		}
		pos = int(start + clen)
		n++
	}
	if jsonChunk == nil {
		return nil, nil, errors.New("gltf: the GLB has no JSON chunk")
	}
	return jsonChunk, bin, nil
}
