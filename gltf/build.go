package gltf

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/gogpu/g3d"
)

type geomKey struct {
	mesh, prim int
	flip       bool   // the instance's transform mirrors the geometry, so its winding is reversed
	uv         uvSpec // the texture coordinates the geometry carries (set and transform of the base color texture)
}

// bbox is a box accumulated in float64.
type bbox struct {
	min, max [3]float64
	set      bool
}

func (b *bbox) add(p [3]float64) {
	if !b.set {
		b.min, b.max, b.set = p, p, true
		return
	}
	for i := 0; i < 3; i++ {
		b.min[i] = math.Min(b.min[i], p[i])
		b.max[i] = math.Max(b.max[i], p[i])
	}
}

func (b bbox) aabb() g3d.AABB {
	if !b.set {
		return g3d.AABB{}
	}
	return g3d.AABB{
		Min: g3d.Vec3{X: float32(b.min[0]), Y: float32(b.min[1]), Z: float32(b.min[2])},
		Max: g3d.Vec3{X: float32(b.max[0]), Y: float32(b.max[1]), Z: float32(b.max[2])},
	}
}

// mat4 is a column-major 4x4 matrix in float64 (the specification asks for double precision when
// node transforms are composed), indexed m[col*4+row] like glTF and g3d.
type mat4 [16]float64

func identity() mat4 { return mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

func (a mat4) mul(b mat4) mat4 {
	var o mat4
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			var s float64
			for k := 0; k < 4; k++ {
				s += a[k*4+r] * b[c*4+k]
			}
			o[c*4+r] = s
		}
	}
	return o
}

func (a mat4) point(p [3]float64) [3]float64 {
	return [3]float64{
		a[0]*p[0] + a[4]*p[1] + a[8]*p[2] + a[12],
		a[1]*p[0] + a[5]*p[1] + a[9]*p[2] + a[13],
		a[2]*p[0] + a[6]*p[1] + a[10]*p[2] + a[14],
	}
}

// det3 is the determinant of the upper-left 3x3 block: negative when the transform mirrors.
func (a mat4) det3() float64 {
	return a[0]*(a[5]*a[10]-a[9]*a[6]) - a[4]*(a[1]*a[10]-a[9]*a[2]) + a[8]*(a[1]*a[6]-a[5]*a[2])
}

// local returns the local transform of a node: its matrix, or translation, rotation and scale
// composed as T*R*S (glTF 2.0, "Transformations").
func (n *node) local() (mat4, error) {
	if len(n.Matrix) != 0 {
		if len(n.Matrix) != 16 {
			return mat4{}, errors.New("the node matrix does not have 16 numbers")
		}
		var m mat4
		copy(m[:], n.Matrix)
		return m, nil
	}
	t, r, s := [3]float64{}, [4]float64{0, 0, 0, 1}, [3]float64{1, 1, 1}
	if len(n.Translation) != 0 {
		if len(n.Translation) != 3 {
			return mat4{}, errors.New("the node translation does not have 3 numbers")
		}
		copy(t[:], n.Translation)
	}
	if len(n.Rotation) != 0 {
		if len(n.Rotation) != 4 {
			return mat4{}, errors.New("the node rotation does not have 4 numbers")
		}
		copy(r[:], n.Rotation)
	}
	if len(n.Scale) != 0 {
		if len(n.Scale) != 3 {
			return mat4{}, errors.New("the node scale does not have 3 numbers")
		}
		copy(s[:], n.Scale)
	}
	x, y, z, w := r[0], r[1], r[2], r[3]
	if l := math.Sqrt(x*x + y*y + z*z + w*w); l > 0 {
		x, y, z, w = x/l, y/l, z/l, w/l
	} else {
		x, y, z, w = 0, 0, 0, 1
	}
	return mat4{
		s[0] * (1 - 2*(y*y+z*z)), s[0] * 2 * (x*y + z*w), s[0] * 2 * (x*z - y*w), 0,
		s[1] * 2 * (x*y - z*w), s[1] * (1 - 2*(x*x+z*z)), s[1] * 2 * (y*z + x*w), 0,
		s[2] * 2 * (x*z + y*w), s[2] * 2 * (y*z - x*w), s[2] * (1 - 2*(x*x+y*y)), 0,
		t[0], t[1], t[2], 1,
	}, nil
}

// decompose splits an affine matrix into translation, rotation (a quaternion) and scale.
// A mirroring matrix gets a negative x scale.
func decompose(m mat4) (t [3]float64, q [4]float64, s [3]float64) {
	t = [3]float64{m[12], m[13], m[14]}
	for c := 0; c < 3; c++ {
		s[c] = math.Sqrt(m[c*4]*m[c*4] + m[c*4+1]*m[c*4+1] + m[c*4+2]*m[c*4+2])
	}
	if m.det3() < 0 {
		s[0] = -s[0]
	}
	var r [9]float64 // rotation matrix, column-major
	for c := 0; c < 3; c++ {
		d := s[c]
		if d == 0 {
			d = 1
		}
		for k := 0; k < 3; k++ {
			r[c*3+k] = m[c*4+k] / d
		}
	}
	// quaternion from the rotation matrix (Shepperd's method)
	m00, m10, m20 := r[0], r[1], r[2]
	m01, m11, m21 := r[3], r[4], r[5]
	m02, m12, m22 := r[6], r[7], r[8]
	tr := m00 + m11 + m22
	switch {
	case tr > 0:
		k := math.Sqrt(tr+1) * 2
		q = [4]float64{(m21 - m12) / k, (m02 - m20) / k, (m10 - m01) / k, k / 4}
	case m00 > m11 && m00 > m22:
		k := math.Sqrt(1+m00-m11-m22) * 2
		q = [4]float64{k / 4, (m01 + m10) / k, (m02 + m20) / k, (m21 - m12) / k}
	case m11 > m22:
		k := math.Sqrt(1+m11-m00-m22) * 2
		q = [4]float64{(m01 + m10) / k, k / 4, (m12 + m21) / k, (m02 - m20) / k}
	default:
		k := math.Sqrt(1+m22-m00-m11) * 2
		q = [4]float64{(m02 + m20) / k, (m12 + m21) / k, k / 4, (m10 - m01) / k}
	}
	return t, q, s
}

// errSkipped says a primitive is left out (points and lines, no POSITION, ...); the reason is in the warnings.
var errSkipped = errors.New("gltf: primitive skipped")

// sceneRoots returns the root node indices of each scene. An asset without scenes gets one scene made
// of the nodes nobody is a child of.
func (l *loader) sceneRoots() [][]int {
	d := l.doc
	if len(d.Scenes) > 0 {
		roots := make([][]int, len(d.Scenes))
		for i := range d.Scenes {
			roots[i] = d.Scenes[i].Nodes
		}
		return roots
	}
	child := map[int]bool{}
	for i := range d.Nodes {
		for _, c := range d.Nodes[i].Children {
			child[c] = true
		}
	}
	var top []int
	for i := range d.Nodes {
		if !child[i] {
			top = append(top, i)
		}
	}
	return [][]int{top}
}

// build turns the document into g3d nodes.
func (l *loader) build() (*Model, error) {
	d := l.doc
	for _, e := range d.ExtensionsUsed {
		if !supportedExtensions[e] {
			l.warnf("the asset uses the extension %s, which is ignored", e)
		}
	}
	roots := l.sceneRoots()
	def := 0
	if d.Scene != nil {
		def = *d.Scene
		if def < 0 || def >= len(roots) {
			return nil, fmt.Errorf("gltf: the default scene %d does not exist (the asset has %d)", def, len(roots))
		}
	}
	m := &Model{}
	for si, top := range roots {
		root := g3d.NewGroup()
		if si < len(d.Scenes) && d.Scenes[si].Name != "" {
			root.SetName(d.Scenes[si].Name)
		}
		l.bounds = bbox{}
		visiting := map[int]bool{}
		for _, ni := range top {
			if err := l.addNode(&root.Node, ni, identity(), visiting); err != nil {
				return nil, err
			}
		}
		m.Scenes = append(m.Scenes, root)
		if si == def {
			m.Root, m.Bounds = root, l.bounds.aabb()
		}
	}
	m.Warnings = l.warnings
	return m, nil
}

func (l *loader) addNode(parent *g3d.Node, ni int, parentWorld mat4, visiting map[int]bool) error {
	if ni < 0 || ni >= len(l.doc.Nodes) {
		return fmt.Errorf("gltf: node %d does not exist (the asset has %d)", ni, len(l.doc.Nodes))
	}
	if visiting[ni] {
		return fmt.Errorf("gltf: node %d is its own ancestor", ni)
	}
	visiting[ni] = true
	defer delete(visiting, ni)
	n := &l.doc.Nodes[ni]
	if n.Skin != nil {
		l.warnf("skins are ignored: skinned meshes are drawn in their rest pose")
	}
	if n.Camera != nil {
		l.warnf("cameras are ignored")
	}
	loc, err := n.local()
	if err != nil {
		return fmt.Errorf("gltf: node %d: %w", ni, err)
	}
	world := parentWorld.mul(loc)
	gn := g3d.NewNode()
	name := n.Name
	if name == "" {
		name = fmt.Sprintf("node%d", ni)
	}
	gn.SetName(name)
	t, q, s := decompose(loc)
	gn.SetPosition(g3d.Vec3{X: float32(t[0]), Y: float32(t[1]), Z: float32(t[2])})
	gn.SetRotation(g3d.Quat{X: float32(q[0]), Y: float32(q[1]), Z: float32(q[2]), W: float32(q[3])}.ToEuler())
	gn.SetScale(g3d.Vec3{X: float32(s[0]), Y: float32(s[1]), Z: float32(s[2])})
	parent.Add(gn)
	if n.Mesh != nil {
		if err := l.addMesh(gn, *n.Mesh, world); err != nil {
			return fmt.Errorf("gltf: node %d: %w", ni, err)
		}
	}
	for _, c := range n.Children {
		if err := l.addNode(gn, c, world, visiting); err != nil {
			return err
		}
	}
	return nil
}

func (l *loader) addMesh(gn *g3d.Node, mi int, world mat4) error {
	if mi < 0 || mi >= len(l.doc.Meshes) {
		return fmt.Errorf("mesh %d does not exist (the asset has %d)", mi, len(l.doc.Meshes))
	}
	msh := &l.doc.Meshes[mi]
	flip := world.det3() < 0
	for pi := range msh.Primitives {
		geom, err := l.geometry(mi, pi, flip)
		if errors.Is(err, errSkipped) {
			continue
		}
		if err != nil {
			return fmt.Errorf("mesh %d primitive %d: %w", mi, pi, err)
		}
		// grow the bounds with the vertices in world space
		v := geom.Vertices()
		for i := 0; i+7 < len(v); i += 8 {
			l.bounds.add(world.point([3]float64{float64(v[i]), float64(v[i+1]), float64(v[i+2])}))
		}
		mesh := g3d.NewMesh(geom, l.material(msh.Primitives[pi].Material))
		if msh.Name != "" {
			mesh.MeshNode().SetName(msh.Name)
		}
		gn.Add(mesh.MeshNode())
	}
	return nil
}

// attrs are the vertex attributes of a primitive that the loader reads.
type attrs struct {
	pos, norm, uv *elems
	spec          uvSpec // how the texture coordinates are transformed
}

// uvAt returns the texture coordinates of vertex i, with the KHR_texture_transform of the material applied.
func (a attrs) uvAt(i int) (u, v float32) {
	if a.uv == nil {
		return 0, 0
	}
	u, v = a.uv.float(i, 0), a.uv.float(i, 1)
	if a.spec.has {
		m := a.spec.m
		return m[0]*u + m[1]*v + m[2], m[3]*u + m[4]*v + m[5]
	}
	return u, v
}

// readAttrs reads POSITION (required), NORMAL and TEXCOORD_0, and says what is skipped.
func (l *loader) readAttrs(p *primitive, uv uvSpec) (attrs, error) {
	a := attrs{spec: uv}
	pos, ok := p.Attributes["POSITION"]
	if !ok {
		l.warnf("a primitive without POSITION is skipped")
		return a, errSkipped
	}
	var err error
	if a.pos, err = l.accessor(pos); err != nil {
		return a, err
	}
	if a.pos.ncomp != 3 {
		return a, errors.New("POSITION must be a VEC3")
	}
	if i, ok := p.Attributes["NORMAL"]; ok {
		if a.norm, err = l.accessor(i); err != nil {
			return a, err
		}
		if a.norm.ncomp != 3 || a.norm.count != a.pos.count {
			return a, errors.New("NORMAL must be a VEC3 with as many elements as POSITION")
		}
	}
	name := fmt.Sprintf("TEXCOORD_%d", uv.set)
	if i, ok := p.Attributes[name]; ok {
		if a.uv, err = l.accessor(i); err != nil {
			return a, err
		}
		if a.uv.ncomp != 2 || a.uv.count != a.pos.count {
			return a, fmt.Errorf("%s must be a VEC2 with as many elements as POSITION", name)
		}
	} else if p.Material != nil && l.baseColor(p.Material) != nil {
		l.warnf("a primitive has a base color texture but no %s: the texture reads one texel", name)
	}
	if _, ok := p.Attributes["COLOR_0"]; ok {
		l.warnf("vertex colors (COLOR_0) are ignored")
	}
	if _, ok := p.Attributes["TANGENT"]; ok {
		l.warnf("tangents are ignored")
	}
	return a, nil
}

// readIndices reads the vertex indices of a primitive, or counts up when it has none.
func (l *loader) readIndices(p *primitive, vertices int) ([]uint32, error) {
	if p.Indices == nil {
		idx := make([]uint32, vertices)
		for i := range idx {
			idx[i] = uint32(i)
		}
		return idx, nil
	}
	ie, err := l.accessor(*p.Indices)
	if err != nil {
		return nil, err
	}
	if ie.ncomp != 1 || ie.ctype == ctByte || ie.ctype == ctShort || ie.ctype == ctFloat {
		return nil, errors.New("indices must be a SCALAR of an unsigned integer type")
	}
	idx := make([]uint32, ie.count)
	for i := range idx {
		idx[i] = ie.uint(i)
		if int64(idx[i]) >= int64(vertices) {
			return nil, fmt.Errorf("index %d points at vertex %d but there are %d", i, idx[i], vertices)
		}
	}
	return idx, nil
}

// geometry builds (and caches) the geometry of one primitive: positions, normals (flat when the
// asset has none) and the first texture coordinates, as a triangle list. It returns errSkipped for
// a primitive the loader leaves out.
func (l *loader) geometry(mi, pi int, flip bool) (*g3d.BufferGeometry, error) {
	p := &l.doc.Meshes[mi].Primitives[pi]
	var uv uvSpec
	if slot := l.baseColor(p.Material); slot != nil {
		uv = slot.uv
	}
	key := geomKey{mi, pi, flip, uv}
	if g, ok := l.geoms[key]; ok {
		if g == nil {
			return nil, errSkipped
		}
		return g, nil
	}
	g, err := l.buildGeometry(p, flip, uv)
	if errors.Is(err, errSkipped) {
		l.geoms[key] = nil
	}
	if err != nil {
		return nil, err
	}
	l.geoms[key] = g
	return g, nil
}

func (l *loader) buildGeometry(p *primitive, flip bool, uv uvSpec) (*g3d.BufferGeometry, error) {
	mode := 4
	if p.Mode != nil {
		mode = *p.Mode
	}
	if mode < 4 || mode > 6 {
		l.warnf("points and lines are ignored (primitive mode %d)", mode)
		return nil, errSkipped
	}
	if len(p.Targets) > 0 && string(p.Targets) != "null" {
		l.warnf("morph targets are ignored")
	}
	a, err := l.readAttrs(p, uv)
	if err != nil {
		return nil, err
	}
	idx, err := l.readIndices(p, a.pos.count)
	if err != nil {
		return nil, err
	}
	tris, err := triangles(idx, mode)
	if err != nil {
		return nil, err
	}
	if flip { // a mirroring transform turns the front faces around
		for i := 0; i+2 < len(tris); i += 3 {
			tris[i+1], tris[i+2] = tris[i+2], tris[i+1]
		}
	}
	if a.norm != nil {
		return g3d.NewBufferGeometry(smoothVertices(a), tris), nil
	}
	verts, out := flatVertices(a, tris)
	return g3d.NewBufferGeometry(verts, out), nil
}

// smoothVertices lays out one vertex per accessor element: position, normal, uv.
func smoothVertices(a attrs) []float32 {
	verts := make([]float32, 0, a.pos.count*8)
	for i := 0; i < a.pos.count; i++ {
		u, v := a.uvAt(i)
		verts = append(verts, a.pos.float(i, 0), a.pos.float(i, 1), a.pos.float(i, 2), a.norm.float(i, 0), a.norm.float(i, 1), a.norm.float(i, 2), u, v)
	}
	return verts
}

// flatVertices gives every triangle its own three vertices with the normal of the triangle (what the
// specification says an asset without NORMAL looks like), and the indices that go with them.
func flatVertices(a attrs, tris []uint32) ([]float32, []uint32) {
	verts := make([]float32, 0, len(tris)*8)
	out := make([]uint32, 0, len(tris))
	var next uint32
	for t := 0; t+2 < len(tris); t += 3 {
		var p [3][3]float32
		for k := 0; k < 3; k++ {
			i := int(tris[t+k])
			p[k] = [3]float32{a.pos.float(i, 0), a.pos.float(i, 1), a.pos.float(i, 2)}
		}
		n := faceNormal(p[0], p[1], p[2])
		for k := 0; k < 3; k++ {
			u, v := a.uvAt(int(tris[t+k]))
			verts = append(verts, p[k][0], p[k][1], p[k][2], n[0], n[1], n[2], u, v)
			out = append(out, next)
			next++
		}
	}
	return verts, out
}

func faceNormal(a, b, c [3]float32) [3]float32 {
	ux, uy, uz := b[0]-a[0], b[1]-a[1], b[2]-a[2]
	vx, vy, vz := c[0]-a[0], c[1]-a[1], c[2]-a[2]
	nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
	l := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
	if l == 0 {
		return [3]float32{0, 1, 0}
	}
	return [3]float32{nx / l, ny / l, nz / l}
}

// triangles turns indices read with a primitive mode into a triangle list. A strip alternates the
// order of every other triangle to keep the winding, and a fan shares the first vertex.
func triangles(idx []uint32, mode int) ([]uint32, error) {
	switch mode {
	case 4:
		if len(idx)%3 != 0 {
			return nil, fmt.Errorf("a triangle list has %d indices, which is not a multiple of 3", len(idx))
		}
		return append([]uint32(nil), idx...), nil
	case 5:
		if len(idx) < 3 {
			return nil, errors.New("a triangle strip needs at least 3 vertices")
		}
		out := make([]uint32, 0, (len(idx)-2)*3)
		for i := 0; i+2 < len(idx); i++ {
			if i%2 == 0 {
				out = append(out, idx[i], idx[i+1], idx[i+2])
			} else {
				out = append(out, idx[i+1], idx[i], idx[i+2])
			}
		}
		return out, nil
	case 6:
		if len(idx) < 3 {
			return nil, errors.New("a triangle fan needs at least 3 vertices")
		}
		out := make([]uint32, 0, (len(idx)-2)*3)
		for i := 1; i+1 < len(idx); i++ {
			out = append(out, idx[0], idx[i], idx[i+1])
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported mode %d", mode)
}

// material converts a glTF material into a g3d material (cached by index). A missing material is the
// default material of the specification: white, fully metallic and rough, lit by the scene.
func (l *loader) material(mi *int) g3d.Material {
	key := -1
	if mi != nil {
		key = *mi
	}
	if m, ok := l.mats[key]; ok {
		return m
	}
	var out g3d.Material
	if mi == nil || *mi < 0 || *mi >= len(l.doc.Materials) {
		out = g3d.NewStandardMaterial(g3d.WithColor(g3d.White), g3d.WithMetallic(1), g3d.WithRoughness(1))
	} else {
		out = l.convertMaterial(&l.doc.Materials[*mi], l.baseColor(mi))
	}
	l.mats[key] = out
	return out
}

// factors reads the metallic-roughness factors (with the specification's defaults) and warns about
// the textures it skips.
func (l *loader) factors(m *material) (color [4]float64, metallic, roughness float64) {
	color, metallic, roughness = [4]float64{1, 1, 1, 1}, 1, 1
	if m.PBR != nil {
		if len(m.PBR.BaseColorFactor) == 4 {
			copy(color[:], m.PBR.BaseColorFactor)
		}
		if m.PBR.MetallicFactor != nil {
			metallic = *m.PBR.MetallicFactor
		}
		if m.PBR.RoughnessFactor != nil {
			roughness = *m.PBR.RoughnessFactor
		}
	}
	l.warnTextures(m)
	return color, metallic, roughness
}

// warnTextures says which texture slots of a material the loader does not use.
func (l *loader) warnTextures(m *material) {
	slots := []struct {
		name string
		raw  json.RawMessage
	}{
		{"normalTexture", m.NormalTexture}, {"occlusionTexture", m.OcclusionTexture}, {"emissiveTexture", m.EmissiveTexture},
	}
	if m.PBR != nil {
		slots = append(slots, struct {
			name string
			raw  json.RawMessage
		}{"metallicRoughnessTexture", m.PBR.MetallicRoughnessTexture})
	}
	for _, t := range slots {
		if len(t.raw) > 0 {
			l.warnf("textures are ignored (%s)", t.name)
		}
	}
}

// convertMaterial builds the g3d material of m. glTF colors are linear and g3d colors are sRGB (the colors you
// would write in CSS), so the factors are converted with ToSRGB: an unlit material then shows the color glTF
// means, and with color management on the lighting is done in linear light as glTF specifies.
func (l *loader) convertMaterial(m *material, slot *mapSlot) g3d.Material {
	color, metallic, roughness := l.factors(m)
	mode := g3d.AlphaModeOpaque
	switch m.AlphaMode {
	case "MASK":
		mode = g3d.AlphaModeMask
	case "BLEND":
		mode = g3d.AlphaModeBlend
	}
	alpha := float32(color[3])
	if mode == g3d.AlphaModeOpaque {
		alpha = 1
	}
	col := g3d.RGBA(float32(color[0]), float32(color[1]), float32(color[2]), alpha).ToSRGB()
	var colorMap *g3d.Texture
	if slot != nil {
		colorMap = slot.tex
	}
	if _, unlit := m.Extensions["KHR_materials_unlit"]; unlit {
		return g3d.NewBasicMaterial(g3d.WithColor(col), g3d.WithDoubleSided(m.DoubleSided), g3d.WithMap(colorMap))
	}
	opts := []g3d.MaterialOption{
		g3d.WithColor(col), g3d.WithMetallic(float32(metallic)), g3d.WithRoughness(float32(roughness)),
		g3d.WithDoubleSided(m.DoubleSided), g3d.WithAlphaMode(mode), g3d.WithMap(colorMap),
	}
	if mode == g3d.AlphaModeMask {
		cutoff := 0.5
		if m.AlphaCutoff != nil {
			cutoff = *m.AlphaCutoff
		}
		opts = append(opts, g3d.WithAlphaCutoff(float32(cutoff)))
	}
	if len(m.EmissiveFactor) == 3 {
		k := emissiveStrength(m)
		opts = append(opts, g3d.WithEmissive(g3d.RGB(float32(m.EmissiveFactor[0]*k), float32(m.EmissiveFactor[1]*k), float32(m.EmissiveFactor[2]*k)).ToSRGB()))
	}
	return g3d.NewStandardMaterial(opts...)
}

// emissiveStrength is the multiplier of KHR_materials_emissive_strength, 1 without it.
func emissiveStrength(m *material) float64 {
	raw, ok := m.Extensions["KHR_materials_emissive_strength"]
	if !ok {
		return 1
	}
	var e struct {
		EmissiveStrength *float64 `json:"emissiveStrength"`
	}
	if json.Unmarshal(raw, &e) == nil && e.EmissiveStrength != nil {
		return *e.EmissiveStrength
	}
	return 1
}
