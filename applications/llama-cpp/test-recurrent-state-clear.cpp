#include "llama-graph.h"
#include "llama-memory-hybrid-idx.h"
#include "llama-model.h"
#include "ggml-alloc.h"
#include "ggml-backend.h"
#include "ggml-cpu.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <memory>
#include <numeric>
#include <stdexcept>
#include <string>
#include <vector>

// CPU graphs only: actual hybrid constructor/input builder/build_rs, no weights,
// inference, copied implementation, global backend registry or device discovery.
static void require(bool ok, const char * message) {
    if (!ok) throw std::runtime_error(message);
}

using words = std::vector<uint32_t>;

static uint32_t bits(float value) {
    uint32_t result;
    std::memcpy(&result, &value, sizeof(result));
    return result;
}

static float number(uint32_t value) {
    float result;
    std::memcpy(&result, &value, sizeof(result));
    return result;
}

static words read(ggml_tensor * tensor) {
    require(tensor && ggml_is_contiguous(tensor) && ggml_nbytes(tensor)%4 == 0, "word-readable tensor");
    words result(ggml_nbytes(tensor)/4);
    ggml_backend_tensor_get(tensor, result.data(), 0, ggml_nbytes(tensor));
    return result;
}

static void write(ggml_tensor * tensor, const words & values) {
    require(values.size()*4 == ggml_nbytes(tensor), "tensor write extent");
    ggml_backend_tensor_set(tensor, values.data(), 0, ggml_nbytes(tensor));
}

static llama_ubatch batch(std::vector<int> ids, std::vector<int> positions) {
    require(ids.size() == positions.size(), "batch shape");
    llama_ubatch result{};
    result.b_equal_seqs = true;
    result.n_seq_tokens = 1;
    result.n_tokens = result.n_seqs = ids.size();
    result.n_pos = 1;
    result.data = std::make_shared<llama_ubatch::data_t>();
    auto & data = *result.data;
    data.token.assign(ids.size(), 1);
    data.pos.assign(positions.begin(), positions.end());
    data.n_seq_id.assign(ids.size(), 1);
    data.seq_id_data.assign(ids.begin(), ids.end());
    for (size_t i = 0; i < ids.size(); ++i) data.seq_id.push_back(&data.seq_id_data[i]);
    data.seq_id_unq = data.seq_id_data;
    std::sort(data.seq_id_unq.begin(), data.seq_id_unq.end());
    data.seq_id_unq.erase(std::unique(data.seq_id_unq.begin(), data.seq_id_unq.end()), data.seq_id_unq.end());
    result.n_seqs_unq = data.seq_id_unq.size();
    result.token = data.token.data();
    result.pos = data.pos.data();
    result.n_seq_id = data.n_seq_id.data();
    result.seq_id = data.seq_id.data();
    result.seq_id_unq = data.seq_id_unq.data();
    return result;
}

struct fixture {
    static constexpr int kv_size = 256;
    std::unique_ptr<llama_model> model;
    std::unique_ptr<llama_memory_hybrid_idx> memory;
    ggml_backend_t cpu;
    bool baseline;
    size_t graphs = 0, poisoned = 0, empty = 0, extra = 0;

    fixture(ggml_backend_t backend, bool old) : cpu(backend), baseline(old) {
        model.reset(llama_model_create(LLM_ARCH_QWEN4EXP, llama_model_default_params()));
        require(model && llama_model_is_hybrid(model.get()), "real Qwen4exp hybrid architecture");
        auto & hp = model->hparams;
        hp = {};
        hp.n_layer_all = 2;
        hp.n_embd = 8;
        hp.n_embd_head_k_full = hp.n_embd_head_v_full = 4;
        hp.n_head_arr.fill(1);
        hp.n_head_kv_arr.fill(1);
        std::iota(hp.kv_source_impl.begin(), hp.kv_source_impl.end(), 0);
        hp.ssm_d_conv = 3;
        hp.ssm_d_inner = 8;
        hp.ssm_d_state = 4;
        hp.ssm_n_group = 1;
        hp.ssm_dt_rank = 2;
        hp.is_recr_impl[0] = 1;
        hp.indexer_head_size = 4;
        hp.indexer_kpool = 4;
        hp.indexer_kpool_row = 2;
        hp.indexer_kpool_by_order = true;
        hp.is_ple_impl.set(0);
        hp.ple_n_heads = 1;
        hp.ple_conv_kernel = 2;
        hp.ple_ngram_size = 2;
        hp.dsv4_hc_mult = 2;
        model->layers.resize(2);

        // Capacity two leaves snapshot-group rows as canaries, not rewind proof.
        memory = std::make_unique<llama_memory_hybrid_idx>(*model, false,
            GGML_TYPE_F16, GGML_TYPE_F16, false, kv_size, 0, LLAMA_SWA_TYPE_NONE,
            GGML_TYPE_F32, GGML_TYPE_F32, 8, 4, 2, false, true,
            nullptr, nullptr, [](int32_t layer) { return layer == 1; });
        for (auto * tensor : states()) require(tensor != nullptr, "real R/S/PLE state");

        llm_graph_result views(16);
        for (auto * tensor : attention(views.get_ctx())) {
            auto values = read(tensor);
            for (size_t i = 0; i < values.size(); ++i) values[i] = 0x38003400 + uint32_t(i%16);
            write(tensor, values); // Nonzero finite F16 sentinels, never poison attention.
        }
    }

    std::array<ggml_tensor *, 3> states() const {
        auto * recurrent = memory->get_mem_recr();
        return {recurrent->r_l.at(0), recurrent->s_l.at(0), recurrent->p_l.at(0)};
    }

    std::array<ggml_tensor *, 3> attention(ggml_context * context) const {
        llama_kv_cache::slot_info whole{};
        whole.s0 = whole.s1 = 0;
        auto * value = memory->get_mem_attn()->get_v(context, 1, kv_size, whole)->view_src;
        require(value != nullptr, "attention V backing");
        return {memory->get_mem_attn()->get_k_storage(1), value, memory->get_mem_idx()->get_k_storage(1)};
    }

    void seed_finite() {
        for (auto * tensor : states()) {
            auto values = read(tensor);
            for (size_t i = 0; i < values.size(); ++i) values[i] = bits((int(i%37) - 18)*0.125f);
            write(tensor, values);
        }
    }

    void run(const char * label, llama_ubatch ubatch, bool poison, bool must_clear) {
        require(memory->get_mem_recr()->prepare({ubatch}), "recurrent prepare");
        auto slots = memory->get_mem_attn()->prepare({ubatch});
        require(!slots.empty(), "attention prepare");
        llama_memory_hybrid_idx_context context(memory.get(), slots, slots, {ubatch});
        require(context.apply(), "hybrid context apply");
        const auto * recurrent = context.get_recr();
        const int zero = recurrent->get_rs_z();
        require((zero >= 0) == must_clear, "expected reset/no-reset lifecycle");
        empty += zero < 0;
        extra += recurrent->get_n_rs() - ubatch.n_seqs;

        // Poison only the actual reset row: a live sibling is never poisoned,
        // cleared or used to excuse nonfinite output. Test the real helper;
        // a hand-built FILL graph would not protect the production call path.
        if (poison) {
            require(zero >= 0, "poison requires a reset row");
            const std::array<uint32_t, 4> bad = {0x7fc12345, 0x7f800000, 0xff800000, 0xffc54321};
            for (auto * tensor : states()) {
                auto values = read(tensor);
                for (int64_t i = 0; i < tensor->ne[0]; ++i) values.at(zero*tensor->ne[0] + i) = bad[i%4];
                write(tensor, values);
            }
            ++poisoned;
        }

        llm_graph_result result(256);
        llm_graph_params params{};
        params.model = model.get();
        params.hparams = model->hparams;
        params.cparams.n_ctx = kv_size;
        params.cparams.n_ubatch = 4;
        params.cparams.causal_attn = true;
        params.ubatch = ubatch;
        params.mctx = &context;
        params.backend_cpu = cpu;
        params.res = &result;
        llm_graph_context graph(params);
        auto * input = graph.build_inp_mem_hybrid();
        std::vector<ggml_tensor *> outputs;
        for (auto * tensor : states()) {
            auto * output = graph.build_rs(input->get_recr(), tensor, tensor->ne[0], ubatch.n_seqs);
            ggml_set_output(output);
            outputs.push_back(output);
        }

        const auto protected_tensors = attention(graph.ctx0);
        std::vector<words> protected_before, before;
        for (auto * tensor : protected_tensors) protected_before.push_back(read(tensor));
        for (auto * tensor : states()) before.push_back(read(tensor));
        size_t fills = 0, scales = 0;
        for (int i = 0; i < ggml_graph_n_nodes(graph.gf); ++i) {
            const auto op = ggml_graph_node(graph.gf, i)->op;
            fills += op == GGML_OP_FILL;
            scales += op == GGML_OP_SCALE;
        }
        require(baseline ? scales == 3 && fills == 0 : fills == 3 && scales == 0,
                "actual production build_rs clear operation");
        std::unique_ptr<ggml_backend_buffer, decltype(&ggml_backend_buffer_free)> allocation(
            ggml_backend_alloc_ctx_tensors(graph.ctx0, cpu), ggml_backend_buffer_free);
        require(bool(allocation), "CPU graph allocation");
        result.set_inputs(&ubatch);
        // s_copy() consumes rollback indices: inspect the setter-populated input
        // instead of calling that accessor a second time for the host oracle.
        const auto source_words = read(input->get_recr()->s_copy);
        std::vector<int32_t> sources(source_words.size());
        std::memcpy(sources.data(), source_words.data(), source_words.size()*sizeof(uint32_t));
        require(sources.size() == recurrent->get_n_rs(), "copy input extent");
        require(ggml_backend_graph_compute(cpu, graph.gf) == GGML_STATUS_SUCCESS, "CPU graph compute");

        for (size_t t = 0; t < states().size(); ++t) {
            auto * tensor = states()[t];
            const size_t width = tensor->ne[0];
            auto cleared = before[t];
            if (zero >= 0) std::fill_n(cleared.begin() + zero*width, width, bits(0.0f));
            auto expected = cleared;
            for (int32_t source : sources) require(source >= 0 && size_t(source + 1)*width <= cleared.size(), "copy source bounds");
            for (size_t row = ubatch.n_seqs; row < sources.size(); ++row) {
                require((recurrent->get_head() + row + 1)*width <= expected.size(), "copy destination bounds");
                std::copy_n(cleared.begin() + sources[row]*width, width,
                            expected.begin() + (recurrent->get_head() + row)*width);
            }
            const auto actual = read(tensor), output = read(outputs[t]);
            require(output.size() == ubatch.n_seqs*width, "gather output extent");
            bool observed_bad = false;
            for (size_t row = 0; row < ubatch.n_seqs; ++row) {
                for (size_t column = 0; column < width; ++column) {
                    const float got = number(output[row*width + column]);
                    if (baseline && poison && sources[row] == zero) {
                        require(!std::isfinite(got), "baseline reset must retain nonfinite poison");
                        observed_bad = true;
                    } else {
                        require(std::isfinite(got) && got == number(cleared[sources[row]*width + column]),
                                "independent clear/gather reference");
                    }
                }
            }
            if (baseline && poison) require(observed_bad, "baseline must exercise a poisoned gather");
            for (size_t i = 0; i < actual.size(); ++i) {
                bool reset = zero >= 0 && i/width == size_t(zero);
                for (size_t row = ubatch.n_seqs; row < sources.size(); ++row)
                    reset |= i/width == recurrent->get_head() + row && sources[row] == zero;
                if (baseline && poison && reset) require(!std::isfinite(number(actual[i])), "baseline persistent poison");
                else if (reset) require(number(actual[i]) == number(expected[i]), "persistent clear/copy reference");
                else require(actual[i] == expected[i], "unowned row or extra-state copy changed");
            }
        }
        for (size_t i = 0; i < protected_tensors.size(); ++i)
            require(read(protected_tensors[i]) == protected_before[i], "attention/indexer payload changed");
        context.next();
        ++graphs;
        std::printf("PASS %s poison=%d rs_zero=%d n_rs=%u n_seq=%u attention=unchanged\n",
                    label, int(poison), zero, recurrent->get_n_rs(), ubatch.n_seqs);
    }
};

int main(int argc, char ** argv) {
    try {
        const bool baseline = argc == 2 && std::string(argv[1]) == "--baseline-characterization";
        require(argc == 1 || baseline, "usage: test-recurrent-state-clear [--baseline-characterization]");
        std::unique_ptr<ggml_backend, decltype(&ggml_backend_free)> cpu(ggml_backend_cpu_init(), ggml_backend_free);
        require(bool(cpu), "direct CPU initialization");
        fixture test(cpu.get(), baseline);
        test.seed_finite();
        test.run("finite-new", batch({0, 1}, {0, 0}), false, true);
        test.seed_finite();
        test.run("finite-continuation", batch({0, 1}, {1, 1}), false, false);
        test.run("third-sequence", batch({2}, {0}), false, true);
        test.seed_finite();
        test.run("nonadjacent-extra-copy", batch({0, 2}, {2, 1}), false, false);
        require(test.memory->seq_rm(0, 0, -1), "remove old sequence");
        test.run("poison-reuse", batch({0, 1}, {0, 2}), true, true);
        test.memory->clear(false);
        test.run("poison-after-metadata-clear", batch({2, 3}, {0, 0}), true, true);
        require(test.graphs == 6 && test.poisoned == 2 && test.empty == 2 && test.extra == 2,
                "six-case lifecycle coverage changed");
        std::printf("PASS recurrent-state-clear graphs=%zu poisoned-clears=%zu empty-clears=%zu extra-copy-rows=%zu attention=unchanged mode=%s\n",
                    test.graphs, test.poisoned, test.empty, test.extra, baseline ? "baseline-characterization" : "candidate");
        return 0;
    } catch (const std::exception & error) {
        std::fprintf(stderr, "FAIL %s\n", error.what());
        return 1;
    }
}
