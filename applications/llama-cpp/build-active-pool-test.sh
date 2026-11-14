#!/usr/bin/env bash
# Compile-only actual-API regression. The caller explicitly runs CPU checks.
set -Eeuo pipefail
umask 022
[[ $# == 3 ]] || { echo 'usage: build-active-pool-test.sh SOURCE INSTALLED_LIBRARY_DIR NEW_OUTPUT_DIR' >&2; exit 2; }
script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
source_dir=$(cd -- "$1" && pwd -P)
library_dir=$(cd -- "$2" && pwd -P)
output_dir=$(realpath -m -- "$3")
[[ ! -e $output_dir && ! -L $output_dir ]] || { echo 'Require a fresh output directory' >&2; exit 1; }
test "$(git -C "$source_dir" rev-parse HEAD)" = ded522f8304554317e7ff8f88698dbbbca5425aa
fixture_hash=$(sha256sum "$script_dir/test-qwen4exp-active-pool.cpp")
test "${fixture_hash%% *}" = 306f4f9e5f97430389d726ececa75201ac5250857c332ddcbc0e5ebfdc3e7c1c
for library in libllama.so libggml.so libggml-base.so libggml-cpu.so; do
 [[ -f $library_dir/$library ]] || { echo "Missing matching installed library: $library" >&2; exit 1; }
done
# A diagnostic may not silently substitute build/bin bytes for the installed
# runtime closure. The caller supplies and later verifies the matching installed
# directory; source, installed-library and binary hashes are retained separately.
case $library_dir in */build/bin|*/build/bin/*) echo 'Use installed libraries, not build/bin' >&2; exit 1;; esac
mkdir -p -- "$output_dir"
export GIT_OPTIONAL_LOCKS=0 GIT_INDEX_FILE
GIT_INDEX_FILE=$(mktemp "$output_dir/source-check.XXXXXX.index")
git -C "$source_dir" read-tree HEAD
git -C "$source_dir" add --update -- .
git -C "$source_dir" add -- common include src ggml tools tests
tree=$(git -C "$source_dir" write-tree)
case $tree in
 a71de650fdb5611b47870e273d360e582f19472c)
  policy=active-only
  cpp=f1e92d94f7e91208cc19202564d2e79c15575573bf75162c0fd28af879b017db
  header=cfd2ef37aed10a95f9eebe032d2e2279eb9e0c6174fab569d137115a04d0e7a7;;
 20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c)
  policy=prefix-active-fill
  cpp=49fd8679a2afc53d0864948efcf5ab43f95a7e4c7a063745af3c919310a17e0f
  header=5f783f7074600954af050b731abff349a92b3e72ed35bbdd82874b96be6e2b93;;
 aeeb3f45e00deae964a7949da69f1389fa98176a)
  policy=baseline-negative-control
  cpp=08cbd47bcac5e481ef66c586c27510b210f34d76042dbafa6d51d9dbc489831a
  header=cfd2ef37aed10a95f9eebe032d2e2279eb9e0c6174fab569d137115a04d0e7a7;;
 *) echo 'Unreviewed source tree: update this fixture deliberately' >&2; exit 1;;
esac
git -C "$source_dir" diff --quiet
git -C "$source_dir" diff --cached --check
unset GIT_INDEX_FILE
printf '%s  %s\n' "$cpp" "$source_dir/src/llama-memory-hybrid-idx.cpp" \
 "$header" "$source_dir/src/llama-memory-hybrid-idx.h" | sha256sum --check -
printf '%s\n' "$tree" > "$output_dir/source-tree.txt"
printf '%s\n' "$policy" > "$output_dir/source-policy.txt"
sha256sum "$source_dir/src/llama-memory-hybrid-idx.cpp" "$source_dir/src/llama-memory-hybrid-idx.h" \
 "$script_dir/test-qwen4exp-active-pool.cpp" "$0" > "$output_dir/source.sha256"
sha256sum "$library_dir/libllama.so" "$library_dir/libggml.so" \
 "$library_dir/libggml-base.so" "$library_dir/libggml-cpu.so" > "$output_dir/installed-libraries.sha256"
"${CXX:-c++}" -std=c++17 -O2 -Wall -Wextra -Werror \
 -I "$source_dir/src" -I "$source_dir/include" -I "$source_dir/ggml/include" \
 "$script_dir/test-qwen4exp-active-pool.cpp" -L "$library_dir" \
 "-Wl,-rpath-link,$library_dir" '-Wl,-rpath,$ORIGIN/../lib:/usr/local/lib' \
 -lllama -lggml -lggml-base -lggml-cpu -o "$output_dir/test-qwen4exp-active-pool"
sha256sum --check "$output_dir/source.sha256"
sha256sum --check "$output_dir/installed-libraries.sha256"
sha256sum "$output_dir/test-qwen4exp-active-pool" > "$output_dir/binary.sha256"
printf 'PASS compile-only active-pool fixture source=%s policy=%s (not executed)\n' "$tree" "$policy"
