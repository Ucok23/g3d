package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gogpu/g3d"
)

// --- helpers that build small assets in memory ---

func f32s(v ...float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func u16s(v ...uint16) []byte {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], x)
	}
	return b
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// glb wraps a JSON text and a binary payload in a GLB container.
func glb(jsonText string, bin []byte) []byte {
	j := []byte(jsonText)
	for len(j)%4 != 0 {
		j = append(j, ' ')
	}
	bin = pad4(append([]byte(nil), bin...))
	total := 12 + 8 + len(j)
	if len(bin) > 0 {
		total += 8 + len(bin)
	}
	out := make([]byte, 0, total)
	out = binary.LittleEndian.AppendUint32(out, 0x46546C67)
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(total))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(j)))
	out = binary.LittleEndian.AppendUint32(out, 0x4E4F534A)
	out = append(out, j...)
	if len(bin) > 0 {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(bin)))
		out = binary.LittleEndian.AppendUint32(out, 0x004E4942)
		out = append(out, bin...)
	}
	return out
}

// triangleBin is one counter-clockwise triangle in the XY plane: 3 positions, 3 normals (+Z) and 3 uint16 indices.
func triangleBin() []byte {
	b := f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)
	b = append(b, f32s(0, 0, 1, 0, 0, 1, 0, 0, 1)...)
	return append(b, pad4(u16s(0, 1, 2))...)
}

const triangleJSON = `{
 "asset":{"version":"2.0"},
 "scene":0,"scenes":[{"nodes":[0]}],
 "nodes":[{"name":"tri","mesh":0%s}],
 "meshes":[{"primitives":[{"attributes":{"POSITION":0,"NORMAL":1},"indices":2%s}]}],
 %s
 "accessors":[
  {"bufferView":0,"componentType":5126,"count":3,"type":"VEC3","min":[0,0,0],"max":[1,1,0]},
  {"bufferView":1,"componentType":5126,"count":3,"type":"VEC3"},
  {"bufferView":2,"componentType":5123,"count":3,"type":"SCALAR"}],
 "bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":36},{"buffer":0,"byteOffset":72,"byteLength":6}],
 "buffers":[{"byteLength":80}]}`

func triangleGLB(nodeExtra, primExtra, materials string) []byte {
	return glb(fmt.Sprintf(triangleJSON, nodeExtra, primExtra, materials), triangleBin())
}

func mustParse(t *testing.T, data []byte, opts ...Option) *Model {
	t.Helper()
	m, err := Parse(data, opts...)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

// firstMesh returns the first mesh found under n.
func firstMesh(n *g3d.Node) *g3d.Mesh {
	if m, ok := n.UserData().(*g3d.Mesh); ok {
		return m
	}
	for _, c := range n.Children() {
		if m := firstMesh(c); m != nil {
			return m
		}
	}
	return nil
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

// --- the happy path ---

func TestParseGLBTriangle(t *testing.T) {
	m := mustParse(t, triangleGLB("", "", ""))
	if m.Root == nil || len(m.Scenes) != 1 {
		t.Fatalf("Root %v, scenes %d", m.Root, len(m.Scenes))
	}
	mesh := firstMesh(&m.Root.Node)
	if mesh == nil {
		t.Fatal("no mesh in the scene")
	}
	g := mesh.Geometry()
	if g.VertexCount() != 3 || len(g.Indices()) != 3 {
		t.Fatalf("vertices %d, indices %d, want 3 and 3", g.VertexCount(), len(g.Indices()))
	}
	v := g.Vertices()
	// vertex 1: position (1,0,0), normal (0,0,1), uv (0,0)
	if !near(v[8], 1) || !near(v[9], 0) || !near(v[13], 1) {
		t.Errorf("vertex 1 = %v", v[8:16])
	}
	if got := m.Bounds; !near(got.Min.X, 0) || !near(got.Max.X, 1) || !near(got.Max.Y, 1) || !near(got.Max.Z, 0) {
		t.Errorf("bounds = %+v", got)
	}
	if len(m.Warnings) != 0 {
		t.Errorf("warnings = %v", m.Warnings)
	}
}

func TestDefaultMaterialIsWhiteMetallicRough(t *testing.T) {
	mat := firstMesh(&mustParse(t, triangleGLB("", "", "")).Root.Node).Material().(*g3d.StandardMaterial)
	if mat.Color() != g3d.White || mat.Metallic() != 1 || mat.Roughness() != 1 {
		t.Errorf("default material = %+v", mat)
	}
}

func TestFlatNormalsWhenNormalIsMissing(t *testing.T) {
	js := strings.Replace(fmt.Sprintf(triangleJSON, "", "", ""), `"POSITION":0,"NORMAL":1`, `"POSITION":0`, 1)
	g := firstMesh(&mustParse(t, glb(js, triangleBin())).Root.Node).Geometry()
	v := g.Vertices()
	for i := 0; i < 3; i++ {
		if !near(v[i*8+3], 0) || !near(v[i*8+4], 0) || !near(v[i*8+5], 1) {
			t.Errorf("vertex %d normal = %v, want +Z", i, v[i*8+3:i*8+6])
		}
	}
}

func TestStridedInterleavedAndNormalizedAccessors(t *testing.T) {
	// one buffer view with a stride of 24: position (3 float) + uv (2 unorm16 + 2 padding bytes) per vertex
	bin := make([]byte, 0, 72)
	for _, p := range [][3]float32{{0, 0, 0}, {2, 0, 0}, {0, 2, 0}} {
		bin = append(bin, f32s(p[0], p[1], p[2])...)
		uv := [2]uint16{0, 65535}
		if p[0] == 2 {
			uv = [2]uint16{65535, 0}
		}
		bin = append(bin, u16s(uv[0], uv[1], 0, 0)...)
		bin = append(bin, f32s(0)...) // pad the vertex to 24 bytes
	}
	js := `{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
	"meshes":[{"primitives":[{"attributes":{"POSITION":0,"TEXCOORD_0":1}}]}],
	"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},
	             {"bufferView":0,"byteOffset":12,"componentType":5123,"normalized":true,"count":3,"type":"VEC2"}],
	"bufferViews":[{"buffer":0,"byteLength":72,"byteStride":24}],"buffers":[{"byteLength":72}]}`
	g := firstMesh(&mustParse(t, glb(js, bin)).Root.Node).Geometry()
	v := g.Vertices()                                         // flat normals: vertices are rebuilt, 8 floats each
	if !near(v[8], 2) || !near(v[14], 1) || !near(v[15], 0) { // vertex 1: x = 2, uv = (1, 0)
		t.Errorf("vertex 1 = %v", v[8:16])
	}
	if !near(v[7], 1) { // vertex 0: uv.y = 65535/65535
		t.Errorf("vertex 0 uv.y = %v", v[7])
	}
}

func TestTriangleStripAndFan(t *testing.T) {
	strip, err := triangles([]uint32{0, 1, 2, 3, 4}, 5)
	if err != nil {
		t.Fatal(err)
	}
	// alternate triangles swap two vertices so every triangle keeps the same winding
	want := []uint32{0, 1, 2, 2, 1, 3, 2, 3, 4}
	if fmt.Sprint(strip) != fmt.Sprint(want) {
		t.Errorf("strip = %v, want %v", strip, want)
	}
	fan, err := triangles([]uint32{9, 1, 2, 3}, 6)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{9, 1, 2, 9, 2, 3}; fmt.Sprint(fan) != fmt.Sprint(want) {
		t.Errorf("fan = %v, want %v", fan, want)
	}
	if _, err := triangles([]uint32{0, 1, 2, 3}, 4); err == nil {
		t.Error("a triangle list of 4 indices should be refused")
	}
}

// --- nodes and transforms ---

func TestNodeTransformsComposeAsTRS(t *testing.T) {
	// translate (1,2,3), rotate 90 degrees about +Y, scale 2: the vertex (1,0,0) ends at (1,2,3) + R*(2,0,0) = (1,2,1)
	s := math.Sin(math.Pi / 4)
	node := fmt.Sprintf(`,"translation":[1,2,3],"rotation":[0,%v,0,%v],"scale":[2,2,2]`, s, s)
	m := mustParse(t, triangleGLB(node, "", ""))
	got := m.Bounds
	// the triangle spans x 0..1, y 0..1 in the plane z=0; after the transform its corner (1,0,0) is at (1,2,1)
	if !near(got.Min.Y, 2) || !near(got.Max.Y, 4) {
		t.Errorf("y range = %v..%v, want 2..4", got.Min.Y, got.Max.Y)
	}
	if !near(got.Min.X, 1) || !near(got.Max.X, 1) || !near(got.Min.Z, 1) || !near(got.Max.Z, 3) {
		t.Errorf("bounds = %+v", got)
	}
	n := m.Root.Children()[0]
	if !near(n.Position.X, 1) || !near(n.Position.Y, 2) || !near(n.Scale.X, 2) {
		t.Errorf("node position %v scale %v", n.Position, n.Scale)
	}
}

func TestNodeMatrixIsColumnMajor(t *testing.T) {
	// a pure translation by (5,6,7) written column-major
	node := `,"matrix":[1,0,0,0, 0,1,0,0, 0,0,1,0, 5,6,7,1]`
	n := mustParse(t, triangleGLB(node, "", "")).Root.Children()[0]
	if !near(n.Position.X, 5) || !near(n.Position.Y, 6) || !near(n.Position.Z, 7) {
		t.Errorf("position = %v", n.Position)
	}
}

func TestMirroringTransformReversesTheWinding(t *testing.T) {
	plain := firstMesh(&mustParse(t, triangleGLB("", "", "")).Root.Node).Geometry().Indices()
	mirrored := firstMesh(&mustParse(t, triangleGLB(`,"scale":[-1,1,1]`, "", "")).Root.Node).Geometry().Indices()
	if plain[1] != mirrored[2] || plain[2] != mirrored[1] {
		t.Errorf("plain %v, mirrored %v: the last two indices should swap", plain, mirrored)
	}
}

func TestNodeCycleIsAnError(t *testing.T) {
	js := `{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"children":[1]},{"children":[0]}]}`
	if _, err := Parse([]byte(js)); err == nil || !strings.Contains(err.Error(), "own ancestor") {
		t.Errorf("err = %v", err)
	}
}

// --- materials ---

func TestMaterialFactorsAlphaAndDoubleSided(t *testing.T) {
	mats := `"materials":[{"pbrMetallicRoughness":{"baseColorFactor":[0.2,0.4,0.6,0.5],"metallicFactor":0.1,"roughnessFactor":0.9},
	  "alphaMode":"MASK","alphaCutoff":0.3,"doubleSided":true,"emissiveFactor":[1,0.5,0],
	  "extensions":{"KHR_materials_emissive_strength":{"emissiveStrength":2}}}],`
	m := mustParse(t, triangleGLB("", `,"material":0`, mats))
	mat := firstMesh(&m.Root.Node).Material().(*g3d.StandardMaterial)
	if mat.Color() != g3d.RGBA(0.2, 0.4, 0.6, 0.5) || !near(mat.Metallic(), 0.1) || !near(mat.Roughness(), 0.9) {
		t.Errorf("material = color %v metallic %v roughness %v", mat.Color(), mat.Metallic(), mat.Roughness())
	}
	if mat.AlphaMode() != g3d.AlphaModeMask || !near(mat.AlphaCutoff(), 0.3) || !mat.DoubleSided() {
		t.Errorf("alpha %v cutoff %v double %v", mat.AlphaMode(), mat.AlphaCutoff(), mat.DoubleSided())
	}
	if e := mat.Emissive(); !near(e.R, 2) || !near(e.G, 1) || !near(e.B, 0) {
		t.Errorf("emissive = %v, want the factor times the strength", e)
	}
}

func TestOpaqueMaterialIgnoresTheAlphaOfTheColor(t *testing.T) {
	mats := `"materials":[{"pbrMetallicRoughness":{"baseColorFactor":[1,1,1,0.25]}}],`
	mat := firstMesh(&mustParse(t, triangleGLB("", `,"material":0`, mats)).Root.Node).Material().(*g3d.StandardMaterial)
	if mat.Color().A != 1 || mat.AlphaMode() != g3d.AlphaModeOpaque {
		t.Errorf("opaque material alpha = %v mode %v", mat.Color().A, mat.AlphaMode())
	}
}

func TestUnlitMaterialIsBasic(t *testing.T) {
	mats := `"materials":[{"extensions":{"KHR_materials_unlit":{}},"pbrMetallicRoughness":{"baseColorFactor":[1,0,0,1]}}],`
	m := mustParse(t, triangleGLB("", `,"material":0`, mats))
	if _, ok := firstMesh(&m.Root.Node).Material().(*g3d.BasicMaterial); !ok {
		t.Errorf("an unlit material should be a BasicMaterial, got %T", firstMesh(&m.Root.Node).Material())
	}
}

func TestMaterialsAreSharedBetweenPrimitives(t *testing.T) {
	js := `{"asset":{"version":"2.0"},"scenes":[{"nodes":[0,1]}],"nodes":[{"mesh":0},{"mesh":0}],
	"meshes":[{"primitives":[{"attributes":{"POSITION":0},"material":0}]}],"materials":[{}],
	"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
	"bufferViews":[{"buffer":0,"byteLength":36}],"buffers":[{"byteLength":36}]}`
	m := mustParse(t, glb(js, f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)))
	a, b := firstMesh(m.Root.Children()[0]), firstMesh(m.Root.Children()[1])
	if a.Material() != b.Material() || a.Geometry() != b.Geometry() {
		t.Error("two instances of one mesh should share the material and the geometry")
	}
}

// --- warnings ---

func TestUnsupportedFeaturesAreWarnedOnce(t *testing.T) {
	mats := `"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}},"normalTexture":{"index":1}}],`
	m := mustParse(t, triangleGLB("", `,"material":0`, mats))
	joined := strings.Join(m.Warnings, "\n")
	for _, want := range []string{"baseColorTexture", "normalTexture"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings %q lack %q", joined, want)
		}
	}
	js := strings.Replace(fmt.Sprintf(triangleJSON, "", "", ""), `"indices":2`, `"indices":2,"mode":1`, 1)
	if w := strings.Join(mustParse(t, glb(js, triangleBin())).Warnings, "\n"); !strings.Contains(w, "lines are ignored") {
		t.Errorf("a line primitive should warn: %q", w)
	}
}

// --- buffers ---

func TestDataURIBuffer(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString(f32s(0, 0, 0, 1, 0, 0, 0, 1, 0))
	js := fmt.Sprintf(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
	"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
	"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
	"bufferViews":[{"buffer":0,"byteLength":36}],
	"buffers":[{"byteLength":36,"uri":"data:application/octet-stream;base64,%s"}]}`, b64)
	if firstMesh(&mustParse(t, []byte(js)).Root.Node) == nil {
		t.Error("a data URI buffer should load")
	}
}

func TestExternalBufferAndPathTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tri.bin"), f32s(0, 0, 0, 1, 0, 0, 0, 1, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	asset := func(uri string) string {
		return fmt.Sprintf(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
		"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
		"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
		"bufferViews":[{"buffer":0,"byteLength":36}],"buffers":[{"byteLength":36,"uri":%q}]}`, uri)
	}
	path := filepath.Join(dir, "tri.gltf")
	if err := os.WriteFile(path, []byte(asset("tri.bin")), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil || firstMesh(&m.Root.Node) == nil {
		t.Fatalf("Load: %v", err)
	}
	for _, uri := range []string{"../tri.bin", "/etc/passwd", "sub/../../tri.bin", "https://example.com/x.bin"} {
		if _, err := Parse([]byte(asset(uri)), WithBaseDir(dir)); err == nil {
			t.Errorf("the uri %q should be refused", uri)
		}
	}
	if _, err := Parse([]byte(asset("tri.bin"))); err == nil || !strings.Contains(err.Error(), "no folder was given") {
		t.Errorf("an external file without a base folder: %v", err)
	}
}

func TestSparseAccessor(t *testing.T) {
	// 3 positions, all zero except element 1, which a sparse substitution sets to (7,8,9)
	bin := append(u16s(1, 0), f32s(7, 8, 9)...) // indices (uint16, padded to 4) then the value
	js := `{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
	"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
	"accessors":[{"componentType":5126,"count":3,"type":"VEC3","sparse":{"count":1,
	   "indices":{"bufferView":0,"componentType":5123},"values":{"bufferView":1}}}],
	"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":4},{"buffer":0,"byteOffset":4,"byteLength":12}],"buffers":[{"byteLength":16}]}`
	g := firstMesh(&mustParse(t, glb(js, bin)).Root.Node).Geometry()
	v := g.Vertices()
	if !near(v[8], 7) || !near(v[9], 8) || !near(v[10], 9) || !near(v[0], 0) {
		t.Errorf("vertices = %v", v)
	}
}

// --- things that must be refused ---

func TestMalformedAssetsAreRefused(t *testing.T) {
	good := triangleGLB("", "", "")
	trunc := good[:len(good)-20]
	badMagic := append([]byte(nil), good...)
	badMagic[0] = 'X'
	oneChunkLong := append([]byte(nil), good...)
	binary.LittleEndian.PutUint32(oneChunkLong[12:], uint32(len(good)))
	for name, c := range map[string]struct {
		data []byte
		want string
	}{
		"short":             {[]byte("glTF"), "shorter than the 12-byte header"},
		"truncated":         {trunc, "header says"},
		"json chunk length": {oneChunkLong, "runs past the end"},
		"not json":          {[]byte("hello"), "JSON cannot be read"},
		"version 1":         {[]byte(`{"asset":{"version":"1.0"}}`), "not supported"},
		"no version":        {[]byte(`{"asset":{}}`), "no asset.version"},
		"min version":       {[]byte(`{"asset":{"version":"2.0","minVersion":"3.0"}}`), "needs glTF 3.0"},
		"draco": {[]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_draco_mesh_compression"],
			"extensionsRequired":["KHR_draco_mesh_compression"]}`), "KHR_draco_mesh_compression"},
		"accessor range": {glb(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
			"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
			"accessors":[{"bufferView":0,"componentType":5126,"count":99,"type":"VEC3"}],
			"bufferViews":[{"buffer":0,"byteLength":36}],"buffers":[{"byteLength":36}]}`, f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)), "past the end of its buffer view"},
		"view outside buffer": {glb(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
			"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
			"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
			"bufferViews":[{"buffer":0,"byteOffset":8,"byteLength":36}],"buffers":[{"byteLength":36}]}`, f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)), "outside its buffer"},
		"bad stride": {glb(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
			"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
			"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
			"bufferViews":[{"buffer":0,"byteLength":36,"byteStride":6}],"buffers":[{"byteLength":36}]}`, f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)), "byteStride"},
		"index out of range": {glb(fmt.Sprintf(triangleJSON, "", "", ""),
			append(append(f32s(0, 0, 0, 1, 0, 0, 0, 1, 0), f32s(0, 0, 1, 0, 0, 1, 0, 0, 1)...), pad4(u16s(0, 1, 9))...)), "points at vertex"},
		"no buffer": {[]byte(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
			"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
			"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
			"bufferViews":[{"buffer":0,"byteLength":36}],"buffers":[{"byteLength":36}]}`), "no GLB BIN chunk"},
		"huge accessor": {glb(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
			"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
			"accessors":[{"bufferView":0,"componentType":5126,"count":2000000000,"type":"VEC3"}],
			"bufferViews":[{"buffer":0,"byteLength":36}],"buffers":[{"byteLength":36}]}`, f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)), "more than this loader reads"},
		"bad scene": {[]byte(`{"asset":{"version":"2.0"},"scene":4,"scenes":[{"nodes":[]}]}`), "default scene 4"},
		"bad node":  {[]byte(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[3]}]}`), "node 3 does not exist"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(c.data)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
	// a GLB that is fine must stay fine
	if _, err := Parse(good); err != nil {
		t.Errorf("the good asset: %v", err)
	}
	if _, _, err := splitGLB(badMagic); err == nil {
		t.Error("a GLB with bad magic bytes should be refused")
	}
}

func TestAssetWithoutScenesTakesTheTopNodes(t *testing.T) {
	js := `{"asset":{"version":"2.0"},"nodes":[{"name":"a","children":[1]},{"name":"b"}]}`
	m := mustParse(t, []byte(js))
	if len(m.Root.Children()) != 1 || m.Root.Children()[0].Name() != "a" {
		t.Errorf("root children = %v", m.Root.Children())
	}
}

func TestEmptyAssetIsAnEmptyScene(t *testing.T) {
	m := mustParse(t, []byte(`{"asset":{"version":"2.0"}}`))
	if m.Root == nil || len(m.Root.Children()) != 0 {
		t.Errorf("root = %v", m.Root)
	}
}

// --- accessor conversions, exactly as the specification defines them ---

func TestNormalizedAndIntegerConversions(t *testing.T) {
	cases := []struct {
		name  string
		ctype int
		norm  bool
		raw   []byte
		want  []float32
	}{
		{"snorm8", ctByte, true, []byte{0x7F, 0x81, 0x80, 0x00}, []float32{1, -1, -1, 0}}, // -127 and -128 both mean -1
		{"unorm8", ctUByte, true, []byte{0, 255, 51, 0}, []float32{0, 1, 0.2, 0}},
		{"sint8", ctByte, false, []byte{0x7F, 0x80, 0, 0}, []float32{127, -128, 0, 0}},
		{"uint8", ctUByte, false, []byte{200, 1, 0, 0}, []float32{200, 1, 0, 0}},
		{"snorm16", ctShort, true, append(u16s(0x7FFF, 0x8000), u16s(0, 0)...), []float32{1, -1, 0, 0}},
		{"unorm16", ctUShort, true, u16s(65535, 0, 32768, 0), []float32{1, 0, 32768.0 / 65535, 0}},
		{"sint16", ctShort, false, u16s(0x8000, 5, 0, 0), []float32{-32768, 5, 0, 0}},
		{"uint16", ctUShort, false, u16s(60000, 1, 0, 0), []float32{60000, 1, 0, 0}},
		{"float32", ctFloat, false, f32s(1.5, -2, 0, 0), []float32{1.5, -2, 0, 0}},
	}
	for _, c := range cases {
		e := &elems{data: c.raw, stride: len(c.raw), count: 1, ncomp: len(c.want), ctype: c.ctype, normalized: c.norm}
		for i, w := range c.want {
			if got := e.float(0, i); !near(got, w) {
				t.Errorf("%s component %d = %v, want %v", c.name, i, got, w)
			}
		}
	}
	u := &elems{data: binary.LittleEndian.AppendUint32(nil, 4294967295), stride: 4, count: 1, ncomp: 1, ctype: ctUInt}
	if got := u.uint(0); got != 4294967295 {
		t.Errorf("uint32 = %d, want it exact (a float32 would lose it)", got)
	}
	var zero elems // an accessor without a buffer view reads as zeros
	if zero.float(0, 0) != 0 || zero.uint(0) != 0 {
		t.Error("an accessor without data should read as zeros")
	}
}

// --- decomposing a node matrix ---

func TestDecomposeRoundTrips(t *testing.T) {
	// every branch of the quaternion extraction: rotations about each axis near 180 degrees, a small one, and a mirrored one
	rots := [][4]float64{
		{0, 0, 0, 1},
		{1, 0, 0, 0},                        // 180 about x
		{0, 1, 0, 0},                        // 180 about y
		{0, 0, 1, 0},                        // 180 about z
		{0.5, 0.5, 0.5, 0.5},                // 120 about (1,1,1)
		{0.1, 0.2, 0.3, 0.9273618495495703}, // an arbitrary one
		{0.7071067811865476, 0, 0, 0.7071067811865476},
	}
	for ri, r := range rots {
		for _, s := range [][3]float64{{1, 1, 1}, {2, 3, 0.5}, {-1, 1, 1}} {
			n := &node{Translation: []float64{1, -2, 3}, Rotation: r[:], Scale: s[:]}
			m, err := n.local()
			if err != nil {
				t.Fatal(err)
			}
			tr, q, sc := decompose(m)
			n2 := &node{Translation: tr[:], Rotation: q[:], Scale: sc[:]}
			m2, _ := n2.local()
			for i := range m {
				if math.Abs(m[i]-m2[i]) > 1e-9 {
					t.Fatalf("rotation %d scale %v: matrix element %d differs: %v vs %v", ri, s, i, m[i], m2[i])
				}
			}
		}
	}
}

func TestBadNodeTransformsAreRefused(t *testing.T) {
	for name, extra := range map[string]string{
		"matrix":      `,"matrix":[1,2,3]`,
		"translation": `,"translation":[1,2]`,
		"rotation":    `,"rotation":[0,0,1]`,
		"scale":       `,"scale":[1]`,
	} {
		if _, err := Parse(triangleGLB(extra, "", "")); err == nil {
			t.Errorf("a %s with the wrong length should be refused", name)
		}
	}
}
