#!/usr/bin/env bash
# Mechanically extract the exact production struct, scanner and whole update.
# No engine/library/model, Go toolchain, device discovery or implementation copy.
set -euo pipefail
[[ $# == 3 && ( $3 == normal || $3 == crossing || $3 == resume-cut || $3 == keep-drafts || $3 == count-only ) ]] || {
    echo 'usage: extract-kpool-prefix-test.sh SOURCE OUTPUT_DIR normal|crossing|resume-cut|keep-drafts|count-only' >&2
    exit 2
}
source_dir=$(cd -- "$1" && pwd -P)
mkdir -p -- "$2"
output_dir=$(cd -- "$2" && pwd -P)
mode=$3
source_file=$source_dir/src/llama-memory-hybrid-idx.cpp
export GIT_OPTIONAL_LOCKS=0 GIT_INDEX_FILE=$output_dir/source-check.index
test "$(git -C "$source_dir" rev-parse HEAD)" = ded522f8304554317e7ff8f88698dbbbca5425aa || {
    echo 'Unsupported llama.cpp source pin; review the prefix layout fixture' >&2
    exit 1
}
# These are reviewed owning-file identities, not permission to adopt either PR.
# PR31913's separate graph.cpp hunk does not alter this file or these regions.
digest=$(sha256sum "$source_file")
case ${digest%% *} in
    3e18b0bd48a49531630c594716efd8a41754ede96236ac6f64b96f95372936fc) identity=prefix-32137 ;;
    49fd8679a2afc53d0864948efcf5ab43f95a7e4c7a063745af3c919310a17e0f) identity=prefix-32137-active-4161abc ;;
    *) echo 'Unsupported prefix owning-file hash; review source/extraction' >&2; exit 1 ;;
esac
printf '%s\n' "$digest"
extract() {
    awk -v first="$1" -v closing="$2" '
        index($0, first) == 1 {starts++; if (starts == 1) inside=1}
        inside {print}
        inside && $0 == closing {ends++; inside=0}
        END {
            if (starts != 1 || ends != 1 || inside) {
                print "Nonunique or incomplete production extraction seam: " first > "/dev/stderr"
                exit 1
            }
        }
    ' "$source_file" > "$3"
}
extract 'struct llama_memory_hybrid_idx::kpool_layout {' '};' "$output_dir/production-decl.inc"
extract 'void kpool_scan(' '}' "$output_dir/production-scan.inc"
extract 'const llama_memory_hybrid_idx::kpool_layout & llama_memory_hybrid_idx::kpool_layout_update() {' '}' "$output_dir/production-update.inc"
# Whole region hashes bind the mock-facing extraction as well as the complete
# owning source. A changed function body is never accepted because it compiles.
(
    cd -- "$output_dir"
    printf '%s\n' \
        'db9ea6fc2b51312ca57dacc7323522ceea2905d38c5a6f4e4194911b3092901f  production-decl.inc' \
        '74199ddfc3c624d0dbcb773a5665a7ff9660e95317dcd05fc420e6f856225247  production-scan.inc' \
        '246ff7524fd9a0882ae9e40f3d11c3c567623d794d8ca456833dd4661b8b28eb  production-update.inc' | sha256sum --check --strict -
)
# Only explicit negative controls alter the extracted update. Literal strings
# avoid regex/replacement metacharacter surprises; every mutation seam is unique.
# keep-drafts also disables the count fallback: removing erase alone safely
# rebuilds and is not a valid broken-layout control.
awk -v mode="$mode" '
    function replace(old, replacement, at, tail) {
        at=index($0, old)
        if (!at) return 0
        tail=substr($0, at+length(old))
        if (index(tail, old)) duplicate=1
        $0=substr($0, 1, at-1) replacement tail
        return 1
    }
    {
        if (mode == "crossing") a+=replace("sq.pools.back() + kpool > sq.cells.size()", "sq.pools.back() > sq.cells.size()")
        if (mode == "resume-cut") a+=replace("sq.j_next = sq.pools.empty() ? 0 : sq.pools.back() + kpool;", "sq.j_next = sq.cells.size();")
        if (mode == "keep-drafts") {
            a+=replace("sq.cells.erase(cut, sq.cells.end());", "(void) cut;")
            b+=replace("keep = sq.cells.size() == sp.size();", "keep = true;")
        }
        if (mode == "count-only") a+=replace("if (stale != POS_CLEAN) {", "if (false) {")
        print
    }
    END {
        if (duplicate || (mode != "normal" && a != 1) || (mode == "keep-drafts" && b != 1)) {
            print "Nonunique production mutation seam" > "/dev/stderr"
            exit 1
        }
    }
' "$output_dir/production-update.inc" > "$output_dir/test-update.inc"
awk '{print}' "$output_dir/production-scan.inc" "$output_dir/test-update.inc" > "$output_dir/production-impl.inc"
echo "PASS production extraction source=$identity mutation=$mode"
