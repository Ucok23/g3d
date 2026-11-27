// shadow.wgsl — Depth-only vertex shader for directional shadow maps.
//
// Bind groups:
//   Group 0, Binding 0: ShadowFrameUniforms (light_view_projection, 64 bytes)
//   Group 1, Binding 0: ObjectUniforms      (model + normal_model, 128 bytes)

struct ShadowFrameUniforms {
    light_view_projection: mat4x4<f32>,
};

struct ObjectUniforms {
    model: mat4x4<f32>,
    normal_model: mat4x4<f32>,
};

@group(0) @binding(0) var<uniform> shadow_frame: ShadowFrameUniforms;
@group(1) @binding(0) var<uniform> object: ObjectUniforms;

struct VertexInput {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) uv: vec2<f32>,
};

@vertex
fn vs_main(in: VertexInput) -> @builtin(position) vec4<f32> {
    let world_pos = object.model * vec4<f32>(in.position, 1.0);
    return shadow_frame.light_view_projection * world_pos;
}
