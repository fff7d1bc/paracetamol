#!/usr/bin/env bash
set -Eeuo pipefail

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

profile="${ROCMLETE_PROFILE:-auto}"
backend="${ROCMLETE_LLAMA_BACKEND:-rocm}"
mode="${ROCMLETE_LLAMA_MODE:-server}"
model="${ROCMLETE_LLAMA_MODEL:-}"
draft_model="${ROCMLETE_LLAMA_DRAFT_MODEL:-}"
speculative_type="${ROCMLETE_LLAMA_SPECULATIVE_TYPE:-}"
draft_tokens="${ROCMLETE_LLAMA_DRAFT_TOKENS:-0}"
context_override="${ROCMLETE_LLAMA_CONTEXT_OVERRIDE:-}"
jinja="${ROCMLETE_LLAMA_JINJA:-0}"
reasoning_preserve="${ROCMLETE_LLAMA_REASONING_PRESERVE:-0}"
chat_template="${ROCMLETE_LLAMA_CHAT_TEMPLATE:-}"
sampling_defaults="${ROCMLETE_LLAMA_SAMPLING_DEFAULTS:-}"
flash_attn_rdna4="${ROCMLETE_LLAMA_FLASH_ATTN_RDNA4:-}"
flash_attn_strix_halo="${ROCMLETE_LLAMA_FLASH_ATTN_STRIX_HALO:-}"
flash_attn_strix_point="${ROCMLETE_LLAMA_FLASH_ATTN_STRIX_POINT:-}"
kv_cache_rdna4="${ROCMLETE_LLAMA_KV_CACHE_RDNA4:-}"
kv_cache_strix_halo="${ROCMLETE_LLAMA_KV_CACHE_STRIX_HALO:-}"
kv_cache_strix_point="${ROCMLETE_LLAMA_KV_CACHE_STRIX_POINT:-}"
router="${ROCMLETE_LLAMA_ROUTER:-0}"
models_max="${ROCMLETE_LLAMA_MODELS_MAX:-2}"
listen="${ROCMLETE_LISTEN:-0.0.0.0}"
host_listen="${ROCMLETE_HOST_LISTEN:-unknown}"
port="${ROCMLETE_PORT:-8080}"
gpu_count="${ROCMLETE_GPU_COUNT:-0}"
runtime_report=/tmp/rocmplete-llama-runtime

case "$profile" in
    auto|rdna4|strix-halo|strix-point|cpu) ;;
    *) die "unknown profile '$profile'" ;;
esac
case "$backend" in
    rocm|vulkan) ;;
    *) die "unknown llama.cpp backend '$backend'" ;;
esac
case "$mode" in
    server|cli|bench) ;;
    *) die "unknown llama.cpp mode '$mode'" ;;
esac
[[ "$gpu_count" =~ ^[0-9]+$ ]] ||
    die "invalid visible GPU count '$gpu_count'"
case "$jinja" in
    0|1) ;;
    *) die "invalid llama.cpp Jinja setting '$jinja'" ;;
esac
case "$reasoning_preserve" in
    0|1) ;;
    *) die "invalid llama.cpp reasoning-preserve setting '$reasoning_preserve'" ;;
esac
# This mirrors the host catalog allowlist so neither boundary accepts paths.
case "$chat_template" in
    ""|kat-coder-v2.5|muse-glimmer-atem|qwen3-0.6b|qwen3.6|qwen3.8|translategemma-manual) ;;
    *) die "unknown managed llama.cpp chat template '$chat_template'" ;;
esac
case "$speculative_type" in
    ""|draft-mtp|draft-dflash) ;;
    *) die "unknown llama.cpp speculative type '$speculative_type'" ;;
esac
[[ "$draft_tokens" =~ ^(0|[1-9]|1[0-5])$ ]] ||
    die "draft-token count must be between 0 and 15"
if [[ -z "$speculative_type" ]]; then
    [[ "$draft_tokens" == 0 ]] ||
        die "draft tokens require a speculative type"
    [[ -z "$draft_model" ]] ||
        die "a draft model requires a speculative type"
else
    [[ "$draft_tokens" != 0 ]] ||
        die "speculative decoding requires a positive draft-token count"
fi
if [[ "$speculative_type" == draft-mtp ]]; then
    ((draft_tokens <= 8)) ||
        die "MTP draft-token count must be between 1 and 8"
fi
if [[ "$speculative_type" == draft-dflash && -z "$draft_model" ]]; then
    die "DFlash requires a draft model"
fi
if [[ -n "$draft_model" ]]; then
    [[ -f "$draft_model" ]] ||
        die "draft model is not a regular file: $draft_model"
fi
if [[ -n "$context_override" ]]; then
    [[ "$context_override" =~ ^[a-z0-9][a-z0-9_-]*\.context_length=int:[1-9][0-9]*(,[a-z0-9][a-z0-9_-]*\.context_length=int:[1-9][0-9]*)*$ ]] ||
        die "invalid managed llama.cpp context override"
fi
case "$flash_attn_strix_halo" in
    ""|on|off|auto) ;;
    *) die "invalid Strix Halo Flash Attention setting '$flash_attn_strix_halo'" ;;
esac
case "$flash_attn_strix_point" in
    ""|on|off|auto) ;;
    *) die "invalid Strix Point Flash Attention setting '$flash_attn_strix_point'" ;;
esac
case "$flash_attn_rdna4" in
    ""|on|off|auto) ;;
    *) die "invalid RDNA4 Flash Attention setting '$flash_attn_rdna4'" ;;
esac
case "$kv_cache_rdna4" in
    ""|f16|q8_0|q4_0) ;;
    *) die "invalid RDNA4 K/V cache setting '$kv_cache_rdna4'" ;;
esac
case "$kv_cache_strix_halo" in
    ""|f16|q8_0|q4_0) ;;
    *) die "invalid Strix Halo K/V cache setting '$kv_cache_strix_halo'" ;;
esac
case "$kv_cache_strix_point" in
    ""|f16|q8_0|q4_0) ;;
    *) die "invalid Strix Point K/V cache setting '$kv_cache_strix_point'" ;;
esac
case "$router" in
    0)
        [[ -n "$model" ]] || die "no GGUF model was selected"
        [[ -f "$model" ]] || die "GGUF model is not a regular file: $model"
        ;;
    1)
        [[ "$mode" == server ]] || die "router mode requires the server"
        [[ -f /run/rocmplete/models.ini ]] ||
            die "router preset is not a regular file"
        [[ "$models_max" =~ ^[1-9][0-9]*$ ]] ||
            die "router models-max must be positive"
        ;;
    *) die "invalid router setting '$router'" ;;
esac

architecture=cpu
device="CPU"
profile_args=()
bench_profile_args=()
speculative_args=()
model_policy_args=()
unified_memory=0
vulkan_f16_kv_contiguize=0
if [[ "$router" == 0 && -n "$context_override" ]]; then
    model_policy_args+=(--fit off --override-kv "$context_override")
fi
if [[ "$router" == 0 && "$jinja" == 1 ]]; then
    model_policy_args+=(--jinja)
fi
if [[ "$router" == 0 && "$reasoning_preserve" == 1 ]]; then
    model_policy_args+=(--reasoning-preserve)
fi
if [[ "$router" == 0 && -n "$chat_template" ]]; then
    chat_template_path="/usr/local/share/rocmplete/llama-chat-templates/${chat_template}.jinja"
    [[ -f "$chat_template_path" && -r "$chat_template_path" ]] ||
        die "managed llama.cpp chat template is not a readable regular file: $chat_template_path"
    model_policy_args+=(--jinja --chat-template-file "$chat_template_path")
fi
if [[ "$mode" == server && -n "$sampling_defaults" ]]; then
    model_policy_args+=(
        --sampling-defaults-by-reasoning "$sampling_defaults"
    )
fi
if [[ "$router" == 0 && -n "$speculative_type" ]]; then
    speculative_args+=(
        --spec-type "$speculative_type"
        --spec-draft-n-max "$draft_tokens"
    )
    if [[ -n "$draft_model" ]]; then
        speculative_args+=(--model-draft "$draft_model")
    fi
fi
if [[ "$profile" == cpu ]]; then
    [[ "$gpu_count" == 0 ]] ||
        die "CPU profile must not expose GPU devices"
    profile_args+=(--device none --gpu-layers 0)
    bench_profile_args+=(--device none --n-gpu-layers 0)
    if [[ -n "$speculative_type" ]]; then
        speculative_args+=(--device-draft none)
    fi
else
    ((gpu_count > 0)) || die "GPU profile requires at least one GPU"
    mapfile -t architectures < <(
        rocminfo 2>/dev/null |
            awk '$1 == "Name:" && $2 ~ /^gfx[0-9]+$/ { print $2 }' |
            sort -u
    )
    ((${#architectures[@]} == 1)) ||
        die "expected exactly one visible AMD GPU architecture"
    architecture="${architectures[0]}"
    case "$architecture" in
        gfx1200) detected_profile=rdna4 ;;
        gfx1201) detected_profile=rdna4 ;;
        gfx1151) detected_profile=strix-halo ;;
        gfx1150) detected_profile=strix-point ;;
        *) die "unsupported GPU architecture '$architecture'" ;;
    esac
    if [[ "$profile" != auto && "$profile" != "$detected_profile" ]]; then
        die "profile '$profile' does not match detected architecture '$architecture'"
    fi
    profile="$detected_profile"
    case "$profile" in
        rdna4) kv_cache="$kv_cache_rdna4" ;;
        strix-halo) kv_cache="$kv_cache_strix_halo" ;;
        strix-point) kv_cache="$kv_cache_strix_point" ;;
    esac
    case "$backend" in
        rocm) device_prefix=ROCm ;;
        vulkan) device_prefix=Vulkan ;;
    esac
    mapfile -t devices < <(
        llama-cli --list-devices 2>&1 |
            awk -v prefix="$device_prefix" '
                $1 ~ ("^" prefix "[0-9]+:$") { sub(/^  /, ""); print }
            '
    )
    ((${#devices[@]} == gpu_count)) ||
        die "expected $gpu_count visible $backend GPUs, found ${#devices[@]}"
    mapfile -t device_names < <(
        printf '%s\n' "${devices[@]}" |
            awk '{ sub(/:$/, "", $1); print $1 }'
    )
    printf -v backend_devices '%s,' "${device_names[@]}"
    backend_devices="${backend_devices%,}"
    profile_args+=(--device "$backend_devices")
    bench_profile_args+=(--device "$backend_devices")
    if [[ -n "$speculative_type" ]]; then
        speculative_args+=(--device-draft "$backend_devices")
    fi
    if [[ "$router" == 0 && -n "$kv_cache" ]]; then
        model_policy_args+=(
            --cache-type-k "$kv_cache"
            --cache-type-v "$kv_cache"
        )
    fi
    printf -v device '%s; ' "${devices[@]}"
    device="${device%; }"
    if ((gpu_count > 1)); then
        profile_args+=(--split-mode layer)
        bench_profile_args+=(--split-mode layer)
    fi
    if [[ "$profile" == rdna4 && "$router" == 0 &&
        -n "$flash_attn_rdna4" ]]; then
        model_policy_args+=(--flash-attn "$flash_attn_rdna4")
    fi
    if [[ "$profile" == strix-halo || "$profile" == strix-point ]]; then
        export GGML_CUDA_ENABLE_UNIFIED_MEMORY=1
        unified_memory=1
        profile_args+=(--load-mode none)
        bench_profile_args+=(--load-mode none)
    fi
    if [[ "$profile" == strix-halo ]]; then
        if [[ "$backend" == vulkan ]]; then
            # The patched path is opt-in so other architectures and Vulkan
            # implementations retain pinned-upstream behavior.
            export GGML_VK_FA_KV_CONTIG=1
            vulkan_f16_kv_contiguize=1
        fi
        if [[ "$router" == 0 && -n "$flash_attn_strix_halo" ]]; then
            model_policy_args+=(--flash-attn "$flash_attn_strix_halo")
        fi
    fi
    if [[ "$profile" == strix-point && "$router" == 0 &&
        -n "$flash_attn_strix_point" ]]; then
        model_policy_args+=(--flash-attn "$flash_attn_strix_point")
    fi
fi

if [[ "$mode" == bench ]]; then
    exec llama-bench \
        --offline \
        --model "$model" \
        "${bench_profile_args[@]}" \
        "$@"
fi

mkdir -p /data/cache /data/home
export HOME=/data/home
export LLAMA_CACHE=/data/cache

runtime_context=""
runtime_api_key=0
runtime_arguments=("$@")
for ((index = 0; index < ${#runtime_arguments[@]}; index += 1)); do
    case "${runtime_arguments[$index]}" in
        --ctx-size)
            if ((index + 1 < ${#runtime_arguments[@]})); then
                runtime_context="${runtime_arguments[$((index + 1))]}"
            fi
            ;;
        --api-key-file)
            runtime_api_key=1
            ;;
    esac
done

# The report lives only in this container's private tmpfs. The host status
# command reads it through a bounded `podman exec cat`, so it reflects the
# profile and backend device names resolved here without persisting stale
# state or scraping unbounded application logs.
{
    printf 'schema=1\n'
    printf 'mode=%s\n' "$mode"
    printf 'profile=%s\n' "$profile"
    printf 'backend=%s\n' "$backend"
    printf 'device=%s\n' "$device"
    printf 'backend_devices=%s\n' "${backend_devices:-none}"
    printf 'architecture=%s\n' "$architecture"
    printf 'gpu_count=%s\n' "$gpu_count"
    printf 'router=%s\n' "$router"
    printf 'models_max=%s\n' "$models_max"
    printf 'context=%s\n' "$runtime_context"
    printf 'listen=%s\n' "$listen"
    printf 'host_listen=%s\n' "$host_listen"
    printf 'port=%s\n' "$port"
    printf 'api_key=%s\n' "$runtime_api_key"
    printf 'unified_memory=%s\n' "$unified_memory"
    printf 'vulkan_f16_kv_contiguize=%s\n' "$vulkan_f16_kv_contiguize"
} > "$runtime_report"
chmod 0444 "$runtime_report"

printf '\nROCmplete: llama.cpp %s\n' "$mode"
printf '  profile:       %s\n' "$profile"
printf '  backend:       %s\n' "$backend"
printf '  device:        %s\n' "$device"
printf '  architecture:  %s\n' "$architecture"
if ((gpu_count > 1)); then
    printf '  GPU split:     layer (%s devices)\n' "$gpu_count"
fi
if [[ "$router" == 1 ]]; then
    printf '  models:        managed router presets (max %s loaded)\n' "$models_max"
else
    printf '  model:         %s\n' "$model"
    if [[ -n "$speculative_type" ]]; then
        printf '  speculation:   %s (%s draft tokens)\n' \
            "$speculative_type" "$draft_tokens"
        if [[ -n "$draft_model" ]]; then
            printf '  draft model:   %s\n' "$draft_model"
        fi
    fi
    if [[ -n "$context_override" ]]; then
        printf '  context policy: forced metadata; automatic fitting off\n'
    fi
    if [[ -n "${kv_cache:-}" ]]; then
        printf '  K/V cache:     %s\n' "$kv_cache"
    fi
fi
if [[ "$mode" == server ]]; then
    printf '  container bind: %s:%s\n' "$listen" "$port"
    printf '  host publish:  %s:%s\n' "$host_listen" "$port"
fi
printf '\n'

if [[ "$mode" == server ]]; then
    if [[ "$router" == 1 ]]; then
        router_preset=/tmp/rocmplete-models.ini
        IFS= read -r preset_version < /run/rocmplete/models.ini
        [[ "$preset_version" == "version = 1" ]] ||
            die "router preset has an unsupported version"
        ! grep -q '^\[\*\]$' /run/rocmplete/models.ini ||
            die "router preset must not contain global settings"
        {
            printf 'version = 1\n'
            tail -n +2 /run/rocmplete/models.ini |
                awk \
                    -v profile="$profile" \
                    -v gpu_count="$gpu_count" \
                    -v backend_devices="${backend_devices:-none}" '
                    /^rocmplete-flash-attn-rdna4 = / {
                        if (profile == "rdna4") {
                            sub(/^rocmplete-flash-attn-rdna4 = /, "")
                            print "flash-attn = " $0
                        }
                        next
                    }
                    /^rocmplete-flash-attn-strix-halo = / {
                        if (profile == "strix-halo") {
                            sub(/^rocmplete-flash-attn-strix-halo = /, "")
                            print "flash-attn = " $0
                        }
                        next
                    }
                    /^rocmplete-flash-attn-strix-point = / {
                        if (profile == "strix-point") {
                            sub(/^rocmplete-flash-attn-strix-point = /, "")
                            print "flash-attn = " $0
                        }
                        next
                    }
                    /^rocmplete-kv-cache-rdna4 = / {
                        if (profile == "rdna4") {
                            sub(/^rocmplete-kv-cache-rdna4 = /, "")
                            print "cache-type-k = " $0
                            print "cache-type-v = " $0
                        }
                        next
                    }
                    /^rocmplete-kv-cache-strix-halo = / {
                        if (profile == "strix-halo") {
                            sub(/^rocmplete-kv-cache-strix-halo = /, "")
                            print "cache-type-k = " $0
                            print "cache-type-v = " $0
                        }
                        next
                    }
                    /^rocmplete-kv-cache-strix-point = / {
                        if (profile == "strix-point") {
                            sub(/^rocmplete-kv-cache-strix-point = /, "")
                            print "cache-type-k = " $0
                            print "cache-type-v = " $0
                        }
                        next
                    }
                    /^\[[^*].*\]$/ {
                        print
                        print "offline = true"
                        if (profile == "cpu") {
                            print "device = none"
                            print "n-gpu-layers = 0"
                        } else {
                            print "device = " backend_devices
                            if (gpu_count > 1) {
                                print "split-mode = layer"
                            }
                            if (profile == "strix-halo" ||
                                profile == "strix-point") {
                                print "load-mode = none"
                            }
                        }
                        next
                    }
                    /^spec-type = draft-(mtp|dflash)$/ {
                        print
                        print "device-draft = " backend_devices
                        next
                    }
                    { print }
                '
        } > "$router_preset"
        exec llama-server \
            --host "$listen" \
            --port "$port" \
            --models-preset "$router_preset" \
            --models-max "$models_max" \
            "$@"
    fi
    exec llama-server \
        --offline \
        --host "$listen" \
        --port "$port" \
        --model "$model" \
        "${profile_args[@]}" \
        "${model_policy_args[@]}" \
        "${speculative_args[@]}" \
        "$@"
fi

exec llama-cli \
    --offline \
    --model "$model" \
    "${profile_args[@]}" \
    "${model_policy_args[@]}" \
    "${speculative_args[@]}" \
    "$@"
