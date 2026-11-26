package gltf

import "encoding/json"

// The JSON document of a glTF asset, only the parts the loader reads. Field names and defaults
// follow the glTF 2.0 specification.

type document struct {
	Asset              asset         `json:"asset"`
	ExtensionsUsed     []string      `json:"extensionsUsed"`
	ExtensionsRequired []string      `json:"extensionsRequired"`
	Scene              *int          `json:"scene"`
	Scenes             []scene       `json:"scenes"`
	Nodes              []node        `json:"nodes"`
	Meshes             []mesh        `json:"meshes"`
	Materials          []material    `json:"materials"`
	Accessors          []accessor    `json:"accessors"`
	BufferViews        []bufferView  `json:"bufferViews"`
	Buffers            []buffer      `json:"buffers"`
	Cameras            []interface{} `json:"cameras"`
}

type asset struct {
	Version    string `json:"version"`
	MinVersion string `json:"minVersion"`
}

type scene struct {
	Name  string `json:"name"`
	Nodes []int  `json:"nodes"`
}

type node struct {
	Name        string    `json:"name"`
	Children    []int     `json:"children"`
	Mesh        *int      `json:"mesh"`
	Camera      *int      `json:"camera"`
	Skin        *int      `json:"skin"`
	Matrix      []float64 `json:"matrix"`
	Translation []float64 `json:"translation"`
	Rotation    []float64 `json:"rotation"`
	Scale       []float64 `json:"scale"`
}

type mesh struct {
	Name       string      `json:"name"`
	Primitives []primitive `json:"primitives"`
}

type primitive struct {
	Attributes map[string]int  `json:"attributes"`
	Indices    *int            `json:"indices"`
	Material   *int            `json:"material"`
	Mode       *int            `json:"mode"`
	Targets    json.RawMessage `json:"targets"`
}

type material struct {
	Name             string                     `json:"name"`
	PBR              *pbr                       `json:"pbrMetallicRoughness"`
	NormalTexture    json.RawMessage            `json:"normalTexture"`
	OcclusionTexture json.RawMessage            `json:"occlusionTexture"`
	EmissiveTexture  json.RawMessage            `json:"emissiveTexture"`
	EmissiveFactor   []float64                  `json:"emissiveFactor"`
	AlphaMode        string                     `json:"alphaMode"`
	AlphaCutoff      *float64                   `json:"alphaCutoff"`
	DoubleSided      bool                       `json:"doubleSided"`
	Extensions       map[string]json.RawMessage `json:"extensions"`
}

type pbr struct {
	BaseColorFactor          []float64       `json:"baseColorFactor"`
	BaseColorTexture         json.RawMessage `json:"baseColorTexture"`
	MetallicFactor           *float64        `json:"metallicFactor"`
	RoughnessFactor          *float64        `json:"roughnessFactor"`
	MetallicRoughnessTexture json.RawMessage `json:"metallicRoughnessTexture"`
}

type accessor struct {
	BufferView    *int    `json:"bufferView"`
	ByteOffset    int     `json:"byteOffset"`
	ComponentType int     `json:"componentType"`
	Normalized    bool    `json:"normalized"`
	Count         int     `json:"count"`
	Type          string  `json:"type"`
	Sparse        *sparse `json:"sparse"`
}

type sparse struct {
	Count   int `json:"count"`
	Indices struct {
		BufferView    int `json:"bufferView"`
		ByteOffset    int `json:"byteOffset"`
		ComponentType int `json:"componentType"`
	} `json:"indices"`
	Values struct {
		BufferView int `json:"bufferView"`
		ByteOffset int `json:"byteOffset"`
	} `json:"values"`
}

type bufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride"`
}

type buffer struct {
	URI        *string `json:"uri"`
	ByteLength int     `json:"byteLength"`
}
