#!/usr/bin/env bash
# Compile only. The caller explicitly runs the actual-helper CPU regression.
set -euo pipefail
[[ $# == 4 && ( $1 == baseline || $1 == candidate ) ]] || {
    echo 'usage: build-recurrent-state-clear-test.sh baseline|candidate SOURCE LIBRARY_DIR OUTPUT_DIR' >&2
    exit 2
}
mode=$1
script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
source_dir=$(cd -- "$2" && pwd -P)
library_dir=$(cd -- "$3" && pwd -P)
mkdir -p -- "$4"
output_dir=$(cd -- "$4" && pwd -P)
# Internal constructor/header and helper layout have no stable ABI. A changed
# pin or owning file requires deliberate fixture review, not a looser matcher.
export GIT_OPTIONAL_LOCKS=0 GIT_INDEX_FILE
GIT_INDEX_FILE=$(mktemp "$output_dir/recurrent-clear-index.XXXXXX")
git -C "$source_dir" read-tree HEAD
test "$(git -C "$source_dir" rev-parse HEAD)" = ded522f8304554317e7ff8f88698dbbbca5425aa || {
    echo 'Unsupported llama.cpp source pin; review the recurrent reset fixture' >&2
    exit 1
}
case $mode in
    baseline) digest=c3d34e39816a7c13f389ad1f0e233b99bd9e60bc70f261c61645cf36f892fcbc ;;
    candidate) digest=9c684fd9069a8b52a29fd0c3a8a48b52fe35074c6cc09d78954f419191cc3c6c ;;
esac
printf '%s  %s\n' "$digest" "$source_dir/src/llama-graph.cpp" | sha256sum --check --strict -
"${CXX:-c++}" -std=c++17 -O2 -Wall -Wextra -Werror \
    -DGGML_SHARED -DGGML_BACKEND_SHARED \
    -I "$source_dir/src" -I "$source_dir/include" -I "$source_dir/ggml/include" \
    "$script_dir/test-recurrent-state-clear.cpp" -L "$library_dir" \
    "-Wl,-rpath-link,$library_dir" '-Wl,-rpath,$ORIGIN/../lib:/usr/local/lib' \
    -lllama -lggml -lggml-base -lggml-cpu -pthread \
    -o "$output_dir/test-recurrent-state-clear"
sha256sum "$source_dir/src/llama-graph.cpp" "$script_dir/test-recurrent-state-clear.cpp" \
    "$library_dir/libllama.so" "$library_dir/libggml-base.so" "$library_dir/libggml-cpu.so" \
    "$output_dir/test-recurrent-state-clear"
# Explicit runs with the identical installed library set:
# LD_LIBRARY_PATH=LIBRARY_DIR OUTPUT_DIR/test-recurrent-state-clear --baseline-characterization
# LD_LIBRARY_PATH=LIBRARY_DIR OUTPUT_DIR/test-recurrent-state-clear
