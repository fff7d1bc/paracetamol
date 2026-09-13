#include "llama-lazy-reader.h"
#include "preset.h"
#include <cmath>
#include <iostream>
#include <numeric>

// A router may inspect several presets before any child starts. That work
// must not alter the environment inherited by unrelated children.
static bool allocation_policy() {
    for (const auto example : {LLAMA_EXAMPLE_SERVER, LLAMA_EXAMPLE_CLI}) {
        common_set_env("GGML_CUDA_ENABLE_UNIFIED_MEMORY", "1");
        common_preset_context context(example);
        common_preset exceptional, ordinary;
        // CLI parsing requires a model argument, but this fixture never loads it.
        exceptional.set_option(context, "LLAMA_ARG_MODEL", "/tmp/policy-test-not-loaded.gguf");
        ordinary.set_option(context, "LLAMA_ARG_MODEL", "/tmp/policy-test-not-loaded.gguf");
        exceptional.set_option(context, "LLAMA_ARG_NO_UNIFIED_MEMORY", "true");
        common_params preview;
        exceptional.apply_to_params(preview);
        if (!preview.no_unified_memory || !std::getenv("GGML_CUDA_ENABLE_UNIFIED_MEMORY")) { return false; }
        for (const auto * preset : {&ordinary, &exceptional, &ordinary}) {
            // Each real router child inherits the unchanged parent environment.
            common_set_env("GGML_CUDA_ENABLE_UNIFIED_MEMORY", "1");
            auto arguments = preset->to_args("allocation-test");
            std::vector<char *> argv;
            for (auto & argument : arguments) { argv.push_back(argument.data()); }
            common_params params;
            if (!common_params_parse((int) argv.size(), argv.data(), params, example)) { return false; }
            if ((std::getenv("GGML_CUDA_ENABLE_UNIFIED_MEMORY") == nullptr) != (preset == &exceptional)) { return false; }
        }
    }
    common_set_env("GGML_CUDA_ENABLE_UNIFIED_MEMORY", "");
    std::cout << "server and CLI allocation policy isolation passed\\n";
    return true;
}

int main() {
    if (!allocation_policy()) { return 10; }
    constexpr int width = 256, rows = 67, prefix = 123;
    int cases = 0;
    for (const auto type : {GGML_TYPE_F32, GGML_TYPE_F16, GGML_TYPE_Q8_0, GGML_TYPE_IQ1_S, GGML_TYPE_IQ4_NL}) {
        const size_t stride = ggml_row_size(type, width);
        std::vector<float> input(rows * width), weights(width, 1.0f);
        for (int i = 0; i < rows * width; ++i) { input[i] = std::sin(float(i) * 0.123f) + float(i % 17) / 11.0f; }
        std::vector<uint8_t> packed(stride * rows);
        ggml_quantize_chunk(type, input.data(), packed.data(), 0, rows, width, weights.data());
        char path[] = "/tmp/ple-reader-test-XXXXXX";
        int fd = mkstemp(path);
        if (fd < 0) { return 1; }
        unlink(path);
        std::vector<uint8_t> bytes(prefix + packed.size());
        memcpy(bytes.data() + prefix, packed.data(), packed.size());
        size_t done = 0;
        while (done < bytes.size()) {
            ssize_t n = write(fd, bytes.data() + done, bytes.size() - done);
            if (n <= 0) { return 2; }
            done += n;
        }
        llama_lazy_reader reader(fd, prefix, stride, rows, 8, type, width);
        const auto to_float = ggml_get_type_traits(type)->to_float;
        for (const int count : {0, 1, 31, 32, 33, 511, 1024, 1025}) {
            std::vector<int32_t> ids(count);
            for (int i = 0; i < count; ++i) { ids[i] = (i * 31 + i / 7) % rows; }
            std::vector<float> got(count * width), want(count * width);
            reader.gather(ids.data(), count, got.data());
            for (int i = 0; i < count; ++i) {
                if (type == GGML_TYPE_F32) {
                    memcpy(want.data() + i * width, packed.data() + ids[i] * stride, width * sizeof(float));
                } else {
                    to_float(packed.data() + ids[i] * stride, want.data() + i * width, width);
                }
            }
            if (!got.empty() && memcmp(got.data(), want.data(), got.size() * sizeof(float))) { return 3; }
            ++cases;
        }
        if (ftruncate(fd, prefix + stride / 2)) { return 4; }
        bool failed = false;
        int32_t zero = 0;
        std::vector<float> dest(width);
        try { reader.gather(&zero, 1, dest.data()); } catch (const std::runtime_error &) { failed = true; }
        if (!failed) { return 5; }
        ++cases;
        std::vector<int32_t> many(128, 0);
        dest.resize(128 * width);
        failed = false;
        try { reader.gather(many.data(), 128, dest.data()); } catch (const std::runtime_error &) { failed = true; }
        if (!failed) { return 6; }
        ++cases;
        std::cout << ggml_type_name(type) << " reference and EOF checks passed\n";
    }
    std::cout << cases << "/50 checks passed\n";
    ggml_quantize_free();
}
