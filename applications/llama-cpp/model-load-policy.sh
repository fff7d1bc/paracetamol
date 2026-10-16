#!/usr/bin/env bash

# One argument tuple serves direct startup and native router INI sections.
# Only the validated Strix Halo stream-token-embedding policy calls this.
llama_stream_token_embedding_args() {
    local backend="$1" mode="$2"
    case "$mode" in server|cli) ;; *) return 1 ;; esac
    case "$backend" in
        vulkan)
            # Preserve the accepted Vulkan mmap reader, without ROCm overrides.
            printf '%s\n' --load-mode mmap --lazy-mode on \
                --override-tensor per_layer_token_embd.weight=CPU
            ;;
        rocm)
            printf '%s\n' --load-mode none --lazy-mode on-direct \
                --override-tensor per_layer_token_embd.weight=CPU \
                --no-unified-memory --no-host --fit off \
                --batch-size 2048 --ubatch-size 2048 --cache-ram 0
            ;;
        *) return 1 ;;
    esac
}

# Keep the accepted separate cache for a single streaming-model slot. Multiple
# slots share the configured token pool: partitioning it would silently shrink
# each conversation, while multiplying it can exhaust a 128 GB host.
llama_server_slot_args() {
    local slots="$1" separate="$2"
    [[ "$slots" =~ ^([1-9]|[1-5][0-9]|6[0-4])$ ]] || return 1
    printf '%s\n' --parallel "$slots"
    if [[ "$separate" == 1 && "$slots" == 1 ]]; then
        printf '%s\n' --no-kv-unified
    else
        [[ "$separate" == 0 || "$separate" == 1 ]] || return 1
        printf '%s\n' --kv-unified
    fi
}

# Run after model/profile policy expansion, when the final cache layout is
# known. Emit one slot/context tuple per section, without duplicate INI keys.
llama_server_slot_ini() {
    local slots="$1"
    [[ "$slots" =~ ^([1-9]|[1-5][0-9]|6[0-4])$ ]] || return 1
    awk -v slots="$slots" '
        function flush() {
            if (!section) return
            if (context !~ /^[1-9][0-9]*$/ || context > 2147483647) {
                print "invalid configured context in managed router preset" > "/dev/stderr"
                bad = 1
                exit 1
            }
            print "parallel = " slots
            print (separate && slots == 1 ? "no-kv-unified = true" : "kv-unified = true")
            printf "c = %.0f\n", context
        }
        /^\[/ { flush(); section=1; context=""; separate=0 }
        /^c = / { context=$3; next }
        /^no-kv-unified = true$/ { separate=1; next }
        /^kv-unified = true$/ { separate=0; next }
        /^parallel = / { next }
        { print }
        END { if (!bad) flush() }
    '
}

llama_stream_token_embedding_ini() {
    local backend="$1" index key value
    local -a args
    local rendered
    rendered="$(llama_stream_token_embedding_args "$backend" server)" || return
    mapfile -t args <<< "$rendered"
    for ((index = 0; index < ${#args[@]}; index += 1)); do
        key="${args[index]#--}"
        value=true
        if ((index + 1 < ${#args[@]})) && [[ "${args[index+1]}" != --* ]]; then
            ((index += 1))
            value="${args[index]}"
        fi
        printf '%s = %s\n' "$key" "$value"
    done
    # The final slot renderer retains this single-slot layout only at slots=1.
    if [[ "$backend" == rocm ]]; then
        printf '%s\n' 'no-kv-unified = true'
    fi
}
