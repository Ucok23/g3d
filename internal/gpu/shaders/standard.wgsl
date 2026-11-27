// standard.wgsl — Blinn-Phong lit shader for StandardMaterial.
//
// Phase 1: Blinn-Phong approximation (ambient + diffuse + specular).
// Phase 2 will upgrade to Cook-Torrance BRDF.
// Used by StandardMaterial (ShaderID = "standard").
//
// Bind groups:
//   Group 0, Binding 0: FrameUniforms  (per-frame, set once)
//   Group 1, Binding 0: ObjectUniforms (per-object)
//   Group 1, Binding 1: MaterialUniforms (per-material, 32 bytes)
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
    view_projection: mat4x4<f32>,       //  64 bytes (offset   0)
    camera_position: vec4<f32>,         //  16 bytes (offset  64) — vec4, NOT vec3!
    lights: array<LightData, 4>,        // 128 bytes (offset  80) — 4 x 32
    light_count: u32,                   //   4 bytes (offset 208)
    flags: u32,                         //   4 bytes (offset 212) — FLAG_MANAGED | FLAG_ENCODE_OUTPUT | FLAG_TONEMAP_*
    exposure: f32,                      //   4 bytes (offset 216) — tone-mapping exposure multiplier
    _pad2: u32,                         //   4 bytes (offset 220)
    light_view_projection: mat4x4<f32>, //  64 bytes (offset 224) — directional shadow matrix
    shadow_params: vec4<f32>,           //  16 bytes (offset 288) — x=depthBias, y=normalBias, z=texelSize, w=lightIdx+1
    shadow_soft: vec4<f32>,             //  16 bytes (offset 304) — x=mapWorldWidth, y=depthRangeWorld, z=softness tan(angular radius), w=mapWorldHeight
};                                      // Total: 320 bytes, 16-byte aligned

struct ObjectUniforms {
    model: mat4x4<f32>,              // 64 bytes (offset  0)
    normal_model: mat4x4<f32>,       // 64 bytes (offset 64)
};                                   // Total: 128 bytes

struct MaterialUniforms {
    color: vec4<f32>,                // 16 bytes (offset  0) — RGBA
    metalness: f32,                  //  4 bytes (offset 16)
    roughness: f32,                  //  4 bytes (offset 20)
    alpha_cutoff: f32,               //  4 bytes (offset 24)
    double_sided: f32,               //  4 bytes (offset 28) — 1.0 when the material is double-sided, else 0.0
    emissive: vec4<f32>,             // 16 bytes (offset 32) — RGB emissive + occlusion_strength (w)
    extra: vec4<f32>,                // 16 bytes (offset 48) — x=normal_scale
};                                   // Total: 64 bytes, 16-byte aligned

// --- Bind groups ---

@group(0) @binding(0) var<uniform> frame: FrameUniforms;
@group(0) @binding(1) var shadow_map: texture_depth_2d;
@group(0) @binding(2) var shadow_sampler: sampler_comparison;
@group(1) @binding(0) var<uniform> object: ObjectUniforms;
@group(1) @binding(1) var<uniform> material: MaterialUniforms;
@group(1) @binding(2) var color_map: texture_2d<f32>;
@group(1) @binding(3) var color_sampler: sampler;
@group(1) @binding(4) var mr_map: texture_2d<f32>;
@group(1) @binding(5) var mr_sampler: sampler;
@group(1) @binding(6) var occlusion_map: texture_2d<f32>;
@group(1) @binding(7) var occlusion_sampler: sampler;
@group(1) @binding(8) var emissive_map: texture_2d<f32>;
@group(1) @binding(9) var emissive_sampler: sampler;
@group(1) @binding(10) var normal_map: texture_2d<f32>;
@group(1) @binding(11) var normal_sampler: sampler;

// --- Vertex I/O ---

struct VertexInput {
    @location(0) position: vec3<f32>,  // 12 bytes (offset  0)
    @location(1) normal: vec3<f32>,    // 12 bytes (offset 12)
    @location(2) uv: vec2<f32>,        //  8 bytes (offset 24)
};                                     // Stride: 32 bytes

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) world_position: vec3<f32>,
    @location(1) world_normal: vec3<f32>,
    @location(2) uv: vec2<f32>,
};

// --- Vertex shader ---

@vertex
fn vs_main(in: VertexInput) -> VertexOutput {
    var out: VertexOutput;

    let world_pos = object.model * vec4<f32>(in.position, 1.0);
    out.clip_position = frame.view_projection * world_pos;
    out.world_position = world_pos.xyz;

    // Normal matrix = transpose(inverse(model)).
    // Passed as mat4x4 to avoid mat3x3 16-byte stride issues.
    // Only the upper-left 3x3 matters; w component discarded.
    let world_normal = (object.normal_model * vec4<f32>(in.normal, 0.0)).xyz;
    out.world_normal = normalize(world_normal);

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
const FLAG_TONEMAP_REINHARD: u32 = 4u;
const FLAG_TONEMAP_ACES: u32 = 8u;

fn srgb_to_linear(c: vec3<f32>) -> vec3<f32> {
    let v = max(c, vec3<f32>(0.0));
    return select(pow((v + vec3<f32>(0.055)) / 1.055, vec3<f32>(2.4)), v / 12.92, v <= vec3<f32>(0.04045));
}

fn linear_to_srgb(c: vec3<f32>) -> vec3<f32> {
    let v = max(c, vec3<f32>(0.0));
    return select(1.055 * pow(v, vec3<f32>(1.0 / 2.4)) - vec3<f32>(0.055), v * 12.92, v <= vec3<f32>(0.0031308));
}

fn tonemap_reinhard(c: vec3<f32>) -> vec3<f32> {
    let v = max(c, vec3<f32>(0.0));
    return v / (vec3<f32>(1.0) + v);
}

// Narkowicz 2015 ACES filmic approximation.
fn tonemap_aces(c: vec3<f32>) -> vec3<f32> {
    let x = max(c, vec3<f32>(0.0));
    let a = 2.51;
    let b = 0.03;
    let d = 2.43;
    let e = 0.59;
    let f = 0.14;
    return clamp((x * (a * x + b)) / (x * (d * x + e) + f), vec3<f32>(0.0), vec3<f32>(1.0));
}


// Perturb surface normal N using a tangent-space normal map (OpenGL/glTF convention:
// +R right (+u), +G up (-v), +B out (+N)) derived from screen-space derivatives of world
// position and UV. Works without vertex TANGENT attributes on both GPU and software backends.
fn perturb_normal(N: vec3<f32>, world_pos: vec3<f32>, uv: vec2<f32>, map_n: vec3<f32>, scale: f32) -> vec3<f32> {
    let tx = (map_n.x * 2.0 - 1.0) * scale;
    let ty = (map_n.y * 2.0 - 1.0) * scale;
    let tz = map_n.z * 2.0 - 1.0;
    // Fast-path for default flat normal (128,128,255) or zero scale.
    if (tx * tx + ty * ty) < 1e-4 {
        return N;
    }
    let dp1 = dpdx(world_pos);
    let dp2 = dpdy(world_pos);
    let duv1 = dpdx(uv);
    let duv2 = dpdy(uv);

    let det = duv1.x * duv2.y - duv1.y * duv2.x;
    if abs(det) < 1e-12 {
        return N;
    }
    let inv_det = 1.0 / det;
    // T points along +u, B_v points along +v; glTF/OpenGL +G points along -v (up).
    let T_raw = (dp1 * duv2.y - dp2 * duv1.y) * inv_det;
    let B_up_raw = -(dp2 * duv1.x - dp1 * duv2.x) * inv_det;

    let T_ortho = T_raw - N * dot(N, T_raw);
    let B_ortho = B_up_raw - N * dot(N, B_up_raw);
    let t_len_sq = dot(T_ortho, T_ortho);
    let b_len_sq = dot(B_ortho, B_ortho);
    if t_len_sq < 1e-12 || b_len_sq < 1e-12 {
        return N;
    }
    let T = T_ortho * inverseSqrt(t_len_sq);
    let B = B_ortho * inverseSqrt(b_len_sq);
    return normalize(T * tx + B * ty + N * tz);
}

// 16-tap Poisson disk (unit radius) used by the soft-shadow blocker search and filter.
const POISSON_COUNT: i32 = 16;
var<private> POISSON: array<vec2<f32>, 16> = array<vec2<f32>, 16>(
    vec2<f32>(-0.94201624, -0.39906216), vec2<f32>(0.94558609, -0.76890725),
    vec2<f32>(-0.09418410, -0.92938870), vec2<f32>(0.34495938, 0.29387760),
    vec2<f32>(-0.91588581, 0.45771432), vec2<f32>(-0.81544232, -0.87912464),
    vec2<f32>(-0.38277543, 0.27676845), vec2<f32>(0.97484398, 0.75648379),
    vec2<f32>(0.44323325, -0.97511554), vec2<f32>(0.53742981, -0.47373420),
    vec2<f32>(-0.26496911, -0.41893023), vec2<f32>(0.79197514, 0.19090188),
    vec2<f32>(-0.24188840, 0.99706507), vec2<f32>(-0.81409955, 0.91437590),
    vec2<f32>(0.19984126, 0.78641367), vec2<f32>(0.14383161, -0.14100790)
);

// Interleaved gradient noise in [0,1): decorrelates the kernel rotation between neighbouring pixels.
fn interleaved_gradient_noise(p: vec2<f32>) -> f32 {
    return fract(52.9829189 * fract(dot(p, vec2<f32>(0.06711056, 0.00583715))));
}

// Directional shadow visibility (1.0 = lit, 0.0 = fully occluded).
// shadow_soft.z == 0: fixed 3x3 PCF. Otherwise contact-hardening PCSS: a blocker search finds the
// mean occluder depth, the penumbra width follows from the receiver-occluder distance, and a
// rotated Poisson PCF of that radius filters the map.
fn sample_shadow(world_pos: vec3<f32>, world_normal: vec3<f32>, light_dir: vec3<f32>, frag_xy: vec2<f32>) -> f32 {
    if frame.shadow_params.w < 0.5 {
        return 1.0;
    }
    let ndotl = max(dot(world_normal, light_dir), 0.0);
    let normal_offset = frame.shadow_params.y * (1.0 - 0.5 * ndotl);
    let pos_biased = world_pos + world_normal * normal_offset;
    let ls = frame.light_view_projection * vec4<f32>(pos_biased, 1.0);
    if abs(ls.w) < 1e-6 {
        return 1.0;
    }
    let proj = ls.xyz / ls.w;
    let uv = vec2<f32>(proj.x * 0.5 + 0.5, 0.5 - proj.y * 0.5);
    if uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || proj.z < 0.0 || proj.z > 1.0 {
        return 1.0;
    }
    let depth = proj.z - frame.shadow_params.x;
    let texel = frame.shadow_params.z;
    let softness = frame.shadow_soft.z;

    if softness <= 0.0 {
        var sum = 0.0;
        for (var y = -1; y <= 1; y = y + 1) {
            for (var x = -1; x <= 1; x = x + 1) {
                let offset = vec2<f32>(f32(x) * texel, f32(y) * texel);
                sum = sum + textureSampleCompareLevel(shadow_map, shadow_sampler, uv + offset, depth);
            }
        }
        return sum / 9.0;
    }

    let world_w = max(frame.shadow_soft.x, 1e-3);
    let world_h = max(frame.shadow_soft.w, 1e-3);
    let range = max(frame.shadow_soft.y, 1e-3);
    let dims = vec2<f32>(textureDimensions(shadow_map));
    // World meters -> shadow-map uv, per axis (the fitted projection need not be square).
    let m2uv = vec2<f32>(1.0 / world_w, 1.0 / world_h);
    let max_texels = 28.0;

    // Per-pixel kernel rotation; with SSAA the noise averages out to a smooth penumbra.
    let angle = 6.2831853 * interleaved_gradient_noise(frag_xy);
    let rot = vec2<f32>(cos(angle), sin(angle));

    // Blocker search over the largest penumbra an occluder up to half the depth range away can cast.
    let search_world = clamp(softness * range * 0.5, 2.0 * texel * world_w, max_texels * texel * world_w);
    var blocker_sum = 0.0;
    var blocker_count = 0.0;
    for (var i = 0; i < POISSON_COUNT; i = i + 1) {
        let p = POISSON[i];
        let r = vec2<f32>(p.x * rot.x - p.y * rot.y, p.x * rot.y + p.y * rot.x);
        let suv = uv + r * search_world * m2uv;
        let tc = vec2<i32>(clamp(suv, vec2<f32>(0.0), vec2<f32>(0.99999)) * dims);
        let d = textureLoad(shadow_map, tc, 0);
        if d < depth {
            blocker_sum = blocker_sum + d;
            blocker_count = blocker_count + 1.0;
        }
    }
    if blocker_count < 0.5 {
        return 1.0;
    }
    let avg_blocker = blocker_sum / blocker_count;
    // Light-space depth is linear (orthographic), so the gap in meters is the depth delta times range.
    let gap_world = max((depth - avg_blocker) * range, 0.0);
    let filter_world = clamp(gap_world * softness, 1.0 * texel * world_w, max_texels * texel * world_w);

    // Slope-scaled bias: the receiver plane drops away across the kernel on surfaces tilted from the light.
    let tan_theta = min(sqrt(max(1.0 - ndotl * ndotl, 0.0)) / max(ndotl, 0.15), 4.0);
    let kernel_bias = filter_world * tan_theta / range;
    let cmp_depth = depth - kernel_bias;

    var sum = 0.0;
    for (var i = 0; i < POISSON_COUNT; i = i + 1) {
        let p = POISSON[i];
        let r = vec2<f32>(p.x * rot.x - p.y * rot.y, p.x * rot.y + p.y * rot.x);
        let suv = uv + r * filter_world * m2uv;
        sum = sum + textureSampleCompareLevel(shadow_map, shadow_sampler, suv, cmp_depth);
    }
    return sum / f32(POISSON_COUNT);
}

// Light type constants (must match Go LightKind values).
const LIGHT_AMBIENT: u32 = 0u;
const LIGHT_DIRECTIONAL: u32 = 1u;

@fragment
fn fs_main(in: VertexOutput) -> @location(0) vec4<f32> {
    // The color map multiplies the base color and the alpha. A material without a map is bound a
    // 1x1 white texture. It is sampled first, in uniform control flow, before any discard.
    let texel = textureSample(color_map, color_sampler, in.uv);
    var tint = material.color.rgb;
    if (frame.flags & FLAG_MANAGED) != 0u {
        tint = srgb_to_linear(tint);
    }
    let base_color = tint * texel.rgb;
    let alpha = material.color.a * texel.a;

    // Alpha test (AlphaModeMask): discard fragments below cutoff.
    if alpha < material.alpha_cutoff {
        discard;
    }

    // Re-normalize after interpolation.
    var N = normalize(in.world_normal);
    // View direction: camera to fragment.
    let V = normalize(frame.camera_position.xyz - in.world_position);
    // The back face of a double-sided material is lit with the normal turned toward the viewer, so it is
    // shaded like a front face seen from the other side (glTF 2.0, "Double Sided"). This does not use the
    // front_facing builtin, which wgpu's software backend does not provide. A single-sided material
    // culls its back faces, so it is unchanged.
    if material.double_sided > 0.5 && dot(N, V) < 0.0 {
        N = -N;
    }
    let norm_sample = textureSample(normal_map, normal_sampler, in.uv).rgb;
    N = perturb_normal(N, in.world_position, in.uv, norm_sample, material.extra.x);

    let mr_sample = textureSample(mr_map, mr_sampler, in.uv);
    let occ_sample = textureSample(occlusion_map, occlusion_sampler, in.uv);
    let em_sample = textureSample(emissive_map, emissive_sampler, in.uv);

    let roughness = clamp(material.roughness * mr_sample.g, 0.0, 1.0);
    let metalness = clamp(material.metalness * mr_sample.b, 0.0, 1.0);
    let ao = 1.0 + material.emissive.w * (occ_sample.r - 1.0);

    var result = vec3<f32>(0.0, 0.0, 0.0);

    // Shininess: high when smooth (roughness=0), low when rough (roughness=1).
    let shininess = mix(256.0, 2.0, roughness);
    // Specular intensity & tint: dielectrics (metalness=0) keep earlier Blinn-Phong
    // behavior; metals attenuate diffuse and tint specular with base_color.
    let diffuse_albedo = base_color * (1.0 - metalness);
    let spec_tint = mix(vec3<f32>(1.0), base_color, metalness);
    let specular_strength = (1.0 - roughness) * mix(0.5, 1.0, metalness);

    let count = min(frame.light_count, 4u);
    for (var i = 0u; i < count; i = i + 1u) {
        let light = frame.lights[i];
        let light_color = light.color * light.intensity;

        if light.light_type == LIGHT_AMBIENT {
            // Ambient: uniform illumination modulated by ambient occlusion.
            result = result + base_color * light_color * ao;
        } else if light.light_type == LIGHT_DIRECTIONAL {
            // Blinn-Phong directional light with metallic-roughness modulation.
            let L = normalize(-light.direction);
            let H = normalize(L + V);

            let NdotL = max(dot(N, L), 0.0);
            let NdotH = max(dot(N, H), 0.0);

            let diffuse = diffuse_albedo * NdotL;
            let specular = spec_tint * pow(NdotH, shininess) * specular_strength;

            var vis = 1.0;
            if frame.shadow_params.w > 0.5 && (i + 1u) == u32(frame.shadow_params.w) {
                vis = sample_shadow(in.world_position, N, L, in.clip_position.xy);
            }
            result = result + (diffuse + specular) * light_color * vis;
        }
    }

    var emissive = material.emissive.rgb;
    if (frame.flags & FLAG_MANAGED) != 0u {
        emissive = srgb_to_linear(emissive);
    }
    result = result + emissive * em_sample.rgb;

    if (frame.flags & (FLAG_TONEMAP_REINHARD | FLAG_TONEMAP_ACES)) != 0u {
        let exp_mul = select(1.0, frame.exposure, frame.exposure > 0.0);
        result = result * exp_mul;
        if (frame.flags & FLAG_TONEMAP_ACES) != 0u {
            result = tonemap_aces(result);
        } else {
            result = tonemap_reinhard(result);
        }
    }

    if (frame.flags & FLAG_ENCODE_OUTPUT) != 0u {
        result = linear_to_srgb(result);
    }
    return vec4<f32>(result, alpha);
}
