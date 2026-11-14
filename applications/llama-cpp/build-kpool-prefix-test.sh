#!/usr/bin/env bash
# CPU integer-layout oracle, production rebuild verifier and negative controls.
# No llama library, model, backend initialization, inference or Go toolchain.
set -euo pipefail
[[ $# == 2 ]] || { echo 'usage: build-kpool-prefix-test.sh SOURCE OUTPUT_DIR' >&2; exit 2; }
script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
source_dir=$(cd -- "$1" && pwd -P)
mkdir -p -- "$2"
output_dir=$(cd -- "$2" && pwd -P)
for mode in normal crossing resume-cut keep-drafts count-only; do
    output=$output_dir/$mode
    bash "$script_dir/extract-kpool-prefix-test.sh" "$source_dir" "$output" "$mode"
    "${CXX:-c++}" -std=c++17 -O2 -Wall -Wextra -Werror -I "$output" \
        "$script_dir/test-kpool-prefix-layout.cpp" -o "$output/test-kpool-prefix-layout"
    if [[ $mode == normal ]]; then
        "$output/test-kpool-prefix-layout" > "$output/oracle.log" 2>&1
        "$output/test-kpool-prefix-layout" --verify > "$output/verifier.log" 2>&1
        grep -Fx 'PASS independent integer layout updates=28032 verifier=0' "$output/oracle.log"
        grep -Fx 'PASS independent integer layout updates=28032 verifier=1' "$output/verifier.log"
    else
        if "$output/test-kpool-prefix-layout" > "$output/oracle.log" 2>&1; then
            echo "Broken layout incorrectly survived independent oracle: $mode" >&2; exit 1
        fi
        grep -F 'FAIL integer oracle mismatch:' "$output/oracle.log"
        if "$output/test-kpool-prefix-layout" --verify > "$output/verifier.log" 2>&1; then
            echo "Broken layout incorrectly survived production verifier: $mode" >&2; exit 1
        fi
        grep -Fx 'FAIL production verifier mismatch' "$output/verifier.log"
    fi
done
"${CXX:-c++}" -std=c++17 -O1 -g -fno-omit-frame-pointer -fsanitize=address,undefined \
    -I "$output_dir/normal" "$script_dir/test-kpool-prefix-layout.cpp" \
    -o "$output_dir/normal/test-kpool-prefix-layout-sanitized"
ASAN_OPTIONS=halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1 \
    "$output_dir/normal/test-kpool-prefix-layout-sanitized" --verify > "$output_dir/normal/sanitizer.log" 2>&1
grep -Fx 'PASS independent integer layout updates=28032 verifier=1' "$output_dir/normal/sanitizer.log"
sha256sum "$source_dir/src/llama-memory-hybrid-idx.cpp" \
    "$script_dir/extract-kpool-prefix-test.sh" "$script_dir/test-kpool-prefix-layout.cpp" \
    "$output_dir/normal/production-decl.inc" "$output_dir/normal/production-scan.inc" \
    "$output_dir/normal/production-update.inc" "$output_dir/normal/test-kpool-prefix-layout" \
    "$output_dir/normal/test-kpool-prefix-layout-sanitized"
echo 'PASS prefix layout production extraction, independent oracle, full-rebuild verifier, four broken-layout controls and ASan/UBSan'
