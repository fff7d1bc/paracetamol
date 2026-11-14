// CPU-only execution of the mechanically extracted MMQ quantization/sum body.
// Build twice, with and without GGML_USE_HIP. This checks the scoped source
// policy, not GPU code generation, matrix accuracy or inference acceptance.
#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <type_traits>

struct float4 { float x, y, z, w; };
struct char4 { int8_t x, y, z, w; };
enum { MMQ_Q8_1_DS_LAYOUT_D4, MMQ_Q8_1_DS_LAYOUT_DS4, MMQ_Q8_1_DS_LAYOUT_D2S6 };
constexpr int WARP_SIZE = 32;
static std::array<float4, 8> inputs;
static int lane, group_size;
static float scale_inv;

template<class T> static T initial(int i) {
    const auto v = inputs[i];
    if constexpr (std::is_same<T, int>::value) {
        return int(std::round(v.x*scale_inv)) + int(std::round(v.y*scale_inv)) +
               int(std::round(v.z*scale_inv)) + int(std::round(v.w*scale_inv));
    } else {
        return v.x + v.y + v.z + v.w;
    }
}

// Model the value presented by the peer before this shuffle stage. All lanes
// have the same scale; each stage uses the snapshot from the previous stage.
template<class T> static T __shfl_xor_sync(unsigned, T, int offset, int) {
    std::array<T, 8> stage;
    for (int i = 0; i < group_size; ++i) stage[i] = initial<T>(i);
    for (int prior = group_size/2; prior > offset; prior >>= 1) {
        const auto before = stage;
        for (int i = 0; i < group_size; ++i) stage[i] += before[i ^ prior];
    }
    return stage[lane ^ offset];
}

template<int ds_layout> static std::array<float, 6> production(float4 xi, float amax) {
    constexpr int vals_per_sum = ds_layout == MMQ_Q8_1_DS_LAYOUT_D2S6 ? 16 : 32;
#include "mmq-activation-sum.inc"
    return { d, sum, float(q.x), float(q.y), float(q.z), float(q.w) };
}

static void require(bool ok, const char * what) {
    if (!ok) { std::fprintf(stderr, "MMQ activation-sum failure: %s\n", what); std::exit(1); }
}

template<int layout> static void check() {
    group_size = layout == MMQ_Q8_1_DS_LAYOUT_D2S6 ? 4 : 8;
    const float amax = 7.0f;
    scale_inv = 127.0f/amax;
    float input_sum = 0.0f;
    int quantized_sum = 0;
    for (int i = 0; i < group_size; ++i) {
        inputs[i] = { 0.13f*float(i+1), -0.21f*float(i+1), 0.37f, i == 0 ? amax : -0.19f };
        input_sum += initial<float>(i);
        quantized_sum += initial<int>(i);
    }
    const float d = 1.0f/scale_inv;
    require(std::fabs(input_sum - d*float(quantized_sum)) > 0.001f,
            "fixture must distinguish original and corrected sums");
    for (lane = 0; lane < group_size; ++lane) {
        const auto actual = production<layout>(inputs[lane], amax);
        require(actual[0] == d, "scale changed");
        const auto v = inputs[lane];
        const std::array<float, 4> components = {v.x, v.y, v.z, v.w};
        for (int k = 0; k < 4; ++k) {
            require(actual[2+k] == std::round(components[k]*scale_inv), "quantized byte changed");
        }
        if constexpr (layout != MMQ_Q8_1_DS_LAYOUT_D4) {
#if defined(GGML_USE_HIP)
            require(std::fabs(actual[1] - input_sum) < 0.00001f, "HIP must retain floating-input sum");
#else
            require(actual[1] == d*float(quantized_sum), "non-HIP must retain corrected quantized-byte sum");
#endif
        }
    }
}

int main() {
    check<MMQ_Q8_1_DS_LAYOUT_D4>();
    check<MMQ_Q8_1_DS_LAYOUT_DS4>();
    check<MMQ_Q8_1_DS_LAYOUT_D2S6>();
#if defined(GGML_USE_HIP)
    std::puts("MMQ activation-sum HIP compatibility CPU source check PASS (3 layouts)");
#else
    std::puts("MMQ activation-sum non-HIP corrected CPU source check PASS (3 layouts)");
#endif
}
