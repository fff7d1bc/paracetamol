#!/usr/bin/env bash
# Compile only; running ROCm0 needs coordinated target-hardware acceptance.
set -euo pipefail
[[ $# == 3 ]] || { echo 'usage: build-conv-rollback-test.sh SOURCE LIBRARY_DIR OUTPUT_DIR' >&2; exit 2; }
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
source_dir=$(cd -- "$1" && pwd)
library_dir=$(cd -- "$2" && pwd)
mkdir -p -- "$3"
output_dir=$(cd -- "$3" && pwd)
# The fixture uses a private backend ABI and the current upstream rollback
# graph. A new pin or a removed owning matcher needs deliberate fixture review.
test "$(git -C "$source_dir" rev-parse HEAD)" = ded522f8304554317e7ff8f88698dbbbca5425aa
tail_source="$source_dir/ggml/src/ggml-cuda/conv-tail.cuh"
graph_source="$source_dir/src/llama-graph.cpp"
cuda_source="$source_dir/ggml/src/ggml-cuda/ggml-cuda.cu"
test "$(awk '/^static inline int64_t ggml_cuda_conv_rollback_window_col\(/ {n++} END {print n+0}' "$tail_source")" = 1
test "$(awk '/^    if \(t->op == GGML_OP_SET_ROWS\) \{/ {n++} END {print n+0}' "$tail_source")" = 1
test "$(awk '/^void llm_graph_context::build_conv_state_store\(/ {n++} END {print n+0}' "$graph_source")" = 1
test "$(awk 'index($0, "ggml_set_rows(ctx0, slots, windows, slot_order)") {n++} END {print n+0}' "$graph_source")" = 1
# This fixture must work against normal production libraries, not require a
# diagnostic-only counter API or silently accept a disabled private matcher.
if awk '/paracetamol_private_conv_launch_counts/ {found=1} END {exit !found}' "$cuda_source"; then
    echo 'Use the production source, without private diagnostic counter hooks' >&2
    exit 1
fi
sha256sum "$tail_source" "$graph_source" "$cuda_source" \
    "$source_dir/ggml/src/ggml-cuda/gdn-conv.cu" \
    "$source_dir/ggml/src/ggml-cuda/ple-conv.cu" \
    "$script_dir/test-qwen4exp-conv-rollback.cpp"
"${CXX:-c++}" -std=c++17 -O2 -Wall -Wextra -Werror \
    -DGGML_SHARED -DGGML_BACKEND_SHARED \
    -I "$source_dir/ggml/include" -I "$source_dir/ggml/src" \
    "$script_dir/test-qwen4exp-conv-rollback.cpp" -L "$library_dir" \
    "-Wl,-rpath-link,$library_dir" '-Wl,-rpath,$ORIGIN/../lib:/usr/local/lib' \
    -lggml -lggml-base -pthread -ldl -o "$output_dir/test-qwen4exp-conv-rollback"
# Run with these matching libraries (or install into the managed runtime):
# LD_LIBRARY_PATH=LIBRARY_DIR OUTPUT_DIR/test-qwen4exp-conv-rollback --cpu
# GPU run omits --cpu. CPU mode never asserts ROCm matcher eligibility.
