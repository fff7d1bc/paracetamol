#!/usr/bin/env bash
set -Eeuo pipefail

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

profile="${PARACETAMOL_PROFILE:-auto}"
model="${PARACETAMOL_GUFO_MODEL:-}"
model_id="${PARACETAMOL_GUFO_MODEL_ID:-}"
context="${PARACETAMOL_GUFO_CONTEXT:-262144}"
output_tokens="${PARACETAMOL_GUFO_OUTPUT_TOKENS:-16384}"
cache_bytes="${PARACETAMOL_GUFO_CACHE_RAM_BYTES:-30064771072}"
listen="${PARACETAMOL_LISTEN:-0.0.0.0}"
port="${PARACETAMOL_PORT:-8080}"
reasoning="${PARACETAMOL_GUFO_REASONING:-medium}"

(($# == 0)) || die 'Gufo does not accept trailing upstream arguments'
case "$profile" in auto|strix-halo) ;; *) die 'Gufo requires the Strix Halo profile' ;; esac
case "$reasoning" in off|medium|xhigh) ;; *) die 'Gufo reasoning must be off, medium, or xhigh' ;; esac
[[ "$context" =~ ^[1-9][0-9]{3,5}$ ]] && ((context >= 4096 && context <= 262144 && context % 1024 == 0)) ||
    die 'Gufo context must be a multiple of 1024 between 4096 and 262144'
[[ "$output_tokens" =~ ^[1-9][0-9]{0,5}$ ]] && ((output_tokens < context)) ||
    die 'Gufo output tokens must be positive and smaller than context'
[[ "$cache_bytes" =~ ^(0|[1-9][0-9]{0,10})$ ]] && ((cache_bytes <= 34359738368)) ||
    die 'Gufo RAM cache budget must be between 0 (auto) and 34359738368 bytes'
[[ "$port" =~ ^[1-9][0-9]{0,4}$ ]] && ((port <= 65535)) || die 'invalid Gufo port'
[[ "$listen" == 0.0.0.0 ]] || die 'managed Gufo requires an internal IPv4 wildcard bind'
[[ "$model_id" =~ ^[a-z0-9][a-z0-9._-]*$ ]] || die 'invalid Gufo model identifier'
[[ "$model" == /content/models/*.gguf && "$model" != *'..'* && -f "$model" && -r "$model" ]] ||
    die 'Gufo requires a readable managed GGUF below /content/models'

mapfile -t architectures < <(rocminfo 2>/dev/null |
    awk '$1 == "Name:" && $2 ~ /^gfx[0-9]+$/ { print $2 }')
((${#architectures[@]} == 1)) && [[ "${architectures[0]}" == gfx1151 ]] ||
    die 'Gufo requires exactly one visible gfx1151 GPU'

mkdir -p /data/home
# Keep this engine's queue policy local to its image. Other engines retain
# their accepted environment and no custom HIP runtime is introduced.
export GPU_MAX_HW_QUEUES=1
printf '\nParacetamol Gufo\n  Profile       strix-halo (gfx1151)\n'
printf '  Model         %s\n  Context       %s tokens\n' "$model_id" "$context"
printf '  Reasoning     %s (request-overridable)\n  RAM cache     %s bytes\n' "$reasoning" "$cache_bytes"
printf '  Sampling      model defaults by thinking mode (request-overridable)\n'
printf '  Speculation   off\n  Disk cache    off\n'
printf '  Container bind %s:%s\n\n' "$listen" "$port"

thinking=(--think on --reasoning-effort "$reasoning")
if [[ "$reasoning" == off ]]; then thinking=(--think off); fi
exec gufo serve llm --model "$model" --served-model-name "$model_id" \
    --host "$listen" --port "$port" --context "$context" --max-tokens "$output_tokens" \
    --sessions 1 --speculative off --cache-ram-bytes "$cache_bytes" \
    --preserve-thinking on "${thinking[@]}"
