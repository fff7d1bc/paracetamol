#!/usr/bin/env bash
# Build diagnostics only. CPU oracle checks are not GPU/inference acceptance.
set -euo pipefail
if [[ $# != 2 ]]; then
    echo "usage: bash build-routing-test.sh SOURCE_DIR OUTPUT_DIR" >&2
    exit 2
fi
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
source_dir=$(cd -- "$1" && pwd)
mkdir -p -- "$2"
output_dir=$(cd -- "$2" && pwd)
source_file="$source_dir/ggml/src/ggml-cuda/mmid.cu"
kernel_file="$output_dir/qwen4exp-routing-kernel.inc"
# Fail closed on the owning source boundaries. Preserve its exact helper text
# rather than maintaining a second device implementation in the fixture.
test "$(awk '/^static __global__ void mm_ids_helper_512_10\(/ {n++} END {print n+0}' "$source_file")" = 1
test "$(awk '/^bool ggml_cuda_qwen_mm_ids\(/ {n++} END {print n+0}' "$source_file")" = 1
awk '
    /^static __global__ void mm_ids_helper_512_10\(/ {
        if (previous != "__launch_bounds__(1024, 1)") exit 1
        print previous
        copy=1
    }
    /^bool ggml_cuda_qwen_mm_ids\(/ {if (!copy) exit 1; copy=0; done=1}
    copy {print}
    {previous=$0}
    END {if (!done) exit 1}
' \
    "$source_file" > "$kernel_file"
test "$(awk '/^static __global__ void mm_ids_helper_512_10\(/ {n++} END {print n+0}' "$kernel_file")" = 1
if ! [[ $(tail -n 2 "$kernel_file" | head -n 1) == '}' ]]; then
    echo 'unexpected production helper boundary' >&2
    exit 1
fi
# These cases intentionally feed invalid IDs. Refuse the pre-fix helper rather
# than accidentally turning a diagnostic invocation into a GPU OOB access.
if [[ $(awk 'index($0, "if ((unsigned) expert >= 512u) continue;") {n++} END {print n+0}' "$kernel_file") != 2 ]]; then
    echo 'routing invalid-ID tests require both reviewed bounds guards' >&2
    exit 1
fi
sha256sum "$source_file" "$kernel_file"
"${CXX:-c++}" -std=c++17 -O2 -Wall -Wextra -Werror -DROUTING_CPU_ONLY \
    "$script_dir/test-qwen4exp-routing.cpp" -o "$output_dir/test-qwen4exp-routing-cpu"
if [[ ${ROUTING_CPU_ONLY:-0} == 1 ]]; then exit 0; fi
hip_compiler=${HIPCXX:-$(rocm-sdk path --bin)/hipcc}
hip_link_flags=()
if [[ -n ${ROUTING_HIP_RPATH:-} ]]; then hip_link_flags+=("-Wl,-rpath,$ROUTING_HIP_RPATH"); fi
"$hip_compiler" -std=c++17 -O3 --offload-arch=gfx1151 -x hip \
    -I "$output_dir" "$script_dir/test-qwen4exp-routing.cpp" "${hip_link_flags[@]}" \
    -o "$output_dir/test-qwen4exp-routing-hip"
