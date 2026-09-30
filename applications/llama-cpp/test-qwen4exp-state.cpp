#include "arg.h"
#include "common.h"
#include "llama.h"

#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <stdexcept>
#include <vector>

// Hardware acceptance fixture, not an image-build smoke. Use the managed
// Flash-Next GGUF, F16 KV, direct embedding reader and the normal ROCm policy.
// Causal mode changes alter our compact QSA graph's shapes, unlike upstream's
// ordinary mask-only mode changes. Exercise them after the sparse graph and
// derived key cache are actually populated, then check restored logits.
static void require(bool ok, const char * message) {
    if (!ok) throw std::runtime_error(message);
}

static std::vector<float> decode(llama_context * ctx, llama_pos pos, int count) {
    auto batch = llama_batch_init(count, 0, 1);
    for (int i = 0; i < count; ++i) {
        common_batch_add(batch, 100 + (pos + i) % 97, pos + i, {0}, i == count - 1);
    }
    const int rc = llama_decode(ctx, batch);
    llama_batch_free(batch);
    require(rc == 0, "decode failed");
    const int size = llama_vocab_n_tokens(llama_model_get_vocab(llama_get_model(ctx)));
    const float * logits = llama_get_logits_ith(ctx, -1);
    require(logits != nullptr, "missing logits");
    std::vector<float> result(logits, logits + size);
    for (float value : result) require(std::isfinite(value), "non-finite logits");
    return result;
}

static void compare(const std::vector<float> & want, const std::vector<float> & got) {
    require(want.size() == got.size(), "logit size mismatch");
    double error = 0, magnitude = 0;
    size_t top_want = 0, top_got = 0;
    for (size_t i = 0; i < want.size(); ++i) {
        const double delta = double(want[i]) - got[i];
        error += delta * delta;
        magnitude += double(want[i]) * want[i];
        if (want[i] > want[top_want]) top_want = i;
        if (got[i] > got[top_got]) top_got = i;
    }
    const double nmse = error / (magnitude ? magnitude : 1);
    std::printf("restored logits nmse=%.9g top=%zu/%zu\n", nmse, top_want, top_got);
    require(nmse <= 1e-6 && top_want == top_got, "restored logits changed");
}

int main(int argc, char ** argv) {
    try {
        for (const char * key : {"PARACETAMOL_QWEN4EXP_QSA_GRAPH", "PARACETAMOL_QWEN4EXP_QSA_KERNEL",
                                "PARACETAMOL_QWEN4EXP_QSA_CACHE"}) {
            const char * value = std::getenv(key);
            require(value && std::string(value) == "1", "requires enabled compact QSA and derived cache");
        }
        common_params params;
        params.n_ctx = 8192;
        params.n_batch = params.n_ubatch = 2048;
        params.n_parallel = 1;
        params.kv_unified = false;
        params.cache_type_k = params.cache_type_v = GGML_TYPE_F16;
        common_init();
        // Use the same option set as managed one-shot inference, including
        // the process-local allocator opt-out required by the direct reader.
        if (!common_params_parse(argc, argv, params, LLAMA_EXAMPLE_CLI)) return 1;
        llama_backend_init();
        auto instance = common_init_from_params(params);
        require(instance && instance->context(), "model initialization failed");
        auto * ctx = instance->context();
        char arch[64] = {};
        llama_model_meta_val_str(instance->model(), "general.architecture", arch, sizeof(arch));
        require(std::string(arch) == "qwen4exp", "requires Qwen4exp model");
        for (int pos = 0; pos < 4096; pos += 2048) decode(ctx, pos, 2048);
        decode(ctx, 4096, 1); // first sparse decode seeds derived keys
        decode(ctx, 4097, 1); // the next append uses them
        std::vector<uint8_t> state(llama_state_seq_get_size(ctx, 0));
        require(llama_state_seq_get_data(ctx, state.data(), state.size(), 0) == state.size(), "save failed");
        const auto expected = decode(ctx, 4098, 1);
        const auto restore = [&]() {
            require(llama_state_seq_set_data(ctx, state.data(), state.size(), 0) == state.size(), "restore failed");
        };
        restore();
        compare(expected, decode(ctx, 4098, 1));
        llama_set_causal_attn(ctx, false);
        restore();
        decode(ctx, 4098, 2);
        llama_set_causal_attn(ctx, true);
        restore();
        compare(expected, decode(ctx, 4098, 1));
        llama_memory_clear(llama_get_memory(ctx), true);
        restore();
        compare(expected, decode(ctx, 4098, 1));
        std::puts("PASS populated Qwen4exp causal transition, restore and clear");
        instance.reset();
        llama_backend_free();
        return 0;
    } catch (const std::exception & e) {
        std::fprintf(stderr, "FAIL %s\n", e.what());
        return 1;
    }
}
