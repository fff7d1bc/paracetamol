#!/usr/bin/env bash
set -Eeuo pipefail

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

profile="${PARACETAMOL_PROFILE:-auto}"
listen_address="${PARACETAMOL_LISTEN:-0.0.0.0}"
host_listen="${PARACETAMOL_HOST_LISTEN:-unknown}"
port="${PARACETAMOL_PORT:-8188}"
disable_bundled_extensions="${PARACETAMOL_DISABLE_BUNDLED_EXTENSIONS:-0}"
memory_policy="${PARACETAMOL_MEMORY_POLICY:-balanced}"
kernel_policy="${PARACETAMOL_KERNEL_POLICY:-default}"
image_python="/opt/venv/bin/python"
custom_python_root="/data/custom-node-python"
bundled_custom_node_root="/tmp/paracetamol-bundled-custom-nodes"
bundled_custom_nodes=()
persistent_node_overrides=()

case "$memory_policy" in
    balanced|conservative) ;;
    *) die "unknown memory policy '$memory_policy'" ;;
esac
case "$kernel_policy" in
    default|experimental) ;;
    *) die "unknown kernel policy '$kernel_policy'" ;;
esac

mkdir -p \
    /data/cache/huggingface \
    /data/cache/pip \
    /data/cache/torch \
    /data/custom_nodes \
    /data/home \
    /data/input \
    /data/output \
    /data/user \
    /tmp/comfy

prepare_custom_node_python() {
    local pending=""
    local image_python_version
    local custom_python_version
    local image_site_packages
    local custom_site_packages
    local path_file
    local path_file_pending

    if [[ -L "$custom_python_root" ]]; then
        die "custom-node Python environment must not be a symlink: $custom_python_root"
    fi
    if [[ ! -e "$custom_python_root" ]]; then
        pending="$(mktemp -d /data/.custom-node-python.XXXXXX)"
        if ! "$image_python" -m venv --without-pip "$pending"; then
            rm -rf -- "$pending"
            die "cannot create persistent custom-node Python environment"
        fi
        if ! mv -T -- "$pending" "$custom_python_root"; then
            rm -rf -- "$pending"
            die "cannot install persistent custom-node Python environment"
        fi
    elif [[ ! -d "$custom_python_root" ]]; then
        die "custom-node Python environment is not a directory: $custom_python_root"
    fi

    [[ -x "$custom_python_root/bin/python" ]] ||
        die "custom-node Python environment is incomplete: $custom_python_root"

    image_python_version="$(
        "$image_python" -c \
            'import sys; print("{}.{}".format(*sys.version_info[:2]))'
    )"
    custom_python_version="$(
        "$custom_python_root/bin/python" -c \
            'import sys; print("{}.{}".format(*sys.version_info[:2]))'
    )"
    if [[ "$custom_python_version" != "$image_python_version" ]]; then
        die "custom-node Python $custom_python_version does not match image Python $image_python_version"
    fi

    image_site_packages="$(
        "$image_python" -c \
            'import sysconfig; print(sysconfig.get_path("purelib"))'
    )"
    custom_site_packages="$(
        "$custom_python_root/bin/python" -c \
            'import sysconfig; print(sysconfig.get_path("purelib"))'
    )"
    path_file="$custom_site_packages/paracetamol-image.pth"
    path_file_pending="$(mktemp "$custom_site_packages/.paracetamol-image.XXXXXX")"
    printf '%s\n' "$image_site_packages" >"$path_file_pending"
    mv -f -- "$path_file_pending" "$path_file"

    export PARACETAMOL_CUSTOM_NODE_ENV=1
    export VIRTUAL_ENV="$custom_python_root"
    export PATH="$custom_python_root/bin:$PATH"
    export PIP_CACHE_DIR=/data/cache/pip
}

prepare_custom_node_python

prepare_bundled_custom_nodes() {
    local name
    local bundled
    local persistent

    mkdir -p "$bundled_custom_node_root"
    for name in ComfyUI-GGUF rgthree-comfy; do
        bundled="/opt/paracetamol/custom_nodes/$name"
        persistent="/data/custom_nodes/$name"
        [[ -d "$bundled" && ! -L "$bundled" ]] ||
            die "bundled custom node is missing or unsafe: $bundled"
        if [[ -e "$persistent" || -L "$persistent" ]]; then
            persistent_node_overrides+=("$name")
            continue
        fi
        ln -s "$bundled" "$bundled_custom_node_root/$name"
        bundled_custom_nodes+=("$name")
    done
}

if [[ "$disable_bundled_extensions" != 1 ]]; then
    prepare_bundled_custom_nodes
fi

common_args=(
    --listen "$listen_address"
    --port "$port"
    --base-directory /data
    --models-directory /content/models
    --temp-directory /tmp/comfy
    --database-url sqlite:////data/user/comfyui.db
    --disable-auto-launch
    --log-stdout
)
profile_args=()

if [[ "$disable_bundled_extensions" != 1 ]]; then
    profile_args+=(
        --extra-model-paths-config
        /opt/paracetamol/extra_model_paths.yaml
    )
fi

profile_info="$(
    python /opt/paracetamol/container_profile.py "$profile"
)" || die "profile detection failed"
mapfile -t profile_fields <<<"$profile_info"
((${#profile_fields[@]} == 5)) || die "unexpected output from profile detection"
profile="${profile_fields[0]}"
detected_arch="${profile_fields[1]}"
detected_name="${profile_fields[2]}"
torch_version="${profile_fields[3]}"
rocm_version="${profile_fields[4]}"

if [[ "$profile" == cpu ]]; then
    profile_args+=(--cpu --disable-all-custom-nodes)
else
    if [[ "$profile" == strix-halo || "$profile" == strix-point ]]; then
        profile_args+=(--disable-mmap)
    fi
    if [[ "$memory_policy" == conservative ]]; then
        profile_args+=(
            --bf16-vae
            --gpu-only
            --disable-smart-memory
            --cache-none
        )
    fi
fi

printf '\nParacetamol: ComfyUI application\n'
printf '  profile:       %s\n' "$profile"
printf '  device:        %s\n' "$detected_name"
printf '  architecture:  %s\n' "$detected_arch"
printf '  PyTorch:       %s\n' "$torch_version"
printf '  ROCm/HIP:      %s\n' "$rocm_version"
printf '  memory policy: %s\n' "$memory_policy"
printf '  kernel policy: %s\n' "$kernel_policy"
printf '  data:          /data\n'
printf '  container bind: %s:%s\n' "$listen_address" "$port"
printf '  host publish:  %s:%s\n' "$host_listen" "$port"
if [[ "$disable_bundled_extensions" == 1 ]]; then
    printf '  bundled nodes: disabled\n'
elif ((${#bundled_custom_nodes[@]})); then
    printf '  bundled nodes: %s\n' "${bundled_custom_nodes[*]}"
fi
if ((${#persistent_node_overrides[@]})); then
    printf '  persistent overrides: %s\n' "${persistent_node_overrides[*]}"
fi
if (($#)); then
    printf '  extra args:'
    printf ' %q' "$@"
    printf '\n'
fi
printf '\n'

exec python main.py "${common_args[@]}" "${profile_args[@]}" "$@"
