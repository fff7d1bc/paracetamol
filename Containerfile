# docker.io/library/ubuntu:resolute-20261105.1 (26.04), multi-architecture index resolved 2026-11-13.
ARG ROCM_RUNTIME_IMAGE=localhost/paracetamol:runtime-ubuntu26.04-rocm10.1-r6
ARG ROCM_BASE_IMAGE=localhost/paracetamol:base-ubuntu26.04-rocm10.1-torch2.14-r9
ARG UBUNTU_IMAGE=docker.io/library/ubuntu@sha256:66b8084a0d45bd4877992bd38e04b51bfa99bd57293364cb6391fb674771846e
ARG ROCM_PACKAGE_INDEX=https://stable.repo.amd.com/rocm/whl-next/
ARG ROCM_VERSION=10.1.0
ARG TORCH_VERSION=2.14.0
# AMD's documented ROCm 10.1 PyTorch 2.14 tuple uses this exact alpha-labeled wheel.
ARG TORCHVISION_VERSION=0.29.0a0
ARG TORCHAUDIO_VERSION=2.11.0.3
ARG LLAMA_CPP_COMMIT=ded522f8304554317e7ff8f88698dbbbca5425aa

FROM ${UBUNTU_IMAGE} AS content-tools

ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/paracetamol/pip
ENV DEBIAN_FRONTEND=noninteractive \
    VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH} \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        python3 \
        python3-venv && \
    rm -rf /var/lib/apt/lists/* && \
    python3 -m venv "${VIRTUAL_ENV}"

COPY containers/content_tools/requirements.txt /opt/content-requirements.txt
RUN python -m pip install \
        --requirement /opt/content-requirements.txt && \
    python -m pip check

COPY containers/content_tools/download.py \
    /opt/paracetamol/container_download.py
RUN chmod 0644 /opt/paracetamol/container_download.py

LABEL org.opencontainers.image.title="Paracetamol content tools" \
      org.opencontainers.image.description="Pinned download utilities for Paracetamol content"

FROM ${UBUNTU_IMAGE} AS rocm-runtime

ARG ROCM_VERSION
ARG ROCM_PACKAGE_INDEX
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/paracetamol/pip
ENV DEBIAN_FRONTEND=noninteractive \
    VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH} \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        libatomic1 \
        libnuma1 \
        python3 \
        python3-venv && \
    rm -rf /var/lib/apt/lists/* && \
    python3 -m venv "${VIRTUAL_ENV}" && \
    python -m pip install \
        --index-url "${ROCM_PACKAGE_INDEX}" \
        "rocm[libraries,device-gfx1150,device-gfx1151,device-gfx1200,device-gfx1201]==${ROCM_VERSION}" && \
    python -m pip check

LABEL org.opencontainers.image.title="Paracetamol ROCm runtime" \
      org.opencontainers.image.description="Shared minimal ROCm runtime for locally built Paracetamol images" \
      io.github.fff7d1bc.paracetamol.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.paracetamol.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201"

FROM ${ROCM_RUNTIME_IMAGE} AS rocm-base

ARG ROCM_VERSION
ARG ROCM_PACKAGE_INDEX
ARG TORCH_VERSION
ARG TORCHVISION_VERSION
ARG TORCHAUDIO_VERSION
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/paracetamol/pip
ENV DEBIAN_FRONTEND=noninteractive \
    VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH} \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    HOME=/data/home \
    XDG_CACHE_HOME=/data/cache \
    HF_HOME=/data/cache/huggingface \
    TORCH_HOME=/data/cache/torch

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        ffmpeg \
        git \
        libgl1 \
        libglib2.0-0t64 \
        python3-dev \
        python3-pip && \
    git config --system init.defaultBranch main && \
    rm -rf /var/lib/apt/lists/*

RUN python -m pip install --upgrade pip setuptools wheel

RUN python -m pip install \
        --index-url "${ROCM_PACKAGE_INDEX}" \
        "torch[device-gfx1150,device-gfx1151,device-gfx1200,device-gfx1201]==${TORCH_VERSION}+rocm${ROCM_VERSION}" \
        "torchvision[device-gfx1150,device-gfx1151,device-gfx1200,device-gfx1201]==${TORCHVISION_VERSION}+rocm${ROCM_VERSION}" \
        "torchaudio==${TORCHAUDIO_VERSION}+rocm${ROCM_VERSION}"

COPY containers/content_tools/download.py \
    /opt/paracetamol/container_download.py
RUN chmod 0644 /opt/paracetamol/container_download.py

FROM ${ROCM_BASE_IMAGE} AS comfyui

ARG ROCM_VERSION
ARG TORCH_VERSION
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/paracetamol/pip
ARG COMFYUI_VERSION=0.28.0
ARG COMFYUI_COMMIT=700821e1364eaab0e8f21c538a2131719fec57bf

WORKDIR /opt
RUN git init ComfyUI && \
    cd ComfyUI && \
    git remote add origin https://github.com/Comfy-Org/ComfyUI.git && \
    git fetch --depth=1 origin "${COMFYUI_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${COMFYUI_COMMIT}" && \
    rm -rf .git

COPY applications/comfyui/constraints.txt /opt/comfyui-constraints.txt
RUN python -m pip install \
        --constraint /opt/comfyui-constraints.txt \
        --requirement /opt/ComfyUI/requirements.txt \
        --requirement /opt/ComfyUI/manager_requirements.txt && \
    python -m pip check

COPY applications/comfyui/patch_manager.py \
    /opt/paracetamol/patch_comfyui_manager.py
RUN python /opt/paracetamol/patch_comfyui_manager.py && \
    python -m compileall -q \
        /opt/venv/lib/python3.14/site-packages/comfyui_manager

ARG COMFYUI_GGUF_COMMIT=6ea2651e7df66d7585f6ffee804b20e92fb38b8a
RUN python -m pip install \
        "gguf==0.19.0" \
        "protobuf==7.35.1" && \
    mkdir -p /opt/paracetamol/custom_nodes && \
    git init /opt/paracetamol/custom_nodes/ComfyUI-GGUF && \
    cd /opt/paracetamol/custom_nodes/ComfyUI-GGUF && \
    git remote add origin https://github.com/city96/ComfyUI-GGUF.git && \
    git fetch --depth=1 origin "${COMFYUI_GGUF_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${COMFYUI_GGUF_COMMIT}" && \
    rm -rf .git && \
    python -m pip check

ARG RGTHREE_COMMIT=6b76ee6f2c5a007710b5a16f97c94330d6ecc871
RUN git init /opt/paracetamol/custom_nodes/rgthree-comfy && \
    cd /opt/paracetamol/custom_nodes/rgthree-comfy && \
    git remote add origin https://github.com/rgthree/rgthree-comfy.git && \
    git fetch --depth=1 origin "${RGTHREE_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${RGTHREE_COMMIT}" && \
    test -f requirements.txt && \
    test ! -s requirements.txt && \
    python -c \
        'import tomllib; assert tomllib.load(open("pyproject.toml", "rb"))["project"]["dependencies"] == []' && \
    mkdir -p /usr/share/licenses/paracetamol/rgthree-comfy && \
    cp LICENSE /usr/share/licenses/paracetamol/rgthree-comfy/LICENSE && \
    rm -rf .git

COPY applications/comfyui/entrypoint.sh /usr/local/bin/paracetamol-entrypoint
COPY containers/common/profile.py /opt/paracetamol/container_profile.py
COPY applications/comfyui/extra-model-paths.yaml \
    /opt/paracetamol/extra_model_paths.yaml
RUN chmod 0755 /usr/local/bin/paracetamol-entrypoint && \
    chmod 0644 \
        /opt/paracetamol/container_profile.py \
        /opt/paracetamol/patch_comfyui_manager.py \
        /opt/paracetamol/extra_model_paths.yaml && \
    mkdir -p /data /tmp/comfy && \
    chmod 1777 /tmp/comfy

LABEL org.opencontainers.image.title="Paracetamol local ROCm application image" \
      org.opencontainers.image.description="Locally built ROCm application image for AMD Strix Point, Strix Halo, and RDNA 4 Radeon GPUs" \
      org.opencontainers.image.licenses="BSD-3-Clause AND GPL-3.0-only AND Apache-2.0 AND MIT" \
      org.opencontainers.image.version="${COMFYUI_VERSION}" \
      org.opencontainers.image.revision="${COMFYUI_COMMIT}" \
      io.github.fff7d1bc.paracetamol.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.paracetamol.pytorch.version="${TORCH_VERSION}" \
      io.github.fff7d1bc.paracetamol.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201" \
      io.github.fff7d1bc.paracetamol.comfyui-gguf.revision="${COMFYUI_GGUF_COMMIT}" \
      io.github.fff7d1bc.paracetamol.comfyui-gguf.license="Apache-2.0" \
      io.github.fff7d1bc.paracetamol.rgthree-comfy.revision="${RGTHREE_COMMIT}" \
      io.github.fff7d1bc.paracetamol.rgthree-comfy.license="MIT"

WORKDIR /opt/ComfyUI
EXPOSE 8188
STOPSIGNAL SIGINT
ENTRYPOINT ["/usr/local/bin/paracetamol-entrypoint"]

# Native applications remain independent of the PyTorch image while sharing
# one minimal ROCm runtime and one exact build-only ROCm development layer.
FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk

ARG DEBIAN_FRONTEND=noninteractive
ARG ROCM_VERSION
ARG ROCM_PACKAGE_INDEX
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/paracetamol/pip

ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH} \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        git \
        python3-pip && \
    git config --system init.defaultBranch main && \
    rm -rf /var/lib/apt/lists/* && \
    python -m pip install \
        --index-url "${ROCM_PACKAGE_INDEX}" \
        "rocm[devel]==${ROCM_VERSION}" && \
    python -m pip check && \
    rocm-sdk init && \
    test -x "$(rocm-sdk path --bin)/hipcc" && \
    test -d "$(rocm-sdk path --cmake)"

FROM native-rocm-sdk AS llama-rocm-sdk

ARG DEBIAN_FRONTEND=noninteractive
ARG GLSLC_VERSION=2026.1-1
ARG VULKAN_VERSION=1.4.341.0-1
ARG SPIRV_HEADERS_VERSION=1.6.1+1.4.341.0-1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        cmake \
        curl \
        libcurl4-openssl-dev \
        libomp-dev \
        "libvulkan-dev=${VULKAN_VERSION}" \
        ninja-build \
        "glslc=${GLSLC_VERSION}" \
        "spirv-headers=${SPIRV_HEADERS_VERSION}" && \
    rm -rf /var/lib/apt/lists/*

FROM llama-rocm-sdk AS llama-builder

ARG LLAMA_CPP_COMMIT

WORKDIR /opt/llama.cpp
COPY applications/llama-cpp/hip-apu-host-buffer.patch \
    /opt/paracetamol/llama-hip-apu-host-buffer.patch
COPY applications/llama-cpp/quantized-kv-flash-attention.patch \
    /opt/paracetamol/llama-quantized-kv-flash-attention.patch
COPY applications/llama-cpp/hip-strix-halo-tiled-gdn.patch \
    /opt/paracetamol/llama-hip-strix-halo-tiled-gdn.patch
COPY applications/llama-cpp/hip-strix-halo-graph-recapture.patch \
    /opt/paracetamol/llama-hip-strix-halo-graph-recapture.patch
COPY applications/llama-cpp/qwen4exp-direct-reader.patch \
    /opt/paracetamol/llama-qwen4exp-direct-reader.patch
COPY applications/llama-cpp/hip-qwen4exp-qsa-kernels.patch \
    /opt/paracetamol/llama-hip-qwen4exp-qsa-kernels.patch
COPY applications/llama-cpp/hip-qwen4exp-graph-optimizations.patch \
    /opt/paracetamol/llama-hip-qwen4exp-graph-optimizations.patch
COPY applications/llama-cpp/hip-qwen4exp-pipeline.patch \
    /opt/paracetamol/llama-hip-qwen4exp-pipeline.patch
COPY applications/llama-cpp/hip-mmq-activation-sum-compat.patch \
    /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch
COPY applications/llama-cpp/server-stream-keepalive.patch \
    /opt/paracetamol/llama-server-stream-keepalive.patch
COPY applications/llama-cpp/topk-moe-boundaries.patch \
    /opt/paracetamol/llama-topk-moe-boundaries.patch
COPY applications/llama-cpp/hip-qwen4exp-decode.patch \
    /opt/paracetamol/llama-hip-qwen4exp-decode.patch
COPY applications/llama-cpp/process-allocation-policy.patch \
    /opt/paracetamol/llama-process-allocation-policy.patch
COPY applications/llama-cpp/test-mmq-activation-sum.cpp \
    applications/llama-cpp/build-mmq-activation-sum-test.sh /opt/paracetamol/
RUN git init . && \
    git remote add origin https://github.com/ggml-org/llama.cpp.git && \
    git fetch --depth=1 origin "${LLAMA_CPP_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${LLAMA_CPP_COMMIT}" && \
    git submodule update --init --recursive --depth=1 && \
    git apply --check /opt/paracetamol/llama-hip-apu-host-buffer.patch && \
    git apply /opt/paracetamol/llama-hip-apu-host-buffer.patch && \
    git apply --check /opt/paracetamol/llama-quantized-kv-flash-attention.patch && \
    git apply /opt/paracetamol/llama-quantized-kv-flash-attention.patch && \
    git apply --check /opt/paracetamol/llama-qwen4exp-direct-reader.patch && \
    git apply /opt/paracetamol/llama-qwen4exp-direct-reader.patch && \
    git apply --check /opt/paracetamol/llama-process-allocation-policy.patch && \
    git apply /opt/paracetamol/llama-process-allocation-policy.patch && \
    git apply --check /opt/paracetamol/llama-hip-strix-halo-graph-recapture.patch && \
    git apply /opt/paracetamol/llama-hip-strix-halo-graph-recapture.patch && \
    git apply --check /opt/paracetamol/llama-hip-strix-halo-tiled-gdn.patch && \
    git apply /opt/paracetamol/llama-hip-strix-halo-tiled-gdn.patch && \
    git apply --check /opt/paracetamol/llama-hip-qwen4exp-qsa-kernels.patch && \
    git apply /opt/paracetamol/llama-hip-qwen4exp-qsa-kernels.patch && \
    git apply --check /opt/paracetamol/llama-hip-qwen4exp-pipeline.patch && \
    git apply /opt/paracetamol/llama-hip-qwen4exp-pipeline.patch && \
    git apply --check /opt/paracetamol/llama-hip-qwen4exp-graph-optimizations.patch && \
    git apply /opt/paracetamol/llama-hip-qwen4exp-graph-optimizations.patch && \
    git apply --check /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch && \
    git apply /opt/paracetamol/llama-hip-mmq-activation-sum-compat.patch && \
    git apply --check /opt/paracetamol/llama-server-stream-keepalive.patch && \
    git apply /opt/paracetamol/llama-server-stream-keepalive.patch && \
    git apply --check /opt/paracetamol/llama-topk-moe-boundaries.patch && \
    git apply /opt/paracetamol/llama-topk-moe-boundaries.patch && \
    git apply --check /opt/paracetamol/llama-hip-qwen4exp-decode.patch && \
    git apply /opt/paracetamol/llama-hip-qwen4exp-decode.patch && \
    bash /opt/paracetamol/build-mmq-activation-sum-test.sh \
        /opt/llama.cpp /tmp/mmq-activation-sum-test && \
    rocm_root="$(rocm-sdk path --root)" && \
    site_packages="$(python -c \
        'import sysconfig; print(sysconfig.get_paths()["purelib"])')" && \
    cmake -S . -B build -G Ninja \
        -DGGML_HIP=ON \
        -DGGML_VULKAN=ON \
        "-DGPU_TARGETS=gfx1150;gfx1151;gfx1200;gfx1201" \
        -DGGML_RPC=OFF \
        -DLLAMA_BUILD_TESTS=OFF \
        -DLLAMA_BUILD_EXAMPLES=OFF \
        -DLLAMA_BUILD_SERVER=ON \
        -DLLAMA_BUILD_UI=OFF \
        -DLLAMA_USE_PREBUILT_UI=OFF \
        -DLLAMA_CONNECT=OFF \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/opt/llama-install \
        "-DCMAKE_PREFIX_PATH=$(rocm-sdk path --cmake)" \
        "-DCMAKE_BUILD_RPATH=${rocm_root}/lib" \
        "-DCMAKE_INSTALL_RPATH=${site_packages}/_rocm_sdk_core/lib;${site_packages}/_rocm_sdk_libraries/lib" && \
    cmake --build build --config Release && \
    cmake --install build --config Release && \
    find /opt/llama-install/bin -type f \
        ! -name llama-cli \
        ! -name llama-server \
        ! -name llama-bench \
        -delete && \
    rm -rf \
        /opt/llama-install/include \
        /opt/llama-install/lib/cmake \
        /opt/llama-install/lib/pkgconfig && \
    mkdir -p /opt/llama-install/share/licenses/paracetamol/llama-cpp && \
    cp LICENSE \
        /opt/llama-install/share/licenses/paracetamol/llama-cpp/LICENSE

COPY applications/llama-cpp/test-native-policy.cpp /opt/paracetamol/test-llama-native-policy.cpp
RUN c++ -O2 -std=c++17 -Icommon -Isrc -Iinclude -Iggml/include -Ivendor \
        /opt/paracetamol/test-llama-native-policy.cpp \
        -L/opt/llama-install/lib -Wl,-rpath,/opt/llama-install/lib \
        -lllama-common -lllama -lggml -lggml-base -pthread \
        -o /tmp/test-llama-native-policy && \
    LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-llama-native-policy

# This checks the independent routing oracle without initializing a GPU.
# Hardware acceptance separately runs the extracted production helper.
COPY applications/llama-cpp/test-qwen4exp-routing.cpp \
    applications/llama-cpp/build-routing-test.sh /opt/paracetamol/
RUN ROUTING_CPU_ONLY=1 bash /opt/paracetamol/build-routing-test.sh \
        /opt/llama.cpp /tmp/qwen4exp-routing-test && \
    /tmp/qwen4exp-routing-test/test-qwen4exp-routing-cpu

# CPU checks the rollback-state oracle. Hardware acceptance also requires the
# allocated ROCm matcher and observed kernels, not numerical fallback alone.
COPY applications/llama-cpp/test-qwen4exp-conv-rollback.cpp \
    applications/llama-cpp/build-conv-rollback-test.sh /opt/paracetamol/
RUN bash /opt/paracetamol/build-conv-rollback-test.sh \
        /opt/llama.cpp /opt/llama-install/lib /tmp/conv-rollback-test && \
    LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/conv-rollback-test/test-qwen4exp-conv-rollback --cpu

# Keep server-only telemetry after the native build so a reporting change does
# not invalidate the accepted GPU kernel compilation or its numerical identity.
COPY applications/llama-cpp/server-speculative-metrics.patch \
    /opt/paracetamol/llama-server-speculative-metrics.patch
COPY applications/llama-cpp/test-server-speculative-metrics.cpp \
    /opt/paracetamol/test-server-speculative-metrics.cpp
RUN git apply --check /opt/paracetamol/llama-server-speculative-metrics.patch && \
    git apply /opt/paracetamol/llama-server-speculative-metrics.patch && \
    cmake --build build --config Release --target llama-server && \
    cmake --install build --config Release && \
    find /opt/llama-install/bin -type f \
        ! -name llama-cli ! -name llama-server ! -name llama-bench -delete && \
    rm -rf /opt/llama-install/include /opt/llama-install/lib/cmake \
        /opt/llama-install/lib/pkgconfig && \
    c++ -O2 -std=c++17 -ffunction-sections -fdata-sections \
        -Itools/server -Itools/mtmd -Icommon -Isrc -Iinclude -Iggml/include -Ivendor \
        /opt/paracetamol/test-server-speculative-metrics.cpp \
        tools/server/server-common.cpp common/json.cpp \
        -L/opt/llama-install/lib -Wl,-rpath,/opt/llama-install/lib \
        -Wl,--gc-sections -lggml-base -pthread \
        -o /tmp/test-server-speculative-metrics && \
    LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-server-speculative-metrics

# PR 30220, frozen at 73647e8ade6fb4293d221c4b92aafb7e82976418.
# Only its two quality-guarantee comment lines are locally qualified; code and
# global cache-consecutivity guards remain exactly those of the pinned source.
# Keep this common-only correction after the reviewed native build: the patch
# fills draft logical-position holes, without changing target/GPU arithmetic.
# A private index prevents source checks from refreshing build-info's index
# dependency. Ordinary Ninja regeneration and install-time RPATH remain intact.
COPY applications/llama-cpp/dflash-mtmd-position-gap.patch \
    /opt/paracetamol/llama-dflash-mtmd-position-gap.patch
RUN printf '%s\n' \
        'c299f24df7c580ccbe013224dbba50c6adc64eaf513afff918c9f5d8cebce91d  /opt/paracetamol/llama-dflash-mtmd-position-gap.patch' \
        | sha256sum -c - && \
    export GIT_OPTIONAL_LOCKS=0 \
        GIT_INDEX_FILE=/tmp/paracetamol-dflash-position-gap.index && \
    test ! -e "$GIT_INDEX_FILE" && \
    git read-tree HEAD && \
    git apply --check /opt/paracetamol/llama-dflash-mtmd-position-gap.patch && \
    git apply /opt/paracetamol/llama-dflash-mtmd-position-gap.patch && \
    cmake --build build --config Release \
        --target llama-common llama-server llama-cli llama-bench && \
    cmake --install build --config Release && \
    find /opt/llama-install/bin -type f \
        ! -name llama-cli ! -name llama-server ! -name llama-bench -delete && \
    rm -rf /opt/llama-install/include /opt/llama-install/lib/cmake \
        /opt/llama-install/lib/pkgconfig && \
    LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-llama-native-policy && \
    LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-server-speculative-metrics

# PR 32137, frozen at 54253df818eb8941ede14647bf412988e02c8027.
# These are host-only layout changes. Apply them after the reviewed GPU build
# and require unchanged backend bytes, not a second set of kernel settings.
# Use a private index so source validation cannot refresh build-info's ordinary
# index dependency. A changed base or patch stack requires deliberate review.
# The shared reset hunk of PR 31913 is pinned at
# 927a48d7d17a2facbfbf20e09e969631cf814f6d. SCALE(0) cannot clear NaN/Inf.
# Characterize that actual old helper before applying the correction. The
# unrelated DeepSeek hunk and hybrid-excluding upstream test are not imported.
# Only commit 4161abc7209db6012fe2ab4badcaa49fa3e481ab of PR 32247 is used:
# score active sequences' pools while preserving every sequence's new-pool
# writes. New attention operations and NVIDIA row-list kernels are excluded.
COPY applications/llama-cpp/kpool-prefix-retention.patch \
    /opt/paracetamol/llama-kpool-prefix-retention.patch
COPY applications/llama-cpp/test-kpool-prefix-layout.cpp \
    applications/llama-cpp/extract-kpool-prefix-test.sh \
    applications/llama-cpp/build-kpool-prefix-test.sh /opt/paracetamol/
COPY applications/llama-cpp/recurrent-state-clear.patch \
    /opt/paracetamol/llama-recurrent-state-clear.patch
COPY applications/llama-cpp/test-recurrent-state-clear.cpp \
    applications/llama-cpp/build-recurrent-state-clear-test.sh /opt/paracetamol/
COPY applications/llama-cpp/qwen4exp-active-pools.patch \
    /opt/paracetamol/llama-qwen4exp-active-pools.patch
COPY applications/llama-cpp/test-qwen4exp-active-pool.cpp \
    applications/llama-cpp/build-active-pool-test.sh /opt/paracetamol/
RUN export GIT_OPTIONAL_LOCKS=0 \
        GIT_INDEX_FILE=/tmp/paracetamol-host-pool.index && \
    test ! -e "$GIT_INDEX_FILE" && \
    git read-tree HEAD && \
    git add --update -- . && \
    git add -- common include src ggml tools tests && \
    test "$(git write-tree)" = aeeb3f45e00deae964a7949da69f1389fa98176a && \
    printf '%s\n' \
        'dcd11fd53a7d608a628a6958aa7e89f0245826eec15fe557373953842fae0492  /opt/paracetamol/llama-kpool-prefix-retention.patch' \
        '0230801b15720da628329b02693715d658a69be45fc4d03df398bed405283a61  /opt/paracetamol/llama-recurrent-state-clear.patch' \
        '0e0819b2cfc8e65159b9a256342a3bb3df8057001aeeb845aeaaa2277ab39b56  /opt/paracetamol/llama-qwen4exp-active-pools.patch' \
        | sha256sum -c - && \
    ulimit -c 0 && \
    sha256sum build/bin/libggml.so build/bin/libggml-base.so \
        build/bin/libggml-cpu.so build/bin/libggml-hip.so build/bin/libggml-vulkan.so \
        > /tmp/host-pool-backends.sha256 && \
    sha256sum /opt/llama-install/lib/libggml.so /opt/llama-install/lib/libggml-base.so \
        /opt/llama-install/lib/libggml-cpu.so /opt/llama-install/lib/libggml-hip.so \
        /opt/llama-install/lib/libggml-vulkan.so \
        > /tmp/host-pool-installed-backends.sha256 && \
    bash /opt/paracetamol/build-recurrent-state-clear-test.sh \
        baseline /opt/llama.cpp /opt/llama-install/lib /tmp/recurrent-clear-baseline && \
    if ! LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/recurrent-clear-baseline/test-recurrent-state-clear --baseline-characterization \
        > /tmp/recurrent-clear-baseline/characterization.log 2>&1; then \
        cat /tmp/recurrent-clear-baseline/characterization.log >&2; exit 1; \
    fi && \
    grep -Fx 'PASS recurrent-state-clear graphs=6 poisoned-clears=2 empty-clears=2 extra-copy-rows=2 attention=unchanged mode=baseline-characterization' \
        /tmp/recurrent-clear-baseline/characterization.log && \
    if LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/recurrent-clear-baseline/test-recurrent-state-clear \
        > /tmp/recurrent-clear-baseline/rejected.log 2>&1; then \
        echo 'Unchanged reset incorrectly passed the candidate contract' >&2; exit 1; \
    fi && \
    if ! grep -Fx 'FAIL actual production build_rs clear operation' \
        /tmp/recurrent-clear-baseline/rejected.log; then \
        cat /tmp/recurrent-clear-baseline/rejected.log >&2; exit 1; \
    fi && \
    bash /opt/paracetamol/build-active-pool-test.sh \
        /opt/llama.cpp /opt/llama-install/lib /tmp/active-pool-baseline && \
    if ! LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/active-pool-baseline/test-qwen4exp-active-pool --baseline \
        > /tmp/active-pool-baseline/oracle.log 2>&1; then \
        cat /tmp/active-pool-baseline/oracle.log >&2; exit 1; \
    fi && \
    grep -Fx 'PASS actual production active-kpool API oracle cases=56 mode=baseline' \
        /tmp/active-pool-baseline/oracle.log && \
    if LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/active-pool-baseline/test-qwen4exp-active-pool \
        > /tmp/active-pool-baseline/rejected.log 2>&1; then \
        echo 'Unchanged pool scoring incorrectly passed the candidate contract' >&2; exit 1; \
    fi && \
    if ! grep -Fx 'FAIL active pool graph shape idle-second' \
        /tmp/active-pool-baseline/rejected.log; then \
        cat /tmp/active-pool-baseline/rejected.log >&2; exit 1; \
    fi && \
    git apply --check /opt/paracetamol/llama-kpool-prefix-retention.patch && \
    git apply /opt/paracetamol/llama-kpool-prefix-retention.patch && \
    git add -- common include src ggml tools tests && \
    test "$(git write-tree)" = 0e4534d7f989c402dafad53e09d816f32a9264af && \
    git apply --check /opt/paracetamol/llama-qwen4exp-active-pools.patch && \
    git apply /opt/paracetamol/llama-qwen4exp-active-pools.patch && \
    git add -- common include src ggml tools tests && \
    test "$(git write-tree)" = c2acb69ac63536cb67d14d3e14e1ca99bbb69adc && \
    git apply --check /opt/paracetamol/llama-recurrent-state-clear.patch && \
    git apply /opt/paracetamol/llama-recurrent-state-clear.patch && \
    git add -- common include src ggml tools tests && \
    test "$(git write-tree)" = 20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c && \
    bash /opt/paracetamol/build-kpool-prefix-test.sh \
        /opt/llama.cpp /tmp/kpool-prefix-test && \
    cmake --build build --config Release \
        --target llama-common llama-server llama-cli llama-bench && \
    sha256sum --check /tmp/host-pool-backends.sha256 && \
    cmake --install build --config Release && \
    sha256sum --check /tmp/host-pool-installed-backends.sha256 && \
    find /opt/llama-install/bin -type f \
        ! -name llama-cli ! -name llama-server ! -name llama-bench -delete && \
    rm -rf /opt/llama-install/include /opt/llama-install/lib/cmake \
        /opt/llama-install/lib/pkgconfig && \
    bash /opt/paracetamol/build-recurrent-state-clear-test.sh \
        candidate /opt/llama.cpp /opt/llama-install/lib /tmp/recurrent-clear-candidate && \
    if ! LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/recurrent-clear-candidate/test-recurrent-state-clear \
        > /tmp/recurrent-clear-candidate/oracle.log 2>&1; then \
        cat /tmp/recurrent-clear-candidate/oracle.log >&2; exit 1; \
    fi && \
    grep -Fx 'PASS recurrent-state-clear graphs=6 poisoned-clears=2 empty-clears=2 extra-copy-rows=2 attention=unchanged mode=candidate' \
        /tmp/recurrent-clear-candidate/oracle.log && \
    bash /opt/paracetamol/build-active-pool-test.sh \
        /opt/llama.cpp /opt/llama-install/lib /tmp/active-pool-candidate && \
    if ! LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/active-pool-candidate/test-qwen4exp-active-pool \
        > /tmp/active-pool-candidate/oracle.log 2>&1; then \
        cat /tmp/active-pool-candidate/oracle.log >&2; exit 1; \
    fi && \
    grep -Fx 'PASS actual production active-kpool API oracle cases=56 mode=candidate' \
        /tmp/active-pool-candidate/oracle.log && \
    if ! GGML_NO_BACKTRACE=1 LD_LIBRARY_PATH=/opt/llama-install/lib \
        /tmp/active-pool-candidate/test-qwen4exp-active-pool --guards \
        > /tmp/active-pool-candidate/guards.log 2>&1; then \
        cat /tmp/active-pool-candidate/guards.log >&2; exit 1; \
    fi && \
    for guard_case in 1 2 3 4; do \
        grep -Fx "PASS production assertion guard=$guard_case" /tmp/active-pool-candidate/guards.log \
            || { cat /tmp/active-pool-candidate/guards.log >&2; exit 1; }; \
    done && \
    test "$(grep -c '^PASS production assertion guard=' /tmp/active-pool-candidate/guards.log)" = 4 && \
    LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-llama-native-policy && \
    LD_LIBRARY_PATH=/opt/llama-install/lib /tmp/test-server-speculative-metrics && \
    python -m pip check

FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp

ARG DEBIAN_FRONTEND=noninteractive
ARG LLAMA_CPP_COMMIT
ARG ROCM_VERSION
ARG MESA_VULKAN_VERSION=26.0.8-1ubuntu0.3
ARG VULKAN_VERSION=1.4.341.0-1
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/paracetamol/pip

ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH} \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        libcurl4t64 \
        libgomp1 \
        "libvulkan1=${VULKAN_VERSION}" \
        "mesa-vulkan-drivers=${MESA_VULKAN_VERSION}" && \
    rm -rf /var/lib/apt/lists/*

COPY --from=llama-builder /opt/llama-install/ /usr/local/
COPY applications/llama-cpp/entrypoint.sh \
    /usr/local/bin/paracetamol-llama-entrypoint
COPY applications/llama-cpp/model-load-policy.sh \
    /usr/local/share/paracetamol/llama-model-load-policy.sh
RUN chmod 0644 /usr/local/share/paracetamol/llama-model-load-policy.sh
COPY applications/llama-cpp/chat-templates/ \
    /usr/local/share/paracetamol/llama-chat-templates/
RUN chmod 0755 /usr/local/bin/paracetamol-llama-entrypoint && \
    chmod 0444 \
        /usr/local/share/paracetamol/llama-chat-templates/*.jinja && \
    mkdir -p /data && \
    ldconfig

ENV HOME=/data/home \
    XDG_CACHE_HOME=/tmp \
    LLAMA_CACHE=/data/cache

LABEL org.opencontainers.image.title="Paracetamol llama.cpp" \
      org.opencontainers.image.description="Locally built llama.cpp server and CLI for AMD GPUs" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.revision="${LLAMA_CPP_COMMIT}" \
      io.github.fff7d1bc.paracetamol.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.paracetamol.llama-cpp.source-tree="20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c" \
      io.github.fff7d1bc.paracetamol.llama-cpp.patches="hip-apu-host-buffer,quantized-kv-flash-attention,qwen4exp-direct-reader,process-allocation-policy,hip-strix-halo-graph-recapture,hip-strix-halo-tiled-gdn,hip-qwen4exp-qsa-kernels,hip-qwen4exp-pipeline,hip-qwen4exp-graph-optimizations,hip-mmq-activation-sum-compat,server-stream-keepalive,topk-moe-boundaries,hip-qwen4exp-decode,server-speculative-metrics,dflash-mtmd-position-gap,kpool-prefix-retention,qwen4exp-active-pools,recurrent-state-clear" \
      io.github.fff7d1bc.paracetamol.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201"

WORKDIR /data
EXPOSE 8080
STOPSIGNAL SIGINT
ENTRYPOINT ["/usr/local/bin/paracetamol-llama-entrypoint"]
