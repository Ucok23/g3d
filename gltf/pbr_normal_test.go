package gltf

import (
	"testing"

	"github.com/gogpu/g3d"
)

func TestGLTFLoadsAllFivePBRTextureSlotsWithoutWarnings(t *testing.T) {
	a := texAsset{
		material: `{"pbrMetallicRoughness":{"baseColorTexture":{"index":0},"metallicRoughnessTexture":{"index":1},"metallicFactor":0.8,"roughnessFactor":0.6},` +
			`"occlusionTexture":{"index":1,"strength":0.7},"emissiveTexture":{"index":0},"emissiveFactor":[1,1,1],` +
			`"normalTexture":{"index":1,"scale":1.25}}`,
		texture: `{"source":0},{"source":0}`,
	}
	m, err := Parse(a.glb(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}
	mat := firstMesh(&m.Root.Node).Material().(*g3d.StandardMaterial)
	if mat.Map() == nil || mat.MetallicRoughnessMap() == nil || mat.OcclusionMap() == nil || mat.EmissiveMap() == nil || mat.NormalMap() == nil {
		t.Fatalf("expected all 5 PBR texture maps populated: base=%v mr=%v occ=%v em=%v norm=%v",
			mat.Map() != nil, mat.MetallicRoughnessMap() != nil, mat.OcclusionMap() != nil, mat.EmissiveMap() != nil, mat.NormalMap() != nil)
	}
	if mat.Map().ColorSpace() != g3d.ColorSpaceSRGB || mat.EmissiveMap().ColorSpace() != g3d.ColorSpaceSRGB {
		t.Errorf("baseColor/emissive maps must be ColorSpaceSRGB")
	}
	if mat.MetallicRoughnessMap().ColorSpace() != g3d.ColorSpaceLinear ||
		mat.OcclusionMap().ColorSpace() != g3d.ColorSpaceLinear ||
		mat.NormalMap().ColorSpace() != g3d.ColorSpaceLinear {
		t.Errorf("metallicRoughness/occlusion/normal maps must be ColorSpaceLinear")
	}
	if !near(mat.OcclusionStrength(), 0.7) {
		t.Errorf("OcclusionStrength = %v, want 0.7", mat.OcclusionStrength())
	}
	if !near(mat.NormalScale(), 1.25) {
		t.Errorf("NormalScale = %v, want 1.25", mat.NormalScale())
	}
}
