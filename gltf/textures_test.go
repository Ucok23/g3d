package gltf

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gogpu/g3d"
)

// pngBytes encodes a w x h image whose pixels come from at(x, y).
func pngBytes(t testing.TB, w, h int, at func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, at(x, y))
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// quadrants is a 2x2 PNG: red, green on the first row; blue, white on the second.
func quadrants(t testing.TB) []byte {
	return pngBytes(t, 2, 2, func(x, y int) color.NRGBA {
		return [][]color.NRGBA{{{255, 0, 0, 255}, {0, 255, 0, 255}}, {{0, 0, 255, 255}, {255, 255, 255, 255}}}[y][x]
	})
}

// texAsset is a one-triangle asset with texture coordinates (TEXCOORD_0 and TEXCOORD_1), one material that uses
// textures[0], and the image in a buffer view of the GLB.
type texAsset struct {
	material string // the JSON of the material, with %s for the base color texture info
	texture  string // the JSON of textures[0] (default: image 0, no sampler)
	samplers string // the JSON of the samplers array, or ""
	images   string // the JSON of the images array (default: image 0 in the buffer view)
	png      []byte
	attrs    string // extra primitive attributes
	uv1      bool   // add TEXCOORD_1 (mirrored coordinates)
}

func (a texAsset) glb(t testing.TB) []byte {
	if a.png == nil {
		a.png = quadrants(t)
	}
	if a.texture == "" {
		a.texture = `{"source":0}`
	}
	if a.images == "" {
		a.images = `[{"bufferView":2,"mimeType":"image/png"}]`
	}
	if a.material == "" {
		a.material = `{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}}`
	}
	smp := ""
	if a.samplers != "" {
		smp = `"samplers":` + a.samplers + `,`
	}
	// positions (36), TEXCOORD_0 (24), TEXCOORD_1 (24), the image
	bin := f32s(0, 0, 0, 1, 0, 0, 0, 1, 0)
	bin = append(bin, f32s(0, 0, 1, 0, 0, 1)...) // TEXCOORD_0: (0,0) (1,0) (0,1)
	bin = append(bin, f32s(1, 1, 0, 1, 1, 0)...) // TEXCOORD_1: (1,1) (0,1) (1,0)
	imgOff := len(bin)
	bin = append(bin, pad4(a.png)...)
	attrs := `"POSITION":0,"TEXCOORD_0":1` + a.attrs
	if a.uv1 {
		attrs += `,"TEXCOORD_1":3`
	}
	js := fmt.Sprintf(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
	"meshes":[{"primitives":[{"attributes":{%s},"material":0}]}],
	"materials":[%s],"textures":[%s],%s"images":%s,
	"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},
	             {"bufferView":1,"componentType":5126,"count":3,"type":"VEC2"},
	             {"bufferView":1,"byteOffset":0,"componentType":5126,"count":3,"type":"VEC2"},
	             {"bufferView":3,"componentType":5126,"count":3,"type":"VEC2"}],
	"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":24},
	               {"buffer":0,"byteOffset":%d,"byteLength":%d},{"buffer":0,"byteOffset":60,"byteLength":24}],
	"buffers":[{"byteLength":%d}]}`, attrs, a.material, a.texture, smp, a.images, imgOff, len(a.png), len(bin))
	return glb(js, bin)
}

func baseMap(t testing.TB, m *Model) *g3d.Texture {
	t.Helper()
	mesh := firstMesh(&m.Root.Node)
	if mesh == nil {
		t.Fatal("no mesh")
	}
	mm, ok := mesh.Material().(*g3d.StandardMaterial)
	if !ok {
		t.Fatalf("material is %T", mesh.Material())
	}
	return mm.Map()
}

func TestBaseColorTextureFromAGLBBufferView(t *testing.T) {
	m := mustParse(t, texAsset{}.glb(t))
	tex := baseMap(t, m)
	if tex == nil {
		t.Fatalf("no texture; warnings: %v", m.Warnings)
	}
	if tex.Width() != 2 || tex.Height() != 2 {
		t.Errorf("size = %dx%d", tex.Width(), tex.Height())
	}
	if p := tex.Pixels(); p[0] != 255 || p[1] != 0 || p[4] != 0 || p[5] != 255 || p[8+2] != 255 {
		t.Errorf("pixels = %v, want red, green, blue, white", p)
	}
	// no sampler: repeat wrapping and trilinear filtering with mipmaps
	if u, v := tex.Wrap(); u != g3d.WrapRepeat || v != g3d.WrapRepeat {
		t.Errorf("default wrap = %v %v, want repeat", u, v)
	}
	if mag, mn := tex.Filter(); mag != g3d.FilterLinear || mn != g3d.FilterLinear || !tex.Mipmaps() || tex.MipmapFilter() != g3d.FilterLinear {
		t.Errorf("default filtering = %v %v mipmaps %v %v", mag, mn, tex.Mipmaps(), tex.MipmapFilter())
	}
	if len(m.Warnings) != 0 {
		t.Errorf("warnings = %v", m.Warnings)
	}
}

func TestSamplerCodesMapToTextureOptions(t *testing.T) {
	code := func(n int) *int { return &n }
	type want struct {
		mag, mn, mip g3d.TextureFilter
		mips         bool
	}
	for name, c := range map[string]struct {
		min  *int
		want want
	}{
		"nearest":                {code(9728), want{g3d.FilterLinear, g3d.FilterNearest, g3d.FilterLinear, false}},
		"linear":                 {code(9729), want{g3d.FilterLinear, g3d.FilterLinear, g3d.FilterLinear, false}},
		"nearest mipmap nearest": {code(9984), want{g3d.FilterLinear, g3d.FilterNearest, g3d.FilterNearest, true}},
		"linear mipmap nearest":  {code(9985), want{g3d.FilterLinear, g3d.FilterLinear, g3d.FilterNearest, true}},
		"nearest mipmap linear":  {code(9986), want{g3d.FilterLinear, g3d.FilterNearest, g3d.FilterLinear, true}},
		"linear mipmap linear":   {code(9987), want{g3d.FilterLinear, g3d.FilterLinear, g3d.FilterLinear, true}},
		"absent":                 {nil, want{g3d.FilterLinear, g3d.FilterLinear, g3d.FilterLinear, true}},
	} {
		tex, err := g3d.NewTexture(1, 1, []byte{0, 0, 0, 255}, samplerOptions(&sampler{MinFilter: c.min})...)
		if err != nil {
			t.Fatal(err)
		}
		mag, mn := tex.Filter()
		if got := (want{mag, mn, tex.MipmapFilter(), tex.Mipmaps()}); got != c.want {
			t.Errorf("minFilter %s: got %+v, want %+v", name, got, c.want)
		}
	}
	tex, _ := g3d.NewTexture(1, 1, []byte{0, 0, 0, 255}, samplerOptions(&sampler{MagFilter: code(9728), WrapS: code(33071), WrapT: code(33648)})...)
	if mag, _ := tex.Filter(); mag != g3d.FilterNearest {
		t.Errorf("magFilter 9728 = %v, want nearest", mag)
	}
	if u, v := tex.Wrap(); u != g3d.WrapClamp || v != g3d.WrapMirror {
		t.Errorf("wrap 33071, 33648 = %v %v, want clamp and mirror", u, v)
	}
	if tex, _ := g3d.NewTexture(1, 1, []byte{0, 0, 0, 255}, samplerOptions(&sampler{WrapS: code(10497)})...); true {
		if u, _ := tex.Wrap(); u != g3d.WrapRepeat {
			t.Errorf("wrap 10497 = %v, want repeat", u)
		}
	}
}

func TestTheSamplerOfATextureIsUsed(t *testing.T) {
	a := texAsset{texture: `{"source":0,"sampler":0}`, samplers: `[{"magFilter":9728,"minFilter":9728,"wrapS":33071,"wrapT":33071}]`}
	tex := baseMap(t, mustParse(t, a.glb(t)))
	if mag, mn := tex.Filter(); mag != g3d.FilterNearest || mn != g3d.FilterNearest || tex.Mipmaps() {
		t.Errorf("filter = %v %v mipmaps %v", mag, mn, tex.Mipmaps())
	}
	if u, v := tex.Wrap(); u != g3d.WrapClamp || v != g3d.WrapClamp {
		t.Errorf("wrap = %v %v", u, v)
	}
}

func TestImageFromADataURIAndAFile(t *testing.T) {
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(quadrants(t))
	if baseMap(t, mustParse(t, texAsset{images: fmt.Sprintf(`[{"uri":%q}]`, data)}.glb(t))) == nil {
		t.Error("an image in a data URI should load")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "q.png"), quadrants(t), 0o644); err != nil {
		t.Fatal(err)
	}
	asset := texAsset{images: `[{"uri":"q.png"}]`}.glb(t)
	if baseMap(t, mustParse(t, asset, WithBaseDir(dir))) == nil {
		t.Error("an image next to the asset should load")
	}
	// an image the asset's folder does not hold, or one outside it, is skipped with a warning, and the asset still loads
	for _, uri := range []string{"missing.png", "../q.png", "/etc/hostname"} {
		m := mustParse(t, texAsset{images: fmt.Sprintf(`[{"uri":%q}]`, uri)}.glb(t), WithBaseDir(dir))
		if baseMap(t, m) != nil || len(m.Warnings) == 0 {
			t.Errorf("the image %q should be skipped with a warning; warnings: %v", uri, m.Warnings)
		}
	}
}

func TestUnusableImagesAreSkippedWithAWarning(t *testing.T) {
	huge := pngBytes(t, 9000, 1, func(x, y int) color.NRGBA { return color.NRGBA{A: 255} }) // wider than 8192
	for name, c := range map[string]struct {
		a    texAsset
		want string
	}{
		"not an image":         {texAsset{png: []byte("this is not a png at all, just text.")}, "not a PNG or JPEG"},
		"too wide":             {texAsset{png: huge}, "limit"},
		"no source":            {texAsset{texture: `{"extensions":{"EXT_texture_webp":{"source":1}}}`}, "no image source"},
		"image out of range":   {texAsset{texture: `{"source":7}`}, "image 7 does not exist"},
		"image without data":   {texAsset{images: `[{}]`}, "neither a uri nor a bufferView"},
		"view with a stride":   {texAsset{images: `[{"bufferView":0,"mimeType":"image/png"}]`}, "not a PNG or JPEG"},
		"texture out of range": {texAsset{material: `{"pbrMetallicRoughness":{"baseColorTexture":{"index":4}}}`}, "texture 4"},
	} {
		m, err := Parse(c.a.glb(t))
		if err != nil {
			t.Errorf("%s: the asset should still load, got %v", name, err)
			continue
		}
		if baseMap(t, m) != nil {
			t.Errorf("%s: no texture expected", name)
		}
		if w := strings.Join(m.Warnings, "\n"); !strings.Contains(w, c.want) {
			t.Errorf("%s: warnings %q lack %q", name, w, c.want)
		}
	}
}

func TestTheSameTextureIsSharedByMaterials(t *testing.T) {
	js := `{"asset":{"version":"2.0"},"scenes":[{"nodes":[0,1]}],"nodes":[{"mesh":0},{"mesh":1}],
	"meshes":[{"primitives":[{"attributes":{"POSITION":0},"material":0}]},{"primitives":[{"attributes":{"POSITION":0},"material":1}]}],
	"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}},{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}}],
	"textures":[{"source":0}],"images":[{"bufferView":1,"mimeType":"image/png"}],
	"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
	"bufferViews":[{"buffer":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":%d}],"buffers":[{"byteLength":%d}]}`
	img := pad4(quadrants(t))
	bin := append(f32s(0, 0, 0, 1, 0, 0, 0, 1, 0), img...)
	m := mustParse(t, glb(fmt.Sprintf(js, len(img), len(bin)), bin))
	a, b := firstMesh(m.Root.Children()[0]), firstMesh(m.Root.Children()[1])
	if a.Material() == b.Material() {
		t.Fatal("two materials should be two g3d materials")
	}
	if a.Material().(*g3d.StandardMaterial).Map() != b.Material().(*g3d.StandardMaterial).Map() {
		t.Error("one glTF texture should be one g3d.Texture, shared by the materials that use it")
	}
}

// uvs returns the texture coordinates of the three vertices of the first mesh (flat normals: 8 floats per vertex).
func uvs(t testing.TB, m *Model) [3][2]float32 {
	t.Helper()
	v := firstMesh(&m.Root.Node).Geometry().Vertices()
	var out [3][2]float32
	for i := 0; i < 3; i++ {
		out[i] = [2]float32{v[i*8+6], v[i*8+7]}
	}
	return out
}

func TestTexCoordSelectsTheSetOfCoordinates(t *testing.T) {
	// TEXCOORD_0 is (0,0) (1,0) (0,1); TEXCOORD_1 is (1,1) (0,1) (1,0)
	set0 := uvs(t, mustParse(t, texAsset{uv1: true}.glb(t)))
	if set0[1] != [2]float32{1, 0} {
		t.Errorf("texCoord 0: vertex 1 uv = %v, want (1,0)", set0[1])
	}
	mat := `{"pbrMetallicRoughness":{"baseColorTexture":{"index":0,"texCoord":1}}}`
	set1 := uvs(t, mustParse(t, texAsset{material: mat, uv1: true}.glb(t)))
	if set1[0] != [2]float32{1, 1} || set1[1] != [2]float32{0, 1} {
		t.Errorf("texCoord 1: uvs = %v, want (1,1) (0,1) (1,0)", set1)
	}
	// a set the primitive does not have: warned, and the texture reads one texel
	missing := mustParse(t, texAsset{material: mat}.glb(t))
	if w := strings.Join(missing.Warnings, "\n"); !strings.Contains(w, "TEXCOORD_1") {
		t.Errorf("warnings %q should name the missing TEXCOORD_1", w)
	}
}

func TestKHRTextureTransformIsBakedIntoTheCoordinates(t *testing.T) {
	info := func(ext string) string {
		return `{"pbrMetallicRoughness":{"baseColorTexture":{"index":0,"extensions":{"KHR_texture_transform":` + ext + `}}}}`
	}
	// scale (2,3) then offset (0.5, 0.25): uv (1,0) -> (2.5, 0.25); uv (0,1) -> (0.5, 3.25)
	got := uvs(t, mustParse(t, texAsset{material: info(`{"offset":[0.5,0.25],"scale":[2,3]}`)}.glb(t)))
	if !near(got[1][0], 2.5) || !near(got[1][1], 0.25) || !near(got[2][0], 0.5) || !near(got[2][1], 3.25) {
		t.Errorf("offset and scale: uvs = %v", got)
	}
	// a quarter turn counter-clockwise: (1,0) -> (0,1) and (0,1) -> (-1,0)
	got = uvs(t, mustParse(t, texAsset{material: info(fmt.Sprintf(`{"rotation":%v}`, math.Pi/2))}.glb(t)))
	if !near(got[1][0], 0) || !near(got[1][1], 1) || !near(got[2][0], -1) || !near(got[2][1], 0) {
		t.Errorf("rotation: uvs = %v", got)
	}
	// the three act together as translation * rotation * scale: scale (2,1), then a quarter turn, then offset (1,0): (1,0) -> (2,0) -> (0,2) -> (1,2)
	got = uvs(t, mustParse(t, texAsset{material: info(fmt.Sprintf(`{"offset":[1,0],"rotation":%v,"scale":[2,1]}`, math.Pi/2))}.glb(t)))
	if !near(got[1][0], 1) || !near(got[1][1], 2) {
		t.Errorf("combined: vertex 1 = %v, want (1,2)", got[1])
	}
	// the extension can pick the coordinate set too
	set := uvs(t, mustParse(t, texAsset{material: info(`{"texCoord":1}`), uv1: true}.glb(t)))
	if set[0] != [2]float32{1, 1} {
		t.Errorf("texCoord from the extension: uvs = %v", set)
	}
	// the identity transform changes nothing, and two materials with different transforms never share vertices
	if same := uvs(t, mustParse(t, texAsset{material: info(`{}`)}.glb(t))); same[1] != [2]float32{1, 0} {
		t.Errorf("identity transform: uvs = %v", same)
	}
}

func TestGeometryIsNotSharedBetweenDifferentTransforms(t *testing.T) {
	js := `{"asset":{"version":"2.0"},"scenes":[{"nodes":[0,1]}],"nodes":[{"mesh":0},{"mesh":1}],
	"meshes":[{"primitives":[{"attributes":{"POSITION":0,"TEXCOORD_0":1},"material":0}]},{"primitives":[{"attributes":{"POSITION":0,"TEXCOORD_0":1},"material":1}]}],
	"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}},
	             {"pbrMetallicRoughness":{"baseColorTexture":{"index":0,"extensions":{"KHR_texture_transform":{"scale":[2,2]}}}}}],
	"textures":[{"source":0}],"images":[{"bufferView":2,"mimeType":"image/png"}],
	"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5126,"count":3,"type":"VEC2"}],
	"bufferViews":[{"buffer":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":24},{"buffer":0,"byteOffset":60,"byteLength":%d}],"buffers":[{"byteLength":%d}]}`
	img := pad4(quadrants(t))
	bin := append(append(f32s(0, 0, 0, 1, 0, 0, 0, 1, 0), f32s(0, 0, 1, 0, 0, 1)...), img...)
	m := mustParse(t, glb(fmt.Sprintf(js, len(img), len(bin)), bin))
	a, b := firstMesh(m.Root.Children()[0]).Geometry(), firstMesh(m.Root.Children()[1]).Geometry()
	if a == b {
		t.Fatal("the same vertices with two different texture transforms must be two geometries")
	}
	// flat normals: 8 floats per vertex, u at 6; vertex 1 is (1,0), scaled to u = 2 only in the second geometry
	if a.Vertices()[8+6] != 1 || b.Vertices()[8+6] != 2 {
		t.Errorf("vertex 1 u = %v and %v, want 1 and 2", a.Vertices()[8+6], b.Vertices()[8+6])
	}
}

func TestFactorsAreConvertedFromLinearToSRGB(t *testing.T) {
	mats := `"materials":[{"pbrMetallicRoughness":{"baseColorFactor":[0.214041,0.214041,0.214041,1]}}],`
	mat := firstMesh(&mustParse(t, triangleGLB("", `,"material":0`, mats)).Root.Node).Material().(*g3d.StandardMaterial)
	if c := mat.Color(); !near(c.R, 0.5) || !near(c.G, 0.5) || c.A != 1 {
		t.Errorf("a linear 0.214 is sRGB 0.5: color = %v", c)
	}
	white := firstMesh(&mustParse(t, triangleGLB("", "", "")).Root.Node).Material().(*g3d.StandardMaterial)
	if white.Color() != g3d.White {
		t.Errorf("the default white stays white: %v", white.Color())
	}
}
