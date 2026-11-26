package gltf_test

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/gogpu/g3d/gltf"
)

// A glTF asset with one triangle, its data in a base64 data URI. Real assets are usually .glb files
// read with gltf.Load.
const triangle = `{
  "asset": {"version": "2.0"},
  "scenes": [{"nodes": [0]}],
  "nodes": [{"name": "triangle", "mesh": 0}],
  "meshes": [{"primitives": [{"attributes": {"POSITION": 0}}]}],
  "materials": [],
  "accessors": [{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"}],
  "bufferViews": [{"buffer": 0, "byteLength": 36}],
  "buffers": [{"byteLength": 36, "uri": "data:application/octet-stream;base64,%s"}]
}`

func ExampleParse() {
	// three float32 positions: (0,0,0) (1,0,0) (0,1,0)
	data := []byte{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0x80, 0x3f, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0x80, 0x3f, 0, 0, 0, 0,
	}
	src := fmt.Sprintf(triangle, base64.StdEncoding.EncodeToString(data))

	model, err := gltf.Parse([]byte(src))
	if err != nil {
		log.Fatal(err)
	}
	// scene.Add(&model.Root.Node) puts it in a g3d scene; Bounds frames a camera.
	fmt.Printf("nodes: %d, bounds: %.0f..%.0f x %.0f..%.0f\n",
		len(model.Root.Children()), model.Bounds.Min.X, model.Bounds.Max.X, model.Bounds.Min.Y, model.Bounds.Max.Y)
	// Output: nodes: 1, bounds: 0..1 x 0..1
}
