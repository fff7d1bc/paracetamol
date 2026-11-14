#!/usr/bin/env bash
# CPU source-policy check only; neither a GPU compiler nor inference gate.
set -euo pipefail
[[ $# == 2 ]] || { echo 'usage: build-mmq-activation-sum-test.sh SOURCE OUTPUT_DIR' >&2; exit 2; }
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
source_dir=$(cd -- "$1" && pwd)
mkdir -p -- "$2"
output_dir=$(cd -- "$2" && pwd)
source_file="$source_dir/ggml/src/ggml-cuda/quantize.cu"
body="$output_dir/mmq-activation-sum.inc"
# Fail closed on the exact producer and compatibility region. The scalar and
# few-row producers must not supply a plausible-looking substitute body.
awk '
    /^static __global__ void quantize_mmq_q8_1\(/ {producer++; inside=1}
    inside && /^#if defined\(GGML_USE_HIP\)$/ {region++; copy=1}
    inside && /\/\/ write the block once \(normal\)/ {copy=0; inside=0; done++}
    copy {print}
    END {if (producer != 1 || region != 1 || done != 1) exit 1}
' "$source_file" > "$body"
sha256sum "$source_file" "$body"
for mode in nonhip hip; do
    definitions=()
    if [[ $mode == hip ]]; then definitions=(-DGGML_USE_HIP); fi
    "${CXX:-c++}" -std=c++17 -O2 -Wall -Wextra -Werror -Wno-unknown-pragmas \
        "${definitions[@]}" -I "$output_dir" \
        "$script_dir/test-mmq-activation-sum.cpp" -o "$output_dir/test-mmq-activation-sum-$mode"
    "$output_dir/test-mmq-activation-sum-$mode"
done
