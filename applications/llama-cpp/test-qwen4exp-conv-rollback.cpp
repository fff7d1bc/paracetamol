#include "ggml.h"
#include "ggml-alloc.h"
#include "ggml-backend.h"
#include "ggml-backend-impl.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <stdexcept>
#include <string>
#include <vector>

// Build: bash build-conv-rollback-test.sh SOURCE LIBRARY_DIR OUTPUT_DIR
// GPU:   OUTPUT_DIR/test-qwen4exp-conv-rollback (coordinated gfx1151 only)
// CPU:   OUTPUT_DIR/test-qwen4exp-conv-rollback --cpu (graph/oracle checks only)
// Current build_conv_state_store uses overlapping 4D windows and SET_ROWS,
// not the old per-slot CONT/CPY graph. Inspect the real backend optimizer after
// allocation, so numerical equality cannot hide a silently rejected matcher.
// This is an eligibility/lifetime contract, NOT observed kernel activation:
// graph-enabled model traces remain a separate hardware acceptance gate.
static void require(bool ok, const char * why) { if (!ok) throw std::runtime_error(why); }
static void same(const std::vector<float> & a, const std::vector<float> & b, const char * why) {
    require(a.size() == b.size(), "size mismatch");
    for (size_t i = 0; i < a.size(); ++i) {
        if (!std::isfinite(a[i]) || !std::isfinite(b[i]) || std::memcmp(&a[i], &b[i], sizeof(float)) != 0) {
            std::fprintf(stderr, "%s index=%zu actual=%.9g expected=%.9g\n", why, i, a[i], b[i]);
            throw std::runtime_error(why);
        }
    }
}
struct spec { int T, K, seq, guard; };
// guard: 0 eligible; 1 whole reader; 2 unknown window reader;
// 3 write gathered history; 4 history aliases slot0; 5 shifted window.
struct fixture {
    ggml_backend_t backend;
    bool ple, scheduled;
    spec s;
    const int C = 256, capacity = 4, head = 1;
    int H;
    ggml_context * params = nullptr, * ctx = nullptr;
    ggml_backend_buffer_t inputs = nullptr, nodes = nullptr;
    ggml_backend_sched_t sched = nullptr;
    ggml_tensor * cache, * x, * w, * ids, * history, * out, * saved, * cc;
    ggml_cgraph * graph;
    int deps = 0, bad_deps = 0;
    std::vector<float> model, activations, weights;
    fixture(ggml_backend_t b, ggml_backend_t cpu_backend, bool p, bool sched_mode, spec sp, bool marked)
        : backend(b), ple(p), scheduled(sched_mode), s(sp), H(p ? 9 : 3) {
        params = ggml_init({2*1024*1024, nullptr, true});
        ctx = ggml_init({8*1024*1024, nullptr, true});
        require(params && ctx, "context allocation");
        cache = ggml_new_tensor_2d(params, GGML_TYPE_F32, H*C, capacity*s.K);
        x = ggml_new_tensor_3d(params, GGML_TYPE_F32, C, s.T, s.seq);
        w = ggml_new_tensor_2d(params, GGML_TYPE_F32, 4, C);
        ids = ggml_new_tensor_1d(params, GGML_TYPE_I32, s.seq);
        for (auto * t : {cache, x, w, ids}) ggml_set_input(t);
        inputs = ggml_backend_alloc_ctx_tensors(params, backend);
        require(inputs, "input allocation");
        graph = ggml_new_graph_custom(ctx, 1024, false);
        if (s.guard == 4) {
            history = ggml_view_3d(ctx, cache, H, C, s.seq, H*sizeof(float), cache->nb[1], head*cache->nb[1]);
        } else {
            auto * gathered = ggml_get_rows(ctx, cache, ids);
            history = ggml_reshape_3d(ctx, gathered, H, C, s.seq);
        }
        cc = ggml_concat(ctx, history, ggml_transpose(ctx, x), 0);
        if (marked) ggml_set_name(cc, ple ? "qwen4exp_ple_history" : "qwen4exp_gdn_history");

        // Exact current upstream ordering, dimensions and destination strides.
        const int Kw = std::min(s.T, s.K);
        auto * rev = ggml_cast(ctx, ggml_scale_bias(ctx, ggml_arange(ctx, 0.0f, float(s.K), 1.0f),
            -1.0f, float(s.K-1)), GGML_TYPE_I32);
        auto * order = ggml_view_1d(ctx, rev, Kw, (s.K-Kw)*sizeof(int32_t));
        auto * windows = ggml_view_4d(ctx, cc, H, Kw, C, s.seq, sizeof(float), cc->nb[1], cc->nb[2],
            (s.T-(Kw-1)-(s.guard == 5))*sizeof(float));
        auto * slots = ggml_view_4d(ctx, cache, H, s.K, C, s.seq,
            capacity*cache->nb[1], H*sizeof(float), cache->nb[1], head*cache->nb[1]);
        auto * store = ggml_set_rows(ctx, slots, windows, order);
        ggml_build_forward_expand(graph, store);
        if (s.guard == 3) {
            auto * changed = ggml_scale(ctx, history, 0.5f);
            ggml_build_forward_expand(graph, ggml_cpy(ctx, changed, history));
        }
        if (!ple) {
            out = ggml_silu(ctx, ggml_ssm_conv(ctx, cc, w));
        } else {
            out = nullptr;
            for (int k = 0; k < 4; ++k) {
                auto * shifted = ggml_cont(ctx, ggml_transpose(ctx, ggml_view_3d(ctx, cc,
                    s.T, C, s.seq, cc->nb[1], cc->nb[2], k*3*sizeof(float))));
                auto * wk = ggml_reshape_1d(ctx, ggml_cont(ctx, ggml_view_2d(ctx, w,
                    1, C, w->nb[1], k*w->nb[0])), C);
                auto * term = ggml_mul(ctx, shifted, wk);
                out = out ? ggml_add(ctx, out, term) : term;
            }
            out = ggml_silu(ctx, out);
        }
        ggml_set_output(out);
        ggml_build_forward_expand(graph, out);
        if (s.guard == 1 || s.guard == 2) {
            auto * extra = ggml_sqr(ctx, s.guard == 1 ? cc : windows);
            ggml_set_output(extra);
            ggml_build_forward_expand(graph, extra);
        }
        // A separate CONT copies the entire backing cache, including unowned
        // cells and unwritten slots. It must run after SET_ROWS and convolution.
        saved = ggml_cont(ctx, cache);
        ggml_set_output(saved);
        ggml_build_forward_expand(graph, saved);

        inspect_matcher();
        if (scheduled) {
            // Scheduler API requires CPU last, including a GPU-only graph.
            // Pin and verify every node so the required API fallback cannot
            // silently turn any numerical/control case into CPU acceptance.
            ggml_backend_t backends[] = {backend, cpu_backend};
            const int n_backends = backend == cpu_backend ? 1 : 2;
            sched = ggml_backend_sched_new(backends, nullptr, n_backends, 2048, false, false);
            require(sched, "scheduler creation");
            for (int i = 0; i < ggml_graph_n_nodes(graph); ++i) {
                auto * node = ggml_graph_node(graph, i);
                require(ggml_backend_supports_op(backend, node), "selected backend does not support fixture node");
                ggml_backend_sched_set_tensor_backend(sched, node, backend);
            }
            require(sched && ggml_backend_sched_alloc_graph(sched, graph), "scheduler allocation");
            for (int i = 0; i < ggml_graph_n_nodes(graph); ++i)
                require(ggml_backend_sched_get_tensor_backend(sched, ggml_graph_node(graph, i)) == backend,
                    "scheduler silently assigned a fixture node to fallback");
        } else {
            nodes = ggml_backend_alloc_ctx_tensors(ctx, backend);
            require(nodes, "persistent graph allocation");
        }
        model.resize(ggml_nelements(cache));
        for (size_t i = 0; i < model.size(); ++i) model[i] = float(int(i%127)-63)/128.0f;
        weights.resize(ggml_nelements(w));
        for (size_t i = 0; i < weights.size(); ++i) weights[i] = float(int(i%13)-6)/32.0f;
        ggml_backend_tensor_set(cache, model.data(), 0, ggml_nbytes(cache));
        ggml_backend_tensor_set(w, weights.data(), 0, ggml_nbytes(w));
    }
    void inspect_matcher() {
        deps = bad_deps = 0;
        ggml_backend_graph_optimize_params opt{
            [](void * u, ggml_tensor * root, ggml_tensor * until) {
                auto & f = *static_cast<fixture *>(u);
                ++f.deps;
                auto * hroot = f.history->view_src ? f.history->view_src : f.history;
                if (until != f.out || (root != f.x && root != hroot && root != f.w)) ++f.bad_deps;
            }, this};
        if (backend->iface.graph_optimize) backend->iface.graph_optimize(backend, graph, &opt);
    }
    ~fixture() {
        if (sched) ggml_backend_sched_free(sched);
        ggml_backend_buffer_free(nodes);
        ggml_backend_buffer_free(inputs);
        if (ctx) ggml_free(ctx);
        if (params) ggml_free(params);
    }
    std::vector<float> step(int round) {
        const int Kw = std::min(s.T, s.K);
        const int replay[] = {0, Kw-1, Kw/2, Kw-1};
        const int rollback = replay[round]; // latest, oldest, middle, repeated oldest
        std::vector<int32_t> index(s.seq);
        for (int q = 0; q < s.seq; ++q) index[q] = head+q+(s.guard == 4 ? 0 : rollback)*capacity;
        activations.resize(ggml_nelements(x));
        for (size_t i = 0; i < activations.size(); ++i) activations[i] = float(int((i+round*17)%113)-56)/128.0f;
        const auto before = model;
        auto value = [&](int q, int c, int col) {
            return col < H ? before[size_t(index[q])*H*C+c*H+col]
                : activations[(size_t(q)*s.T+col-H)*C+c];
        };
        for (int q = 0; q < s.seq; ++q) for (int slot = 0; slot < Kw; ++slot)
            for (int c = 0; c < C; ++c) for (int h = 0; h < H; ++h)
                model[size_t(head+q+slot*capacity)*H*C+c*H+h] = value(q,c,s.T-slot+h-(s.guard == 5));
        ggml_backend_tensor_set(ids, index.data(), 0, ggml_nbytes(ids));
        ggml_backend_tensor_set(x, activations.data(), 0, ggml_nbytes(x));
        const auto status = scheduled ? ggml_backend_sched_graph_compute(sched, graph) : ggml_backend_graph_compute(backend, graph);
        require(status == GGML_STATUS_SUCCESS, "graph compute");
        ggml_backend_synchronize(backend);
        std::vector<float> got(model.size());
        ggml_backend_tensor_get(saved, got.data(), 0, got.size()*sizeof(float));
        same(got, model, "every-slot/unowned-cell host oracle");
        std::vector<float> output(ggml_nelements(out));
        ggml_backend_tensor_get(out, output.data(), 0, output.size()*sizeof(float));
        return output;
    }
};

int main(int argc, char ** argv) {
    std::setvbuf(stdout, nullptr, _IONBF, 0);
    const bool cpu = argc == 2 && std::strcmp(argv[1], "--cpu") == 0;
    if (argc != 1 && !cpu) return 2;
    // Match the managed Qwen graph policy without disabling graph capture or
    // adding a test-only backend proc. Feature opt-outs must fail GPU matcher
    // acceptance, not turn this into an accidental fallback-only test.
    if (setenv("PARACETAMOL_QWEN4EXP_QSA_GRAPH", "1", 1)) return 2;
    ggml_backend_load_all();
    auto * backend = ggml_backend_init_by_name(cpu ? "CPU" : "ROCm0", nullptr);
    if (!backend) return 2;
    auto * cpu_backend = cpu ? backend : ggml_backend_init_by_name("CPU", nullptr);
    if (!cpu_backend) { ggml_backend_free(backend); return 2; }
    if (!cpu && !backend->iface.graph_optimize) {
        std::fprintf(stderr, "Required backend graph optimizer missing\n");
        ggml_backend_free(cpu_backend);
        ggml_backend_free(backend);
        return 2;
    }
    int cases = 0;
    try {
        for (bool ple : {false, true}) for (bool scheduled : {false, true}) {
            for (const spec s : std::vector<spec>{{256,8,1,0},{257,512,1,0},{512,64,1,0},{2048,8,1,0},
                {256,8,1,1},{256,8,1,2},{256,8,1,3},{256,8,1,4},{257,8,1,5},{256,8,2,0}}) {
                fixture fast(backend, cpu_backend, ple, scheduled, s, true), reference(backend, cpu_backend, ple, scheduled, s, false);
                const bool eligible = s.guard == 0 && s.seq == 1;
                for (int round = 0; round < 4; ++round) {
                    // Re-run the actual production matcher with allocated
                    // pointers before every execution. In particular, stores
                    // that alias history must now decline, even if preallocation
                    // inspection could not yet see the physical overlap.
                    fast.inspect_matcher();
                    reference.inspect_matcher();
                    if (!cpu) {
                        require(fast.deps == (eligible ? 3 : 0) && fast.bad_deps == 0,
                            "postallocation matcher/lifetime eligibility mismatch");
                        require(reference.deps == 0 && reference.bad_deps == 0,
                            "unmarked reference matched a private fusion");
                    }
                    const auto a = fast.step(round);
                    const auto b = reference.step(round);
                    same(a,b,"fused/unmarked convolution bitexact");
                }
                std::printf("CONV_ROLLBACK kind=%s allocation=%s T=%d K=%d seq=%d guard=%d rounds=4 all_slots=1 unowned=1 bitexact=1 matcher=%d deps=%d bad_deps=%d PASS\n",
                    ple ? "PLE" : "GDN", scheduled ? "scheduler" : "persistent", s.T,s.K,s.seq,s.guard,
                    cpu ? -1 : int(eligible), fast.deps, fast.bad_deps);
                ++cases;
            }
        }
        require(cases == 40, "case count");
        std::printf("CONV_ROLLBACK_SUMMARY cases=40 rounds=160 mode=%s PASS\n", cpu ? "CPU-policy-only" : "ROCm-matcher-required");
    } catch (const std::exception & e) {
        std::fprintf(stderr, "CONV_ROLLBACK FAIL: %s\n", e.what());
        if (cpu_backend != backend) ggml_backend_free(cpu_backend);
        ggml_backend_free(backend);
        return 1;
    }
    if (cpu_backend != backend) ggml_backend_free(cpu_backend);
    ggml_backend_free(backend);
    return 0;
}
