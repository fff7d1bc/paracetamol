"""ROCmplete host defaults and validation."""

from __future__ import annotations

import ipaddress
import os
import re
import stat
import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Optional, Sequence, Tuple

from .hardware_profiles import GPU_PROFILES, PROFILES

from .errors import LauncherError


@dataclass(frozen=True)
class ApplicationSpec:
    identifier: str
    image: str
    container_name: str
    build_target: str
    port: Optional[int] = None
    shell: bool = True
    logs: bool = False
    shared_pytorch_base: bool = True
    multi_gpu: bool = False
    after_build: str = ""
    after_content: str = ""


ROCM_RUNTIME_IMAGE = (
    "localhost/rocmplete:runtime-ubuntu26.04-rocm7.14-r2"
)
ROCM_RUNTIME_BUILD_TARGET = "rocm-runtime"
ROCM_BASE_IMAGE = (
    "localhost/rocmplete:base-ubuntu26.04-rocm7.14-torch2.11-r5"
)
ROCM_BASE_BUILD_TARGET = "rocm-base"
CONTENT_TOOLS_IMAGE = (
    "localhost/rocmplete:content-ubuntu26.04-huggingface1.27-r1"
)
CONTENT_TOOLS_BUILD_TARGET = "content-tools"
LLAMA_BACKENDS = ("rocm", "vulkan")
DWARFSTAR_DEFAULT_CONTEXT = 131072
DWARFSTAR_DEFAULT_OUTPUT_TOKENS = 16000
DWARFSTAR_DEFAULT_MODEL_BUNDLE = (
    "dwarfstar-deepseek-v4-flash-0731-q2-imatrix"
)
DWARFSTAR_DSPARK_MODEL_BUNDLE = (
    "dwarfstar-deepseek-v4-flash-0731-q2-imatrix-dspark"
)
COMFY_BENCHMARK_CONTAINER_NAME = "rocmplete-benchmark"
LLAMA_BENCHMARK_CONTAINER_NAME = "rocmplete-llama-benchmark"
LLAMA_SPECULATIVE_BENCHMARK_CONTAINER_NAME = (
    "rocmplete-llama-speculative-benchmark"
)
TRANSIENT_CONTAINER_APPLICATIONS = {
    COMFY_BENCHMARK_CONTAINER_NAME: "comfyui",
    LLAMA_BENCHMARK_CONTAINER_NAME: "llama-cpp",
    LLAMA_SPECULATIVE_BENCHMARK_CONTAINER_NAME: "llama-cpp",
}


APPLICATIONS = {
    "comfyui": ApplicationSpec(
        identifier="comfyui",
        image="localhost/rocmplete:comfyui-ubuntu26.04-rocm7.14-0.28.0-r11",
        container_name="rocmplete-comfyui",
        build_target="comfyui",
        port=8188,
        logs=True,
        multi_gpu=True,
        after_build="./rocmplete content install comfyui",
        after_content="./rocmplete run comfyui",
    ),
    "llama-cpp": ApplicationSpec(
        identifier="llama-cpp",
        image=(
            "localhost/rocmplete:"
            "llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29"
        ),
        container_name="rocmplete-llama-cpp",
        build_target="llama-cpp",
        port=8080,
        logs=True,
        shared_pytorch_base=False,
        multi_gpu=True,
        after_build="./rocmplete content install llama-cpp qwen3.8",
        after_content=(
            "./rocmplete run llama-cpp server "
            "--preset qwen3.8-27b-mtp-ud-q8-k-xl"
        ),
    ),
    "dwarfstar": ApplicationSpec(
        identifier="dwarfstar",
        image=(
            "localhost/rocmplete:"
            "dwarfstar-ubuntu26.04-rocm7.14-84cc882-r7"
        ),
        container_name="rocmplete-dwarfstar",
        build_target="dwarfstar",
        port=8000,
        logs=True,
        shared_pytorch_base=False,
        multi_gpu=False,
        after_build=(
            "./rocmplete content install dwarfstar "
            "flash-0731-q2-imatrix"
        ),
        after_content="./rocmplete run dwarfstar server",
    ),
}

APPLICATION_NAMES = tuple(APPLICATIONS)
BUILD_APPLICATIONS = tuple(
    name for name, spec in APPLICATIONS.items() if spec.build_target
)
SHELL_APPLICATIONS = tuple(
    name for name, spec in APPLICATIONS.items() if spec.shell
)
LOG_APPLICATIONS = tuple(
    name for name, spec in APPLICATIONS.items() if spec.logs
)
WEB_APPLICATIONS = tuple(
    name for name, spec in APPLICATIONS.items() if spec.port is not None
)

DEFAULT_LISTEN = "127.0.0.1"
MEMORY_POLICIES = ("balanced", "conservative")
KERNEL_POLICIES = ("default", "experimental")

_MANAGED_COMFY_OPTIONS = {
    "--listen",
    "--port",
    "--base-directory",
    "--models-directory",
    "--input-directory",
    "--output-directory",
    "--temp-directory",
    "--user-directory",
    "--database-url",
    "--cpu",
}


def environment_value(
    environ: Mapping[str, str], name: str, default: Optional[str] = None
) -> Optional[str]:
    return environ.get("ROCMLETE_{}".format(name), default)


def config_file(environ: Optional[Mapping[str, str]] = None) -> Optional[Path]:
    """Return the optional XDG configuration file without creating it."""
    env = os.environ if environ is None else environ
    config_home = env.get("XDG_CONFIG_HOME")
    if config_home:
        root = Path(config_home)
        if not root.is_absolute():
            raise LauncherError("XDG_CONFIG_HOME must be an absolute path")
    else:
        home = env.get("HOME")
        if not home:
            return None
        root = Path(home) / ".config"
    return root / "rocmplete" / "config.toml"


def _configured_data_dir(
    environ: Optional[Mapping[str, str]] = None,
) -> Optional[Path]:
    path = config_file(environ)
    if path is None:
        return None
    try:
        status = path.stat()
    except FileNotFoundError:
        return None
    except OSError as error:
        raise LauncherError(
            "cannot inspect ROCmplete configuration {}: {}".format(
                path, error
            )
        )
    if not stat.S_ISREG(status.st_mode):
        raise LauncherError(
            "ROCmplete configuration is not a regular file: {}".format(path)
        )
    if status.st_size > 64 * 1024:
        raise LauncherError(
            "ROCmplete configuration is unexpectedly large: {}".format(path)
        )
    try:
        with path.open("rb") as handle:
            document = tomllib.load(handle)
    except (OSError, UnicodeError, tomllib.TOMLDecodeError) as error:
        raise LauncherError(
            "cannot read ROCmplete configuration {}: {}".format(path, error)
        )

    unknown_sections = sorted(set(document) - {"storage"})
    if unknown_sections:
        raise LauncherError(
            "unknown ROCmplete configuration section{} in {}: {}".format(
                "s" if len(unknown_sections) != 1 else "",
                path,
                ", ".join(unknown_sections),
            )
        )
    storage = document.get("storage", {})
    if not isinstance(storage, dict):
        raise LauncherError(
            "ROCmplete configuration [storage] must be a table: {}".format(
                path
            )
        )
    unknown_storage = sorted(set(storage) - {"data_dir"})
    if unknown_storage:
        raise LauncherError(
            "unknown ROCmplete [storage] setting{} in {}: {}".format(
                "s" if len(unknown_storage) != 1 else "",
                path,
                ", ".join(unknown_storage),
            )
        )
    value = storage.get("data_dir")
    if value is None:
        return None
    if not isinstance(value, str) or not value:
        raise LauncherError(
            "ROCmplete [storage].data_dir must be a non-empty string: {}".format(
                path
            )
        )
    configured = Path(value)
    if not configured.is_absolute():
        raise LauncherError(
            "ROCmplete [storage].data_dir must be an absolute path: {}".format(
                path
            )
        )
    return configured


def default_data_dir(environ: Optional[Mapping[str, str]] = None) -> Path:
    env = os.environ if environ is None else environ
    configured = _configured_data_dir(env)
    if configured is not None:
        return configured
    if env.get("XDG_DATA_HOME"):
        root = Path(env["XDG_DATA_HOME"])
    else:
        home = env.get("HOME")
        if not home:
            raise LauncherError("HOME is not set")
        root = Path(home) / ".local" / "share"
    if not root.is_absolute():
        raise LauncherError("XDG_DATA_HOME must be an absolute path")
    return root / "rocmplete"


def selected_data_dir(
    value: Optional[str] = None,
    environ: Optional[Mapping[str, str]] = None,
) -> Path:
    """Apply CLI, environment, configuration, and XDG precedence."""
    env = os.environ if environ is None else environ
    if value is not None:
        if not value:
            raise LauncherError("data directory must not be empty")
        return Path(value)
    environment = environment_value(env, "DATA_DIR")
    if environment:
        return Path(environment)
    return default_data_dir(env)


def validate_port(value: object) -> int:
    text = str(value)
    if not re.fullmatch(r"[0-9]+", text):
        raise LauncherError("port must be an integer")
    port = int(text)
    if not 1 <= port <= 65535:
        raise LauncherError("port must be between 1 and 65535")
    return port


def validate_listen_address(value: object) -> str:
    text = str(value)
    try:
        return str(ipaddress.ip_address(text))
    except ValueError:
        raise LauncherError(
            "listen address must be an IPv4 or IPv6 address: {!r}".format(
                text
            )
        )


def is_loopback_address(value: str) -> bool:
    return ipaddress.ip_address(value).is_loopback


def validate_profile(profile: str) -> str:
    if profile not in PROFILES:
        raise LauncherError(
            "profile must be one of {}".format(", ".join(PROFILES))
        )
    return profile


def validate_memory_policy(policy: str) -> str:
    if policy not in MEMORY_POLICIES:
        raise LauncherError("memory policy must be balanced or conservative")
    return policy


def validate_kernel_policy(policy: str) -> str:
    if policy not in KERNEL_POLICIES:
        raise LauncherError("kernel policy must be default or experimental")
    return policy


def reject_managed_comfy_args(arguments: Sequence[str]) -> None:
    for argument in arguments:
        option = argument.split("=", 1)[0]
        if option in _MANAGED_COMFY_OPTIONS:
            raise LauncherError(
                "ComfyUI argument {!r} is managed by the launcher".format(argument)
            )


def version_at_least(actual: str, minimum: str) -> bool:
    def numeric_version(value: str) -> Tuple[int, ...]:
        match = re.match(r"^[0-9]+(?:\.[0-9]+)*", value)
        if not match:
            return ()
        return tuple(int(part) for part in match.group(0).split("."))

    actual_parts = numeric_version(actual)
    minimum_parts = numeric_version(minimum)
    length = max(len(actual_parts), len(minimum_parts))
    return actual_parts + (0,) * (length - len(actual_parts)) >= (
        minimum_parts + (0,) * (length - len(minimum_parts))
    )
