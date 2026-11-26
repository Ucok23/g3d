// basic.wgsl — Unlit shader for BasicMaterial.
//
// Renders geometry with a solid color, no lighting calculations.
// Used by BasicMaterial (ShaderID = "basic").
//
// Bind groups:
//   Group 0, Binding 0: FrameUniforms  (per-frame, set once)
//   Group 1, Binding 0: ObjectUniforms (per-object)
//   Group 1, Binding 1: MaterialUniforms (per-material, 16 bytes)
//   Group 1, Binding 2: color map (texture_2d, a 1x1 white texture when the material has none)
//   Group 1, Binding 3: its sampler

// --- Uniform structs (must match Go layout byte-for-byte) ---

struct LightData {
    direction: vec3<f32>,    // 12 bytes (offset 0)
    light_type: u32,         //  4 bytes (offset 12) — row total: 16
    color: vec3<f32>,        // 12 bytes (offset 16)
    intensity: f32,          //  4 bytes (offset 28) — row total: 16
};                           // Total: 32 bytes, 16-byte aligned

struct FrameUniforms {
    view_projection: mat4x4<f32>,    //  64 bytes (offset   0)
    camera_position: vec4<f32>,      //  16 bytes (offset  64) — vec4, NOT vec3!
    lights: array<LightData, 4>,     // 128 bytes (offset  80) — 4 x 32
    light_count: u32,                //   4 bytes (offset 208)
    flags: u32,                      //   4 bytes (offset 212) — FLAG_MANAGED | FLAG_ENCODE_OUTPUT
    _pad1: u32,                      //   4 bytes (offset 216)
    _pad2: u32,                      //   4 bytes (offset 220)
};                                   // Total: 224 bytes, 16-byte aligned

struct ObjectUniforms {
    model: mat4x4<f32>,              // 64 bytes (offset  0)
    normal_model: mat4x4<f32>,       // 64 bytes (offset 64)
};                                   // Total: 128 bytes

struct MaterialUniforms {
    color: vec4<f32>,                // 16 bytes (offset 0) — RGBA
};                                   // Total: 16 bytes

// --- Bind groups ---

@group(0) @binding(0) var<uniform> frame: FrameUniforms;
@group(1) @binding(0) var<uniform> object: ObjectUniforms;
@group(1) @binding(1) var<uniform> material: MaterialUniforms;
@group(1) @binding(2) var color_map: texture_2d<f32>;
@group(1) @binding(3) var color_sampler: sampler;

// --- Vertex I/O ---

struct VertexInput {
    @location(0) position: vec3<f32>,  // 12 bytes (offset  0)
    @location(1) normal: vec3<f32>,    // 12 bytes (offset 12)
    @location(2) uv: vec2<f32>,        //  8 bytes (offset 24)
};                                     // Stride: 32 bytes

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) uv: vec2<f32>,
};

// --- Vertex shader ---

@vertex
fn vs_main(in: VertexInput) -> VertexOutput {
    var out: VertexOutput;

    let world_pos = object.model * vec4<f32>(in.position, 1.0);
    out.clip_position = frame.view_projection * world_pos;
    out.uv = in.uv;

    return out;
}

// --- Fragment shader ---

// --- Color management ---
//
// With frame.flags & FLAG_MANAGED the colors given by the application (material color, texels of an
// sRGB texture are decoded by the GPU) are sRGB inputs: they are converted to linear light for the lighting
// and, with FLAG_ENCODE_OUTPUT (the target is not an sRGB format), encoded back to sRGB at the end.
// Light colors are converted on the CPU.

const FLAG_MANAGED: u32 = 1u;
const FLAG_ENCODE_OUTPUT: u32 = 2u;

fn srgb_to_linear(c: vec3<f32>) -> vec3<f32> {
    let v = max(c, vec3<f32>(0.0));
    return select(pow((v + vec3<f32>(0.055)) / 1.055, vec3<f32>(2.4)), v / 12.92, v <= vec3<f32>(0.04045));
}

fn linear_to_srgb(c: vec3<f32>) -> vec3<f32> {
    let v = max(c, vec3<f32>(0.0));
    return select(1.055 * pow(v, vec3<f32>(1.0 / 2.4)) - vec3<f32>(0.055), v * 12.92, v <= vec3<f32>(0.0031308));
}


@fragment
fn fs_main(in: VertexOutput) -> @location(0) vec4<f32> {
    // The color map multiplies the color. A material without a map is bound a 1x1 white texture.
    let texel = textureSample(color_map, color_sampler, in.uv);
    var color = material.color.rgb;
    if (frame.flags & FLAG_MANAGED) != 0u {
        color = srgb_to_linear(color);
    }
    var rgb = color * texel.rgb;
    if (frame.flags & FLAG_ENCODE_OUTPUT) != 0u {
        rgb = linear_to_srgb(rgb);
    }
    return vec4<f32>(rgb, material.color.a * texel.a);
}
