#include "ggml.h"
#include "ggml-alloc.h"
#include "ggml-backend.h"
#include "ggml-backend-impl.h"

#include <cmath>
#include <cstdio>

// GPU acceptance fixture, not an image-build test. Inspect the optimizer's
// allocation dependencies for a terminal MoE group and every partial norm
// suffix. No weights or inference are needed. Compile against the matching
// builder because the backend interface is private and has no stable ABI.
struct dependencies {
    ggml_tensor * final;
    int count = 0;
    int bad = 0;
};

int main() {
    std::setvbuf(stdout, nullptr, _IONBF, 0);
    ggml_backend_load_all();
    auto * backend = ggml_backend_init_by_name("ROCm0", nullptr);
    if (!backend || !backend->iface.graph_optimize) return 2;
    int failures = 0;
    for (int suffix = 0; suffix <= 5; ++suffix) {
        for (int scaled = 0; scaled <= (suffix == 0 || suffix == 5); ++scaled) {
            auto * ctx = ggml_init({2*1024*1024, nullptr, true});
            if (!ctx) return 3;
            auto * logits = ggml_new_tensor_2d(ctx, GGML_TYPE_F32, 128, 1);
            auto * probs = ggml_soft_max(ctx, logits);
            auto * ids = ggml_argsort_top_k(ctx, probs, 8);
            auto * base = ggml_get_rows(ctx, ggml_reshape_3d(ctx, probs, 1, 128, 1), ids);
            auto * root = base;
            ggml_tensor * shaped = nullptr;
            if (suffix >= 1) root = shaped = ggml_reshape_2d(ctx, root, 8, 1);
            if (suffix >= 2) root = ggml_sum_rows(ctx, root);
            if (suffix >= 3) root = ggml_clamp(ctx, root, 6.103515625e-5f, INFINITY);
            if (suffix >= 4) root = ggml_div(ctx, shaped, root);
            if (suffix >= 5) root = ggml_reshape_3d(ctx, root, 1, 8, 1);
            if (scaled) root = ggml_scale(ctx, root, 2.0f);
            auto * final = suffix == 0 || suffix == 5 ? root : base;

            // Do not mark views as public outputs. That also marks their
            // sources and intentionally prevents the fusion under test.
            auto * graph = ggml_new_graph_custom(ctx, 64, false);
            ggml_build_forward_expand(graph, root);
            auto * buffer = ggml_backend_alloc_ctx_tensors(ctx, backend);
            if (!buffer) return 3;
            dependencies deps{final};
            ggml_backend_graph_optimize_params params{
                [](void * user, ggml_tensor *, ggml_tensor * until) {
                    auto & d = *static_cast<dependencies *>(user);
                    ++d.count;
                    if (until != d.final) ++d.bad;
                }, &deps};
            backend->iface.graph_optimize(backend, graph, &params);
            const bool pass = deps.count > 0 && deps.bad == 0;
            std::printf("suffix=%d scaled=%d nodes=%d deps=%d invalid=%d %s\n",
                suffix, scaled, ggml_graph_n_nodes(graph), deps.count, deps.bad, pass ? "PASS" : "FAIL");
            failures += !pass;
            ggml_backend_buffer_free(buffer);
            ggml_free(ctx);
        }
    }
    ggml_backend_free(backend);
    return failures ? 1 : 0;
}
