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
            if [[ "$mode" == server ]]; then
                printf '%s\n' --parallel 1 --no-kv-unified
            fi
            ;;
        *) return 1 ;;
    esac
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
}
