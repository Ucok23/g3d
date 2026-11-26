// Package gltf loads glTF 2.0 assets (.gltf and .glb) into a g3d scene graph.
//
// It reads the JSON and the binary data with encoding/json and encoding/binary only (no
// dependencies), follows the Khronos glTF 2.0 specification, and builds g3d nodes, meshes
// and materials from it:
//
//	model, err := gltf.Load("house.glb")
//	if err != nil { ... }
//	scene.Add(&model.Root.Node)
//
// What it supports: .glb and .gltf, buffers from the GLB BIN chunk, data URIs and files next to
// the asset, accessors with byte strides, normalized integers and sparse data, triangle lists, strips
// and fans, flat normals when the asset has none, the node hierarchy (matrix or translation, rotation
// and scale), the metallic-roughness factors, emissive factor, alpha mode, double-sided
// materials, and the KHR_materials_unlit, KHR_materials_emissive_strength and KHR_mesh_quantization
// extensions.
//
// What it does not do yet, and says so in Model.Warnings instead of failing: textures, vertex colors,
// tangents, points and lines, cameras, lights, skins, morph targets and animations. The emissive factor
// is set on the material, but g3d's standard shader does not draw emission yet. An asset that
// requires an extension the loader does not know (Draco, meshopt, ...) is refused with an error that
// names it.
//
// glTF is right-handed with +Y up and meters as the unit, like g3d, so positions are used as they are.
package gltf
