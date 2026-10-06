#include "arg.h"
#include "common.h"
#include "llama.h"

#include <chrono>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <stdexcept>
#include <string>
#include <vector>

// Hardware acceptance fixture, not an image-build smoke. Use the managed
// Flash-Next GGUF, F16 KV, direct embedding reader and the normal ROCm policy.
// The default also covers our compact QSA graph's causal-mode transitions.
// --state-smoke isolates clear/restore without that transition or speculation.
// Keep this usable with unpatched upstream as well as the downstream stack.
static void require(bool ok, const char * message) {
    if (!ok) throw std::runtime_error(message);
}

static std::vector<float> decode(llama_context * ctx, llama_pos pos, int count) {
    auto batch = llama_batch_init(count, 0, 1);
    for (int i = 0; i < count; ++i) {
        // Avoid version-specific common helpers when comparing source pins.
        batch.token[i] = 100 + (pos + i) % 97;
        batch.pos[i] = pos + i;
        batch.n_seq_id[i] = 1;
        batch.seq_id[i][0] = 0;
        batch.logits[i] = i == count - 1;
        ++batch.n_tokens;
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

static bool compare(const char * name, const std::vector<float> & want, const std::vector<float> & got) {
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
    const bool pass = nmse <= 1e-6 && top_want == top_got;
    std::printf("%s nmse=%.9g top=%zu/%zu %s\n", name, nmse, top_want, top_got, pass ? "PASS" : "FAIL");
    return pass;
}

int main(int argc, char ** argv) {
    std::setvbuf(stdout, nullptr, _IONBF, 0);
    const auto start = std::chrono::steady_clock::now();
    try {
        bool smoke = false;
        std::vector<char *> args = {argv[0]};
        for (int i = 1; i < argc; ++i) {
            if (std::strcmp(argv[i], "--state-smoke") == 0) smoke = true;
            else args.push_back(argv[i]);
        }
        args.push_back(nullptr);
        for (const char * key : {"PARACETAMOL_QWEN4EXP_QSA_GRAPH", "PARACETAMOL_QWEN4EXP_QSA_KERNEL",
                                "PARACETAMOL_QWEN4EXP_QSA_CACHE", "GGML_CUDA_DISABLE_GRAPHS",
                                "LLAMA_GRAPH_REUSE_DISABLE"}) {
            const char * value = std::getenv(key);
            std::printf("%s=%s\n", key, value ? value : "unset");
        }
        common_params params;
        params.n_ctx = 8192;
        params.n_batch = params.n_ubatch = 2048;
        params.n_parallel = 1;
        params.kv_unified = false;
        params.cache_type_k = params.cache_type_v = GGML_TYPE_F16;
        common_init();
        // Compile against the exact source under test. common_params is not a
        // stable ABI, even between upstream and a patched build of one commit.
        if (!common_params_parse((int) args.size() - 1, args.data(), params, LLAMA_EXAMPLE_CLI)) return 1;
        llama_backend_init();
        auto instance = common_init_from_params(params);
        require(instance && instance->context(), "model initialization failed");
        auto * ctx = instance->context();
        char arch[64] = {};
        llama_model_meta_val_str(instance->model(), "general.architecture", arch, sizeof(arch));
        require(std::string(arch) == "qwen4exp", "requires Qwen4exp model");
        const auto loaded = std::chrono::steady_clock::now();
        std::printf("state test mode=%s load_seconds=%.3f\n", smoke ? "smoke" : "full",
                    std::chrono::duration<double>(loaded - start).count());
        for (int pos = 0; pos < 4096; pos += 2048) decode(ctx, pos, 2048);
        decode(ctx, 4096, 1); // first sparse decode seeds derived keys
        decode(ctx, 4097, 1); // the next append uses them
        std::vector<uint8_t> state(llama_state_seq_get_size(ctx, 0));
        require(llama_state_seq_get_data(ctx, state.data(), state.size(), 0) == state.size(), "save failed");
        const auto expected = decode(ctx, 4098, 1);
        const auto restore = [&]() {
            require(llama_state_seq_set_data(ctx, state.data(), state.size(), 0) == state.size(), "restore failed");
        };
        bool pass = true;
        restore();
        pass &= compare("restore", expected, decode(ctx, 4098, 1));
        if (!smoke) {
            llama_set_causal_attn(ctx, false);
            restore();
            decode(ctx, 4098, 2);
            llama_set_causal_attn(ctx, true);
            restore();
            pass &= compare("causal-restore", expected, decode(ctx, 4098, 1));
        }
        for (int i = 0; i < (smoke ? 3 : 1); ++i) {
            llama_synchronize(ctx);
            llama_memory_clear(llama_get_memory(ctx), true);
            restore();
            pass &= compare("clear-restore", expected, decode(ctx, 4098, 1));
        }
        const auto finished = std::chrono::steady_clock::now();
        std::printf("%s populated Qwen4exp state test, test_seconds=%.3f total_seconds=%.3f\n",
                    pass ? "PASS" : "FAIL", std::chrono::duration<double>(finished - loaded).count(),
                    std::chrono::duration<double>(finished - start).count());
        instance.reset();
        llama_backend_free();
        return pass ? 0 : 1;
    } catch (const std::exception & e) {
        std::fprintf(stderr, "FAIL %s\n", e.what());
        return 1;
    }
}
