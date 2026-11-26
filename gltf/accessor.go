package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Limits that keep a hostile or broken asset from asking for absurd amounts of memory.
const (
	maxElements = 1 << 26 // elements in one accessor
	maxBytes    = 1 << 30 // bytes of one decoded accessor
)

// Component types of an accessor (glTF 2.0, "Accessor Data Types").
const (
	ctByte   = 5120
	ctUByte  = 5121
	ctShort  = 5122
	ctUShort = 5123
	ctUInt   = 5125
	ctFloat  = 5126
)

func componentSize(ct int) int {
	switch ct {
	case ctByte, ctUByte:
		return 1
	case ctShort, ctUShort:
		return 2
	case ctUInt, ctFloat:
		return 4
	}
	return 0
}

func typeComponents(t string) int {
	switch t {
	case "SCALAR":
		return 1
	case "VEC2":
		return 2
	case "VEC3":
		return 3
	case "VEC4":
		return 4
	}
	return 0 // matrices are not read by this loader
}

// elems is a decoded view of an accessor: count elements of ncomp components at a stride.
type elems struct {
	data       []byte // nil: all zeros (an accessor without a buffer view)
	offset     int
	stride     int
	count      int
	ncomp      int
	ctype      int
	normalized bool
}

// float returns component c of element i as a float, applying the normalized conversions of the
// specification: snorm8 max(c,-127)/127, unorm8 c/255, snorm16 max(c,-32767)/32767, unorm16 c/65535.
func (e *elems) float(i, c int) float32 {
	if e.data == nil {
		return 0
	}
	p := e.offset + i*e.stride + c*componentSize(e.ctype)
	switch e.ctype {
	case ctFloat:
		return math.Float32frombits(binary.LittleEndian.Uint32(e.data[p:]))
	case ctByte:
		v := int(e.data[p])
		if v > 127 { // two's complement
			v -= 256
		}
		if e.normalized {
			return float32(maxInt(v, -127)) / 127
		}
		return float32(v)
	case ctUByte:
		v := e.data[p]
		if e.normalized {
			return float32(v) / 255
		}
		return float32(v)
	case ctShort:
		v := int(binary.LittleEndian.Uint16(e.data[p:]))
		if v > 32767 {
			v -= 65536
		}
		if e.normalized {
			return float32(maxInt(v, -32767)) / 32767
		}
		return float32(v)
	case ctUShort:
		v := binary.LittleEndian.Uint16(e.data[p:])
		if e.normalized {
			return float32(v) / 65535
		}
		return float32(v)
	case ctUInt:
		return float32(binary.LittleEndian.Uint32(e.data[p:]))
	}
	return 0
}

// uint returns element i of a scalar unsigned integer accessor, exactly (a float32 would lose indices
// above 16 million).
func (e *elems) uint(i int) uint32 {
	if e.data == nil {
		return 0
	}
	p := e.offset + i*e.stride
	switch e.ctype {
	case ctUByte:
		return uint32(e.data[p])
	case ctUShort:
		return uint32(binary.LittleEndian.Uint16(e.data[p:]))
	case ctUInt:
		return binary.LittleEndian.Uint32(e.data[p:])
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// buffer returns the bytes of buffer i: the BIN chunk of a GLB (buffer 0 without a uri), a data URI,
// or a file next to the asset.
func (l *loader) buffer(i int) ([]byte, error) {
	if i < 0 || i >= len(l.doc.Buffers) {
		return nil, fmt.Errorf("gltf: buffer %d does not exist (the asset has %d)", i, len(l.doc.Buffers))
	}
	if l.buffers == nil {
		l.buffers = make([][]byte, len(l.doc.Buffers))
	}
	if l.buffers[i] != nil {
		return l.buffers[i], nil
	}
	b := l.doc.Buffers[i]
	var data []byte
	switch {
	case b.URI == nil:
		if i != 0 || l.bin == nil {
			return nil, fmt.Errorf("gltf: buffer %d has no uri and there is no GLB BIN chunk for it", i)
		}
		data = l.bin
	case strings.HasPrefix(*b.URI, "data:"):
		comma := strings.IndexByte(*b.URI, ',')
		if comma < 0 || !strings.HasSuffix((*b.URI)[:comma], ";base64") {
			return nil, fmt.Errorf("gltf: buffer %d has a data URI that is not base64", i)
		}
		d, err := base64.StdEncoding.DecodeString((*b.URI)[comma+1:])
		if err != nil {
			return nil, fmt.Errorf("gltf: buffer %d: bad base64: %w", i, err)
		}
		data = d
	default:
		d, err := l.readExternal(*b.URI)
		if err != nil {
			return nil, fmt.Errorf("gltf: buffer %d: %w", i, err)
		}
		data = d
	}
	if len(data) < b.ByteLength {
		return nil, fmt.Errorf("gltf: buffer %d is %d bytes but the asset says %d", i, len(data), b.ByteLength)
	}
	l.buffers[i] = data
	return data, nil
}

// readExternal reads a file named by a relative URI. The read goes through os.Root, so it never leaves
// the asset's folder: an uploaded asset cannot name /etc/passwd, ../../secrets or a symlink that points out.
func (l *loader) readExternal(uri string) ([]byte, error) {
	if l.baseDir == "" {
		return nil, fmt.Errorf("the asset points to the file %q but no folder was given (use Load, or WithBaseDir)", uri)
	}
	name, err := url.PathUnescape(uri)
	if err != nil {
		return nil, fmt.Errorf("bad uri %q: %w", uri, err)
	}
	if strings.Contains(name, "://") || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return nil, fmt.Errorf("the uri %q is not a relative path", uri)
	}
	root, err := os.OpenRoot(l.baseDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.FromSlash(name))
	if err != nil {
		return nil, fmt.Errorf("the uri %q cannot be read from the asset's folder: %w", uri, err)
	}
	return data, nil
}

// view returns the bytes of a buffer view, checked against the buffer.
func (l *loader) view(i int) ([]byte, *bufferView, error) {
	if i < 0 || i >= len(l.doc.BufferViews) {
		return nil, nil, fmt.Errorf("gltf: buffer view %d does not exist (the asset has %d)", i, len(l.doc.BufferViews))
	}
	bv := &l.doc.BufferViews[i]
	buf, err := l.buffer(bv.Buffer)
	if err != nil {
		return nil, nil, err
	}
	if bv.ByteOffset < 0 || bv.ByteLength < 0 || int64(bv.ByteOffset)+int64(bv.ByteLength) > int64(len(buf)) {
		return nil, nil, fmt.Errorf("gltf: buffer view %d (offset %d, length %d) is outside its buffer of %d bytes", i, bv.ByteOffset, bv.ByteLength, len(buf))
	}
	if bv.ByteStride != 0 && (bv.ByteStride < 4 || bv.ByteStride > 252 || bv.ByteStride%4 != 0) {
		return nil, nil, fmt.Errorf("gltf: buffer view %d has a byteStride of %d (it must be a multiple of 4 from 4 to 252)", i, bv.ByteStride)
	}
	return buf[bv.ByteOffset : bv.ByteOffset+bv.ByteLength], bv, nil
}

// accessor decodes accessor i into an elems view, validating every range the specification
// requires, and applies sparse substitutions.
func (l *loader) accessor(i int) (*elems, error) {
	if i < 0 || i >= len(l.doc.Accessors) {
		return nil, fmt.Errorf("gltf: accessor %d does not exist (the asset has %d)", i, len(l.doc.Accessors))
	}
	a := &l.doc.Accessors[i]
	size := componentSize(a.ComponentType)
	nc := typeComponents(a.Type)
	if size == 0 {
		return nil, fmt.Errorf("gltf: accessor %d has the component type %d, which is not supported", i, a.ComponentType)
	}
	if nc == 0 {
		return nil, fmt.Errorf("gltf: accessor %d has the type %q, which this loader does not read", i, a.Type)
	}
	if a.Normalized && (a.ComponentType == ctUInt || a.ComponentType == ctFloat) {
		return nil, fmt.Errorf("gltf: accessor %d is normalized but its component type %d cannot be", i, a.ComponentType)
	}
	if a.Count < 0 || a.Count > maxElements || int64(a.Count)*int64(size*nc) > maxBytes {
		return nil, fmt.Errorf("gltf: accessor %d has %d elements, which is more than this loader reads", i, a.Count)
	}
	esize := size * nc
	e := &elems{count: a.Count, ncomp: nc, ctype: a.ComponentType, normalized: a.Normalized, stride: esize}
	if a.BufferView != nil {
		if err := l.bindView(i, a, e, esize); err != nil {
			return nil, err
		}
	}
	if a.Sparse != nil {
		return l.applySparse(i, a, e, esize)
	}
	return e, nil
}

// bindView points e at the accessor's buffer view, after checking that every element lies inside it.
func (l *loader) bindView(i int, a *accessor, e *elems, esize int) error {
	data, bv, err := l.view(*a.BufferView)
	if err != nil {
		return fmt.Errorf("accessor %d: %w", i, err)
	}
	stride := esize
	if bv.ByteStride != 0 {
		stride = bv.ByteStride
		if stride < esize {
			return fmt.Errorf("gltf: accessor %d: the buffer view's byteStride (%d) is smaller than one element (%d)", i, stride, esize)
		}
	}
	if a.ByteOffset < 0 {
		return fmt.Errorf("gltf: accessor %d has a negative byteOffset", i)
	}
	if a.Count > 0 && int64(a.ByteOffset)+int64(a.Count-1)*int64(stride)+int64(esize) > int64(len(data)) {
		return fmt.Errorf("gltf: accessor %d reads past the end of its buffer view", i)
	}
	e.data, e.offset, e.stride = data, a.ByteOffset, stride
	return nil
}

// applySparse copies the accessor into tight storage and overwrites the elements the sparse
// object lists (glTF 2.0, "Sparse Accessors").
func (l *loader) applySparse(i int, a *accessor, base *elems, esize int) (*elems, error) {
	s := a.Sparse
	if s.Count < 1 || s.Count > a.Count {
		return nil, fmt.Errorf("gltf: accessor %d has a sparse count of %d for %d elements", i, s.Count, a.Count)
	}
	dense := make([]byte, a.Count*esize)
	if base.data != nil {
		for k := 0; k < a.Count; k++ {
			copy(dense[k*esize:(k+1)*esize], base.data[base.offset+k*base.stride:])
		}
	}
	idxData, _, err := l.view(s.Indices.BufferView)
	if err != nil {
		return nil, fmt.Errorf("accessor %d sparse indices: %w", i, err)
	}
	isz := componentSize(s.Indices.ComponentType)
	if isz == 0 || s.Indices.ComponentType == ctByte || s.Indices.ComponentType == ctShort || s.Indices.ComponentType == ctFloat {
		return nil, fmt.Errorf("gltf: accessor %d sparse indices have the component type %d (they must be unsigned)", i, s.Indices.ComponentType)
	}
	valData, _, err := l.view(s.Values.BufferView)
	if err != nil {
		return nil, fmt.Errorf("accessor %d sparse values: %w", i, err)
	}
	if int64(s.Indices.ByteOffset)+int64(s.Count)*int64(isz) > int64(len(idxData)) ||
		int64(s.Values.ByteOffset)+int64(s.Count)*int64(esize) > int64(len(valData)) {
		return nil, fmt.Errorf("gltf: accessor %d sparse data runs past its buffer view", i)
	}
	ie := &elems{data: idxData, offset: s.Indices.ByteOffset, stride: isz, count: s.Count, ncomp: 1, ctype: s.Indices.ComponentType}
	for k := 0; k < s.Count; k++ {
		at := int(ie.uint(k))
		if at >= a.Count {
			return nil, fmt.Errorf("gltf: accessor %d sparse index %d is outside its %d elements", i, at, a.Count)
		}
		copy(dense[at*esize:(at+1)*esize], valData[s.Values.ByteOffset+k*esize:s.Values.ByteOffset+(k+1)*esize])
	}
	return &elems{data: dense, stride: esize, count: a.Count, ncomp: base.ncomp, ctype: base.ctype, normalized: base.normalized}, nil
}
