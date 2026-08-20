#!/usr/bin/env bash
set -Eeuo pipefail

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

profile="${PARACETAMOL_PROFILE:-auto}"
mode="${PARACETAMOL_DWARFSTAR_MODE:-server}"
model="${PARACETAMOL_DWARFSTAR_MODEL:-}"
listen="${PARACETAMOL_LISTEN:-0.0.0.0}"
host_listen="${PARACETAMOL_HOST_LISTEN:-unknown}"
port="${PARACETAMOL_PORT:-8000}"
context="${PARACETAMOL_DWARFSTAR_CONTEXT:-131072}"
output_tokens="${PARACETAMOL_DWARFSTAR_OUTPUT_TOKENS:-16000}"
prompt="${PARACETAMOL_DWARFSTAR_PROMPT:-}"
no_thinking="${PARACETAMOL_DWARFSTAR_NO_THINKING:-0}"
dspark="${PARACETAMOL_DWARFSTAR_DSPARK:-0}"
dspark_model="${PARACETAMOL_DWARFSTAR_DSPARK_MODEL:-}"

case "$profile" in
    auto|rdna4|strix-halo|strix-point) ;;
    *) die "unknown DwarfStar profile '$profile'" ;;
esac
case "$mode" in
    server|cli) ;;
    *) die "unknown DwarfStar mode '$mode'" ;;
esac
case "$no_thinking" in
    0|1) ;;
    *) die "invalid DwarfStar thinking setting '$no_thinking'" ;;
esac
case "$dspark" in
    0|1) ;;
    *) die "invalid DwarfStar DSpark setting '$dspark'" ;;
esac
[[ "$port" =~ ^[0-9]+$ ]] && ((port >= 1 && port <= 65535)) ||
    die "invalid DwarfStar port '$port'"
[[ "$context" =~ ^[0-9]+$ ]] && ((context >= 4096 && context <= 1048576)) ||
    die "DwarfStar context must be between 4096 and 1048576 tokens"
[[ "$output_tokens" =~ ^[0-9]+$ ]] &&
    ((output_tokens >= 1 && output_tokens < context)) ||
    die "DwarfStar output tokens must be positive and smaller than context"
[[ -n "$model" ]] || die "no DwarfStar GGUF model was selected"
[[ -f "$model" && -r "$model" ]] ||
    die "DwarfStar GGUF model is not a readable regular file: $model"
if [[ "$dspark" == 1 ]]; then
    [[ -n "$dspark_model" ]] ||
        die "no DwarfStar DSpark support GGUF was selected"
    [[ -f "$dspark_model" && -r "$dspark_model" ]] ||
        die "DwarfStar DSpark support GGUF is not a readable regular file: ${dspark_model}"
fi

mapfile -t architectures < <(
    rocminfo 2>/dev/null |
        awk '$1 == "Name:" && $2 ~ /^gfx[0-9]+$/ { print $2 }' |
        sort -u
)
((${#architectures[@]} == 1)) ||
    die "expected exactly one visible AMD GPU architecture"
architecture="${architectures[0]}"
case "$architecture" in
    gfx1200|gfx1201) detected_profile=rdna4 ;;
    gfx1151) detected_profile=strix-halo ;;
    gfx1150) detected_profile=strix-point ;;
    *) die "unsupported GPU architecture '$architecture'" ;;
esac
if [[ "$profile" != auto && "$profile" != "$detected_profile" ]]; then
    die "profile '$profile' does not match detected architecture '$architecture'"
fi
profile="$detected_profile"

mkdir -p /data/home

printf '\nParacetamol: DwarfStar %s\n' "$mode"
printf '  profile:        %s\n' "$profile"
printf '  architecture:   %s\n' "$architecture"
printf '  model:          %s\n' "$model"
printf '  context:        %s tokens\n' "$context"
printf '  output limit:   %s tokens\n' "$output_tokens"
if [[ "$dspark" == 1 ]]; then
    printf '  DSpark:         enabled (%s)\n' "$dspark_model"
else
    printf '  DSpark:         disabled\n'
fi
if [[ "$mode" == server ]]; then
    printf '  container bind: %s:%s (container namespace)\n' "$listen" "$port"
    printf '  host publish:   %s:%s\n' "$host_listen" "$port"
fi
printf '\n'

common=(
    --rocm
    --model "$model"
    --ctx "$context"
    --tokens "$output_tokens"
)
if [[ "$dspark" == 1 ]]; then
    common+=(--mtp "$dspark_model" --dspark)
fi

if [[ "$mode" == server ]]; then
    exec ds4-server \
        "${common[@]}" \
        --host "$listen" \
        --port "$port"
fi

thinking=(--think)
if [[ "$no_thinking" == 1 ]]; then
    thinking=(--nothink)
fi
cli_sampling=()
if [[ "$dspark" == 1 ]]; then
    cli_sampling=(--temp 0)
fi
if [[ -n "$prompt" ]]; then
    exec ds4 \
        "${common[@]}" \
        "${cli_sampling[@]}" \
        "${thinking[@]}" \
        --prompt "$prompt"
fi
exec ds4 "${common[@]}" "${cli_sampling[@]}" "${thinking[@]}"
