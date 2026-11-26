package gltf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gogpu/g3d"
)

// Model is a loaded glTF asset.
type Model struct {
	// Root holds the nodes of the asset's default scene (the "scene" property, or the first scene).
	// Add it to a g3d scene with scene.Add(&model.Root.Node).
	Root *g3d.Group
	// Scenes holds every scene of the asset, in order. Root is one of them.
	Scenes []*g3d.Group
	// Bounds is the box around all geometry of the default scene in world space, in meters. It is
	// the zero box when the scene has no geometry. Use it to place a camera.
	Bounds g3d.AABB
	// Warnings lists what the asset uses that the loader skipped (textures, vertex colors, ...),
	// each reason once.
	Warnings []string
}

// Option configures Load and Parse.
type Option func(*loader)

// WithBaseDir sets the folder that relative uris of a parsed asset are read from. Load sets it to
// the folder of the file. Without it, Parse refuses assets that point to external files.
func WithBaseDir(dir string) Option { return func(l *loader) { l.baseDir = dir } }

// Load reads a .glb or .gltf file. Buffers the asset keeps in other files are read from the same
// folder, and never from outside it.
func Load(path string, opts ...Option) (*Model, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the caller chooses which file to load
	if err != nil {
		return nil, err
	}
	return Parse(data, append([]Option{WithBaseDir(filepath.Dir(path))}, opts...)...)
}

// Parse reads a glTF asset from memory, either a GLB (recognized by its magic bytes) or glTF JSON.
func Parse(data []byte, opts ...Option) (*Model, error) {
	l := &loader{geoms: map[geomKey]*g3d.BufferGeometry{}, mats: map[int]g3d.Material{}, seen: map[string]bool{}}
	for _, o := range opts {
		o(l)
	}
	jsonData := data
	if len(data) >= 4 && string(data[:4]) == "glTF" {
		j, bin, err := splitGLB(data)
		if err != nil {
			return nil, err
		}
		jsonData, l.bin = j, bin
	}
	l.doc = &document{}
	dec := json.NewDecoder(bytes.NewReader(jsonData))
	if err := dec.Decode(l.doc); err != nil {
		return nil, fmt.Errorf("gltf: the JSON cannot be read: %w", err)
	}
	if err := l.check(); err != nil {
		return nil, err
	}
	return l.build()
}

// loader holds the state of one Parse.
type loader struct {
	doc      *document
	bin      []byte
	baseDir  string
	buffers  [][]byte
	warnings []string
	seen     map[string]bool
	geoms    map[geomKey]*g3d.BufferGeometry
	mats     map[int]g3d.Material
	bounds   bbox
}

// warnf records a reason once.
func (l *loader) warnf(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	if !l.seen[s] {
		l.seen[s] = true
		l.warnings = append(l.warnings, s)
	}
}

// supportedExtensions are the extensions this loader understands, and what it does with them.
var supportedExtensions = map[string]bool{
	"KHR_materials_unlit":             true, // an unlit g3d.BasicMaterial
	"KHR_materials_emissive_strength": true, // multiplies the emissive factor
	"KHR_mesh_quantization":           true, // integer attributes, handled by the accessor reader
	"KHR_texture_transform":           true, // textures are not used yet, so there is nothing to transform
}

// check validates the asset version and the required extensions.
func (l *loader) check() error {
	v := l.doc.Asset.Version
	if v == "" {
		return errors.New("gltf: the asset has no asset.version")
	}
	if major, _, ok := strings.Cut(v, "."); !ok || major != "2" {
		return fmt.Errorf("gltf: asset version %q is not supported (only 2.x)", v)
	}
	if mv := l.doc.Asset.MinVersion; mv != "" {
		major, _, _ := strings.Cut(mv, ".")
		if n, err := strconv.Atoi(major); err != nil || n > 2 {
			return fmt.Errorf("gltf: the asset needs glTF %s or newer, this loader reads 2.x", mv)
		}
	}
	var missing []string
	for _, e := range l.doc.ExtensionsRequired {
		if !supportedExtensions[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("gltf: the asset requires extensions this loader does not support: %s", strings.Join(missing, ", "))
	}
	return nil
}
