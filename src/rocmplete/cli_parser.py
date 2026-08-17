"""ROCmplete command-line parser and copyable usage examples."""

from __future__ import annotations

import argparse
from pathlib import Path
from typing import List, Sequence, Tuple

from . import __version__
from .config import (
    APPLICATION_NAMES,
    BUILD_APPLICATIONS,
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
    GPU_PROFILES,
    LLAMA_BACKENDS,
    LOG_APPLICATIONS,
    PROFILES,
    SHELL_APPLICATIONS,
)
from .hardware_profiles import SUPPORTED_ARCHITECTURES
from .llama_speculative_benchmark import (
    DEFAULT_GENERATION_TOKENS as SPECULATIVE_DEFAULT_GENERATION_TOKENS,
    DEFAULT_REPETITIONS as SPECULATIVE_DEFAULT_REPETITIONS,
    DEFAULT_SEED as SPECULATIVE_DEFAULT_SEED,
    DEFAULT_SERVER_CONTEXT as SPECULATIVE_DEFAULT_SERVER_CONTEXT,
)
from .recipes import APPLICATION_RECIPES
from .remote_import import IMPORT_KINDS

# Public recipes are application-first and resolve to exact catalog bundles.
# Broad groups remain available only through explicit family or aggregate
# targets; they are not beginner recommendations.
CONTENT_APPLICATIONS = (
    ("comfyui", "ComfyUI"),
    ("llama-cpp", "llama.cpp"),
    ("dwarfstar", "DwarfStar"),
)
CONTENT_APPLICATION_RECIPES = APPLICATION_RECIPES
CONTENT_FAMILIES = (
    ("qwen", "Qwen image content for ComfyUI"),
    ("wan", "Wan video content for ComfyUI"),
)
EXACT_BUNDLE_CATEGORIES = (
    ("comfyui-images", "ComfyUI — image models"),
    ("comfyui-videos", "ComfyUI — video models"),
    ("comfyui-addons", "ComfyUI — workflows and add-ons"),
    ("llama-cpp", "llama.cpp"),
    ("dwarfstar", "DwarfStar"),
)
CONTENT_EXAMPLES = """\
Try one of these:

  Browse content by consuming application:
    ./rocmplete content list

  Browse every exact advanced bundle:
    ./rocmplete content list --bundles

  Preview downloads without installing:
    ./rocmplete content install comfyui image --dry-run

  Install an application recipe:
    ./rocmplete content install comfyui edit

  Install English/Japanese translation:
    ./rocmplete content install llama-cpp translation-gemma --accept-license

  Install every managed llama.cpp model:
    ./rocmplete content install llama-cpp all

  Install the DeepSeek V4 Flash 0731 Q2 imatrix model for DwarfStar:
    ./rocmplete content install dwarfstar flash-0731-q2-imatrix

  Install a ComfyUI model family:
    ./rocmplete content install family qwen

  Install local content packs:
    ./rocmplete content install --from-file local-content/models.json

  Import one remote file interactively:
    ./rocmplete content import

  Import directly from a provider URL:
    ./rocmplete content import https://huggingface.co/OWNER/REPOSITORY

  Reuse verified files from an old data directory:
    ./rocmplete content install all --local-mirror /path/to/old-rocmplete

  Inspect installed content:
    ./rocmplete content status comfyui image

  List managed runnable models and discovered local llama.cpp GGUFs:
    ./rocmplete content list --models

  Show managed context, template, MTP, and Flash Attention policy:
    ./rocmplete content list --models --details

  Also scan an external model directory:
    ./rocmplete content list --models --scan /path/to/ggufs
"""
GUIDE_EXAMPLES = """\
Read one focused application walkthrough:

  ./rocmplete guide comfyui
  ./rocmplete guide llama-cpp
  ./rocmplete guide dwarfstar

Run './rocmplete guide' to see the application index.
"""
LIFECYCLE_EXAMPLES = """\
Typical lifecycle:

  ./rocmplete guide APPLICATION
  ./rocmplete build APPLICATION
  ./rocmplete content install APPLICATION RECIPE
  ./rocmplete run APPLICATION

Inspect the current machine and application state:

  ./rocmplete status
"""
BUILD_EXAMPLES = """\
Try one of these:

  ./rocmplete build all
  ./rocmplete build base
  ./rocmplete build content-tools
  ./rocmplete build comfyui
  ./rocmplete build comfyui --no-layer-cache
  ./rocmplete build llama-cpp
  ./rocmplete build dwarfstar
"""
BUILD_TARGETS = ("base", "content-tools") + BUILD_APPLICATIONS
BUILD_TARGET_DESCRIPTIONS = {
    "all": "all application images",
    "base": "shared ROCm runtime and PyTorch diagnostic base",
    "content-tools": "shared verified content download tools",
    "comfyui": "ComfyUI web interface",
    "llama-cpp": "llama.cpp server and interactive CLI",
    "dwarfstar": "DeepSeek V4 Flash server and interactive CLI",
}
IMAGE_EXAMPLES = """\
Move locally built ROCmplete images between machines:

  ./rocmplete images export all --output /backup/rocmplete-images.tar
  ./rocmplete images export comfyui --output /backup/comfyui-images.tar
  ./rocmplete images import /backup/rocmplete-images.tar --dry-run
  ./rocmplete images import /backup/rocmplete-images.tar
"""
AGENT_EXAMPLES = """\
Run a supported coding agent against managed local models:

  ./rocmplete agent opencode
  ./rocmplete agent pi
  ./rocmplete agent omp
  ./rocmplete agent maki

The PATH-friendly launchers in bin/ provide the same guarded defaults without
the ROCmplete command prefix.
"""
OPENCODE_EXAMPLES = """\
Run OpenCode with the current ROCmplete model catalog:

  export PATH="$PWD/bin:$PATH"
  ./rocmplete run llama-cpp server --router --models-max 1
  opencode

The PATH launcher uses bubblewrap by default. To troubleshoot without it:

  ./rocmplete agent opencode --no-sandbox --

Forward normal OpenCode arguments through the launcher:

  opencode -m rocmplete/qwen3.8-27b-mtp-ud-q8-k-xl

Use a separately running DwarfStar server:

  ./rocmplete run dwarfstar server
  opencode -m dwarfstar/deepseek-v4-flash-0731-q2-imatrix

For a router on another local port:

  ROCMLETE_OPENCODE_PORT=9090 opencode

For DwarfStar on another local port:

  ROCMLETE_OPENCODE_DWARFSTAR_PORT=8001 opencode
"""
PI_EXAMPLES = """\
Run Pi with the current ROCmplete model catalog:

  export PATH="$PWD/bin:$PATH"
  ./rocmplete run llama-cpp server --router --models-max 1
  pi

The PATH launcher uses bubblewrap by default. To troubleshoot without it:

  ./rocmplete agent pi --no-sandbox --

Forward normal Pi arguments through the launcher:

  pi --model qwen3.8-27b-mtp-ud-q8-k-xl --thinking medium

Use a separately running DwarfStar server:

  ./rocmplete run dwarfstar server
  pi --provider dwarfstar --model deepseek-v4-flash-0731-q2-imatrix --thinking high

For a router on another local port:

  ROCMLETE_PI_PORT=9090 pi

For DwarfStar on another local port:

  ROCMLETE_PI_DWARFSTAR_PORT=8001 pi
"""
OMP_EXAMPLES = """\
Run Oh My Pi with the current ROCmplete model catalog:

  export PATH="$PWD/bin:$PATH"
  ./rocmplete run llama-cpp server --router --models-max 1
  omp

The PATH launcher uses bubblewrap by default. To troubleshoot without it:

  ./rocmplete agent omp --no-sandbox --

Forward normal OMP arguments through the launcher:

  omp --model rocmplete-llama-cpp/qwen3.8-27b-mtp-ud-q8-k-xl --thinking medium

Use a separately running DwarfStar server:

  ./rocmplete run dwarfstar server
  omp --model rocmplete-dwarfstar/deepseek-v4-flash-0731-q2-imatrix --thinking high

For servers on other local ports, set ROCMLETE_OMP_PORT or
ROCMLETE_OMP_DWARFSTAR_PORT.
"""
MAKI_EXAMPLES = """\
Run Maki with the current ROCmplete model catalog:

  export PATH="$PWD/bin:$PATH"
  ./rocmplete run llama-cpp server --router --models-max 1
  maki

The PATH launcher uses bubblewrap by default. To troubleshoot without it:

  ./rocmplete agent maki --no-sandbox --

Forward normal Maki arguments through the launcher:

  maki -m rocmplete/qwen3.8-27b-mtp-ud-q8-k-xl

Use a separately running DwarfStar server:

  ./rocmplete run dwarfstar server
  maki -m dwarfstar/deepseek-v4-flash-0731-q2-imatrix

For servers on other local ports, set ROCMLETE_MAKI_PORT or
ROCMLETE_MAKI_DWARFSTAR_PORT.
"""
SHELL_EXAMPLES = """\
Try one of these:

  ./rocmplete shell comfyui
  ./rocmplete shell llama-cpp
  ./rocmplete shell dwarfstar
"""
LOG_EXAMPLES = """\
Try one of these:

  ./rocmplete logs comfyui
  ./rocmplete logs llama-cpp --follow
  ./rocmplete logs dwarfstar --follow
  ./rocmplete logs comfyui --all
"""
STATUS_EXAMPLES = """\
Show the normal local dashboard:

  ./rocmplete status

Print a shareable report for a running direct llama.cpp server:

  ./rocmplete status llama-cpp

Select one model from a running router:

  ./rocmplete status llama-cpp --model qwen3.8-27b-mtp-ud-q8-k-xl
"""
STOP_EXAMPLES = """\
Try one of these:

  ./rocmplete stop comfyui
  ./rocmplete stop llama-cpp
  ./rocmplete stop dwarfstar
  ./rocmplete stop all
"""
CLEANUP_EXAMPLES = """\
Choose one explicit cleanup scope:

  ./rocmplete cleanup containers
  ./rocmplete cleanup build-cache
  ./rocmplete cleanup caches
  ./rocmplete cleanup downloads
  ./rocmplete cleanup images
  ./rocmplete cleanup data

Every non-empty cleanup plan requires confirmation. For unattended use:

  ./rocmplete cleanup downloads --yes --non-interactive
"""
WORKFLOW_EXAMPLES = """\
Advanced workflow maintenance:

  ./rocmplete content workflows list
  ./rocmplete content workflows status
  ./rocmplete content workflows install WORKFLOW
"""
BENCHMARK_EXAMPLES = """\
Try one of these:

  ./rocmplete benchmark run BUNDLE --dry-run
  ./rocmplete benchmark agent --preset qwen3.6-27b-q8-0 --dry-run
  ./rocmplete benchmark agent --preset qwen3.8-27b-mtp-ud-q8-k-xl \
    --thinking medium --dry-run
  ./rocmplete benchmark agent --preset PRESET --task re-cancel
  ./rocmplete benchmark agent --list-tasks
  ./rocmplete benchmark llama-cpp --preset qwen3-0.6b-q8-0 --dry-run
  ./rocmplete benchmark llama-cpp --preset PRESET --compare-backends
  ./rocmplete benchmark llama-cpp --preset PRESET --context-depth 32768 \
    --cache-type-k q8_0 --cache-type-v q8_0 --flash-attn on
  ./rocmplete benchmark llama-cpp-speculative \
    --preset qwen3.8-27b-mtp-ud-q8-k-xl --thinking medium --dry-run
  ./rocmplete benchmark llama-cpp-speculative --preset PRESET \
    --resume RESULT.json
  ./rocmplete benchmark suite --family qwen --dry-run
  ./rocmplete benchmark report SUITE.json
"""
ACCEPTANCE_EXAMPLES = """\
Run one bounded GPU smoke workload per practical application:

  ./rocmplete acceptance run

Automated workloads finish before generated media is presented for review.

Preview preparation and execution without changing anything:

  ./rocmplete acceptance run --dry-run

Run or resume only one application path:

  ./rocmplete acceptance run --application llama-cpp
  ./rocmplete acceptance run --application dwarfstar
  ./rocmplete acceptance run --resume RESULT.json
"""
RUN_EXAMPLES = """\
Try one of these:

  Start the ComfyUI web interface:
    ./rocmplete run comfyui

  Start llama.cpp with an explicit local GGUF model:
    ./rocmplete run llama-cpp server --model /path/to/model.gguf

  Start llama.cpp with the managed default model:
    ./rocmplete run llama-cpp server --preset qwen3.8-27b-mtp-ud-q8-k-xl

  Start the dense Qwen3.6 MTP model:
    ./rocmplete run llama-cpp server --preset qwen3.6-27b-mtp-q8-0

  Start managed Gemma 4 31B IT Q8_0 with its Q8_0 MTP draft:
    ./rocmplete run llama-cpp server --preset gemma4-31b-it-q8-0-mtp

  Start the manually prompted TranslateGemma model:
    ./rocmplete run llama-cpp server --preset translategemma-27b-it-q8-0

  Open an interactive llama.cpp conversation:
    ./rocmplete run llama-cpp cli --model /path/to/model.gguf

  Start DeepSeek V4 Flash with DwarfStar:
    ./rocmplete run dwarfstar server

  Start its exact managed DSpark pair:
    ./rocmplete run dwarfstar server --dspark

Web applications publish only their application port on 127.0.0.1 by default.
Add --listen 0.0.0.0 to publish it on every host interface.
"""
COMFYUI_RUN_EXAMPLES = """\
Pass ComfyUI-owned arguments after --:

  Enable the image-bundled ComfyUI Manager:
    ./rocmplete run comfyui -- --enable-manager

  Publish on one host IP and enable Manager:
    ./rocmplete run comfyui --listen 192.168.1.50 -- --enable-manager

ROCmplete continues to own listen, port, profile, devices, and data paths.
"""
LLAMA_RUN_EXAMPLES = """\
Try one of these:

  Start the OpenAI-compatible server:
    ./rocmplete run llama-cpp server --model /path/to/model.gguf

  Start the managed default model:
    ./rocmplete run llama-cpp server --preset qwen3.8-27b-mtp-ud-q8-k-xl

  Start the dense Qwen3.6 MTP model:
    ./rocmplete run llama-cpp server --preset qwen3.6-27b-mtp-q8-0

  Start Muse Glimmer with DFlash at forced 256K:
    ./rocmplete run llama-cpp server --preset muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k

  Start Gemma 4 31B IT Q8_0 with its Q8_0 MTP draft:
    ./rocmplete run llama-cpp server --preset gemma4-31b-it-q8-0-mtp

  Route requests among all installed managed presets:
    ./rocmplete run llama-cpp server --router

  Restrict the server to this host:
    ./rocmplete run llama-cpp server --model /path/to/model.gguf --listen 127.0.0.1

  Open an interactive conversation:
    ./rocmplete run llama-cpp cli --model /path/to/model.gguf

  Run one prompt without an interactive terminal:
    ./rocmplete run llama-cpp cli --model /path/to/model.gguf --prompt "Hello"
"""
DWARFSTAR_RUN_EXAMPLES = """\
Try one of these on a host with enough GPU-mapped memory:

  Start the OpenAI-compatible server with the managed default model:
    ./rocmplete run dwarfstar server

  Start the exact managed target/support pair with DSpark:
    ./rocmplete run dwarfstar server --dspark

  Start a compatible local GGUF instead:
    ./rocmplete run dwarfstar server --model /path/to/deepseek-v4.gguf

  Open an interactive terminal conversation:
    ./rocmplete run dwarfstar cli

  Run one deterministic direct answer:
    ./rocmplete run dwarfstar cli --no-thinking --prompt "Say hello"

The managed image is built from pinned source. The model is installed and
verified separately with
'content install dwarfstar flash-0731-q2-imatrix'.
The optional DSpark pair is installed with
'content install dwarfstar flash-0731-q2-imatrix-dspark'.
"""
def _add_render_node_arguments(
    parser: argparse.ArgumentParser, multi_gpu: bool = False
) -> None:
    help_text = "exact GPU render node"
    if multi_gpu:
        help_text += "; repeat to expose an exact multi-GPU set"
    parser.add_argument(
        "--render-node",
        action="append",
        metavar="PATH",
        help=help_text,
    )


def _add_web_run_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--profile",
        choices=PROFILES,
        help="execution profile (default: auto)",
    )
    parser.add_argument(
        "--listen",
        help="host IP on which to publish the web port (default: 127.0.0.1)",
    )
    parser.add_argument("--port", help="web port (application-specific default)")
    parser.add_argument("--data-dir", help="persistent data directory")
    _add_render_node_arguments(parser, multi_gpu=True)
    parser.add_argument(
        "--detach", action="store_true", help="run in the background"
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="print the resolved Podman command",
    )
    parser.add_argument(
        "--unconfined",
        action="store_true",
        help="disable the container seccomp filter",
    )
    parser.add_argument(
        "--disable-bundled-extensions",
        action="store_true",
        help="disable image-bundled ComfyUI extensions such as GGUF",
    )
    parser.add_argument(
        "--memory-policy",
        choices=("balanced", "conservative"),
        help="memory behavior (default: balanced)",
    )
    parser.add_argument("--image", help="override the local image tag")
    parser.add_argument(
        "--kernel-policy",
        choices=("default", "experimental"),
        help="ROCm kernel behavior (default: default)",
    )


def _add_llama_run_arguments(
    parser: argparse.ArgumentParser, server: bool
) -> None:
    source = parser.add_mutually_exclusive_group()
    source.add_argument(
        "--model",
        help="exact local GGUF file; its containing directory is mounted read-only",
    )
    source.add_argument(
        "--preset",
        help="installed catalog llama.cpp preset",
    )
    if server:
        source.add_argument(
            "--router",
            action="store_true",
            help="serve all installed catalog llama.cpp presets",
        )
    parser.add_argument(
        "--profile",
        choices=PROFILES,
        help="execution profile (default: auto)",
    )
    parser.add_argument(
        "--backend",
        choices=LLAMA_BACKENDS,
        default="rocm",
        help="GPU inference backend (default: rocm)",
    )
    _add_render_node_arguments(parser, multi_gpu=True)
    parser.add_argument("--data-dir", help="persistent data directory")
    parser.add_argument("--image", help="override the local image tag")
    parser.add_argument(
        "--context",
        type=int,
        help=(
            "context size; overrides the preset/model default and applies "
            "to every model in router mode"
        ),
    )
    parser.add_argument("--unconfined", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    if server:
        parser.add_argument(
            "--listen",
            help=(
                "host IP on which to publish the server port "
                "(default: 127.0.0.1)"
            ),
        )
        parser.add_argument("--port", help="web port (default: 8080)")
        parser.add_argument(
            "--detach", action="store_true", help="run in the background"
        )
        parser.add_argument(
            "--api-key-file",
            help="exact local API-key file mounted read-only",
        )
        parser.add_argument(
            "--models-max",
            type=int,
            help="router models loaded simultaneously (default: 2)",
        )
        parser.set_defaults(prompt=None)
    else:
        parser.add_argument(
            "--prompt",
            help="run one prompt; omit for an interactive conversation",
        )
        parser.set_defaults(
            listen="127.0.0.1",
            port="8080",
            detach=False,
            api_key_file=None,
            router=False,
            models_max=None,
        )


def _add_dwarfstar_run_arguments(
    parser: argparse.ArgumentParser, server: bool
) -> None:
    parser.add_argument(
        "--model",
        help=(
            "exact local DwarfStar-compatible GGUF file; defaults to the "
            "installed flash-0731-q2-imatrix model"
        ),
    )
    parser.add_argument(
        "--profile",
        choices=("auto",) + GPU_PROFILES,
        help="execution profile (default: auto)",
    )
    _add_render_node_arguments(parser)
    parser.add_argument("--data-dir", help="persistent data directory")
    parser.add_argument("--image", help="override the local image tag")
    parser.add_argument(
        "--context",
        type=int,
        default=DWARFSTAR_DEFAULT_CONTEXT,
        help="allocated context tokens (default: 131072)",
    )
    parser.add_argument(
        "--output-tokens",
        type=int,
        default=DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
        help="default or CLI maximum output tokens (default: 16000)",
    )
    parser.add_argument(
        "--dspark",
        action="store_true",
        help=(
            "enable the exact managed 0731 DSpark support GGUF "
            "(default: off)"
        ),
    )
    parser.add_argument("--unconfined", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    if server:
        parser.add_argument(
            "--listen",
            help=(
                "host IP on which to publish the server port "
                "(default: 127.0.0.1)"
            ),
        )
        parser.add_argument("--port", help="web port (default: 8000)")
        parser.add_argument(
            "--detach", action="store_true", help="run in the background"
        )
        parser.set_defaults(prompt=None, no_thinking=False)
    else:
        parser.add_argument(
            "--prompt",
            help="run one prompt; omit for an interactive conversation",
        )
        parser.add_argument(
            "--no-thinking",
            action="store_true",
            help="disable thinking and request a direct answer",
        )
        parser.set_defaults(listen="127.0.0.1", port="8000", detach=False)


def _add_benchmark_execution_arguments(
    parser: argparse.ArgumentParser,
) -> None:
    parser.add_argument(
        "--profile",
        help="auto or {}".format(", ".join(GPU_PROFILES)),
    )
    _add_render_node_arguments(parser)
    parser.add_argument("--data-dir", help="persistent data directory")
    parser.add_argument("--image", help="override the local image tag")
    parser.add_argument(
        "--port", help="local benchmark server port (default: 8190)"
    )
    parser.add_argument(
        "--runs",
        type=int,
        default=2,
        help="number of runs (default: 2; first fresh-process, rest warm)",
    )
    parser.add_argument(
        "--seed", type=int, default=10, help="first deterministic seed"
    )
    parser.add_argument(
        "--unconfined",
        action="store_true",
        help="disable the container seccomp filter",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="validate and print the benchmark container command",
    )
    parser.add_argument(
        "--accept-license",
        action="store_true",
        help="accept all model agreements listed for the selection",
    )
    parser.add_argument(
        "--non-interactive",
        action="store_true",
        help="never prompt; fail if a required confirmation flag is missing",
    )
    parser.add_argument(
        "--memory-policy",
        choices=("balanced", "conservative"),
        help="memory behavior (default: balanced)",
    )
    parser.add_argument(
        "--kernel-policy",
        choices=("default", "experimental"),
        help="ROCm kernel behavior (default: default)",
    )
    parser.add_argument(
        "--cache-mode",
        choices=("persistent", "isolated"),
        default="persistent",
        help=(
            "compiler cache behavior: reuse persistent caches or start with "
            "an isolated empty cache (default: persistent)"
        ),
    )


def _add_cleanup_confirmation_arguments(
    parser: argparse.ArgumentParser,
) -> None:
    parser.add_argument(
        "--yes",
        action="store_true",
        help="confirm the cleanup plan without prompting",
    )
    parser.add_argument(
        "--non-interactive",
        action="store_true",
        help="never prompt; require --yes for a non-empty plan",
    )


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="./rocmplete",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        description=(
            "Bootstrap and run ROCm applications in rootless containers "
            "for {}.".format(", ".join(SUPPORTED_ARCHITECTURES))
        ),
        epilog=LIFECYCLE_EXAMPLES,
    )
    parser.add_argument(
        "--version",
        action="version",
        version="ROCmplete {}".format(__version__),
    )
    subparsers = parser.add_subparsers(dest="command", metavar="COMMAND")

    build = subparsers.add_parser(
        "build", help="build the local image", allow_abbrev=False
    )
    build_cache = build.add_mutually_exclusive_group()
    build_cache.add_argument(
        "--no-layer-cache",
        action="store_true",
        help=(
            "rebuild selected image layers while checking prerequisites "
            "with their layer cache and reusing downloaded Python packages"
        ),
    )
    build_cache.add_argument(
        "--no-cache",
        action="store_true",
        help=(
            "cold-build the selected image and its prerequisites without "
            "image layers or downloaded-package caches"
        ),
    )
    build.add_argument(
        "application",
        choices=("all",) + BUILD_TARGETS,
        nargs="?",
        help="image target to build",
    )
    build.add_argument("--image", help="override the local image tag")
    build.set_defaults(command_parser=build)

    guide = subparsers.add_parser(
        "guide",
        help="show a focused application walkthrough",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=GUIDE_EXAMPLES,
    )
    guide.add_argument(
        "application",
        choices=APPLICATION_NAMES,
        nargs="?",
        help="application to explain",
    )

    agent = subparsers.add_parser(
        "agent",
        help="run a coding agent with managed local model providers",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=AGENT_EXAMPLES,
    )
    agent_clients = agent.add_subparsers(
        dest="agent_client", metavar="CLIENT"
    )
    opencode = agent_clients.add_parser(
        "opencode",
        help="run OpenCode with the managed local model providers",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=OPENCODE_EXAMPLES,
    )
    opencode.add_argument(
        "--port",
        help=(
            "local llama.cpp router port (default: "
            "ROCMLETE_OPENCODE_PORT or 8080)"
        ),
    )
    opencode.add_argument(
        "--dwarfstar-port",
        help=(
            "local DwarfStar server port (default: "
            "ROCMLETE_OPENCODE_DWARFSTAR_PORT or 8000)"
        ),
    )
    opencode.add_argument(
        "--data-dir", help="persistent data directory"
    )
    sandbox = opencode.add_mutually_exclusive_group()
    sandbox.add_argument(
        "--sandbox",
        dest="sandbox",
        action="store_true",
        default=True,
        help=(
            "confine OpenCode to the launch directory with bubblewrap "
            "(default)"
        ),
    )
    sandbox.add_argument(
        "--no-sandbox",
        dest="sandbox",
        action="store_false",
        help="run OpenCode with normal host filesystem access",
    )
    opencode.add_argument(
        "opencode_arguments",
        nargs=argparse.REMAINDER,
        help=argparse.SUPPRESS,
    )
    opencode.set_defaults(command_parser=opencode)

    pi = agent_clients.add_parser(
        "pi",
        help="run Pi with the managed local model providers",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=PI_EXAMPLES,
    )
    pi.add_argument(
        "--port",
        help=(
            "local llama.cpp router port (default: "
            "ROCMLETE_PI_PORT or 8080)"
        ),
    )
    pi.add_argument(
        "--dwarfstar-port",
        help=(
            "local DwarfStar server port (default: "
            "ROCMLETE_PI_DWARFSTAR_PORT or 8000)"
        ),
    )
    pi.add_argument("--data-dir", help="persistent data directory")
    pi_sandbox = pi.add_mutually_exclusive_group()
    pi_sandbox.add_argument(
        "--sandbox",
        dest="sandbox",
        action="store_true",
        default=True,
        help="confine Pi to the launch directory with bubblewrap (default)",
    )
    pi_sandbox.add_argument(
        "--no-sandbox",
        dest="sandbox",
        action="store_false",
        help="run Pi with normal host filesystem access",
    )
    pi.add_argument(
        "pi_arguments",
        nargs=argparse.REMAINDER,
        help=argparse.SUPPRESS,
    )
    pi.set_defaults(command_parser=pi)

    omp = agent_clients.add_parser(
        "omp",
        help="run Oh My Pi with the managed local model providers",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=OMP_EXAMPLES,
    )
    omp.add_argument(
        "--port",
        help=(
            "local llama.cpp router port (default: "
            "ROCMLETE_OMP_PORT or 8080)"
        ),
    )
    omp.add_argument(
        "--dwarfstar-port",
        help=(
            "local DwarfStar server port (default: "
            "ROCMLETE_OMP_DWARFSTAR_PORT or 8000)"
        ),
    )
    omp.add_argument("--data-dir", help="persistent data directory")
    omp_sandbox = omp.add_mutually_exclusive_group()
    omp_sandbox.add_argument(
        "--sandbox",
        dest="sandbox",
        action="store_true",
        default=True,
        help="confine OMP to the launch directory with bubblewrap (default)",
    )
    omp_sandbox.add_argument(
        "--no-sandbox",
        dest="sandbox",
        action="store_false",
        help="run OMP with normal host filesystem access",
    )
    omp.add_argument(
        "omp_arguments",
        nargs=argparse.REMAINDER,
        help=argparse.SUPPRESS,
    )
    omp.set_defaults(command_parser=omp)

    maki = agent_clients.add_parser(
        "maki",
        help="run Maki with the managed local model providers",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=MAKI_EXAMPLES,
    )
    maki.add_argument(
        "--port",
        help=(
            "local llama.cpp router port (default: "
            "ROCMLETE_MAKI_PORT or 8080)"
        ),
    )
    maki.add_argument(
        "--dwarfstar-port",
        help=(
            "local DwarfStar server port (default: "
            "ROCMLETE_MAKI_DWARFSTAR_PORT or 8000)"
        ),
    )
    maki.add_argument("--data-dir", help="persistent data directory")
    maki_sandbox = maki.add_mutually_exclusive_group()
    maki_sandbox.add_argument(
        "--sandbox",
        dest="sandbox",
        action="store_true",
        default=True,
        help="confine Maki to the launch directory with bubblewrap (default)",
    )
    maki_sandbox.add_argument(
        "--no-sandbox",
        dest="sandbox",
        action="store_false",
        help="run Maki with normal host filesystem access",
    )
    maki.add_argument(
        "maki_arguments",
        nargs=argparse.REMAINDER,
        help=argparse.SUPPRESS,
    )
    maki.set_defaults(command_parser=maki)
    agent.set_defaults(command_parser=agent)

    images = subparsers.add_parser(
        "images",
        help="export and import locally built images",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=IMAGE_EXAMPLES,
    )
    image_commands = images.add_subparsers(
        dest="images_command", metavar="COMMAND"
    )
    export_images = image_commands.add_parser(
        "export",
        help="save managed images to one Docker archive",
        allow_abbrev=False,
    )
    export_images.add_argument(
        "target", choices=("all",) + BUILD_APPLICATIONS
    )
    export_images.add_argument(
        "--output", required=True, help="new archive path"
    )
    export_images.add_argument(
        "--dry-run",
        action="store_true",
        help="validate and print the export without creating a file",
    )
    import_images = image_commands.add_parser(
        "import",
        help="validate and load a managed image archive",
        allow_abbrev=False,
    )
    import_images.add_argument("archive", help="archive created by export")
    import_images.add_argument(
        "--dry-run",
        action="store_true",
        help="validate archive, tags, and conflicts without loading",
    )
    images.set_defaults(command_parser=images)

    doctor = subparsers.add_parser(
        "doctor",
        help="inspect the host and run a small GPU diagnostic when available",
        allow_abbrev=False,
    )
    _add_render_node_arguments(doctor, multi_gpu=True)
    doctor.add_argument("--data-dir", help="persistent data directory")
    doctor.add_argument(
        "--image",
        help="PyTorch diagnostic image (default: a built managed image)",
    )

    acceptance = subparsers.add_parser(
        "acceptance",
        help="run checkpointed target-hardware smoke acceptance",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=ACCEPTANCE_EXAMPLES,
    )
    acceptance_commands = acceptance.add_subparsers(
        dest="acceptance_command", metavar="COMMAND"
    )
    acceptance_run = acceptance_commands.add_parser(
        "run",
        help="prepare and run the bounded smoke suite",
        allow_abbrev=False,
    )
    acceptance_run.add_argument(
        "--profile",
        choices=("auto",) + GPU_PROFILES,
        default="auto",
        help="expected profile; auto validates the detected hardware",
    )
    _add_render_node_arguments(acceptance_run)
    acceptance_run.add_argument(
        "--application",
        action="append",
        choices=APPLICATION_NAMES,
        default=[],
        help="limit application workloads; repeatable",
    )
    acceptance_run.add_argument(
        "--data-dir", help="persistent data directory"
    )
    acceptance_run.add_argument(
        "--port",
        default="8190",
        help="private ComfyUI smoke port (default: 8190)",
    )
    acceptance_output = acceptance_run.add_mutually_exclusive_group()
    acceptance_output.add_argument(
        "--output", help="new acceptance JSON result path"
    )
    acceptance_output.add_argument(
        "--resume", help="resume a compatible acceptance JSON result"
    )
    acceptance_run.add_argument(
        "--prepare",
        action="store_true",
        help="build missing images and install missing smoke content",
    )
    acceptance_run.add_argument(
        "--dry-run",
        action="store_true",
        help="show preparation and smoke cases without changing anything",
    )
    acceptance_run.add_argument(
        "--non-interactive",
        action="store_true",
        help="never prompt; generated visual outputs remain BLOCKED",
    )
    acceptance_run.add_argument(
        "--accept-license",
        action="store_true",
        help="accept all model agreements required by smoke content",
    )
    acceptance_run.add_argument(
        "--acknowledge-license-risk",
        action="store_true",
        help="allow smoke content whose hosted-file rights are unverified",
    )
    acceptance_run.add_argument(
        "--memory-policy",
        choices=("balanced", "conservative"),
        default="balanced",
    )
    acceptance_run.add_argument(
        "--kernel-policy",
        choices=("default", "experimental"),
        default="default",
    )
    acceptance.set_defaults(command_parser=acceptance)

    status = subparsers.add_parser(
        "status",
        help="show images, containers, devices, and persistent data",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=STATUS_EXAMPLES,
    )
    status.add_argument(
        "application",
        choices=("llama-cpp",),
        nargs="?",
        help="show a shareable live application report",
    )
    status.add_argument("--data-dir", help="persistent data directory")
    status.add_argument(
        "--model",
        help="managed preset to select for model-specific details",
    )

    run = subparsers.add_parser(
        "run",
        help="run a managed application",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=RUN_EXAMPLES,
    )
    run_applications = run.add_subparsers(
        dest="application", metavar="APPLICATION"
    )
    comfyui = run_applications.add_parser(
        "comfyui",
        help="start the ComfyUI web interface",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=COMFYUI_RUN_EXAMPLES,
    )
    _add_web_run_arguments(comfyui)
    llama = run_applications.add_parser(
        "llama-cpp",
        help="run a local GGUF model with llama.cpp",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=LLAMA_RUN_EXAMPLES,
    )
    llama_modes = llama.add_subparsers(dest="mode", metavar="MODE")
    llama_server = llama_modes.add_parser(
        "server",
        help="start the OpenAI-compatible llama.cpp server",
        allow_abbrev=False,
    )
    _add_llama_run_arguments(llama_server, server=True)
    llama_cli = llama_modes.add_parser(
        "cli",
        help="run llama.cpp in interactive or one-prompt mode",
        allow_abbrev=False,
    )
    _add_llama_run_arguments(llama_cli, server=False)
    dwarfstar = run_applications.add_parser(
        "dwarfstar",
        help="run DeepSeek V4 Flash with DwarfStar",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=DWARFSTAR_RUN_EXAMPLES,
    )
    dwarfstar_modes = dwarfstar.add_subparsers(
        dest="mode", metavar="MODE"
    )
    dwarfstar_server = dwarfstar_modes.add_parser(
        "server",
        help="start the OpenAI-compatible DwarfStar server",
        allow_abbrev=False,
    )
    _add_dwarfstar_run_arguments(dwarfstar_server, server=True)
    dwarfstar_cli = dwarfstar_modes.add_parser(
        "cli",
        help="run DwarfStar in interactive or one-prompt mode",
        allow_abbrev=False,
    )
    _add_dwarfstar_run_arguments(dwarfstar_cli, server=False)
    run.set_defaults(command_parser=run)
    llama.set_defaults(application_parser=llama)
    dwarfstar.set_defaults(application_parser=dwarfstar)

    shell = subparsers.add_parser(
        "shell",
        help="open a constrained container shell",
        allow_abbrev=False,
    )
    shell.add_argument(
        "application",
        choices=SHELL_APPLICATIONS,
        nargs="?",
    )
    shell.add_argument("--data-dir", help="persistent data directory")
    shell.add_argument("--image", help="override the local image tag")
    shell.set_defaults(command_parser=shell)
    logs = subparsers.add_parser(
        "logs", help="show detached container logs", allow_abbrev=False
    )
    logs.add_argument(
        "application",
        choices=LOG_APPLICATIONS,
        nargs="?",
    )
    logs.add_argument("--follow", action="store_true", help="follow log output")
    log_range = logs.add_mutually_exclusive_group()
    log_range.add_argument(
        "--tail",
        type=int,
        default=200,
        help="number of recent lines to show (default: 200)",
    )
    log_range.add_argument(
        "--all",
        action="store_true",
        help="show complete retained logs",
    )
    logs.set_defaults(command_parser=logs)
    stop = subparsers.add_parser(
        "stop", help="stop managed containers", allow_abbrev=False
    )
    stop.add_argument(
        "application",
        choices=APPLICATION_NAMES + ("all",),
        nargs="?",
    )
    stop.set_defaults(command_parser=stop)
    cleanup = subparsers.add_parser(
        "cleanup",
        help="remove one explicit ROCmplete resource scope",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=CLEANUP_EXAMPLES,
    )
    cleanup_commands = cleanup.add_subparsers(
        dest="cleanup_command", metavar="SCOPE"
    )
    cleanup_containers = cleanup_commands.add_parser(
        "containers", help="stop and remove managed containers"
    )
    cleanup_containers.add_argument(
        "application",
        choices=APPLICATION_NAMES + ("all",),
        nargs="?",
        default="all",
    )
    _add_cleanup_confirmation_arguments(cleanup_containers)
    cleanup_caches = cleanup_commands.add_parser(
        "caches", help="remove reproducible application and compiler caches"
    )
    cleanup_caches.add_argument("--data-dir")
    _add_cleanup_confirmation_arguments(cleanup_caches)
    cleanup_build_cache = cleanup_commands.add_parser(
        "build-cache",
        help="remove reusable locally downloaded build packages",
    )
    _add_cleanup_confirmation_arguments(cleanup_build_cache)
    cleanup_downloads = cleanup_commands.add_parser(
        "downloads", help="remove resumable download staging"
    )
    cleanup_downloads.add_argument("--data-dir")
    _add_cleanup_confirmation_arguments(cleanup_downloads)
    cleanup_images = cleanup_commands.add_parser(
        "images", help="remove locally built application images"
    )
    cleanup_images.add_argument(
        "application",
        choices=APPLICATION_NAMES + ("all",),
        nargs="?",
        default="all",
    )
    cleanup_images.add_argument(
        "--image-tag",
        help="remove one exact custom image tag instead",
    )
    _add_cleanup_confirmation_arguments(cleanup_images)
    cleanup_data = cleanup_commands.add_parser(
        "data", help="permanently remove all selected persistent data"
    )
    cleanup_data.add_argument("--data-dir")
    _add_cleanup_confirmation_arguments(cleanup_data)
    cleanup.set_defaults(command_parser=cleanup)

    content = subparsers.add_parser(
        "content",
        help="list, install, and inspect models and workflows",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=CONTENT_EXAMPLES,
    )
    content_commands = content.add_subparsers(
        dest="content_command", metavar="COMMAND"
    )
    content_list = content_commands.add_parser(
        "list",
        help="list recipes, bundles, families, or runnable models",
        allow_abbrev=False,
    )
    content_list_views = content_list.add_mutually_exclusive_group()
    content_list_views.add_argument(
        "--bundles",
        action="store_true",
        help="list exact advanced bundles instead of recipes",
    )
    content_list_views.add_argument(
        "--families",
        action="store_true",
        help="list model-family and global selections instead of recipes",
    )
    content_list_views.add_argument(
        "--models",
        action="store_true",
        help="list managed runnable models and discovered local GGUFs",
    )
    content_list.add_argument(
        "--application",
        choices=tuple(CONTENT_APPLICATION_RECIPES),
        help="limit --bundles or --models to one consuming application",
    )
    content_list.add_argument("--data-dir")
    content_list.add_argument(
        "--details",
        action="store_true",
        help="show managed model runtime policy and catalog file totals",
    )
    content_list.add_argument(
        "--scan",
        action="append",
        default=[],
        type=Path,
        metavar="PATH",
        help=(
            "with --models, also scan one llama.cpp GGUF file or directory; "
            "repeatable"
        ),
    )
    content_status = content_commands.add_parser(
        "status",
        help="inspect installed content",
        allow_abbrev=False,
    )
    content_status.add_argument("target", nargs="?", metavar="TARGET")
    content_status.add_argument("selection", nargs="?", metavar="SELECTION")
    content_status.add_argument("--data-dir")
    content_status.add_argument(
        "--verify",
        action="store_true",
        help="read every installed file and verify SHA-256",
    )
    content_status.add_argument(
        "--details",
        action="store_true",
        help="show every artifact and workflow state",
    )
    content_install = content_commands.add_parser(
        "install",
        help="install a selection or exact bundle",
        allow_abbrev=False,
    )
    content_install.add_argument(
        "target",
        nargs="?",
        metavar="TARGET",
        help=(
            "application, family, exact bundle name, or all"
        ),
    )
    content_install.add_argument(
        "selection",
        nargs="?",
        metavar="SELECTION",
        help=(
            "application recipe such as image, edit, t2v, or i2v; all "
            "bundles owned by that application; or the qwen/wan family"
        ),
    )
    content_interaction = content_install.add_mutually_exclusive_group()
    content_interaction.add_argument(
        "--interactive",
        action="store_true",
        help="choose content in a guided menu",
    )
    content_interaction.add_argument(
        "--non-interactive",
        action="store_true",
        help="never prompt; fail if a required selection or approval is missing",
    )
    content_install.add_argument(
        "--from-file",
        action="append",
        default=[],
        type=Path,
        metavar="PATH",
        help=(
            "extend the catalog and install every bundle from a local "
            "content pack; repeatable"
        ),
    )
    content_install.add_argument(
        "--local-mirror",
        type=Path,
        metavar="PATH",
        help=(
            "recursively reuse files from an old content directory after "
            "exact size and SHA-256 verification"
        ),
    )
    content_install.add_argument(
        "--local-mirror-move",
        action="store_true",
        help=(
            "move verified mirror files instead of copying them; requires "
            "--local-mirror"
        ),
    )
    content_install.add_argument("--data-dir")
    content_install.add_argument(
        "--image", help="override the local content-tools image tag"
    )
    content_install.add_argument(
        "--dry-run", action="store_true", help="show the complete plan only"
    )
    content_install.add_argument(
        "--force-workflow",
        action="store_true",
        help="replace an existing modified workflow",
    )
    content_install.add_argument(
        "--accept-license",
        action="store_true",
        help="accept all model agreements listed for the selection",
    )
    content_install.add_argument(
        "--acknowledge-license-risk",
        action="store_true",
        help="allow download-only artifacts whose license is unverified",
    )
    content_import = content_commands.add_parser(
        "import",
        help="resolve and install one pinned remote model or workflow file",
        allow_abbrev=False,
    )
    content_import.add_argument(
        "url",
        nargs="?",
        help="Civitai model/version URL or Hugging Face model/file URL",
    )
    content_import.add_argument(
        "--version",
        type=int,
        metavar="ID",
        help="exact Civitai model-version ID when the URL does not select one",
    )
    content_import.add_argument(
        "--file",
        metavar="FILE",
        help="provider file ID, filename, or Hugging Face repository path",
    )
    content_import.add_argument(
        "--as",
        dest="import_kind",
        choices=tuple(IMPORT_KINDS),
        metavar="TYPE",
        help="destination type when it cannot be inferred safely",
    )
    content_import.add_argument(
        "--save-pack",
        type=Path,
        metavar="PATH",
        help="save the reusable local content pack at PATH",
    )
    content_import.add_argument("--data-dir")
    content_import.add_argument(
        "--image", help="override the local content-tools image tag"
    )
    content_import.add_argument(
        "--dry-run",
        action="store_true",
        help="resolve and validate the import without saving or downloading",
    )
    content_import.add_argument(
        "--non-interactive",
        action="store_true",
        help="never prompt; require explicit choices and acknowledgment",
    )
    content_import.add_argument(
        "--acknowledge-license-risk",
        action="store_true",
        help="allow download when ROCmplete cannot verify hosted-file rights",
    )
    content_workflows = content_commands.add_parser(
        "workflows",
        help="advanced workflow inspection and repair",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=WORKFLOW_EXAMPLES,
    )
    workflow_commands = content_workflows.add_subparsers(
        dest="workflows_command", metavar="COMMAND"
    )
    workflow_commands.add_parser(
        "list", help="list available workflows", allow_abbrev=False
    )
    workflow_status = workflow_commands.add_parser(
        "status", help="inspect installed workflows", allow_abbrev=False
    )
    workflow_status.add_argument("workflow", nargs="?")
    workflow_status.add_argument("--data-dir")
    workflow_install = workflow_commands.add_parser(
        "install", help="install a curated workflow", allow_abbrev=False
    )
    workflow_install.add_argument("workflow")
    workflow_install.add_argument("--data-dir")
    workflow_install.add_argument(
        "--image", help="override the local ComfyUI image tag"
    )
    workflow_install.add_argument(
        "--force",
        action="store_true",
        help="replace an existing modified workflow",
    )
    content.set_defaults(command_parser=content)
    content_workflows.set_defaults(command_parser=content_workflows)

    benchmark = subparsers.add_parser(
        "benchmark",
        help="run or report managed performance and coding evaluations",
        allow_abbrev=False,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=BENCHMARK_EXAMPLES,
    )
    benchmark_commands = benchmark.add_subparsers(
        dest="benchmark_command", metavar="COMMAND"
    )
    benchmark_run = benchmark_commands.add_parser(
        "run", help="benchmark one exact content bundle", allow_abbrev=False
    )
    benchmark_run.add_argument("bundle", metavar="BUNDLE")
    _add_benchmark_execution_arguments(benchmark_run)
    benchmark_suite = benchmark_commands.add_parser(
        "suite", help="benchmark a bundle selection", allow_abbrev=False
    )
    _add_benchmark_execution_arguments(benchmark_suite)
    benchmark_suite.add_argument(
        "--family",
        choices=("comfyui", "qwen", "wan", "ltx", "hunyuan"),
        help="limit a suite to one catalog family",
    )
    benchmark_suite.add_argument(
        "--include",
        action="append",
        default=[],
        metavar="BUNDLE",
        help="include an explicit bundle in a suite; repeat as needed",
    )
    benchmark_suite.add_argument(
        "--resume",
        metavar="SUITE_JSON",
        help="resume a compatible suite result file",
    )
    benchmark_suite.add_argument(
        "--keep-going",
        action="store_true",
        help="continue a suite after an individual benchmark failure",
    )
    benchmark_suite.add_argument(
        "--report-format",
        choices=("markdown", "html", "both", "none"),
        default="both",
        help="suite report output (default: both)",
    )
    agent_benchmark = benchmark_commands.add_parser(
        "agent",
        help="evaluate one managed model with the frozen coding-agent suite",
        allow_abbrev=False,
    )
    agent_model = agent_benchmark.add_mutually_exclusive_group()
    agent_model.add_argument(
        "--preset", help="installed agent-capable llama.cpp preset"
    )
    agent_model.add_argument(
        "--dwarfstar",
        action="store_true",
        help="evaluate the installed DwarfStar DeepSeek V4 Flash model",
    )
    agent_benchmark.add_argument(
        "--task",
        action="append",
        default=[],
        metavar="TASK",
        help="limit the frozen suite to one task; repeatable",
    )
    agent_benchmark.add_argument(
        "--list-tasks",
        action="store_true",
        help="list frozen task identifiers without preparing or running them",
    )
    agent_benchmark.add_argument(
        "--repetitions",
        type=int,
        default=1,
        help="fresh attempts per selected task (default: 1)",
    )
    agent_benchmark.add_argument(
        "--context",
        type=int,
        default=None,
        help="server context shared by every task (default: 131072)",
    )
    agent_benchmark.add_argument(
        "--thinking",
        choices=("off", "minimal", "low", "medium", "high", "xhigh", "max"),
        default=None,
        help="explicit Pi thinking level (default: selected model policy)",
    )
    agent_benchmark.add_argument(
        "--profile",
        choices=("auto",) + GPU_PROFILES,
        default="auto",
        help="GPU execution profile (default: auto)",
    )
    agent_benchmark.add_argument(
        "--backend",
        choices=LLAMA_BACKENDS,
        default="rocm",
        help="llama.cpp backend (default: rocm)",
    )
    _add_render_node_arguments(agent_benchmark, multi_gpu=True)
    agent_benchmark.add_argument("--data-dir", help="persistent data directory")
    agent_benchmark.add_argument(
        "--port", default="8187", help="private model-server port (default: 8187)"
    )
    agent_benchmark.add_argument(
        "--output", help="new result JSON below managed evaluation storage"
    )
    agent_benchmark.add_argument(
        "--keep-going",
        action="store_true",
        help="continue after an individual evaluation infrastructure failure",
    )
    agent_benchmark.add_argument(
        "--dry-run",
        action="store_true",
        help="validate and print the suite without fetching, starting, or writing",
    )
    llama_benchmark = benchmark_commands.add_parser(
        "llama-cpp",
        help="run native llama-bench against one GGUF model",
        allow_abbrev=False,
    )
    llama_source = llama_benchmark.add_mutually_exclusive_group()
    llama_source.add_argument("--model", help="exact local GGUF file")
    llama_source.add_argument(
        "--preset", help="installed catalog llama.cpp preset"
    )
    llama_benchmark.add_argument(
        "--profile",
        choices=PROFILES,
        help="execution profile (default: auto)",
    )
    llama_backend = llama_benchmark.add_mutually_exclusive_group()
    llama_backend.add_argument(
        "--backend",
        choices=LLAMA_BACKENDS,
        default="rocm",
        help="GPU inference backend (default: rocm)",
    )
    llama_backend.add_argument(
        "--compare-backends",
        action="store_true",
        help="run ROCm and Vulkan consecutively and compare their results",
    )
    _add_render_node_arguments(llama_benchmark, multi_gpu=True)
    llama_benchmark.add_argument("--data-dir", help="persistent data directory")
    llama_benchmark.add_argument("--image", help="override the local image tag")
    llama_benchmark.add_argument(
        "--repetitions",
        type=int,
        default=5,
        help="repetitions per test (default: 5)",
    )
    llama_benchmark.add_argument(
        "--prompt-tokens",
        type=int,
        default=512,
        help="prompt-processing tokens (default: 512)",
    )
    llama_benchmark.add_argument(
        "--generation-tokens",
        type=int,
        default=128,
        help="text-generation tokens (default: 128)",
    )
    llama_benchmark.add_argument(
        "--context-depth",
        type=int,
        default=0,
        help="tokens already present before each test (default: 0)",
    )
    llama_benchmark.add_argument(
        "--batch-size",
        type=int,
        default=2048,
        help="logical prompt batch size (default: 2048)",
    )
    llama_benchmark.add_argument(
        "--ubatch-size",
        type=int,
        default=512,
        help="physical prompt microbatch size (default: 512)",
    )
    llama_benchmark.add_argument(
        "--cache-type-k",
        choices=("f16", "q8_0", "q4_0"),
        default="f16",
        help="KV key-cache type (default: f16)",
    )
    llama_benchmark.add_argument(
        "--cache-type-v",
        choices=("f16", "q8_0", "q4_0"),
        default="f16",
        help="KV value-cache type (default: f16)",
    )
    llama_benchmark.add_argument(
        "--flash-attn",
        choices=("on", "off", "auto"),
        default="auto",
        help="Flash Attention policy (default: auto)",
    )
    llama_benchmark.add_argument(
        "--output",
        help=(
            "result or comparison JSON path; default is application "
            "benchmark storage"
        ),
    )
    llama_benchmark.add_argument("--unconfined", action="store_true")
    llama_benchmark.add_argument("--dry-run", action="store_true")
    speculative_benchmark = benchmark_commands.add_parser(
        "llama-cpp-speculative",
        help="sweep server-side speculative draft depths",
        allow_abbrev=False,
    )
    speculative_benchmark.add_argument(
        "--preset", required=True, help="installed speculative llama.cpp preset"
    )
    speculative_benchmark.add_argument(
        "--draft-depth",
        action="append",
        default=[],
        type=int,
        metavar="TOKENS",
        help="draft depth to test; repeatable (default: every supported depth)",
    )
    speculative_benchmark.add_argument(
        "--context-depth",
        action="append",
        default=[],
        type=int,
        metavar="TOKENS",
        help=(
            "target prompt depth; repeatable "
            "(default: 4096, 32768, 65536, 122880)"
        ),
    )
    speculative_benchmark.add_argument(
        "--repetitions",
        type=int,
        default=SPECULATIVE_DEFAULT_REPETITIONS,
        help="fresh seeded repetitions per depth and context (default: 3)",
    )
    speculative_benchmark.add_argument(
        "--generation-tokens",
        type=int,
        default=SPECULATIVE_DEFAULT_GENERATION_TOKENS,
        help="maximum generated tokens per request (default: 512)",
    )
    speculative_benchmark.add_argument(
        "--seed",
        type=int,
        default=SPECULATIVE_DEFAULT_SEED,
        help="first request seed; repetitions increment it (default: 42)",
    )
    speculative_benchmark.add_argument(
        "--context",
        type=int,
        default=SPECULATIVE_DEFAULT_SERVER_CONTEXT,
        help="single-slot server context (default: 131072)",
    )
    speculative_benchmark.add_argument(
        "--thinking",
        choices=("off", "minimal", "low", "medium", "high", "xhigh", "max"),
        default=None,
        help="explicit model reasoning condition (default: preset policy)",
    )
    speculative_benchmark.add_argument(
        "--draft-probability-min",
        type=float,
        default=0.0,
        metavar="P",
        help=(
            "minimum speculative draft probability from 0 through 1 "
            "(default: 0)"
        ),
    )
    speculative_benchmark.add_argument(
        "--draft-backend-sampling",
        choices=("on", "off"),
        default="on",
        help="draft sampler backend offload policy (default: on)",
    )
    speculative_benchmark.add_argument(
        "--ngram-simple",
        action="store_true",
        help="try draftless n-gram matching before the preset draft strategy",
    )
    speculative_benchmark.add_argument(
        "--graph-optimization",
        action="store_true",
        help="enable llama.cpp's experimental CUDA/HIP graph optimizer",
    )
    speculative_benchmark.add_argument(
        "--disable-graphs",
        action="store_true",
        help="disable llama.cpp CUDA/HIP graph capture for this measurement",
    )
    speculative_benchmark.add_argument(
        "--poll",
        type=int,
        metavar="0...100",
        help="llama.cpp worker polling level (default: upstream policy)",
    )
    speculative_benchmark.add_argument(
        "--no-host",
        action="store_true",
        help="bypass llama.cpp host buffers for this measurement",
    )
    speculative_benchmark.add_argument(
        "--flash-attn",
        choices=("preset", "on", "off", "auto"),
        default="preset",
        help="Flash Attention condition (default: preset policy)",
    )
    speculative_benchmark.add_argument(
        "--cache-type-k",
        choices=("preset", "f16", "q8_0", "q4_0"),
        default="preset",
        help="target key-cache condition (default: preset policy)",
    )
    speculative_benchmark.add_argument(
        "--cache-type-v",
        choices=("preset", "f16", "q8_0", "q4_0"),
        default="preset",
        help="target value-cache condition (default: preset policy)",
    )
    speculative_benchmark.add_argument(
        "--batch-size",
        type=int,
        default=2048,
        help="logical prompt batch size (default: 2048)",
    )
    speculative_benchmark.add_argument(
        "--ubatch-size",
        type=int,
        default=512,
        help="physical prompt microbatch size (default: 512)",
    )
    speculative_benchmark.add_argument(
        "--profile",
        choices=("auto",) + GPU_PROFILES,
        default="auto",
        help="GPU execution profile (default: auto)",
    )
    speculative_benchmark.add_argument(
        "--backend",
        choices=LLAMA_BACKENDS,
        default="rocm",
        help="llama.cpp backend (default: rocm)",
    )
    _add_render_node_arguments(speculative_benchmark, multi_gpu=True)
    speculative_benchmark.add_argument(
        "--port", default="8190", help="private benchmark server port (default: 8190)"
    )
    speculative_benchmark.add_argument(
        "--data-dir", help="persistent data directory"
    )
    speculative_benchmark.add_argument(
        "--image", help="override the local llama.cpp image tag"
    )
    speculative_destination = speculative_benchmark.add_mutually_exclusive_group()
    speculative_destination.add_argument(
        "--output", help="new checkpoint JSON; default is llama.cpp benchmark storage"
    )
    speculative_destination.add_argument(
        "--resume", help="resume a compatible speculative benchmark JSON"
    )
    speculative_benchmark.add_argument(
        "--keep-going",
        action="store_true",
        help="continue after an individual server or request failure",
    )
    speculative_benchmark.add_argument("--unconfined", action="store_true")
    speculative_benchmark.add_argument(
        "--dry-run",
        action="store_true",
        help="validate and print the complete sweep without starting or writing",
    )
    benchmark_report = benchmark_commands.add_parser(
        "report", help="render reports from an existing suite JSON"
    )
    benchmark_report.add_argument("subject", metavar="SUITE_JSON")
    benchmark_report.add_argument(
        "--report-format",
        choices=("markdown", "html", "both"),
        default="both",
        help="report output format (default: both)",
    )
    benchmark_report.add_argument(
        "--output",
        help="output path; requires markdown or html format",
    )
    benchmark.set_defaults(command_parser=benchmark)

    return parser


def parse_arguments(
    argv: Sequence[str],
) -> Tuple[argparse.ArgumentParser, argparse.Namespace]:
    parser = _parser()
    arguments = list(argv)
    if arguments == ["help"]:
        arguments = ["--help"]
    comfy_args: List[str] = []
    command = arguments[0] if arguments else None
    if command == "run" and "--" in arguments:
        separator = arguments.index("--")
        comfy_args = arguments[separator + 1 :]
        arguments = arguments[:separator]
    namespace = parser.parse_args(arguments)
    namespace.comfy_args = comfy_args
    return parser, namespace
