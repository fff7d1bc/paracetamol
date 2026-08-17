"""Runtime command construction for llama.cpp services and benchmarks."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import List, Mapping, Optional, Sequence

from .. import podman
from ..config import (
    APPLICATIONS,
    GPU_PROFILES,
    LLAMA_BENCHMARK_CONTAINER_NAME,
)
from ..layout import StorageLayout
from .common import (
    container_listen_address,
    gpu_device_arguments,
    publication_network_arguments,
    published_port,
    read_only_shared_suffix,
)


@dataclass(frozen=True)
class LlamaOptions:
    image: str
    profile: str
    mode: str
    data_dir: Path
    backend: str = "rocm"
    source_revision: str = ""
    model: Optional[Path] = None
    managed_model: str = ""
    managed_draft: str = ""
    speculative_type: str = ""
    draft_tokens: int = 0
    context_override_architectures: Sequence[str] = field(
        default_factory=tuple
    )
    jinja: bool = False
    reasoning_preserve: bool = False
    chat_template: str = ""
    sampling_defaults: str = ""
    profile_flash_attention: Mapping[str, str] = field(default_factory=dict)
    profile_kv_cache: Mapping[str, str] = field(default_factory=dict)
    router_preset: Optional[Path] = None
    models_max: int = 2
    render_nodes: Sequence[str] = field(default_factory=tuple)
    listen: str = "127.0.0.1"
    port: int = 8080
    context: int = 0
    prompt: Optional[str] = None
    api_key_file: Optional[Path] = None
    detach: bool = False
    interactive: bool = False
    unconfined: bool = False
    container_name: str = ""
    container_role: str = "application"
    auto_remove: bool = True


@dataclass(frozen=True)
class LlamaBenchmarkOptions:
    image: str
    profile: str
    data_dir: Path
    backend: str = "rocm"
    model: Optional[Path] = None
    managed_model: str = ""
    render_nodes: Sequence[str] = field(default_factory=tuple)
    repetitions: int = 5
    prompt_tokens: int = 512
    generation_tokens: int = 128
    context_depth: int = 0
    batch_size: int = 2048
    ubatch_size: int = 512
    cache_type_k: str = "f16"
    cache_type_v: str = "f16"
    flash_attention: str = "auto"
    unconfined: bool = False


def llama_command(options: LlamaOptions, volume_suffix: str) -> List[str]:
    layout = StorageLayout(options.data_dir)
    read_only_suffix = read_only_shared_suffix(volume_suffix)
    container_name = (
        options.container_name
        or APPLICATIONS["llama-cpp"].container_name
    )
    if options.model is not None:
        container_model = "/content/models/{}".format(options.model.name)
        model_root = options.model.parent
    elif options.managed_model:
        container_model = "/content/models/{}".format(options.managed_model)
        model_root = layout.llama_models
    else:
        container_model = ""
        model_root = layout.llama_models
    command = ["podman", "run"]
    if options.auto_remove:
        command.append("--rm")
    command.extend([
        "--userns", "keep-id",
        "--umask", podman.current_umask(), "--name",
        container_name,
        *podman.managed_container_arguments(
            application="llama-cpp", role=options.container_role
        ),
        "--read-only", "--cap-drop", "all",
        "--security-opt", "no-new-privileges",
        "--pids-limit", "2048", "--ulimit", "core=0:0",
        "--shm-size", "8g",
        "--tmpfs", "/tmp:rw,nosuid,nodev,size=1g",
        "--volume", "{}:/data{}".format(
            layout.application("llama-cpp"), volume_suffix
        ),
        "--volume", "{}:/content/models{}".format(
            model_root, read_only_suffix
        ),
        "--env", "ROCMLETE_PROFILE={}".format(options.profile),
        "--env", "ROCMLETE_LLAMA_BACKEND={}".format(options.backend),
        "--env", "ROCMLETE_SOURCE_REVISION={}".format(
            options.source_revision
        ),
        "--env", "ROCMLETE_LLAMA_MODE={}".format(options.mode),
        "--env", "ROCMLETE_LLAMA_MODEL={}".format(container_model),
        "--env", "ROCMLETE_LLAMA_DRAFT_MODEL={}".format(
            "/content/models/{}".format(options.managed_draft)
            if options.managed_draft
            else ""
        ),
        "--env", "ROCMLETE_LLAMA_SPECULATIVE_TYPE={}".format(
            options.speculative_type
        ),
        "--env", "ROCMLETE_LLAMA_DRAFT_TOKENS={}".format(
            options.draft_tokens
        ),
        "--env", "ROCMLETE_LLAMA_CONTEXT_OVERRIDE={}".format(
            ",".join(
                "{}.context_length=int:{}".format(
                    architecture, options.context
                )
                for architecture in options.context_override_architectures
            )
        ),
        "--env", "ROCMLETE_LLAMA_JINJA={}".format(
            "1" if options.jinja else "0"
        ),
        "--env", "ROCMLETE_LLAMA_REASONING_PRESERVE={}".format(
            "1" if options.reasoning_preserve else "0"
        ),
        "--env", "ROCMLETE_LLAMA_CHAT_TEMPLATE={}".format(
            options.chat_template
        ),
        "--env", "ROCMLETE_LLAMA_SAMPLING_DEFAULTS={}".format(
            options.sampling_defaults
        ),
        "--env", "ROCMLETE_LISTEN={}".format(
            container_listen_address(options.listen)
        ),
        "--env", "ROCMLETE_HOST_LISTEN={}".format(options.listen),
        "--env", "ROCMLETE_PORT={}".format(options.port),
        "--env", "ROCMLETE_GPU_COUNT={}".format(len(options.render_nodes)),
        "--env", "ROCMLETE_RENDER_NODES={}".format(
            ",".join(options.render_nodes)
        ),
    ])
    for profile in GPU_PROFILES:
        command.extend(
            [
                "--env",
                "ROCMLETE_LLAMA_FLASH_ATTN_{}={}".format(
                    profile.upper().replace("-", "_"),
                    options.profile_flash_attention.get(profile, ""),
                ),
            ]
        )
        command.extend(
            [
                "--env",
                "ROCMLETE_LLAMA_KV_CACHE_{}={}".format(
                    profile.upper().replace("-", "_"),
                    options.profile_kv_cache.get(profile, ""),
                ),
            ]
        )
    if options.mode != "server":
        command.extend(["--network", "none"])
    if options.router_preset is not None:
        command.extend(
            [
                "--volume",
                "{}:/run/rocmplete/models.ini{}".format(
                    options.router_preset, read_only_suffix
                ),
                "--env",
                "ROCMLETE_LLAMA_ROUTER=1",
                "--env",
                "ROCMLETE_LLAMA_MODELS_MAX={}".format(options.models_max),
            ]
        )
    if options.mode == "server":
        command.extend(publication_network_arguments(options.listen))
        command.extend(
            ["--publish", published_port(options.listen, options.port)]
        )
    if options.profile != "cpu":
        command.extend(gpu_device_arguments(options.render_nodes))
    if options.api_key_file is not None:
        command.extend(
            [
                "--volume",
                "{}:/run/secrets/llama-api-key{}".format(
                    options.api_key_file, read_only_suffix
                ),
            ]
        )
    if options.unconfined:
        command.extend(["--security-opt", "seccomp=unconfined"])
    if options.detach:
        command.append("--detach")
    if options.interactive:
        command.extend(["--interactive", "--tty"])
    command.append(options.image)
    if options.context:
        command.extend(["--ctx-size", str(options.context)])
    if options.api_key_file is not None:
        command.extend(["--api-key-file", "/run/secrets/llama-api-key"])
    if options.prompt is not None:
        # A predefined prompt is the non-interactive CLI path. Without
        # --single-turn llama-cli answers once and then keeps reading stdin;
        # EOF over SSH can make it spin through empty prompts indefinitely.
        command.extend(["--prompt", options.prompt, "--single-turn"])
    return command


def llama_benchmark_command(
    options: LlamaBenchmarkOptions, volume_suffix: str
) -> List[str]:
    layout = StorageLayout(options.data_dir)
    read_only_suffix = read_only_shared_suffix(volume_suffix)
    if options.model is not None:
        container_model = "/content/models/{}".format(options.model.name)
        model_root = options.model.parent
    else:
        container_model = "/content/models/{}".format(options.managed_model)
        model_root = layout.llama_models
    command = [
        "podman", "run", "--rm", "--userns", "keep-id",
        "--umask", podman.current_umask(),
        "--name", LLAMA_BENCHMARK_CONTAINER_NAME,
        *podman.managed_container_arguments(
            application="llama-cpp", role="benchmark"
        ),
        "--network", "none", "--read-only", "--cap-drop", "all",
        "--security-opt", "no-new-privileges",
        "--pids-limit", "2048", "--ulimit", "core=0:0",
        "--shm-size", "8g",
        "--tmpfs", "/tmp:rw,nosuid,nodev,size=1g",
        "--volume", "{}:/content/models{}".format(
            model_root, read_only_suffix
        ),
        "--env", "HOME=/tmp",
        "--env", "ROCMLETE_PROFILE={}".format(options.profile),
        "--env", "ROCMLETE_LLAMA_BACKEND={}".format(options.backend),
        "--env", "ROCMLETE_LLAMA_MODE=bench",
        "--env", "ROCMLETE_LLAMA_MODEL={}".format(container_model),
        "--env", "ROCMLETE_GPU_COUNT={}".format(len(options.render_nodes)),
    ]
    if options.profile != "cpu":
        command.extend(gpu_device_arguments(options.render_nodes))
    if options.unconfined:
        command.extend(["--security-opt", "seccomp=unconfined"])
    command.extend(
        [
            options.image,
            "--repetitions", str(options.repetitions),
            "--n-prompt", str(options.prompt_tokens),
            "--n-gen", str(options.generation_tokens),
            "--n-depth", str(options.context_depth),
            "--batch-size", str(options.batch_size),
            "--ubatch-size", str(options.ubatch_size),
            "--cache-type-k", options.cache_type_k,
            "--cache-type-v", options.cache_type_v,
            "--flash-attn", options.flash_attention,
            "--output", "json",
            "--progress",
        ]
    )
    return command
