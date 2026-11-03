#include "common.h"
#include "llama.h"
#include "llama-context.h"
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <stdexcept>

// Actual-model smoke, never an image-build test. Compile against the exact
// patched source/common ABI; run only under coordinated hardware acceptance.
static void require(bool ok, const char * message) {
    if (!ok) throw std::runtime_error(message);
}
struct observations { int phase = -1; bool seen[2] = {}; bool bad = false; };
static bool observe(ggml_tensor * t, bool ask, void * opaque) {
    auto & out = *static_cast<observations *>(opaque);
    if (!ask || out.phase < 0 || t->op != GGML_OP_FLASH_ATTN_EXT) return false;
    const auto * q = t->src[0], * k = t->src[1], * mask = t->src[3];
    if (!mask || std::strncmp(mask->name, "kq_mask_qsa", 11) != 0) return false;
    const int queries = out.phase == 0 ? 64 : 1;
    const bool valid = q && k && mask && !t->src[5] && !t->src[6] &&
        q->ne[3] == 2 && k->ne[3] == 2 && mask->ne[3] == 2 &&
        mask->ne[0] == k->ne[1] && mask->ne[1] == queries && q->ne[1] == queries &&
        mask->ne[2] == 1 && mask->type == GGML_TYPE_F16 && ggml_is_contiguous(mask) &&
        mask->op == GGML_OP_CONT && mask->src[0] && mask->src[0]->op == GGML_OP_VIEW &&
        mask->src[0]->src[0] && mask->src[0]->src[0]->op == GGML_OP_SET_ROWS;
    out.bad |= !valid;
    if (valid && !out.seen[out.phase]) {
        std::printf("mask phase=%s shape=[%lld,%lld,1,2] shared_rows=0 selected_ids=0 dense_nonfused=1\n",
            out.phase == 0 ? "prefill" : "decode", (long long) mask->ne[0], (long long) mask->ne[1]);
        out.seen[out.phase] = true;
    }
    return false; // metadata only: no tensor read, synchronization, or artificial split
}
static void advance(llama_context * ctx, int position, int count) {
    const int n_vocab = llama_vocab_n_tokens(llama_model_get_vocab(llama_get_model(ctx)));
    require(n_vocab > 1024, "unexpected vocabulary");
    auto batch = llama_batch_init(2*count, 0, 1);
    for (int sequence = 0; sequence < 2; ++sequence) for (int i = 0; i < count; ++i) {
        const int row = batch.n_tokens++;
        batch.token[row] = 100 + sequence*128 + ((position+i)*17)%97;
        batch.pos[row] = position+i;
        batch.n_seq_id[row] = 1;
        batch.seq_id[row][0] = sequence;
        batch.logits[row] = i == count-1;
    }
    const int rc = llama_decode(ctx, batch);
    llama_batch_free(batch);
    require(rc == 0, "balanced two-sequence decode failed");
    for (int sequence = 0; sequence < 2; ++sequence) {
        const float * values = llama_get_logits_ith(ctx, (sequence+1)*count-1);
        require(values != nullptr, "missing last-token logits");
        for (int token = 0; token < n_vocab; ++token)
            require(std::isfinite(values[token]), "non-finite last-token logit");
    }
}
int main(int argc, char ** argv) {
    std::setvbuf(stdout, nullptr, _IONBF, 0);
    try {
        require(argc == 2, "usage: test-qwen4exp-multistream MODEL.gguf");
        common_init();
        common_params params;
        // Programmatic params skip the CLI's CPU-default resolution.
        postprocess_cpu_params(params.cpuparams);
        postprocess_cpu_params(params.cpuparams_batch, &params.cpuparams);
        require(params.cpuparams.n_threads > 0 && params.cpuparams_batch.n_threads > 0,
                "requires resolved CPU thread counts");
        params.model.path = argv[1]; // local verified model; no remote resolution
        // Match the existing managed Flash-Next probe policy before allocation.
        params.no_unified_memory = true;
        common_set_env("GGML_CUDA_ENABLE_UNIFIED_MEMORY", "");
        require(std::getenv("GGML_CUDA_ENABLE_UNIFIED_MEMORY") == nullptr, "unified memory still enabled");
        ggml_backend_load_all();
        auto * device = ggml_backend_dev_by_name("ROCm0");
        auto * cpu = ggml_backend_dev_by_name("CPU");
        require(device && cpu, "requires explicit ROCm0 and CPU buffer override");
        auto * cpu_buffer = ggml_backend_dev_buffer_type(cpu);
        require(cpu_buffer && std::strcmp(ggml_backend_buft_name(cpu_buffer), "CPU") == 0, "requires CPU buffer");
        params.devices = { device, nullptr };
        params.load_mode = LLAMA_LOAD_MODE_NONE;
        params.lazy_mode = LLAMA_LAZY_MODE_DIRECT;
        params.no_host = true;
        params.tensor_buft_overrides = {
            { "per_layer_token_embd.weight", cpu_buffer }, { nullptr, nullptr }
        };
        params.n_gpu_layers = 999;
        params.n_parallel = params.n_sequences = 2;
        params.kv_unified = false;
        params.n_ctx = 4096;
        params.n_batch = params.n_ubatch = 256;
        params.cache_type_k = params.cache_type_v = GGML_TYPE_F16;
        params.flash_attn_type = LLAMA_FLASH_ATTN_TYPE_ENABLED;
        params.fit_params = params.warmup = false;
        observations out;
        params.cb_eval = observe;
        params.cb_eval_user_data = &out;
        llama_backend_init();
        auto instance = common_init_from_params(params);
        require(instance && instance->context(), "model/context initialization failed");
        auto * ctx = instance->context();
        char arch[64] = {};
        llama_model_meta_val_str(instance->model(), "general.architecture", arch, sizeof(arch));
        require(std::strcmp(arch, "qwen4exp") == 0, "requires Flash-Next qwen4exp");
        require(llama_n_seq_max(ctx) == 2 && llama_n_ctx(ctx) == 4096 &&
            llama_n_batch(ctx) == 256 && llama_n_ubatch(ctx) == 256, "effective bounds differ");
        // Test-only access on a non-const context: force the exact fallback,
        // not the 2-D fused TOP_K_MASK path that already survives the old bug.
        auto & test_policy = const_cast<llama_cparams &>(ctx->get_cparams());
        test_policy.auto_ftkm = test_policy.fused_top_k_mask = false;
        require(ctx->get_causal_attn() && !test_policy.embeddings, "requires causal logits context");
        // Unlike the causal flag, an embedding toggle marks the scheduler's
        // initial reservation stale. Restore logits mode before reserving the
        // forced fallback graph; no decode runs under the temporary setting.
        ctx->set_embeddings(true);
        ctx->set_embeddings(false);
        ctx->sched_reserve();
        require(ctx->get_causal_attn() && !test_policy.embeddings, "context mode changed");
        require(!test_policy.auto_ftkm && !test_policy.fused_top_k_mask, "fallback policy changed");
        out.phase = 0;
        advance(ctx, 0, 64);
        require(out.seen[0] && !out.bad, "prefill did not prove a two-stream full-cache mask");
        out.phase = 1;
        advance(ctx, 64, 1);
        require(out.seen[1] && !out.bad, "decode did not prove a two-stream full-cache mask");
        require(!test_policy.auto_ftkm && !test_policy.fused_top_k_mask, "fallback policy changed");
        instance.reset();
        llama_backend_free();
        std::puts("PASS non-unified two-stream F16: 64+1 tokens/sequence; four finite logit vectors");
        return 0;
    } catch (const std::exception & e) {
        std::fprintf(stderr, "FAIL %s\n", e.what());
        return 1;
    }
}
