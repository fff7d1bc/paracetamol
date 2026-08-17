# docker.io/library/ubuntu:resolute-20260724.1 (26.04), resolved 2026-08-17.
ARG ROCM_RUNTIME_IMAGE=localhost/rocmplete:runtime-ubuntu26.04-rocm7.14-r2
ARG ROCM_BASE_IMAGE=localhost/rocmplete:base-ubuntu26.04-rocm7.14-torch2.11-r5
ARG UBUNTU_IMAGE=docker.io/library/ubuntu@sha256:678c6550cc43645e08669028bc177f50be4e7c5b8cca677067b1914d4afc7a03
ARG ROCM_VERSION=7.14.0
ARG TORCH_VERSION=2.11.0
ARG TORCHVISION_VERSION=0.26.0
ARG TORCHAUDIO_VERSION=2.11.0
ARG LLAMA_CPP_COMMIT=3cb7ffb1a1f612d5e4a46244ae5a3c77ad934a70
ARG DWARFSTAR_COMMIT=84cc882352757baf628a1776badf7cc54d584e28

FROM ${UBUNTU_IMAGE} AS content-tools

ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/rocmplete/pip
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
    /opt/rocmplete/container_download.py
RUN chmod 0644 /opt/rocmplete/container_download.py

LABEL org.opencontainers.image.title="ROCmplete content tools" \
      org.opencontainers.image.description="Pinned download utilities for ROCmplete content"

FROM ${UBUNTU_IMAGE} AS rocm-runtime

ARG ROCM_VERSION
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/rocmplete/pip
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
        --index-url https://repo.amd.com/rocm/whl-multi-arch/ \
        "rocm[libraries,device-gfx1150,device-gfx1151,device-gfx1200,device-gfx1201]==${ROCM_VERSION}" && \
    python -m pip check

LABEL org.opencontainers.image.title="ROCmplete ROCm runtime" \
      org.opencontainers.image.description="Shared minimal ROCm runtime for locally built ROCmplete images" \
      io.github.fff7d1bc.rocmplete.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.rocmplete.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201"

FROM ${ROCM_RUNTIME_IMAGE} AS rocm-base

ARG ROCM_VERSION
ARG TORCH_VERSION
ARG TORCHVISION_VERSION
ARG TORCHAUDIO_VERSION
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/rocmplete/pip
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
        --index-url https://repo.amd.com/rocm/whl-multi-arch/ \
        "torch[device-gfx1150,device-gfx1151,device-gfx1200,device-gfx1201]==${TORCH_VERSION}+rocm${ROCM_VERSION}" \
        "torchvision[device-gfx1150,device-gfx1151,device-gfx1200,device-gfx1201]==${TORCHVISION_VERSION}+rocm${ROCM_VERSION}" \
        "torchaudio==${TORCHAUDIO_VERSION}+rocm${ROCM_VERSION}"

COPY containers/content_tools/download.py \
    /opt/rocmplete/container_download.py
RUN chmod 0644 /opt/rocmplete/container_download.py

FROM ${ROCM_BASE_IMAGE} AS comfyui

ARG ROCM_VERSION
ARG TORCH_VERSION
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/rocmplete/pip
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
    /opt/rocmplete/patch_comfyui_manager.py
RUN python /opt/rocmplete/patch_comfyui_manager.py && \
    python -m compileall -q \
        /opt/venv/lib/python3.14/site-packages/comfyui_manager

ARG COMFYUI_GGUF_COMMIT=6ea2651e7df66d7585f6ffee804b20e92fb38b8a
RUN python -m pip install \
        "gguf==0.19.0" \
        "protobuf==7.35.1" && \
    mkdir -p /opt/rocmplete/custom_nodes && \
    git init /opt/rocmplete/custom_nodes/ComfyUI-GGUF && \
    cd /opt/rocmplete/custom_nodes/ComfyUI-GGUF && \
    git remote add origin https://github.com/city96/ComfyUI-GGUF.git && \
    git fetch --depth=1 origin "${COMFYUI_GGUF_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${COMFYUI_GGUF_COMMIT}" && \
    rm -rf .git && \
    python -m pip check

ARG RGTHREE_COMMIT=6b76ee6f2c5a007710b5a16f97c94330d6ecc871
RUN git init /opt/rocmplete/custom_nodes/rgthree-comfy && \
    cd /opt/rocmplete/custom_nodes/rgthree-comfy && \
    git remote add origin https://github.com/rgthree/rgthree-comfy.git && \
    git fetch --depth=1 origin "${RGTHREE_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${RGTHREE_COMMIT}" && \
    test -f requirements.txt && \
    test ! -s requirements.txt && \
    python -c \
        'import tomllib; assert tomllib.load(open("pyproject.toml", "rb"))["project"]["dependencies"] == []' && \
    mkdir -p /usr/share/licenses/rocmplete/rgthree-comfy && \
    cp LICENSE /usr/share/licenses/rocmplete/rgthree-comfy/LICENSE && \
    rm -rf .git

COPY applications/comfyui/entrypoint.sh /usr/local/bin/rocmplete-entrypoint
COPY containers/common/profile.py /opt/rocmplete/container_profile.py
COPY src/rocmplete/__init__.py src/rocmplete/hardware_profiles.py \
    /opt/rocmplete/rocmplete/
COPY applications/comfyui/extra-model-paths.yaml \
    /opt/rocmplete/extra_model_paths.yaml
RUN chmod 0755 /usr/local/bin/rocmplete-entrypoint && \
    chmod 0644 \
        /opt/rocmplete/container_profile.py \
        /opt/rocmplete/rocmplete/__init__.py \
        /opt/rocmplete/rocmplete/hardware_profiles.py \
        /opt/rocmplete/patch_comfyui_manager.py \
        /opt/rocmplete/extra_model_paths.yaml && \
    mkdir -p /data /tmp/comfy && \
    chmod 1777 /tmp/comfy

LABEL org.opencontainers.image.title="ROCmplete local ROCm application image" \
      org.opencontainers.image.description="Locally built ROCm application image for AMD Strix Point, Strix Halo, and RDNA 4 Radeon GPUs" \
      org.opencontainers.image.licenses="BSD-3-Clause AND GPL-3.0-only AND Apache-2.0 AND MIT" \
      org.opencontainers.image.version="${COMFYUI_VERSION}" \
      org.opencontainers.image.revision="${COMFYUI_COMMIT}" \
      io.github.fff7d1bc.rocmplete.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.rocmplete.pytorch.version="${TORCH_VERSION}" \
      io.github.fff7d1bc.rocmplete.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201" \
      io.github.fff7d1bc.rocmplete.comfyui-gguf.revision="${COMFYUI_GGUF_COMMIT}" \
      io.github.fff7d1bc.rocmplete.comfyui-gguf.license="Apache-2.0" \
      io.github.fff7d1bc.rocmplete.rgthree-comfy.revision="${RGTHREE_COMMIT}" \
      io.github.fff7d1bc.rocmplete.rgthree-comfy.license="MIT"

WORKDIR /opt/ComfyUI
EXPOSE 8188
STOPSIGNAL SIGINT
ENTRYPOINT ["/usr/local/bin/rocmplete-entrypoint"]

# Native applications remain independent of the PyTorch image while sharing
# one minimal ROCm runtime and one exact build-only ROCm development layer.
FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk

ARG DEBIAN_FRONTEND=noninteractive
ARG ROCM_VERSION
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/rocmplete/pip

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
        --index-url https://repo.amd.com/rocm/whl-multi-arch/ \
        "rocm[devel]==${ROCM_VERSION}" && \
    python -m pip check && \
    rocm-sdk init && \
    test -x "$(rocm-sdk path --bin)/hipcc" && \
    test -d "$(rocm-sdk path --cmake)"

FROM native-rocm-sdk AS llama-rocm-sdk

ARG DEBIAN_FRONTEND=noninteractive
ARG GLSLC_ROCM714_VERSION=2026.1-1
ARG VULKAN_ROCM714_VERSION=1.4.341.0-1
ARG SPIRV_HEADERS_ROCM714_VERSION=1.6.1+1.4.341.0-1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        cmake \
        curl \
        libcurl4-openssl-dev \
        libomp-dev \
        "libvulkan-dev=${VULKAN_ROCM714_VERSION}" \
        ninja-build \
        "glslc=${GLSLC_ROCM714_VERSION}" \
        "spirv-headers=${SPIRV_HEADERS_ROCM714_VERSION}" && \
    rm -rf /var/lib/apt/lists/*

FROM llama-rocm-sdk AS llama-builder

ARG LLAMA_CPP_COMMIT

WORKDIR /opt/llama.cpp
COPY applications/llama-cpp/hip-apu-host-buffer.patch \
    /opt/rocmplete/llama-hip-apu-host-buffer.patch
COPY applications/llama-cpp/reasoning-controls.patch \
    /opt/rocmplete/llama-reasoning-controls.patch
COPY applications/llama-cpp/quantized-kv-flash-attention.patch \
    /opt/rocmplete/llama-quantized-kv-flash-attention.patch
COPY applications/llama-cpp/vulkan-f16-kv-contiguize.patch \
    /opt/rocmplete/llama-vulkan-f16-kv-contiguize.patch
RUN git init . && \
    git remote add origin https://github.com/ggml-org/llama.cpp.git && \
    git fetch --depth=1 origin "${LLAMA_CPP_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${LLAMA_CPP_COMMIT}" && \
    git submodule update --init --recursive --depth=1 && \
    git apply --check /opt/rocmplete/llama-hip-apu-host-buffer.patch && \
    git apply /opt/rocmplete/llama-hip-apu-host-buffer.patch && \
    git apply --check /opt/rocmplete/llama-reasoning-controls.patch && \
    git apply /opt/rocmplete/llama-reasoning-controls.patch && \
    git apply --check /opt/rocmplete/llama-quantized-kv-flash-attention.patch && \
    git apply /opt/rocmplete/llama-quantized-kv-flash-attention.patch && \
    git apply --check /opt/rocmplete/llama-vulkan-f16-kv-contiguize.patch && \
    git apply /opt/rocmplete/llama-vulkan-f16-kv-contiguize.patch && \
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
    mkdir -p /opt/llama-install/share/licenses/rocmplete/llama-cpp && \
    cp LICENSE \
        /opt/llama-install/share/licenses/rocmplete/llama-cpp/LICENSE

FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp

ARG DEBIAN_FRONTEND=noninteractive
ARG LLAMA_CPP_COMMIT
ARG ROCM_VERSION
ARG MESA_VULKAN_ROCM714_VERSION=26.0.3-1ubuntu1
ARG VULKAN_ROCM714_VERSION=1.4.341.0-1
ARG PIP_NO_CACHE_DIR=true
ARG PIP_CACHE_DIR=/var/cache/rocmplete/pip

ENV VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:${PATH} \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        libcurl4t64 \
        libgomp1 \
        "libvulkan1=${VULKAN_ROCM714_VERSION}" \
        "mesa-vulkan-drivers=${MESA_VULKAN_ROCM714_VERSION}" && \
    rm -rf /var/lib/apt/lists/*

COPY --from=llama-builder /opt/llama-install/ /usr/local/
COPY applications/llama-cpp/entrypoint.sh \
    /usr/local/bin/rocmplete-llama-entrypoint
COPY applications/llama-cpp/chat-templates/ \
    /usr/local/share/rocmplete/llama-chat-templates/
RUN chmod 0755 /usr/local/bin/rocmplete-llama-entrypoint && \
    chmod 0444 \
        /usr/local/share/rocmplete/llama-chat-templates/*.jinja && \
    mkdir -p /data && \
    ldconfig

ENV HOME=/data/home \
    XDG_CACHE_HOME=/tmp \
    LLAMA_CACHE=/data/cache

LABEL org.opencontainers.image.title="ROCmplete llama.cpp" \
      org.opencontainers.image.description="Locally built llama.cpp server and CLI for AMD GPUs" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.revision="${LLAMA_CPP_COMMIT}" \
      io.github.fff7d1bc.rocmplete.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.rocmplete.llama-cpp.patches="hip-apu-host-buffer,reasoning-controls,quantized-kv-flash-attention,vulkan-f16-kv-contiguize" \
      io.github.fff7d1bc.rocmplete.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201"

WORKDIR /data
EXPOSE 8080
STOPSIGNAL SIGINT
ENTRYPOINT ["/usr/local/bin/rocmplete-llama-entrypoint"]

# DwarfStar is deliberately narrower than upstream's complete build surface:
# one locally compiled multi-architecture ROCm engine, with only its CLI, HTTP
# server, and benchmark binary retained. Model acquisition remains entirely
# outside the image and goes through ROCmplete's verified content catalog.
FROM native-rocm-sdk AS dwarfstar-builder

ARG DWARFSTAR_COMMIT

COPY applications/dwarfstar/multiarch-wmma-fallback.patch \
    /opt/rocmplete/dwarfstar-multiarch-wmma-fallback.patch
WORKDIR /opt/dwarfstar
RUN git init . && \
    git remote add origin https://github.com/antirez/ds4.git && \
    git fetch --depth=1 origin "${DWARFSTAR_COMMIT}" && \
    git checkout --detach FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = "${DWARFSTAR_COMMIT}" && \
    git apply --check \
        /opt/rocmplete/dwarfstar-multiarch-wmma-fallback.patch && \
    git apply /opt/rocmplete/dwarfstar-multiarch-wmma-fallback.patch && \
    site_packages="$(python -c \
        'import sysconfig; print(sysconfig.get_paths()["purelib"])')" && \
    runtime_rpath="${site_packages}/_rocm_sdk_core/lib:\
${site_packages}/_rocm_sdk_libraries/lib" && \
    make -j"$(nproc)" strix-halo \
        NATIVE_CPU_FLAG=-march=x86-64-v3 \
        ROCM_CFLAGS='-O3 -ffast-math -fno-finite-math-only -fPIE -pthread -D__HIP_PLATFORM_AMD__ -Wno-unused-command-line-argument --offload-jobs=jobserver --offload-arch=gfx1150 --offload-arch=gfx1151 --offload-arch=gfx1200 --offload-arch=gfx1201' \
        ROCM_LDLIBS="-lm -pthread -lhipblas -lhipblaslt -lamdhip64 \
-Wl,-rpath,${runtime_rpath}" && \
    ./ds4 --help >/dev/null && \
    ./ds4-server --help >/dev/null && \
    ./ds4-bench --help >/dev/null && \
    mkdir -p \
        /opt/dwarfstar-install/bin \
        /opt/dwarfstar-install/share/licenses/rocmplete/dwarfstar && \
    install -m 0755 ds4 ds4-server ds4-bench \
        /opt/dwarfstar-install/bin/ && \
    install -m 0444 LICENSE \
        /opt/dwarfstar-install/share/licenses/rocmplete/dwarfstar/LICENSE && \
    ldd /opt/dwarfstar-install/bin/ds4-server | \
        tee /tmp/dwarfstar-ldd.txt && \
    ! grep -q 'not found' /tmp/dwarfstar-ldd.txt

FROM ${ROCM_RUNTIME_IMAGE} AS dwarfstar

ARG DWARFSTAR_COMMIT
ARG ROCM_VERSION

COPY --from=dwarfstar-builder /opt/dwarfstar-install/ /usr/local/
COPY applications/dwarfstar/entrypoint.sh \
    /usr/local/bin/rocmplete-dwarfstar-entrypoint
RUN chmod 0755 /usr/local/bin/rocmplete-dwarfstar-entrypoint

ENV HOME=/data/home \
    XDG_CACHE_HOME=/tmp

LABEL org.opencontainers.image.title="ROCmplete DwarfStar" \
      org.opencontainers.image.description="Locally built DwarfStar server and CLI for DeepSeek V4 Flash on supported AMD GPUs" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.revision="${DWARFSTAR_COMMIT}" \
      io.github.fff7d1bc.rocmplete.rocm.version="${ROCM_VERSION}" \
      io.github.fff7d1bc.rocmplete.gpu.targets="gfx1150,gfx1151,gfx1200,gfx1201"

WORKDIR /data
EXPOSE 8000
STOPSIGNAL SIGINT
ENTRYPOINT ["/usr/local/bin/rocmplete-dwarfstar-entrypoint"]
