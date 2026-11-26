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
// materials, the base color texture (PNG and JPEG images from a buffer view, a data URI or a file, with the
// wrapping and filtering of its sampler, any set of texture coordinates, and KHR_texture_transform), and the
// KHR_materials_unlit, KHR_materials_emissive_strength and KHR_mesh_quantization extensions.
//
// glTF colors are linear; g3d colors are sRGB. The loader converts the factors, and the base color texture is
// an sRGB texture, so with Renderer.SetColorManagement(g3d.ColorManagementSRGB) the result is what glTF specifies.
//
// What it does not do yet, and says so in Model.Warnings instead of failing: the other textures (metallic-roughness,
// normal, occlusion, emissive), vertex colors,
// tangents, points and lines, cameras, lights, skins, morph targets and animations. The emissive factor
// is set on the material, but g3d's standard shader does not draw emission yet. An asset that
// requires an extension the loader does not know (Draco, meshopt, ...) is refused with an error that
// names it.
//
// glTF is right-handed with +Y up and meters as the unit, like g3d, so positions are used as they are.
package gltf
