"""Public ROCmplete command-line interface."""

from __future__ import annotations

import argparse
import glob
import json
import os
import platform
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import (
    Callable,
    List,
    Mapping,
    MutableMapping,
    Optional,
    Sequence,
    Tuple,
    cast,
)

from . import podman
from .acceptance import (
    AcceptanceCase,
    acceptance_definition,
    acceptance_fingerprint,
    begin_case,
    blocked_visual_identifiers,
    case_entry,
    checkpoint as checkpoint_acceptance,
    complete_case,
    create_result as create_acceptance_result,
    default_result_path as default_acceptance_result_path,
    fail_case,
    finish as finish_acceptance,
    load_result as load_acceptance_result,
    pending_case_identifiers,
    probe_hardware,
    required_bundles as acceptance_required_bundles,
    required_images as acceptance_required_images,
    run_application_case,
    run_host_case,
    selected_cases as selected_acceptance_cases,
    source_identity,
    validate_result_fingerprint as validate_acceptance_result_fingerprint,
)
from .application_guides import print_application_guide
from .config import (
    APPLICATIONS,
    APPLICATION_NAMES,
    BUILD_APPLICATIONS,
    CONTENT_TOOLS_BUILD_TARGET,
    CONTENT_TOOLS_IMAGE,
    DEFAULT_LISTEN,
    DWARFSTAR_DEFAULT_MODEL_BUNDLE,
    DWARFSTAR_DSPARK_MODEL_BUNDLE,
    GPU_PROFILES,
    TRANSIENT_CONTAINER_APPLICATIONS,
    ROCM_BASE_BUILD_TARGET,
    ROCM_BASE_IMAGE,
    ROCM_RUNTIME_BUILD_TARGET,
    ROCM_RUNTIME_IMAGE,
    WEB_APPLICATIONS,
    environment_value,
    is_loopback_address,
    reject_managed_comfy_args,
    selected_data_dir,
    validate_port,
    validate_listen_address,
    validate_kernel_policy,
    validate_memory_policy,
    validate_profile,
    version_at_least,
)
from .catalog import (
    Agreement,
    Bundle,
    Catalog,
    LlamaPreset,
    load_catalog,
    load_content_packs,
)
from .bundles import (
    ArtifactStatus,
    LocalMirror,
    artifact_path,
    content_status_ready,
    content_status_state,
    content_install_lock,
    human_size,
    inspect_bundle,
    install_artifacts,
    install_bundle,
    missing_unverified,
    print_plan,
    print_selection_plan,
    print_selection_summary,
    selection_artifacts,
    verify_status,
)
from .errors import LauncherError
from .content_verification import VerificationStore
from .image_archive import (
    inspect_archive,
    load_command as image_load_command,
    save_command as image_save_command,
    selected_image_references,
    validate_managed_archive,
)
from .layout import StorageLayout
from .benchmark import (
    BenchmarkOptions,
    DEFAULT_BENCHMARK_PORT,
    render_suite_html,
    render_suite_markdown,
    run_benchmark,
    run_benchmark_suite,
)
from .agent_evaluation import (
    AgentEvaluationOptions,
    DEFAULT_CONTEXT as AGENT_EVALUATION_DEFAULT_CONTEXT,
    load_coding_suite,
    run_agent_evaluation,
)
from .agent_models import (
    agent_sampling_parameters,
    is_agent_capable,
    reasoning_client_default,
    reasoning_native_value,
)
from .build import (
    build_cache_dir,
    build_command,
    prepare_pip_build_cache,
)
from .cli_parser import (
    ACCEPTANCE_EXAMPLES,
    AGENT_EXAMPLES,
    BENCHMARK_EXAMPLES,
    BUILD_EXAMPLES,
    BUILD_TARGET_DESCRIPTIONS,
    BUILD_TARGETS,
    CLEANUP_EXAMPLES,
    CONTENT_APPLICATION_RECIPES,
    CONTENT_APPLICATIONS,
    CONTENT_FAMILIES,
    CONTENT_EXAMPLES,
    DWARFSTAR_RUN_EXAMPLES,
    EXACT_BUNDLE_CATEGORIES,
    IMAGE_EXAMPLES,
    LLAMA_RUN_EXAMPLES,
    LOG_EXAMPLES,
    RUN_EXAMPLES,
    SHELL_EXAMPLES,
    STOP_EXAMPLES,
    WORKFLOW_EXAMPLES,
    parse_arguments,
)
from .llama_benchmark import (
    run_llama_benchmark,
    write_backend_comparison,
)
from .llama_speculative_benchmark import (
    CONTAINER_NAME as LLAMA_SPECULATIVE_BENCHMARK_CONTAINER_NAME,
    DEFAULT_CONTEXT_DEPTHS as LLAMA_SPECULATIVE_CONTEXT_DEPTHS,
    SpeculativeBenchmarkOptions,
    run_speculative_benchmark,
)
from .model_inventory import LlamaModel, llama_models
from .opencode import (
    create_launch_plan as create_opencode_launch_plan,
    create_sandbox_plan as create_opencode_sandbox_plan,
    launch_environment as opencode_launch_environment,
    passthrough_command as opencode_passthrough_command,
    prepare_sandbox_paths as prepare_opencode_sandbox_paths,
    sandbox_paths as opencode_sandbox_paths,
)
from .pi_agent import (
    create_launch_plan as create_pi_launch_plan,
    create_sandbox_plan as create_pi_sandbox_plan,
    launch_environment as pi_launch_environment,
    prepare_state as prepare_pi_state,
    sandbox_paths as pi_sandbox_paths,
)
from .pi_runtime import install_pi_runtime
from .omp_agent import (
    create_launch_plan as create_omp_launch_plan,
    create_sandbox_plan as create_omp_sandbox_plan,
    launch_environment as omp_launch_environment,
    prepare_state as prepare_omp_state,
    sandbox_paths as omp_sandbox_paths,
)
from .maki_agent import (
    create_launch_plan as create_maki_launch_plan,
    create_sandbox_plan as create_maki_sandbox_plan,
    launch_environment as maki_launch_environment,
    prepare_state as prepare_maki_state,
    sandbox_paths as maki_sandbox_paths,
)
from .runtime.diagnostic import (
    gpu_diagnostic_command,
    parse_gpu_diagnostic_output,
)
from .runtime.dwarfstar import DwarfStarOptions, dwarfstar_command
from .runtime.llama import (
    LlamaBenchmarkOptions,
    LlamaOptions,
    llama_benchmark_command,
    llama_command,
)
from .runtime.shell import shell_command
from .runtime.web import WebOptions, web_command
from .recipes import (
    application_recipes,
    content_recipe,
    recipe_bundles,
)
from .project import PROJECT_ROOT
from .remote_import import (
    ImportKind,
    RemoteDiscovery,
    RemoteFile,
    RemoteImportPlan,
    automatic_file,
    automatic_kind,
    build_import_plan,
    candidate_kinds,
    civitai_version_choices,
    compatible_kinds,
    default_pack_path,
    discover_remote,
    pack_bytes,
    remote_provider,
    save_pack,
    select_file,
    select_kind,
)
from .ui import (
    ColumnSpec,
    next_actions,
    next_step,
    next_steps,
    print_columns,
    print_numbered_choices,
    prompt,
    state as format_state,
    style,
)
from .workflows import (
    install_workflow,
    workflow_destination,
    workflow_state,
)

def _print_incomplete_command(
    parser: argparse.ArgumentParser, error: str, examples: str
) -> int:
    parser.print_usage(sys.stderr)
    print(
        "{}: {}".format(
            parser.prog,
            style("error: {}".format(error), "error", sys.stderr),
        ),
        file=sys.stderr,
    )
    print(file=sys.stderr)
    print(examples, file=sys.stderr, end="")
    return 2


def inspect_data_path(requested: Path) -> Path:
    try:
        return requested.expanduser().resolve(strict=False)
    except OSError as error:
        raise LauncherError(
            "cannot resolve data directory {}: {}".format(requested, error)
        )


def prepare_data_dir(requested: Path) -> Path:
    try:
        expanded = requested.expanduser()
        expanded.mkdir(parents=True, exist_ok=True)
        return expanded.resolve(strict=True)
    except OSError as error:
        raise LauncherError(
            "cannot resolve data directory {}: {}".format(requested, error)
        )


def requested_render_nodes(
    values: Optional[Sequence[str]],
    environ: Optional[Mapping[str, str]] = None,
) -> Tuple[str, ...]:
    if values is not None:
        return tuple(values)
    env = os.environ if environ is None else environ
    plural = env.get("ROCMLETE_RENDER_NODES")
    if plural is not None:
        nodes = tuple(part.strip() for part in plural.split(","))
        if not nodes or any(not node for node in nodes):
            raise LauncherError(
                "ROCMLETE_RENDER_NODES must be a comma-separated list "
                "of exact render nodes"
            )
        return nodes
    singular = environment_value(env, "RENDER_NODE", "")
    return (singular,) if singular else ()


def select_render_nodes(requested: Sequence[str]) -> Tuple[str, ...]:
    if requested:
        selected = tuple(requested)
        if len(set(selected)) != len(selected):
            raise LauncherError("render nodes must not contain duplicates")
        for render_node in selected:
            if not re.fullmatch(r"/dev/dri/renderD[0-9]+", render_node):
                raise LauncherError(
                    "render node must look like /dev/dri/renderD128"
                )
            if not Path(render_node).exists():
                raise LauncherError(
                    "render node does not exist: {}".format(render_node)
                )
        return selected

    nodes = sorted(glob.glob("/dev/dri/renderD*"))
    if not nodes:
        raise LauncherError(
            "no /dev/dri/renderD* nodes found; use "
            "'run APPLICATION --profile cpu' for a CPU-only smoke test"
        )
    if len(nodes) > 1:
        print("Multiple render nodes found:", file=sys.stderr)
        for node in nodes:
            print("  {}".format(node), file=sys.stderr)
        raise LauncherError(
            "select an exact set with one or more --render-node options "
            "or ROCMLETE_RENDER_NODES"
        )
    return (nodes[0],)


def check_device_access(device: str) -> None:
    if not Path(device).exists():
        raise LauncherError("device does not exist: {}".format(device))
    if not os.access(device, os.R_OK | os.W_OK):
        raise LauncherError(
            "current user needs read and write access to {}; "
            "run './rocmplete doctor' for a persistent host fix".format(
                device
            )
        )


def check_gpu_device_access(render_nodes: Sequence[str]) -> None:
    check_device_access("/dev/kfd")
    for render_node in render_nodes:
        check_device_access(render_node)
    # On enforcing SELinux hosts ordinary device permissions are insufficient:
    # ROCr maps /dev/kfd when its first real queue is created. Refuse the known
    # denial instead of letting ROCr turn it into a misleading application crash.
    podman.require_container_device_access()


def resolve_run_options(
    arguments: argparse.Namespace,
    environ: Optional[Mapping[str, str]] = None,
    prepare_data: bool = True,
) -> WebOptions:
    env = os.environ if environ is None else environ
    application = arguments.application
    application_spec = APPLICATIONS[application]
    if arguments.profile is not None:
        profile = arguments.profile
    else:
        profile = environment_value(env, "PROFILE", "auto")
    profile = validate_profile(profile)

    listen = validate_listen_address(
        arguments.listen
        if arguments.listen is not None
        else environment_value(env, "LISTEN", DEFAULT_LISTEN)
    )
    port_value = (
        arguments.port
        if arguments.port is not None
        else environment_value(
            env, "PORT", str(application_spec.port)
        )
    )
    port = validate_port(port_value)
    image = (
        arguments.image
        if arguments.image is not None
        else environment_value(env, "IMAGE", application_spec.image)
    )
    data_path = selected_data_dir(arguments.data_dir, env)
    data_path = (
        prepare_data_dir(data_path)
        if prepare_data
        else inspect_data_path(data_path)
    )
    if prepare_data:
        StorageLayout(data_path).prepare_runtime(application)
    render_nodes = requested_render_nodes(arguments.render_node, env)

    if application == "comfyui":
        reject_managed_comfy_args(arguments.comfy_args)
    elif arguments.comfy_args:
        raise LauncherError("arguments after -- are only supported by ComfyUI")
    if profile != "cpu":
        render_nodes = select_render_nodes(render_nodes)
        if len(render_nodes) > 1 and not application_spec.multi_gpu:
            raise LauncherError(
                "{} does not support a multi-GPU workload".format(application)
            )
        check_gpu_device_access(render_nodes)
    else:
        render_nodes = ()

    return WebOptions(
        image=image,
        profile=profile,
        listen=listen,
        port=port,
        data_dir=data_path,
        render_nodes=render_nodes,
        detach=arguments.detach,
        unconfined=arguments.unconfined,
        disable_bundled_extensions=arguments.disable_bundled_extensions,
        comfy_args=tuple(arguments.comfy_args),
        container_name=application_spec.container_name,
        application=application,
        memory_policy=(
            validate_memory_policy(
                arguments.memory_policy
                or environment_value(env, "MEMORY_POLICY", "balanced")
            )
            if application == "comfyui"
            else "balanced"
        ),
        kernel_policy=validate_kernel_policy(
            arguments.kernel_policy
            or environment_value(env, "KERNEL_POLICY", "default")
        ),
    )


def _pip_build_cache(
    disabled: bool,
) -> Tuple[Optional[Path], str]:
    if disabled:
        return None, ":rw"
    return prepare_pip_build_cache(), podman.selinux_volume_suffix()


def _build_content_tools(
    image: str,
    no_layer_cache: bool,
    pip_cache_dir: Optional[Path],
    volume_suffix: str,
) -> int:
    print(
        "{} {} (content download tools)".format(
            style("Building", "heading"), image
        ),
        flush=True,
    )
    return podman.run(
        build_command(
            PROJECT_ROOT,
            image,
            no_layer_cache,
            target=CONTENT_TOOLS_BUILD_TARGET,
            pip_cache_dir=pip_cache_dir,
            volume_suffix=volume_suffix,
        )
    )


def _build_pytorch_base(
    image: str,
    no_layer_cache: bool,
    pip_cache_dir: Optional[Path],
    volume_suffix: str,
) -> int:
    print(
        "{} {} (shared ROCm/PyTorch base)".format(
            style("Building", "heading"), image
        ),
        flush=True,
    )
    return podman.run(
        build_command(
            PROJECT_ROOT,
            image,
            no_layer_cache,
            target=ROCM_BASE_BUILD_TARGET,
            runtime_image=ROCM_RUNTIME_IMAGE,
            pip_cache_dir=pip_cache_dir,
            volume_suffix=volume_suffix,
        )
    )


def _build_rocm_runtime(
    image: str,
    no_layer_cache: bool,
    pip_cache_dir: Optional[Path],
    volume_suffix: str,
) -> int:
    print(
        "{} {} (shared minimal ROCm runtime)".format(
            style("Building", "heading"), image
        ),
        flush=True,
    )
    return podman.run(
        build_command(
            PROJECT_ROOT,
            image,
            no_layer_cache,
            target=ROCM_RUNTIME_BUILD_TARGET,
            pip_cache_dir=pip_cache_dir,
            volume_suffix=volume_suffix,
        )
    )


def command_build(arguments: argparse.Namespace) -> int:
    if arguments.application is None:
        if not sys.stdin.isatty():
            return _print_incomplete_command(
                arguments.command_parser,
                "choose an image target",
                BUILD_EXAMPLES,
            )
        arguments.application = _interactive_build_target()
    podman.require_rootless()
    if arguments.image and arguments.application == "all":
        raise LauncherError("--image requires one build target, not all")
    pip_cache_dir, volume_suffix = _pip_build_cache(arguments.no_cache)
    selected_no_layer_cache = (
        arguments.no_layer_cache or arguments.no_cache
    )
    if arguments.application == "content-tools":
        image = arguments.image or CONTENT_TOOLS_IMAGE
        result = _build_content_tools(
            image,
            selected_no_layer_cache,
            pip_cache_dir,
            volume_suffix,
        )
        if result != 0:
            return result
        _print_built_images((("content-tools", image),))
        next_step("./rocmplete content list")
        return 0
    if arguments.application == "base":
        image = arguments.image or ROCM_BASE_IMAGE
        result = _build_rocm_runtime(
            ROCM_RUNTIME_IMAGE,
            selected_no_layer_cache,
            pip_cache_dir,
            volume_suffix,
        )
        if result != 0:
            return result
        result = _build_pytorch_base(
            image,
            selected_no_layer_cache,
            pip_cache_dir,
            volume_suffix,
        )
        if result != 0:
            _print_built_images(
                (("runtime", ROCM_RUNTIME_IMAGE),), partial=True
            )
            return result
        _print_built_images(
            (("runtime", ROCM_RUNTIME_IMAGE), ("base", image))
        )
        doctor_command = "./rocmplete doctor"
        if arguments.image:
            doctor_command += " --image {}".format(shlex.quote(image))
        next_step(doctor_command)
        return 0
    targets = (
        BUILD_APPLICATIONS
        if arguments.application == "all"
        else (arguments.application,)
    )
    built_images = []
    # A tag proves that a prerequisite exists, not that its build-context
    # inputs still match the checkout. Always let Podman evaluate its cached
    # layers so `git pull && ./rocmplete build all` cannot retain stale code.
    prerequisite_no_layer_cache = arguments.no_cache
    result = _build_content_tools(
        CONTENT_TOOLS_IMAGE,
        prerequisite_no_layer_cache,
        pip_cache_dir,
        volume_suffix,
    )
    if result != 0:
        return result
    built_images.append(("content", CONTENT_TOOLS_IMAGE))
    result = _build_rocm_runtime(
        ROCM_RUNTIME_IMAGE,
        prerequisite_no_layer_cache,
        pip_cache_dir,
        volume_suffix,
    )
    if result != 0:
        _print_built_images(built_images, partial=True)
        return result
    built_images.append(("runtime", ROCM_RUNTIME_IMAGE))
    needs_pytorch_base = any(
        APPLICATIONS[target].shared_pytorch_base for target in targets
    )
    if needs_pytorch_base:
        result = _build_pytorch_base(
            ROCM_BASE_IMAGE,
            prerequisite_no_layer_cache,
            pip_cache_dir,
            volume_suffix,
        )
        if result != 0:
            return result
        built_images.append(("base", ROCM_BASE_IMAGE))
    for target in targets:
        application_spec = APPLICATIONS[target]
        image = arguments.image or application_spec.image
        print(
            "{} {} ({})".format(
                style("Building", "heading"), image, target
            ),
            flush=True,
        )
        result = podman.run(
            build_command(
                PROJECT_ROOT,
                image,
                selected_no_layer_cache,
                target=application_spec.build_target,
                base_image=(
                    ROCM_BASE_IMAGE
                    if application_spec.shared_pytorch_base
                    else None
                ),
                runtime_image=(
                    None
                    if application_spec.shared_pytorch_base
                    else ROCM_RUNTIME_IMAGE
                ),
                pip_cache_dir=pip_cache_dir,
                volume_suffix=volume_suffix,
            )
        )
        if result != 0:
            _print_built_images(built_images, partial=True)
            return result
        built_images.append((target, image))
    _print_built_images(built_images)
    if arguments.application == "all":
        next_actions(
            (
                (
                    "./rocmplete content install",
                    "Choose and install content interactively.",
                ),
                (
                    "./rocmplete content list",
                    "Or inspect available selections without installing.",
                ),
            )
        )
    else:
        next_step(
            APPLICATIONS[arguments.application].after_build
        )
    return 0


def _resolved_archive_output(value: str) -> Path:
    output = Path(value).expanduser().resolve()
    if output.exists() or output.is_symlink():
        raise LauncherError(
            "image archive output already exists: {}".format(output)
        )
    if not output.parent.is_dir():
        raise LauncherError(
            "image archive parent is not a directory: {}".format(
                output.parent
            )
        )
    if not os.access(str(output.parent), os.W_OK | os.X_OK):
        raise LauncherError(
            "image archive parent is not writable: {}".format(output.parent)
        )
    return output


def _print_image_archive_plan(
    heading: str,
    archive: Path,
    images: Sequence[Tuple[str, str]],
) -> None:
    print(style(heading, "heading"))
    for reference, state in images:
        print(
            "  {}  {}".format(
                format_state(state, 12),
                reference,
            )
        )
    print("{} {}".format(style("Archive:", "label"), archive))


def _command_images_export(arguments: argparse.Namespace) -> int:
    output = _resolved_archive_output(arguments.output)
    references = selected_image_references(arguments.target)
    podman.require_rootless()
    missing = [
        reference
        for reference in references
        if not podman.image_exists(reference)
    ]
    if missing:
        raise LauncherError(
            "cannot export missing images: {}; build {} first".format(
                ", ".join(missing), arguments.target
            )
        )
    local_ids = {
        reference: podman.image_id(reference) for reference in references
    }
    _print_image_archive_plan(
        "Image export:",
        output,
        tuple((reference, "ready") for reference in references),
    )
    if arguments.dry_run:
        print(
            "{} {}".format(
                style("Command:", "label"),
                shlex.join(
                    image_save_command(references, Path("<temporary-archive>"))
                ),
            )
        )
        return 0

    partial = output.parent / ".{}.partial-{}".format(
        output.name, uuid.uuid4().hex
    )
    try:
        result = podman.run(
            list(image_save_command(references, partial))
        )
        if result != 0:
            return result
        archive = inspect_archive(partial)
        validate_managed_archive(archive, references)
        archived_ids = {
            image.reference: image.image_id for image in archive.images
        }
        changed = [
            reference
            for reference in references
            if archived_ids.get(reference) != local_ids[reference]
        ]
        if changed:
            raise LauncherError(
                "exported image identity changed while saving: {}".format(
                    ", ".join(changed)
                )
            )
        reserved_output = False
        try:
            descriptor = os.open(
                str(output),
                os.O_CREAT | os.O_EXCL | os.O_WRONLY,
                0o600,
            )
            os.close(descriptor)
            reserved_output = True
            os.replace(str(partial), str(output))
            reserved_output = False
        except FileExistsError:
            raise LauncherError(
                "image archive output appeared during export: {}".format(
                    output
                )
            )
        except OSError as error:
            if reserved_output:
                try:
                    output.unlink()
                except OSError:
                    pass
            raise LauncherError(
                "cannot finalize image archive {}: {}".format(output, error)
            )
    finally:
        try:
            partial.unlink()
        except FileNotFoundError:
            pass
        except OSError as error:
            print(
                style(
                    "WARNING: cannot remove partial image archive {}: {}".format(
                        partial, error
                    ),
                    "warning",
                    sys.stderr,
                ),
                file=sys.stderr,
            )
    print()
    print(
        "{} {} ({})".format(
            style("Exported:", "success"),
            output,
            human_size(output.stat().st_size),
        )
    )
    return 0


def _command_images_import(arguments: argparse.Namespace) -> int:
    path = Path(arguments.archive).expanduser().resolve()
    archive = inspect_archive(path)
    validate_managed_archive(archive)
    podman.require_rootless()
    states = []
    conflicts = []
    missing = []
    for image in archive.images:
        if not podman.image_exists(image.reference):
            state = "missing"
            missing.append(image.reference)
        else:
            local_id = podman.image_id(image.reference)
            if local_id == image.image_id:
                state = "identical"
            else:
                state = "conflict"
                conflicts.append(
                    (image.reference, local_id, image.image_id)
                )
        states.append((image.reference, state))
    _print_image_archive_plan("Image import:", path, tuple(states))
    print("{} {}".format(style("Size:", "label"), human_size(archive.size)))
    if conflicts:
        references = ", ".join(item[0] for item in conflicts)
        raise LauncherError(
            "current tags refer to different images: {}; remove each exact "
            "tag with './rocmplete cleanup images --image-tag TAG' before "
            "importing".format(references)
        )
    if not missing:
        print()
        print(style("All archived images are already present.", "success"))
        return 0
    if arguments.dry_run:
        print(
            "{} {}".format(
                style("Command:", "label"),
                shlex.join(image_load_command(path)),
            )
        )
        return 0
    try:
        result = podman.run(list(image_load_command(path)))
    except KeyboardInterrupt:
        ready = [
            image.reference
            for image in archive.images
            if podman.image_exists(image.reference)
            and podman.image_id(image.reference) == image.image_id
        ]
        if ready:
            print()
            print(style("Imported before interruption:", "warning"))
            for reference in ready:
                print("  {}".format(reference))
        raise
    if result != 0:
        ready = [
            image.reference
            for image in archive.images
            if podman.image_exists(image.reference)
            and podman.image_id(image.reference) == image.image_id
        ]
        if ready:
            print()
            print(style("Imported before failure:", "warning"))
            for reference in ready:
                print("  {}".format(reference))
        return result
    invalid = []
    for image in archive.images:
        if (
            not podman.image_exists(image.reference)
            or podman.image_id(image.reference) != image.image_id
        ):
            invalid.append(image.reference)
    if invalid:
        raise LauncherError(
            "Podman load completed but image verification failed for: {}".format(
                ", ".join(invalid)
            )
        )
    print()
    print(
        style(
            "Imported {} managed images.".format(len(archive.images)),
            "success",
        )
    )
    return 0


def command_images(arguments: argparse.Namespace) -> int:
    if arguments.images_command is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose export or import",
            IMAGE_EXAMPLES,
        )
    if arguments.images_command == "export":
        return _command_images_export(arguments)
    return _command_images_import(arguments)


def command_opencode(
    arguments: argparse.Namespace, catalog: Optional[Catalog] = None
) -> int:
    env = os.environ
    passthrough = opencode_passthrough_command(
        arguments.opencode_arguments, env
    )
    if passthrough is not None:
        try:
            os.execvpe(passthrough[0], list(passthrough), dict(env))
        except OSError as error:
            raise LauncherError("cannot start OpenCode: {}".format(error))
        return 0
    port_value = arguments.port or environment_value(
        env, "OPENCODE_PORT", "8080"
    )
    dwarfstar_port_value = arguments.dwarfstar_port or environment_value(
        env, "OPENCODE_DWARFSTAR_PORT", "8000"
    )
    data_dir = _content_data_dir(arguments.data_dir, prepare=False)
    plan = create_opencode_launch_plan(
        catalog or load_catalog(),
        data_dir,
        validate_port(port_value),
        arguments.opencode_arguments,
        env,
        dwarfstar_port=validate_port(dwarfstar_port_value),
    )
    if arguments.sandbox:
        paths = opencode_sandbox_paths(data_dir)
        sandbox = create_opencode_sandbox_plan(
            plan, data_dir, Path.cwd(), env
        )
        prepare_opencode_sandbox_paths(paths, data_dir)
        print("OpenCode sandbox", file=sys.stderr)
        print(
            "  Writable project  {}".format(sandbox.workdir),
            file=sys.stderr,
        )
        print(
            "  Private state     {}".format(sandbox.state_root),
            file=sys.stderr,
        )
        print(
            "  Network           host network retained for {} and {}".format(
                plan.endpoint, plan.dwarfstar_endpoint
            ),
            file=sys.stderr,
        )
        command = sandbox.command
        child = sandbox.environment
    else:
        command = plan.command
        child = opencode_launch_environment(plan, env)
    try:
        os.execvpe(
            command[0],
            list(command),
            dict(child),
        )
    except OSError as error:
        raise LauncherError("cannot start OpenCode: {}".format(error))
    return 0


def command_pi(
    arguments: argparse.Namespace, catalog: Optional[Catalog] = None
) -> int:
    env = os.environ
    if arguments.llama_url is not None:
        llama_url = arguments.llama_url
    elif arguments.port is not None:
        # An explicit local port overrides a remote URL inherited from the
        # environment, matching the normal command-line precedence rule.
        llama_url = None
    else:
        llama_url = environment_value(env, "PI_LLAMA_URL")
    port_value = arguments.port or environment_value(env, "PI_PORT", "8080")
    dwarfstar_port_value = arguments.dwarfstar_port or environment_value(
        env, "PI_DWARFSTAR_PORT", "8000"
    )
    data_dir = _content_data_dir(arguments.data_dir, prepare=False)
    plan = create_pi_launch_plan(
        catalog or load_catalog(),
        data_dir,
        validate_port(port_value),
        arguments.pi_arguments,
        env,
        dwarfstar_port=validate_port(dwarfstar_port_value),
        llama_url=llama_url,
    )
    if plan.mode == "passthrough":
        try:
            os.execvpe(plan.command[0], list(plan.command), dict(env))
        except OSError as error:
            raise LauncherError("cannot start Pi: {}".format(error))
        return 0
    if plan.mode == "management" or (
        plan.mode == "session" and plan.remote_llama
    ):
        # A remote-only client has no local content installation to have
        # created DATA_DIR, but Pi still needs its private managed state.
        data_dir = prepare_data_dir(data_dir)
    paths = pi_sandbox_paths(data_dir)
    agent_dir = prepare_pi_state(plan, paths, data_dir)
    if plan.mode == "session" and plan.remote_llama:
        print("Pi remote model server", file=sys.stderr)
        print("  llama.cpp        {}".format(plan.endpoint), file=sys.stderr)
        warning = "prompts and tool results use an unauthenticated"
        if plan.endpoint.startswith("http://"):
            warning += ", unencrypted connection"
        else:
            warning += " connection"
        print(
            "{} {}. Trust the remote host and network boundary.".format(
                style("WARNING:", "warning", sys.stderr), warning
            ),
            file=sys.stderr,
        )
    if arguments.sandbox:
        sandbox = create_pi_sandbox_plan(plan, data_dir, Path.cwd(), env)
        print("Pi sandbox", file=sys.stderr)
        print(
            "  Writable project  {}".format(sandbox.workdir),
            file=sys.stderr,
        )
        print(
            "  Private state     {}".format(sandbox.state_root),
            file=sys.stderr,
        )
        if plan.mode == "session":
            network = "host network retained for {} and {}".format(
                plan.endpoint, plan.dwarfstar_endpoint
            )
        else:
            network = "host network retained for explicit Pi command"
        print("  Network           {}".format(network), file=sys.stderr)
        command = sandbox.command
        child = sandbox.environment
    else:
        command = plan.command
        child = pi_launch_environment(
            agent_dir, env, offline=plan.mode == "session"
        )
    try:
        os.execvpe(command[0], list(command), dict(child))
    except OSError as error:
        raise LauncherError("cannot start Pi: {}".format(error))
    return 0


def command_omp(
    arguments: argparse.Namespace, catalog: Optional[Catalog] = None
) -> int:
    env = os.environ
    port_value = arguments.port or environment_value(env, "OMP_PORT", "8080")
    dwarfstar_port_value = arguments.dwarfstar_port or environment_value(
        env, "OMP_DWARFSTAR_PORT", "8000"
    )
    data_dir = _content_data_dir(arguments.data_dir, prepare=False)
    plan = create_omp_launch_plan(
        catalog or load_catalog(),
        data_dir,
        validate_port(port_value),
        arguments.omp_arguments,
        env,
        dwarfstar_port=validate_port(dwarfstar_port_value),
    )
    if plan.mode == "passthrough":
        try:
            os.execvpe(plan.command[0], list(plan.command), dict(env))
        except OSError as error:
            raise LauncherError("cannot start OMP: {}".format(error))
        return 0

    data_dir = prepare_data_dir(data_dir)
    paths = omp_sandbox_paths(data_dir)
    prepare_omp_state(plan, paths, data_dir)
    if arguments.sandbox:
        sandbox = create_omp_sandbox_plan(
            plan, data_dir, Path.cwd(), env
        )
        print("OMP sandbox", file=sys.stderr)
        print(
            "  Writable project  {}".format(sandbox.workdir),
            file=sys.stderr,
        )
        print(
            "  Private state     {}".format(sandbox.state_root),
            file=sys.stderr,
        )
        if plan.mode == "session":
            network = "host network retained for {} and {}".format(
                plan.endpoint, plan.dwarfstar_endpoint
            )
        else:
            network = "host network retained for explicit OMP command"
        print("  Network           {}".format(network), file=sys.stderr)
        command = sandbox.command
        child = sandbox.environment
    else:
        command = plan.command
        child = omp_launch_environment(paths, env)
    try:
        os.execvpe(command[0], list(command), dict(child))
    except OSError as error:
        raise LauncherError("cannot start OMP: {}".format(error))
    return 0


def command_maki(
    arguments: argparse.Namespace, catalog: Optional[Catalog] = None
) -> int:
    env = os.environ
    port_value = arguments.port or environment_value(env, "MAKI_PORT", "8080")
    dwarfstar_port_value = arguments.dwarfstar_port or environment_value(
        env, "MAKI_DWARFSTAR_PORT", "8000"
    )
    data_dir = _content_data_dir(arguments.data_dir, prepare=False)
    plan = create_maki_launch_plan(
        catalog or load_catalog(),
        data_dir,
        validate_port(port_value),
        arguments.maki_arguments,
        env,
        dwarfstar_port=validate_port(dwarfstar_port_value),
    )
    if plan.mode == "passthrough":
        try:
            os.execvpe(plan.command[0], list(plan.command), dict(env))
        except OSError as error:
            raise LauncherError("cannot start Maki: {}".format(error))
        return 0

    data_dir = prepare_data_dir(data_dir)
    paths = maki_sandbox_paths(data_dir)
    prepare_maki_state(plan, paths, data_dir)
    if arguments.sandbox:
        sandbox = create_maki_sandbox_plan(
            plan, data_dir, Path.cwd(), env
        )
        print("Maki sandbox", file=sys.stderr)
        print(
            "  Writable project  {}".format(sandbox.workdir),
            file=sys.stderr,
        )
        print(
            "  Private state     {}".format(sandbox.state_root),
            file=sys.stderr,
        )
        if plan.mode == "session":
            network = "host network retained for {} and {}".format(
                plan.endpoint, plan.dwarfstar_endpoint
            )
        else:
            network = "host network retained for explicit Maki command"
        print("  Network           {}".format(network), file=sys.stderr)
        command = sandbox.command
        child = sandbox.environment
    else:
        command = plan.command
        child = maki_launch_environment(paths, env)
    try:
        os.execvpe(command[0], list(command), dict(child))
    except OSError as error:
        raise LauncherError("cannot start Maki: {}".format(error))
    return 0


def command_agent(arguments: argparse.Namespace) -> int:
    if arguments.agent_client is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose install, opencode, pi, omp, or maki",
            AGENT_EXAMPLES,
        )
    if arguments.agent_client == "install":
        if arguments.managed_agent_client is None:
            return _print_incomplete_command(
                arguments.command_parser,
                "choose pi",
                AGENT_EXAMPLES,
            )
        data_dir = prepare_data_dir(
            _content_data_dir(arguments.data_dir, prepare=False)
        )
        result = install_pi_runtime(data_dir, os.environ)
        if result.installed:
            state = "Installed"
        else:
            state = "Already installed"
        print(
            "{} Pi {}".format(
                style(state + ":", "heading"),
                result.runtime.package_version,
            )
        )
        print(
            "{} {}".format(
                style("Runtime:", "label"), result.runtime.root
            )
        )
        print(
            "{} {} ({})".format(
                style("System Node.js:", "label"),
                result.runtime.node_version,
                result.runtime.node,
            )
        )
        return 0
    if arguments.agent_client == "opencode":
        return command_opencode(arguments)
    if arguments.agent_client == "pi":
        return command_pi(arguments)
    if arguments.agent_client == "omp":
        return command_omp(arguments)
    return command_maki(arguments)


def _interactive_build_target() -> str:
    if not sys.stdin.isatty():
        raise LauncherError(
            "build requires TARGET in noninteractive use"
        )
    targets = ("all",) + BUILD_TARGETS
    print(style("Build targets:", "heading"))
    print_numbered_choices(
        (
            (target, BUILD_TARGET_DESCRIPTIONS[target])
            for target in targets
        )
    )
    try:
        raw = input(
            prompt("Choose build target [1-{}]: ".format(len(targets)))
        )
    except EOFError:
        raw = ""
    try:
        choice = int(raw)
    except ValueError:
        raise LauncherError("build selection must be a menu number")
    if not 1 <= choice <= len(targets):
        raise LauncherError("build selection is out of range")
    return targets[choice - 1]


def _print_built_images(
    images: Sequence[Tuple[str, str]], partial: bool = False
) -> None:
    if not images:
        return
    print()
    if partial:
        heading = "Built before failure:"
        role = "warning"
    else:
        heading = "Built image:" if len(images) == 1 else "Built images:"
        role = "success"
    print(style(heading, role))
    target_width = max(len(target) for target, _ in images)
    for target, image in images:
        print(
            "  {}  {}".format(
                style(
                    "{:<{}}".format(target, target_width),
                    "command",
                ),
                image,
            )
        )


def _browser_url(listen: str, port: int) -> str:
    host = "127.0.0.1" if listen in ("0.0.0.0", "::") else listen
    if ":" in host and not host.startswith("["):
        host = "[{}]".format(host)
    return "http://{}:{}".format(host, port)


def command_run(arguments: argparse.Namespace) -> int:
    if arguments.application is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose an application",
            RUN_EXAMPLES,
        )
    if arguments.application == "llama-cpp":
        if arguments.mode is None:
            return _print_incomplete_command(
                arguments.application_parser,
                "choose server or cli",
                LLAMA_RUN_EXAMPLES,
            )
        if arguments.comfy_args:
            raise LauncherError("arguments after -- are only supported by ComfyUI")
        return command_llama(arguments, load_catalog())
    if arguments.application == "dwarfstar":
        if arguments.mode is None:
            return _print_incomplete_command(
                arguments.application_parser,
                "choose server or cli",
                DWARFSTAR_RUN_EXAMPLES,
            )
        if arguments.comfy_args:
            raise LauncherError("arguments after -- are only supported by ComfyUI")
        return command_dwarfstar(arguments, load_catalog())
    podman.require_rootless()
    options = resolve_run_options(
        arguments, prepare_data=not arguments.dry_run
    )
    command = web_command(options, podman.selinux_volume_suffix())
    if not is_loopback_address(options.listen):
        print(
            "{} {} is published on {}:{} "
            "without authentication.".format(
                style("WARNING:", "warning"),
                options.application,
                options.listen,
                options.port,
            ),
            flush=True,
        )
    print(
        "{} {}".format(
            style("Application data:", "label"),
            StorageLayout(options.data_dir).application(options.application),
        ),
        flush=True,
    )
    print(
        "{} {}".format(
            style("Managed content:", "label"),
            StorageLayout(options.data_dir).root / "content",
        ),
        flush=True,
    )

    if arguments.dry_run:
        print(
            "{}\n  {}".format(
                style("Resolved command:", "heading"),
                style(shlex.join(command), "command"),
            )
        )
        return 0

    if not podman.image_exists(options.image):
        if arguments.image:
            raise LauncherError("image not found: {}".format(options.image))
        raise LauncherError(
            "image not found: {}\n"
            "  Build image:    ./rocmplete build {}\n"
            "  Install content: {}".format(
                options.image,
                options.application,
                APPLICATIONS[options.application].after_build,
            )
        )
    if podman.container_exists(options.container_name):
        raise LauncherError(
            "container {!r} already exists.\n"
            "  Logs: ./rocmplete logs {}\n"
            "  Stop: ./rocmplete stop {}".format(
                options.container_name,
                options.application,
                options.application,
            )
        )
    print(
        "{} {}".format(
            style("Open:", "heading"),
            style(
                _browser_url(options.listen, options.port),
                "command",
            ),
        )
    )
    print(
        "{} {}".format(
            style("Logs:", "label"),
            style(
                "./rocmplete logs {}".format(options.application),
                "command",
            ),
        )
    )
    print(
        "{} {}".format(
            style("Stop:", "label"),
            style(
                "./rocmplete stop {}".format(options.application),
                "command",
            ),
        )
    )
    if options.detach:
        return podman.run(command)
    return podman.run_managed_foreground(
        command,
        options.container_name,
        "{} failed".format(options.application),
    )


_TTM_MODULE_NAMES = ("amdttm", "amd_ttm", "ttm")
_PAGES_PER_GIB = (1024**3) // 4096
_DOCTOR_FIELD_WIDTH = 14


def _print_doctor_section(title: str, leading_blank: bool = True) -> None:
    prefix = "\n" if leading_blank else ""
    print("{}{}".format(prefix, style(title, "heading")))


def _print_doctor_field(label: str, value: object) -> None:
    padded = "{:<{}}".format(label, _DOCTOR_FIELD_WIDTH)
    print("  {} {}".format(style(padded, "label"), value))


def _parse_gpu_diagnostic_output(output: str) -> Mapping[str, str]:
    return parse_gpu_diagnostic_output(output)


def _read_positive_integer(path: Path) -> Optional[int]:
    try:
        value = path.read_text().strip()
    except OSError:
        return None
    if not value.isdigit():
        return None
    parsed = int(value)
    return parsed if parsed > 0 else None


def _read_ttm_state(
    module_root: Path = Path("/sys/module"),
) -> Optional[Tuple[str, int, Optional[int]]]:
    """Return the active TTM module, allocation limit, and page-pool limit."""
    for module in _TTM_MODULE_NAMES:
        parameters = module_root / module / "parameters"
        pages = _read_positive_integer(parameters / "pages_limit")
        if pages is not None:
            pool_pages = _read_positive_integer(
                parameters / "page_pool_size"
            )
            return module, pages, pool_pages
    return None


def _read_system_memory_bytes(
    meminfo: Path = Path("/proc/meminfo"),
) -> Optional[int]:
    try:
        lines = meminfo.read_text().splitlines()
    except OSError:
        return None
    for line in lines:
        match = re.fullmatch(r"MemTotal:\s+([0-9]+)\s+kB", line)
        if match:
            return int(match.group(1)) * 1024
    return None


def _read_gtt_total_bytes(render_node: str) -> Optional[int]:
    path = (
        Path("/sys/class/drm")
        / Path(render_node).name
        / "device"
        / "mem_info_gtt_total"
    )
    return _read_positive_integer(path)


def _rdna35_ttm_target_gib(system_memory_bytes: int) -> Optional[int]:
    """Choose documented starting points with tolerance for reserved RAM."""
    system_gib = system_memory_bytes / float(1024**3)
    for minimum_gib, target_gib in (
        (120, 112),
        (112, 100),
        (56, 48),
        (40, 32),
    ):
        if system_gib >= minimum_gib:
            return target_gib
    return None


def _rdna35_kernel_parameters(
    module: str,
    target_gib: int,
) -> Tuple[str, ...]:
    target_pages = target_gib * _PAGES_PER_GIB
    parameters = []
    if target_gib >= 112:
        parameters.append(
            "amdgpu.gttsize={}".format(target_gib * 1024)
        )
    parameters.append("{}.pages_limit={}".format(module, target_pages))
    if target_gib >= 112:
        parameters.append(
            "{}.page_pool_size={}".format(module, target_pages)
        )
    return tuple(parameters)


def _rdna35_module_options(
    module: str,
    target_gib: int,
) -> Tuple[str, ...]:
    target_pages = target_gib * _PAGES_PER_GIB
    if target_gib >= 112:
        return (
            "options amdgpu gttsize={}".format(target_gib * 1024),
            "options {} pages_limit={} page_pool_size={}".format(
                module,
                target_pages,
                target_pages,
            ),
        )
    return (
        "options {} pages_limit={}".format(module, target_pages),
    )


def _initramfs_refresh_command() -> Optional[str]:
    for executable, command in (
        ("update-initramfs", "sudo update-initramfs -u"),
        ("dracut", "sudo dracut --force"),
        ("mkinitcpio", "sudo mkinitcpio -P"),
    ):
        if shutil.which(executable):
            return command
    return None


def _uses_rpm_ostree_boot(
    ostree_booted: Path = Path("/run/ostree-booted"),
) -> bool:
    """Require both an OSTree boot marker and the transactional host tool."""
    return ostree_booted.exists() and shutil.which("rpm-ostree") is not None


def _uses_grub_drop_in(
    config_dir: Path = Path("/etc/default/grub.d"),
) -> bool:
    """Use a separate owned drop-in only when the host supports updating it."""
    return config_dir.is_dir() and shutil.which("update-grub") is not None


def _uses_grubby() -> bool:
    """Use the Fedora/RHEL boot-entry editor when it is installed."""
    return shutil.which("grubby") is not None


def _print_rdna35_memory_guidance(
    render_node: str,
    ttm_state: Optional[Tuple[str, int, Optional[int]]],
    platform_name: str,
) -> None:
    system_bytes = _read_system_memory_bytes()
    gtt_bytes = _read_gtt_total_bytes(render_node)

    _print_doctor_section("{} shared memory".format(platform_name))
    if system_bytes is not None:
        _print_doctor_field(
            "System RAM",
            "{:.2f} GiB".format(system_bytes / 1024**3),
        )
    if ttm_state is not None:
        module, pages, pool_pages = ttm_state
        _print_doctor_field(
            "TTM ceiling",
            "{:.2f} GiB ({}; {} pages)".format(
                pages / _PAGES_PER_GIB,
                module,
                pages,
            ),
        )
        if pool_pages is not None:
            _print_doctor_field(
                "TTM pool",
                "{:.2f} GiB ({} pages)".format(
                    pool_pages / _PAGES_PER_GIB,
                    pool_pages,
                ),
            )
    if gtt_bytes is not None:
        _print_doctor_field(
            "GTT ready",
            "{:.2f} GiB".format(gtt_bytes / 1024**3),
        )

    if system_bytes is None:
        _print_doctor_field(
            "Status",
            style(
                "could not read total system RAM; see README hardware tuning",
                "warning",
            ),
        )
        return
    target_gib = _rdna35_ttm_target_gib(system_bytes)
    if target_gib is None:
        _print_doctor_field(
            "Status",
            style(
                "no automatic TTM starting point is defined for this RAM "
                "size; see README hardware tuning",
                "info",
            ),
        )
        return
    if ttm_state is None:
        _print_doctor_field(
            "Status",
            style(
                "could not identify the active TTM pages_limit parameter; "
                "see README hardware tuning",
                "warning",
            ),
        )
        return

    module, pages, pool_pages = ttm_state
    target_pages = target_gib * _PAGES_PER_GIB
    parameters = _rdna35_kernel_parameters(module, target_gib)
    # The initialized GTT manager is authoritative after a live sysfs write.
    effective_gib = (
        gtt_bytes / 1024**3 if gtt_bytes is not None else pages / _PAGES_PER_GIB
    )
    pool_ready = (
        target_gib < 112
        or (
            pool_pages is not None
            and pool_pages >= target_pages
        )
    )
    if effective_gib >= target_gib and pool_ready:
        _print_doctor_field(
            "Status",
            style(
                "meets the {} GiB starting point".format(target_gib),
                "success",
            ),
        )
        return

    _print_doctor_field(
        "Status",
        style(
            "effective GTT or TTM pool is below the {} GiB "
            "starting point".format(target_gib),
            "warning",
        ),
    )
    print(
        "\n{} administrator access and a reboot are required:".format(
            style("Host action:", "heading")
        )
    )
    if _uses_rpm_ostree_boot():
        active_parameters = [
            "{}.pages_limit={}".format(module, pages)
        ]
        if pool_pages is not None:
            active_parameters.append(
                "{}.page_pool_size={}".format(module, pool_pages)
            )
        karg_operations = [
            "--delete-if-present '{}'".format(parameter)
            for parameter in active_parameters
        ] + [
            "--append-if-missing '{}'".format(parameter)
            for parameter in parameters
        ]
        print(
            "  Replace the detected active TTM values with the {} GiB "
            "starting point:".format(target_gib)
        )
        print(
            "    {}".format(
                style(
                    "sudo rpm-ostree kargs \\\n      {}".format(
                        " \\\n      ".join(karg_operations)
                    ),
                    "command",
                )
            )
        )
    elif _uses_grub_drop_in():
        config = (
            'GRUB_CMDLINE_LINUX_DEFAULT="${{GRUB_CMDLINE_LINUX_DEFAULT}} '
            '{}"'.format(" ".join(parameters))
        )
        print(
            "  {}".format(
                style(
                    "printf '%s\\n' '{}' | sudo tee "
                    "/etc/default/grub.d/70-rocmplete-ttm.cfg".format(
                        config
                    ),
                    "command",
                )
            )
        )
        print("  {}".format(style("sudo update-grub", "command")))
    elif _uses_grubby():
        parameter_names = " ".join(
            parameter.partition("=")[0] for parameter in parameters
        )
        print(
            "  {}".format(
                style(
                    "sudo grubby --update-kernel=ALL \\\n"
                    "    --remove-args='{}' \\\n"
                    "    --args='{}'".format(
                        parameter_names,
                        " ".join(parameters),
                    ),
                    "command",
                )
            )
        )
    else:
        options = _rdna35_module_options(module, target_gib)
        quoted_options = " ".join(
            "'{}'".format(option) for option in options
        )
        print(
            "  {}".format(
                style(
                    "printf '%s\\n' {} | sudo tee "
                    "/etc/modprobe.d/rocmplete-ttm.conf".format(
                        quoted_options
                    ),
                    "command",
                )
            )
        )
        refresh = _initramfs_refresh_command()
        if refresh:
            print("  {}".format(style(refresh, "command")))
        else:
            print(
                "  {}".format(
                    style(
                        "Rebuild the initramfs with the host distribution's "
                        "tool.",
                        "warning",
                    )
                )
            )
    print("  {}".format(style("sudo reboot", "command")))
    print(
        "\n{} These are dynamic GPU-mapping and page-pool ceilings, not "
        "reserved memory.".format(style("Note:", "label"))
    )


def _print_doctor_devices(
    kfd: Path = Path("/dev/kfd"),
    render_nodes: Optional[Sequence[Path]] = None,
) -> None:
    """Report required GPU-device absence as clearly as access state."""
    if render_nodes is None:
        render_nodes = tuple(
            Path(item) for item in sorted(glob.glob("/dev/dri/renderD*"))
        )
    devices = (kfd,) + tuple(render_nodes)
    insufficient_access = False
    if not kfd.exists():
        _print_doctor_field(
            "KFD",
            "{} ({})".format(kfd, format_state("missing")),
        )
    for device in devices:
        if device == kfd and not device.exists():
            continue
        access = (
            "read/write"
            if os.access(str(device), os.R_OK | os.W_OK)
            else "insufficient access"
        )
        if access == "insufficient access":
            insufficient_access = True
        _print_doctor_field(
            "KFD" if device == kfd else "Render node",
            "{} ({})".format(device, format_state(access)),
        )
    if not render_nodes:
        _print_doctor_field(
            "Render node",
            "{} ({})".format("/dev/dri/renderD*", format_state("missing")),
        )
    if insufficient_access:
        _print_doctor_field(
            "Access scope",
            style(
                "the persistent rule below permits every local user",
                "warning",
            ),
        )
        _print_doctor_field(
            "Host action",
            style(
                "printf '%s\\n' "
                "'KERNEL==\"kfd\", MODE=\"0666\"' "
                "'SUBSYSTEM==\"drm\", KERNEL==\"renderD*\", "
                "MODE=\"0666\"' | sudo tee "
                "/etc/udev/rules.d/70-rocmplete-gpu.rules",
                "command",
            ),
        )
        _print_doctor_field(
            "Apply",
            style(
                "sudo udevadm control --reload-rules && "
                "sudo udevadm trigger",
                "command",
            ),
        )


def _print_doctor_selinux_device_policy() -> Optional[bool]:
    allowed = podman.selinux_container_device_access()
    if allowed is None:
        return None
    state = "allowed" if allowed else "blocked; container_use_devices is off"
    _print_doctor_field(
        "SELinux",
        style(state, "success" if allowed else "error"),
    )
    if not allowed:
        print(
            "\n{} administrator access is required:".format(
                style("Host action:", "heading")
            ),
        )
        print(
            "  {}".format(
                style(
                    podman.SELINUX_CONTAINER_DEVICE_COMMAND,
                    "command",
                )
            )
        )
    return allowed


def _print_doctor_apparmor_userns_policy(
    restriction: Path = Path(
        "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
    ),
) -> None:
    """Report Ubuntu's optional AppArmor mediation of user namespaces."""
    if not restriction.exists():
        return
    try:
        value = int(restriction.read_text().strip())
    except (OSError, ValueError):
        _print_doctor_field(
            "AppArmor",
            style("user namespace restriction state is unreadable", "warning"),
        )
        return
    if value == 0:
        _print_doctor_field(
            "AppArmor",
            style("user namespace restriction is off", "success"),
        )
        return

    _print_doctor_field(
        "AppArmor",
        style("restricts unprivileged user namespaces", "warning"),
    )
    _print_doctor_field(
        "Impact",
        style(
            "bubblewrap without a matching AppArmor profile may be blocked",
            "warning",
        ),
    )
    _print_doctor_field(
        "Host action",
        style(
            "printf '%s\\n' "
            "'kernel.apparmor_restrict_unprivileged_userns = 0' | "
            "sudo tee /etc/sysctl.d/70-rocmplete-userns.conf",
            "command",
        ),
    )
    _print_doctor_field(
        "Apply",
        style("sudo sysctl --system", "command"),
    )
    _print_doctor_field(
        "Security",
        style(
            "this disables the AppArmor restriction system-wide",
            "warning",
        ),
    )


def _strix_halo_kfd_warning(kernel_release: str) -> Optional[str]:
    kernel_version = kernel_release.split("-", 1)[0]
    if version_at_least(kernel_version, "6.18.4"):
        return None
    return (
        "kernel {}; verify gfx1151 queue/context-save backports "
        "(upstream 6.18.4+)".format(kernel_version)
    )


def command_doctor(arguments: argparse.Namespace) -> int:
    podman.require_rootless()
    env = os.environ
    data_path = inspect_data_path(selected_data_dir(arguments.data_dir, env))
    kernel_release = platform.release()

    podman_version = podman.capture(["podman", "--version"], "cannot query Podman")
    podman_prefix = "podman version "
    if podman_version.startswith(podman_prefix):
        podman_version = podman_version[len(podman_prefix) :]
    _print_doctor_section("Host", leading_blank=False)
    _print_doctor_field(
        "Podman",
        "{} ({})".format(podman_version, style("rootless", "success")),
    )
    _print_doctor_field("Kernel", kernel_release)
    if data_path.is_dir():
        data_state = (
            "writable"
            if os.access(str(data_path), os.R_OK | os.W_OK)
            else "insufficient access"
        )
    elif data_path.exists():
        data_state = "not a directory"
    else:
        parent = _nearest_existing_parent(data_path)
        data_state = (
            "not created; parent writable"
            if os.access(str(parent), os.W_OK | os.X_OK)
            else "not created; parent not writable"
        )
    _print_doctor_field(
        "Data",
        "{} ({})".format(
            data_path,
            style(
                data_state,
                "success"
                if data_state
                in ("writable", "not created; parent writable")
                else "error",
            ),
        ),
    )
    _print_doctor_apparmor_userns_policy()
    _print_doctor_section("GPU access")
    _print_doctor_devices()
    selinux_device_access = _print_doctor_selinux_device_policy()

    ttm_state = _read_ttm_state()

    candidates = (
        ROCM_BASE_IMAGE,
        APPLICATIONS["comfyui"].image,
    )
    if arguments.image is not None:
        image = arguments.image
        if not podman.image_exists(image):
            raise LauncherError(
                "GPU diagnostic image is not built: {}".format(image)
            )
    else:
        image = next(
            (
                candidate
                for candidate in candidates
                if podman.image_exists(candidate)
            ),
            None,
        )
    if image is None:
        _print_doctor_section("GPU probe")
        _print_doctor_field("Image", style("not built", "warning"))
        _print_doctor_field("Operation", style("skipped", "warning"))
        print(
            "\nThe containerized GPU probe needs a managed PyTorch image."
        )
        next_step("./rocmplete build base")
        return 0

    if selinux_device_access is False:
        raise LauncherError(
            "SELinux blocks GPU memory mapping for containers because "
            "container_use_devices is off; apply the reported Host action "
            "and retry"
        )

    render_nodes = select_render_nodes(
        requested_render_nodes(arguments.render_node, env)
    )
    check_gpu_device_access(render_nodes)
    gpu_output = podman.capture(
        gpu_diagnostic_command(image, render_nodes),
        "GPU diagnostics failed",
    )
    gpu_fields = _parse_gpu_diagnostic_output(gpu_output)
    architecture = gpu_fields["Architecture"]
    if not re.fullmatch(r"gfx[0-9]+", architecture):
        raise LauncherError(
            "GPU diagnostics returned invalid architecture: {}".format(
                architecture
            )
        )
    _print_doctor_section("GPU probe")
    _print_doctor_field("Image", image)
    _print_doctor_field("Render nodes", ", ".join(render_nodes))
    for label, source in (
        ("PyTorch", "PyTorch"),
        ("ROCm/HIP", "ROCm/HIP"),
        ("Device", "Device"),
        ("Architecture", "Architecture"),
        ("Operation", "GPU operation"),
        ("Isolation", "GPU devices"),
    ):
        value = gpu_fields[source]
        _print_doctor_field(
            label,
            (
                style(value, "success")
                if source in ("GPU operation", "GPU devices")
                else value
            ),
        )
    if architecture == "gfx1151":
        kfd_warning = _strix_halo_kfd_warning(kernel_release)
        if kfd_warning is not None:
            _print_doctor_field(
                "KFD baseline",
                style(kfd_warning, "warning"),
            )
    if architecture in ("gfx1150", "gfx1151"):
        _print_rdna35_memory_guidance(
            render_nodes[0],
            ttm_state,
            "Strix Point" if architecture == "gfx1150" else "Strix Halo",
        )
    return 0


def _nearest_existing_parent(path: Path) -> Path:
    candidate = path
    while not candidate.exists() and candidate != candidate.parent:
        candidate = candidate.parent
    return candidate


def _container_environment(name: str) -> Mapping[str, str]:
    output = podman.capture(
        [
            "podman",
            "inspect",
            "--format",
            "{{range .Config.Env}}{{println .}}{{end}}",
            name,
        ],
        "cannot inspect container {!r}".format(name),
    )
    values = {}
    for line in output.splitlines():
        key, separator, value = line.partition("=")
        if separator:
            values[key] = value
    return values


_LLAMA_RUNTIME_REPORT = "/tmp/rocmplete-llama-runtime"
_LLAMA_RUNTIME_REPORT_KEYS = frozenset(
    (
        "schema",
        "mode",
        "profile",
        "backend",
        "device",
        "backend_devices",
        "architecture",
        "gpu_count",
        "router",
        "models_max",
        "context",
        "listen",
        "host_listen",
        "port",
        "api_key",
        "unified_memory",
        "vulkan_f16_kv_contiguize",
    )
)


def _project_revision() -> str:
    try:
        revision = subprocess.run(
            ["git", "-C", str(PROJECT_ROOT), "rev-parse", "HEAD"],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        changes = subprocess.run(
            [
                "git",
                "-C",
                str(PROJECT_ROOT),
                "status",
                "--porcelain",
            ],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            check=False,
        )
    except OSError:
        return "unavailable"
    value = revision.stdout.strip()
    if revision.returncode != 0 or not re.fullmatch(r"[0-9a-f]{40}", value):
        return "unavailable"
    if changes.returncode == 0 and changes.stdout.strip():
        return "{} (dirty)".format(value)
    return value


def _parse_llama_runtime_report(contents: str) -> Mapping[str, str]:
    values = {}
    for line in contents.splitlines():
        key, separator, value = line.partition("=")
        if not separator or not key or key in values:
            raise LauncherError("invalid llama.cpp runtime report")
        values[key] = value
    unknown = sorted(set(values) - _LLAMA_RUNTIME_REPORT_KEYS)
    missing = sorted(_LLAMA_RUNTIME_REPORT_KEYS - set(values))
    if unknown or missing or values.get("schema") != "1":
        raise LauncherError("unsupported llama.cpp runtime report")
    if values["mode"] != "server":
        raise LauncherError("the managed llama.cpp container is not a server")
    if values["backend"] not in ("rocm", "vulkan"):
        raise LauncherError("invalid backend in llama.cpp runtime report")
    if values["profile"] not in ("cpu",) + GPU_PROFILES:
        raise LauncherError("invalid profile in llama.cpp runtime report")
    for key in ("router", "api_key", "unified_memory", "vulkan_f16_kv_contiguize"):
        if values[key] not in ("0", "1"):
            raise LauncherError("invalid {} in llama.cpp runtime report".format(key))
    for key in ("gpu_count", "models_max"):
        if not values[key].isdigit():
            raise LauncherError("invalid {} in llama.cpp runtime report".format(key))
    if int(values["models_max"]) < 1:
        raise LauncherError("invalid models_max in llama.cpp runtime report")
    if values["context"] and not re.fullmatch(r"[1-9][0-9]*", values["context"]):
        raise LauncherError("invalid context in llama.cpp runtime report")
    validate_listen_address(values["listen"])
    validate_listen_address(values["host_listen"])
    validate_port(values["port"])
    return values


def _llama_runtime_report(name: str) -> Mapping[str, str]:
    contents = podman.capture(
        ["podman", "exec", name, "cat", _LLAMA_RUNTIME_REPORT],
        "cannot read the live llama.cpp runtime report; retry after startup",
    )
    return _parse_llama_runtime_report(contents)


def _container_image_name(name: str) -> str:
    value = podman.capture(
        ["podman", "inspect", "--format", "{{.Config.Image}}", name],
        "cannot inspect container {!r}".format(name),
    )
    if not value or any(character in value for character in "\r\n"):
        raise LauncherError("Podman returned an invalid container image name")
    return value


def _container_image_identity(name: str) -> str:
    value = podman.capture(
        ["podman", "inspect", "--format", "{{.Image}}", name],
        "cannot inspect container {!r}".format(name),
    )
    if not value or any(character in value for character in "\r\n"):
        raise LauncherError("Podman returned an invalid container image ID")
    return value


def _image_labels(image: str) -> Mapping[str, str]:
    output = podman.capture(
        ["podman", "image", "inspect", "--format", "{{json .Labels}}", image],
        "cannot inspect image {}".format(image),
    )
    try:
        document = json.loads(output)
    except json.JSONDecodeError as error:
        raise LauncherError("Podman returned invalid image labels") from error
    if document is None:
        return {}
    if not isinstance(document, dict) or not all(
        isinstance(key, str) and isinstance(value, str)
        for key, value in document.items()
    ):
        raise LauncherError("Podman returned invalid image labels")
    return document


def _parse_llama_router_snapshot(contents: str) -> Mapping[str, Mapping[str, str]]:
    lines = contents.splitlines()
    if not lines or lines[0] != "version = 1":
        raise LauncherError("running llama.cpp router has an unsupported preset")
    sections = {}
    current = None
    for line in lines[1:]:
        if not line:
            continue
        section = re.fullmatch(r"\[([a-z0-9][a-z0-9._-]*)\]", line)
        if section:
            identifier = section.group(1)
            if identifier in sections:
                raise LauncherError(
                    "running llama.cpp router preset has duplicate models"
                )
            current = {}
            sections[identifier] = current
            continue
        key, separator, value = line.partition(" = ")
        if current is None or not separator or not key or key in current:
            raise LauncherError("running llama.cpp router preset is invalid")
        current[key] = value
    if not sections:
        raise LauncherError("running llama.cpp router has no models")
    return sections


def _llama_router_snapshot(name: str) -> Mapping[str, Mapping[str, str]]:
    contents = podman.capture(
        ["podman", "exec", name, "cat", "/run/rocmplete/models.ini"],
        "cannot read the running llama.cpp router preset",
    )
    return _parse_llama_router_snapshot(contents)


def _llama_direct_preset(
    catalog: Catalog,
    environment: Mapping[str, str],
    backend: str,
    requested: Optional[str],
) -> Optional[LlamaPreset]:
    model = environment.get("ROCMLETE_LLAMA_MODEL", "")
    draft_model = environment.get("ROCMLETE_LLAMA_DRAFT_MODEL", "")
    speculation = environment.get("ROCMLETE_LLAMA_SPECULATIVE_TYPE", "")
    draft_tokens = environment.get("ROCMLETE_LLAMA_DRAFT_TOKENS", "0")
    candidates = []
    for preset in catalog.llama_presets.values():
        artifact = catalog.artifact(preset.artifact)
        expected_model = "/content/models/{}".format(artifact.destination)
        expected_draft = ""
        if preset.draft_artifact:
            expected_draft = "/content/models/{}".format(
                catalog.artifact(preset.draft_artifact).destination
            )
        if (
            model == expected_model
            and draft_model == expected_draft
            and speculation == preset.speculative_type
            and draft_tokens == str(preset.draft_tokens_for_backend(backend))
        ):
            candidates.append(preset)
    if requested is not None:
        selected = catalog.llama_preset(requested)
        if selected not in candidates:
            raise LauncherError(
                "running llama.cpp server is not using preset {!r}".format(requested)
            )
        return selected
    if len(candidates) == 1:
        return candidates[0]
    if len(candidates) > 1:
        raise LauncherError(
            "running model matches multiple presets; select one with --model"
        )
    return None


def _llama_sampling_rows(
    catalog: Catalog, preset: Optional[LlamaPreset], raw: str
) -> Tuple[Tuple[str, str], ...]:
    if raw:
        try:
            document = json.loads(raw)
        except json.JSONDecodeError as error:
            raise LauncherError(
                "running llama.cpp sampling policy is invalid"
            ) from error
    elif preset is not None and preset.sampling_policy:
        document = catalog.llama_sampling_policy(
            preset.sampling_policy
        ).modes()
    else:
        return (("Sampling", "request values or llama.cpp defaults"),)
    if not isinstance(document, dict):
        raise LauncherError("running llama.cpp sampling policy is invalid")
    rows = []
    fields = (
        ("temperature", "temp"),
        ("top_p", "top-p"),
        ("top_k", "top-k"),
        ("min_p", "min-p"),
        ("presence_penalty", "presence"),
        ("repeat_penalty", "repeat"),
    )
    for mode, label in (
        ("thinking", "Thinking sampling"),
        ("non_thinking", "Off sampling"),
    ):
        values = document.get(mode)
        if not isinstance(values, dict) or any(
            key not in values for key, _ in fields
        ):
            raise LauncherError(
                "running llama.cpp sampling policy is invalid"
            )
        rendered = ", ".join(
            "{} {}".format(display, values[key])
            for key, display in fields
        )
        rows.append((label, rendered))
    rows.append(("Precedence", "explicit request values override these defaults"))
    return tuple(rows)


def _llama_profile_setting(
    environment: Mapping[str, str], prefix: str, profile: str
) -> str:
    key = "ROCMLETE_LLAMA_{}_{}".format(
        prefix, profile.upper().replace("-", "_")
    )
    return environment.get(key, "")


def _llama_process_command(name: str) -> Tuple[str, ...]:
    raw = podman.capture_bytes(
        ["podman", "exec", name, "cat", "/proc/1/cmdline"],
        "cannot inspect the running llama.cpp command",
    )
    try:
        command = tuple(
            part.decode("utf-8") for part in raw.rstrip(b"\0").split(b"\0")
        )
    except UnicodeDecodeError as error:
        raise LauncherError("running llama.cpp command is not UTF-8") from error
    if not command or Path(command[0]).name != "llama-server":
        raise LauncherError("running container PID 1 is not llama-server")
    redacted = list(command)
    if "--api-key-file" in redacted:
        index = redacted.index("--api-key-file")
        if index + 1 < len(redacted):
            redacted[index + 1] = "API_KEY_FILE"
    return tuple(redacted)


def _llama_reproduction_command(
    environment: Mapping[str, str],
    runtime: Mapping[str, str],
    preset: Optional[LlamaPreset],
) -> List[str]:
    command = ["./rocmplete", "run", "llama-cpp", "server"]
    if runtime["router"] == "1":
        command.extend(("--router", "--models-max", runtime["models_max"]))
    elif preset is not None:
        command.extend(("--preset", preset.identifier))
    else:
        command.extend(("--model", "GGUF_PATH"))
    command.extend(("--backend", runtime["backend"], "--profile", runtime["profile"]))
    render_nodes = tuple(
        node
        for node in environment.get("ROCMLETE_RENDER_NODES", "").split(",")
        if node
    )
    for node in render_nodes:
        command.extend(("--render-node", node))
    command.extend(("--listen", runtime["host_listen"], "--port", runtime["port"]))
    if runtime["context"]:
        command.extend(("--context", runtime["context"]))
    elif preset is not None and runtime["router"] == "0":
        command.extend(("--context", str(preset.default_context)))
    if runtime["api_key"] == "1":
        command.extend(("--api-key-file", "API_KEY_FILE"))
    return command


def _print_llama_status_section(
    title: str, rows: Sequence[Tuple[object, object]]
) -> None:
    print()
    print(style(title, "heading"))
    print_columns(
        rows,
        columns=(ColumnSpec(role="label"), ColumnSpec()),
        indent="  ",
    )


def _command_llama_status(arguments: argparse.Namespace, catalog: Catalog) -> int:
    name = APPLICATIONS["llama-cpp"].container_name
    if not podman.container_exists(name):
        raise LauncherError("container {!r} does not exist".format(name))
    state = podman.capture(
        ["podman", "inspect", "--format", "{{.State.Status}}", name],
        "cannot inspect container {!r}".format(name),
    )
    if state != "running":
        raise LauncherError("container {!r} is {}".format(name, state))
    runtime = _llama_runtime_report(name)
    environment = _container_environment(name)
    image = _container_image_name(name)
    image_identity = _container_image_identity(name)
    labels = _image_labels(image_identity)
    process_command = _llama_process_command(name)

    router_sections = None
    preset = None
    selected_section = None
    model_names = ()
    if runtime["router"] == "1":
        router_sections = _llama_router_snapshot(name)
        model_names = tuple(router_sections)
        if arguments.model:
            if arguments.model not in router_sections:
                raise LauncherError(
                    "running router does not expose model {!r}".format(
                        arguments.model
                    )
                )
            preset = catalog.llama_preset(arguments.model)
            selected_section = router_sections[arguments.model]
    else:
        preset = _llama_direct_preset(
            catalog, environment, runtime["backend"], arguments.model
        )

    print(style("ROCmplete llama.cpp runtime", "heading"))
    print_columns(
        (
            ("State", state),
            (
                "ROCmplete",
                environment.get("ROCMLETE_SOURCE_REVISION", "")
                or _project_revision(),
            ),
            ("Image", image),
            ("Image ID", image_identity),
            (
                "llama.cpp",
                labels.get("org.opencontainers.image.revision", "unknown"),
            ),
            (
                "ROCm",
                labels.get(
                    "io.github.fff7d1bc.rocmplete.rocm.version", "unknown"
                ),
            ),
        ),
        columns=(ColumnSpec(role="label"), ColumnSpec()),
        indent="  ",
    )
    hardware_rows = [
        ("Backend", runtime["backend"]),
        (
            "Profile",
            "{} (requested {})".format(
                runtime["profile"], environment["ROCMLETE_PROFILE"]
            )
            if environment.get("ROCMLETE_PROFILE")
            and environment["ROCMLETE_PROFILE"] != runtime["profile"]
            else runtime["profile"],
        ),
        ("Architecture", runtime["architecture"]),
        ("Device", runtime["device"]),
        (
            "Render nodes",
            environment.get("ROCMLETE_RENDER_NODES", "") or "none",
        ),
    ]
    if int(runtime["gpu_count"]) > 1:
        hardware_rows.append(
            (
                "GPU split",
                "layer across {} devices".format(runtime["gpu_count"]),
            )
        )
    _print_llama_status_section("Hardware", hardware_rows)

    server_rows = [
        (
            "Mode",
            "router"
            if runtime["router"] == "1"
            else "direct preset" if preset is not None else "direct model",
        ),
        ("Container bind", "{}:{}".format(runtime["listen"], runtime["port"])),
        ("Host publish", "{}:{}".format(runtime["host_listen"], runtime["port"])),
        (
            "Authentication",
            "configured (value redacted)"
            if runtime["api_key"] == "1"
            else "none",
        ),
        ("Unified memory", "on" if runtime["unified_memory"] == "1" else "off"),
        (
            "Vulkan F16 KV fix",
            "on" if runtime["vulkan_f16_kv_contiguize"] == "1" else "off",
        ),
    ]
    if runtime["router"] == "1":
        server_rows.extend(
            (
                ("Loaded-model limit", runtime["models_max"]),
                ("Configured models", len(model_names)),
            )
        )
    _print_llama_status_section("Server", server_rows)

    if runtime["router"] == "1" and preset is None:
        print()
        print(style("Router models", "heading"))
        for identifier in model_names:
            print("  {}".format(style(identifier, "command")))
        print(
            "\n{} Select one with {}".format(
                style("Details:", "label"),
                style(
                    "./rocmplete status llama-cpp --model MODEL",
                    "command",
                ),
            )
        )
    else:
        model_rows = []
        if selected_section is not None:
            model_path = selected_section.get("model", "unknown")
            context = runtime["context"] or selected_section.get(
                "c", "model metadata"
            )
            template_path = selected_section.get(
                "chat-template-file", ""
            )
            template = (
                "managed {}".format(Path(template_path).stem)
                if template_path
                else "model metadata"
            )
            reasoning_output = (
                "preserved in the API response"
                if selected_section.get("reasoning-preserve") == "true"
                else "llama.cpp default"
            )
            speculative_type = selected_section.get("spec-type", "")
            draft_tokens = selected_section.get("spec-draft-n-max", "0")
            flash_attention = selected_section.get(
                "rocmplete-flash-attn-{}".format(runtime["profile"]),
                "llama.cpp default",
            )
            kv_cache = selected_section.get(
                "rocmplete-kv-cache-{}".format(runtime["profile"]),
                "llama.cpp default",
            )
            sampling = selected_section.get(
                "sampling-defaults-by-reasoning", ""
            )
        else:
            model_path = environment.get(
                "ROCMLETE_LLAMA_MODEL", "unknown"
            )
            context = runtime["context"] or "model metadata"
            template_name = environment.get(
                "ROCMLETE_LLAMA_CHAT_TEMPLATE", ""
            )
            template = (
                "managed {}".format(template_name)
                if template_name
                else "model metadata"
            )
            reasoning_output = (
                "preserved in the API response"
                if environment.get("ROCMLETE_LLAMA_REASONING_PRESERVE")
                == "1"
                else "llama.cpp default"
            )
            speculative_type = environment.get(
                "ROCMLETE_LLAMA_SPECULATIVE_TYPE", ""
            )
            draft_tokens = environment.get(
                "ROCMLETE_LLAMA_DRAFT_TOKENS", "0"
            )
            flash_attention = _llama_profile_setting(
                environment, "FLASH_ATTN", runtime["profile"]
            ) or "llama.cpp default"
            kv_cache = _llama_profile_setting(
                environment, "KV_CACHE", runtime["profile"]
            ) or "llama.cpp default"
            sampling = environment.get(
                "ROCMLETE_LLAMA_SAMPLING_DEFAULTS", ""
            )
        if speculative_type:
            strategy = {
                "draft-mtp": "MTP",
                "draft-dflash": "DFlash",
            }.get(speculative_type, speculative_type)
            speculation = "{}, {} draft tokens".format(
                strategy, draft_tokens
            )
        else:
            speculation = "off"

        if preset is None:
            model_rows.extend(
                (
                    ("Preset", "local GGUF"),
                    ("GGUF", model_path),
                )
            )
            context_policy = "model metadata"
            reasoning = "not catalog-managed"
        else:
            artifact = catalog.artifact(preset.artifact)
            context_policy = _llama_context_policy(preset)
            reasoning = _llama_reasoning_policy(preset)
            model_rows.extend(
                (
                    ("Preset", preset.identifier),
                    ("Description", artifact.description),
                    ("GGUF", model_path),
                    (
                        "Source",
                        "{} @ {} / {}".format(
                            artifact.source.repository,
                            artifact.source.revision,
                            artifact.source.path,
                        ),
                    ),
                    ("SHA-256", artifact.sha256),
                )
            )
        model_rows.extend(
            (
                (
                    "Context",
                    "{} tokens".format(context)
                    if context != "model metadata"
                    else context,
                ),
                ("Context policy", context_policy),
                ("Template", template),
                ("Reasoning", reasoning),
                ("Reasoning output", reasoning_output),
                ("Speculation", speculation),
                ("Flash Attention", flash_attention),
                ("K/V cache", kv_cache),
            )
        )
        model_rows.extend(_llama_sampling_rows(catalog, preset, sampling))
        _print_llama_status_section("Model policy", model_rows)

    _print_llama_status_section(
        "ROCmplete behavior",
        (
            (
                "Downstream patches",
                labels.get(
                    "io.github.fff7d1bc.rocmplete.llama-cpp.patches",
                    "unknown",
                ),
            ),
            (
                "Policy boundary",
                "catalog defaults are selected after reasoning mode resolution",
            ),
            (
                "Secrets",
                "API-key values and host secret paths are never printed",
            ),
        ),
    )

    reproduction = _llama_reproduction_command(environment, runtime, preset)
    print()
    print(style("Reproduce effective ROCmplete launch", "heading"))
    print("  {}".format(style(shlex.join(reproduction), "command")))
    print()
    print(style("Exact running llama.cpp command", "heading"))
    print("  {}".format(style(shlex.join(process_command), "command")))
    return 0


def command_status(
    arguments: argparse.Namespace, catalog: Optional[Catalog] = None
) -> int:
    """Show operational state without creating data or starting containers."""
    podman.require_rootless()
    if arguments.application == "llama-cpp":
        if arguments.data_dir:
            raise LauncherError("--data-dir is only valid for the normal status view")
        return _command_llama_status(arguments, catalog or load_catalog())
    if arguments.model:
        raise LauncherError("--model requires 'status llama-cpp'")
    requested = selected_data_dir(arguments.data_dir).expanduser()
    try:
        data_path = requested.resolve(strict=False)
        disk_path = _nearest_existing_parent(data_path)
        free = shutil.disk_usage(disk_path).free
    except OSError as error:
        raise LauncherError(
            "cannot inspect persistent data path {}: {}".format(
                requested, error
            )
        )

    print(style("Persistent data", "heading"))
    data_state = "ready" if data_path.is_dir() else "missing"
    print(
        "  {} {} ({} free)".format(
            format_state(data_state, 12), data_path, human_size(free)
        )
    )

    print()
    print(style("GPU devices", "heading"))
    devices = [Path("/dev/kfd")] + [
        Path(item) for item in sorted(glob.glob("/dev/dri/renderD*"))
    ]
    for device in devices:
        if not device.exists():
            print("  {} {}".format(format_state("missing", 12), device))
            continue
        state = (
            "read/write"
            if os.access(str(device), os.R_OK | os.W_OK)
            else "no access"
        )
        print("  {} {}".format(format_state(state, 12), device))

    print()
    print(style("Images", "heading"))
    content_state = (
        "ready" if podman.image_exists(CONTENT_TOOLS_IMAGE) else "missing"
    )
    print(
        "  {} {:<12} {}".format(
            format_state(content_state, 12),
            "content",
            CONTENT_TOOLS_IMAGE,
        )
    )
    runtime_state = (
        "ready" if podman.image_exists(ROCM_RUNTIME_IMAGE) else "missing"
    )
    print(
        "  {} {:<12} {}".format(
            format_state(runtime_state, 12),
            "runtime",
            ROCM_RUNTIME_IMAGE,
        )
    )
    base_state = (
        "ready" if podman.image_exists(ROCM_BASE_IMAGE) else "missing"
    )
    print(
        "  {} {:<12} {}".format(
            format_state(base_state, 12), "base", ROCM_BASE_IMAGE
        )
    )
    for application, application_spec in APPLICATIONS.items():
        state = (
            "ready"
            if podman.image_exists(application_spec.image)
            else "missing"
        )
        print(
            "  {} {:<12} {}".format(
                format_state(state, 12),
                application,
                application_spec.image,
            )
        )

    print()
    print(style("Managed containers", "heading"))
    for application, application_spec in APPLICATIONS.items():
        name = application_spec.container_name
        if not podman.container_exists(name):
            print(
                "  {} {:<12} {}".format(
                    format_state("absent", 12), application, name
                )
            )
            continue
        state = podman.capture(
            [
                "podman",
                "inspect",
                "--format",
                "{{.State.Status}}",
                name,
            ],
            "cannot inspect container {!r}".format(name),
        )
        detail = name
        if application in WEB_APPLICATIONS and state == "running":
            container_env = _container_environment(name)
            listen = container_env.get(
                "ROCMLETE_HOST_LISTEN",
                container_env.get("ROCMLETE_LISTEN", DEFAULT_LISTEN),
            )
            try:
                port = validate_port(
                    container_env.get(
                        "ROCMLETE_PORT", str(application_spec.port)
                    )
                )
                detail = "{}  {}".format(detail, _browser_url(listen, port))
            except LauncherError:
                pass
        print(
            "  {} {:<12} {}".format(
                format_state(state, 12), application, detail
            )
        )
    return 0


def command_shell(arguments: argparse.Namespace) -> int:
    if arguments.application is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose an application",
            SHELL_EXAMPLES,
        )
    podman.require_rootless()
    env = os.environ
    image = (
        arguments.image
        or environment_value(
            env, "IMAGE", APPLICATIONS[arguments.application].image
        )
    )
    data_path = prepare_data_dir(selected_data_dir(arguments.data_dir, env))
    StorageLayout(data_path).prepare_runtime(arguments.application)
    if not podman.image_exists(image):
        raise LauncherError("image not found: {}".format(image))
    podman.replace_process(
        shell_command(
            image,
            data_path,
            podman.selinux_volume_suffix(),
            arguments.application,
        )
    )
    return 0


def command_logs(arguments: argparse.Namespace) -> int:
    if arguments.application is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose an application",
            LOG_EXAMPLES,
        )
    if not arguments.all and arguments.tail < 1:
        raise LauncherError("--tail must be at least 1")
    podman.require_rootless()
    name = APPLICATIONS[arguments.application].container_name
    if not podman.container_exists(name):
        raise LauncherError(
            "container {!r} does not exist".format(name)
        )
    command = ["podman", "logs"]
    if arguments.follow:
        command.append("--follow")
    if not arguments.all:
        command.extend(("--tail", str(arguments.tail)))
    command.append(name)
    return podman.run(command)


def _stop_container(
    name: str, known_present: bool = False, stop_timeout: int = 2
) -> int:
    if not known_present and not podman.container_exists(name):
        print(
            "{} {}".format(
                style("Container not present:", "muted"), name
            )
        )
        return 0
    podman.remove_container(name, stop_timeout=stop_timeout)
    print(
        "{} {}".format(style("Removed container:", "success"), name)
    )
    return 0


def _existing_managed_containers(
    application: str = "all", *, report_absent_applications: bool = False
) -> Tuple[str, ...]:
    selected_applications = (
        APPLICATION_NAMES if application == "all" else (application,)
    )
    labelled = set(
        podman.managed_container_names(
            "" if application == "all" else application
        )
    )
    containers = []

    def append(name: str) -> None:
        if name not in containers:
            containers.append(name)

    for identifier in selected_applications:
        name = APPLICATIONS[identifier].container_name
        if name in labelled or podman.container_exists(name):
            append(name)
        elif report_absent_applications:
            print(
                "{} {}".format(
                    style("Container not present:", "muted"), name
                )
            )

    for name, owner in TRANSIENT_CONTAINER_APPLICATIONS.items():
        if application != "all" and owner != application:
            continue
        if name in labelled or podman.container_exists(name):
            append(name)

    if application == "all":
        for name in podman.managed_download_container_names():
            append(name)

    for name in sorted(labelled):
        append(name)
    return tuple(containers)


def command_stop(arguments: argparse.Namespace) -> int:
    if arguments.application is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose an application",
            STOP_EXAMPLES,
        )
    podman.require_rootless()
    applications = (
        APPLICATION_NAMES
        if arguments.application == "all"
        else (arguments.application,)
    )
    for application in applications:
        result = _stop_container(APPLICATIONS[application].container_name)
        if result != 0:
            return result
    return 0


def _cleanup_data_dir(value: Optional[str]) -> Path:
    requested = selected_data_dir(value)
    try:
        resolved = requested.expanduser().resolve(strict=False)
        home = Path(os.environ.get("HOME", str(Path.home()))).resolve(strict=False)
    except OSError as error:
        raise LauncherError(
            "cannot resolve cleanup data directory {}: {}".format(
                requested, error
            )
        )

    protected = {
        Path("/"),
        Path("/etc"),
        Path("/home"),
        Path("/opt"),
        Path("/tmp"),
        Path("/usr"),
        Path("/var"),
        home,
        home.parent,
    }
    if resolved in protected or len(resolved.parts) < 3:
        raise LauncherError(
            "refusing to remove broad data directory: {}".format(resolved)
        )
    return resolved


def _directory_size(path: Path) -> int:
    total = 0
    pending = [path]
    while pending:
        directory = pending.pop()
        try:
            entries = tuple(os.scandir(directory))
        except OSError as error:
            raise LauncherError("cannot inspect cleanup path {}: {}".format(
                directory, error
            ))
        for entry in entries:
            try:
                if entry.is_symlink():
                    total += entry.stat(follow_symlinks=False).st_size
                elif entry.is_dir(follow_symlinks=False):
                    pending.append(Path(entry.path))
                else:
                    total += entry.stat(follow_symlinks=False).st_size
            except OSError as error:
                raise LauncherError(
                    "cannot inspect cleanup entry {}: {}".format(
                        entry.path, error
                    )
                )
    return total


def _generated_data_targets(
    data_dir: Path, cache: bool, staging: bool
) -> Tuple[Tuple[Path, int], ...]:
    relative_paths = []
    if cache:
        for application in APPLICATION_NAMES:
            relative_paths.extend(
                (
                    Path("apps") / application / "cache",
                    Path("apps") / application / "home" / ".cache",
                    Path("apps") / application / "home" / ".miopen",
                    Path("apps") / application / "home" / ".triton",
                )
            )
    if staging:
        relative_paths.extend(
            (
                Path("staging"),
                Path("apps/comfyui/benchmarks/.cache"),
            )
        )

    targets = []
    for relative in relative_paths:
        target = data_dir / relative
        component = data_dir
        for part in relative.parts:
            component = component / part
            if component.is_symlink():
                raise LauncherError(
                    "refusing cleanup through symlinked path: {}".format(
                        component
                    )
                )
        if not target.exists():
            print(
                "{} {}".format(
                    style("Generated data not present:", "muted"),
                    target,
                )
            )
            continue
        if not target.is_dir():
            raise LauncherError(
                "cleanup target is not a directory: {}".format(target)
            )
        targets.append((target, _directory_size(target)))

    return tuple(targets)


def _remove_generated_data(
    targets: Sequence[Tuple[Path, int]],
) -> None:
    for target, size in targets:
        try:
            shutil.rmtree(target)
        except OSError as error:
            raise LauncherError(
                "cannot remove generated data {}: {}".format(target, error)
            )
        print(
            "{} {} ({})".format(
                style("Removed generated data:", "success"),
                target,
                human_size(size),
            )
        )


def _print_cleanup_plan(resources: Sequence[str]) -> None:
    print(style("Cleanup plan:", "heading"))
    for resource in resources:
        print("  {}".format(resource))
    sys.stdout.flush()


def _confirm_cleanup(
    arguments: argparse.Namespace,
    scope: str,
    question: Optional[str] = None,
) -> None:
    if arguments.yes:
        return
    if arguments.non_interactive or not sys.stdin.isatty():
        raise LauncherError(
            "{} cleanup requires confirmation in noninteractive use; "
            "repeat with --yes".format(scope)
        )
    try:
        response = input(
            prompt(
                "{} [y/N] ".format(
                    question
                    or "Proceed with {} cleanup?".format(scope)
                )
            )
        )
    except EOFError:
        response = ""
    if response.strip().lower() not in ("y", "yes"):
        raise LauncherError("{} cleanup declined".format(scope))


def command_cleanup(arguments: argparse.Namespace) -> int:
    scope = arguments.cleanup_command
    if scope is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose a cleanup scope",
            CLEANUP_EXAMPLES,
        )

    if scope == "containers":
        podman.require_rootless()
        containers = _existing_managed_containers(
            arguments.application,
            report_absent_applications=True,
        )
        if not containers:
            return 0
        _print_cleanup_plan(
            tuple("container: {}".format(name) for name in containers)
        )
        _confirm_cleanup(arguments, scope)
        for name in containers:
            result = _stop_container(
                name, known_present=True, stop_timeout=0
            )
            if result != 0:
                return result
        return 0

    if scope == "images":
        podman.require_rootless()
        if arguments.image_tag and arguments.application != "all":
            raise LauncherError(
                "--image-tag cannot be combined with an application"
            )
        applications = (
            APPLICATION_NAMES
            if arguments.application == "all"
            else (arguments.application,)
        )
        if arguments.image_tag:
            images = (arguments.image_tag,)
        else:
            images = list(
                APPLICATIONS[item].image for item in applications
            )
            if arguments.application == "all":
                images.extend(
                    (
                        ROCM_BASE_IMAGE,
                        ROCM_RUNTIME_IMAGE,
                        CONTENT_TOOLS_IMAGE,
                    )
                )
            images = tuple(images)
            running = _existing_managed_containers(arguments.application)
            if running:
                raise LauncherError(
                    "managed containers are still present: {}; run "
                    "'./rocmplete cleanup containers {}' before removing "
                    "images".format(
                        ", ".join(running), arguments.application
                    )
                )
        present_images = []
        for image in images:
            if podman.image_exists(image):
                present_images.append(image)
            else:
                print(
                    "{} {}".format(
                        style("Image not present:", "muted"), image
                    )
                )
        if not present_images:
            return 0
        _print_cleanup_plan(
            tuple("image: {}".format(image) for image in present_images)
        )
        _confirm_cleanup(arguments, scope)
        for image in present_images:
            result = podman.run_quiet_stdout(
                ["podman", "image", "rm", image]
            )
            if result != 0 and podman.image_exists(image):
                return result
            print(
                "{} {}".format(
                    style("Removed image:", "success"), image
                )
            )
        return 0

    if scope == "build-cache":
        cache_dir = build_cache_dir()
        for component in (cache_dir.parent, cache_dir):
            if component.is_symlink():
                raise LauncherError(
                    "refusing cleanup through symlinked path: {}".format(
                        component
                    )
                )
        if not cache_dir.exists():
            print(
                "{} {}".format(
                    style("Build cache not present:", "muted"), cache_dir
                )
            )
            return 0
        if not cache_dir.is_dir():
            raise LauncherError(
                "build cache path is not a directory: {}".format(cache_dir)
            )
        size = _directory_size(cache_dir)
        _print_cleanup_plan(
            ("build cache: {} ({})".format(cache_dir, human_size(size)),)
        )
        _confirm_cleanup(
            arguments,
            scope,
            "Remove these reusable build downloads?",
        )
        try:
            shutil.rmtree(cache_dir)
        except OSError as error:
            raise LauncherError(
                "cannot remove build cache {}: {}".format(cache_dir, error)
            )
        print(
            "{} {} ({})".format(
                style("Removed build cache:", "success"),
                cache_dir,
                human_size(size),
            )
        )
        return 0

    data_dir = _cleanup_data_dir(arguments.data_dir)
    if not data_dir.exists():
        print(
            "{} {}".format(
                style("Data not present:", "muted"), data_dir
            )
        )
        return 0
    if not data_dir.is_dir():
        raise LauncherError(
            "data path is not a directory: {}".format(data_dir)
        )
    targets: Tuple[Tuple[Path, int], ...] = ()
    data_size: Optional[int] = None
    if scope in ("caches", "downloads"):
        targets = _generated_data_targets(
            data_dir,
            cache=scope == "caches",
            staging=scope == "downloads",
        )
        if not targets:
            return 0
    elif scope == "data":
        data_size = _directory_size(data_dir)
    else:
        raise LauncherError("unknown cleanup scope: {}".format(scope))

    podman.require_rootless()
    if _existing_managed_containers():
        raise LauncherError(
            "managed containers are still present; run "
            "'./rocmplete cleanup containers' first"
        )
    if scope in ("caches", "downloads"):
        _print_cleanup_plan(
            tuple(
                "generated data: {} ({})".format(target, human_size(size))
                for target, size in targets
            )
        )
        _confirm_cleanup(arguments, scope)
        _remove_generated_data(
            targets,
        )
        return 0
    if scope == "data" and data_size is not None:
        _print_cleanup_plan(
            (
                "persistent data: {} ({})".format(
                    data_dir, human_size(data_size)
                ),
            )
        )
        _confirm_cleanup(
            arguments,
            scope,
            "Permanently remove all ROCmplete data in this plan?",
        )
        try:
            shutil.rmtree(data_dir)
        except OSError as error:
            raise LauncherError(
                "cannot remove data directory {}: {}".format(
                    data_dir, error
                )
            )
        print(
            "{} {}".format(
                style("Removed persistent data:", "success"), data_dir
            )
        )
        return 0
    raise LauncherError("unknown cleanup scope: {}".format(scope))


def _content_data_dir(value: Optional[str], prepare: bool) -> Path:
    requested = selected_data_dir(value)
    return (
        prepare_data_dir(requested)
        if prepare
        else inspect_data_path(requested)
    )


def _content_tools_image(value: Optional[str]) -> str:
    return (
        value
        if value is not None
        else environment_value(
            os.environ, "IMAGE", CONTENT_TOOLS_IMAGE
        )
    )


def _comfyui_image(value: Optional[str]) -> str:
    return (
        value
        if value is not None
        else environment_value(
            os.environ, "IMAGE", APPLICATIONS["comfyui"].image
        )
    )


def _remote_import_url(arguments: argparse.Namespace) -> str:
    if arguments.url:
        return arguments.url
    if arguments.non_interactive or not sys.stdin.isatty():
        raise LauncherError(
            "content import requires URL in noninteractive use"
        )
    try:
        value = input(
            prompt(
                "Civitai or Hugging Face URL: ",
                leading_blank=False,
            )
        ).strip()
    except EOFError:
        value = ""
    if not value:
        raise LauncherError("remote content URL is required")
    arguments.url = value
    return value


def _remote_import_version(
    arguments: argparse.Namespace, provider: str, civitai_token: str
) -> int:
    if arguments.version is not None:
        if provider != "civitai":
            raise LauncherError("--version is only valid for Civitai URLs")
        if arguments.version <= 0:
            raise LauncherError("Civitai --version must be positive")
        return arguments.version
    if provider != "civitai":
        return 0
    choices = civitai_version_choices(arguments.url, civitai_token)
    if len(choices) == 1:
        return int(choices[0][0])
    if arguments.non_interactive or not sys.stdin.isatty():
        raise LauncherError(
            "Civitai model page has several versions; select one in the URL "
            "or repeat with --version ID"
        )
    print()
    return int(
        _interactive_menu_choice(
            "Civitai model version:",
            choices,
            subject="model version",
        )
    )


def _remote_import_file(
    arguments: argparse.Namespace, discovery: RemoteDiscovery
) -> RemoteFile:
    if arguments.file:
        return select_file(discovery, arguments.file)
    selected = automatic_file(discovery)
    if selected is not None:
        return selected
    if arguments.non_interactive or not sys.stdin.isatty():
        names = ", ".join(item.identifier for item in discovery.files)
        raise LauncherError(
            "remote source has several supported files; repeat with "
            "--file FILE (choose {})".format(names)
        )
    print()
    choices = tuple(
        (
            item.identifier,
            "{} ({}){}".format(
                item.name,
                human_size(item.size),
                " — primary" if item.primary else "",
            ),
        )
        for item in discovery.files
    )
    return select_file(
        discovery,
        _interactive_menu_choice(
            "Remote file:",
            choices,
            subject="remote file",
        ),
    )


def _remote_import_kind(
    arguments: argparse.Namespace,
    discovery: RemoteDiscovery,
    file: RemoteFile,
) -> ImportKind:
    if arguments.import_kind:
        return select_kind(arguments.import_kind, file)
    selected = automatic_kind(discovery, file)
    candidates = candidate_kinds(discovery, file)
    if selected is not None:
        alternatives = compatible_kinds(file)
        if (
            len(alternatives) == 1
            or arguments.non_interactive
            or not sys.stdin.isatty()
        ):
            return selected
        try:
            response = input(
                prompt(
                    "Detected {}. Install under {}/? [Y/n] ".format(
                        selected.label,
                        selected.destination_prefix,
                    )
                )
            )
        except EOFError:
            response = ""
        normalized = response.strip().lower()
        if normalized in ("", "y", "yes"):
            return selected
        if normalized not in ("n", "no"):
            raise LauncherError(
                "destination confirmation must be yes or no"
            )
        # The user rejected a metadata-based suggestion. Restore every
        # suffix-compatible manual destination as an explicit escape hatch.
        candidates = alternatives
    if not candidates:
        provider = (
            "Civitai"
            if discovery.provider == "civitai"
            else discovery.provider
        )
        provider_type = discovery.model_type or "unknown"
        raise LauncherError(
            "{} model type {!r} does not map safely to a supported "
            "destination for {!r}; use --as TYPE only after verifying the "
            "file's intended loader".format(
                provider,
                provider_type,
                file.name,
            )
        )
    checkpoint_choice = (
        discovery.provider == "civitai"
        and discovery.model_type.lower().replace(" ", "") == "checkpoint"
        and len(candidates) > 1
    )
    descriptions = (
        {
            "comfyui:checkpoint": (
                "complete checkpoint (CheckpointLoaderSimple)"
            ),
            "comfyui:diffusion-model": (
                "standalone diffusion model / UNet (UNETLoader)"
            ),
        }
        if checkpoint_choice
        else {}
    )
    choices = tuple(
        (kind.identifier, descriptions.get(kind.identifier, kind.label))
        for kind in candidates
    )
    if arguments.non_interactive or not sys.stdin.isatty():
        raise LauncherError(
            "ROCmplete cannot infer where to install {!r}; repeat with "
            "--as TYPE (choose {})".format(
                file.name,
                ", ".join(identifier for identifier, _ in choices),
            )
        )
    print()
    if checkpoint_choice:
        print(
            style(
                "Civitai describes this as a checkpoint. Choose the "
                "ComfyUI loader it was packaged for:",
                "muted",
            )
        )
    return select_kind(
        _interactive_menu_choice(
            "Checkpoint destination:" if checkpoint_choice else "Install as:",
            choices,
            subject="import type",
        ),
        file,
    )


def _print_remote_import_plan(
    plan: RemoteImportPlan, pack_path: Path, dry_run: bool
) -> None:
    print(style("Remote import:", "heading"))
    details = [
        ("Source:", plan.discovery.source_url),
        ("Title:", plan.discovery.title),
    ]
    if plan.discovery.model_type:
        details.append(("Provider type:", plan.discovery.model_type))
    details.extend(
        (
            (
                "File:",
                "{} ({})".format(
                    plan.file.name,
                    human_size(plan.file.size),
                ),
            ),
            ("SHA-256:", plan.file.sha256),
            ("Install as:", plan.kind.label),
            ("Destination:", plan.destination),
            (
                "Local pack:",
                "{}{}".format(
                    pack_path,
                    " (not saved by dry run)" if dry_run else "",
                ),
            ),
            (
                "License:",
                "NOASSERTION; hosted-file rights are unverified",
            ),
        )
    )
    print_columns(
        details,
        columns=(ColumnSpec(role="label"), ColumnSpec()),
        indent="  ",
    )


def _remote_import_install_arguments(
    arguments: argparse.Namespace,
    pack_path: Path,
    display_path: Path,
) -> argparse.Namespace:
    return argparse.Namespace(
        target=None,
        selection=None,
        interactive=False,
        non_interactive=arguments.non_interactive,
        from_file=[pack_path],
        display_pack_paths=[display_path],
        local_mirror=None,
        local_mirror_move=False,
        data_dir=arguments.data_dir,
        image=arguments.image,
        dry_run=arguments.dry_run,
        force_workflow=False,
        accept_license=False,
        acknowledge_license_risk=arguments.acknowledge_license_risk,
        suppress_next_actions=True,
        summary_only=False,
    )


def _command_content_import(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    url = _remote_import_url(arguments)
    provider = remote_provider(url)
    civitai_token = os.environ.get("CIVITAI_TOKEN", "")
    version = _remote_import_version(
        arguments, provider, civitai_token
    )
    discovery = discover_remote(
        url,
        version_id=version,
        hf_token=os.environ.get("HF_TOKEN", ""),
        civitai_token=civitai_token,
    )
    file = _remote_import_file(arguments, discovery)
    kind = _remote_import_kind(arguments, discovery, file)
    plan = build_import_plan(discovery, file, kind)
    pack_path = arguments.save_pack or default_pack_path(plan)
    _print_remote_import_plan(plan, pack_path, arguments.dry_run)
    if arguments.dry_run:
        save_pack(pack_path, plan, dry_run=True)

    try:
        temporary = tempfile.TemporaryDirectory(
            prefix="rocmplete-import-"
        )
    except OSError as error:
        raise LauncherError(
            "cannot create import validation directory: {}".format(error)
        )

    result = 0
    with temporary as directory:
        validation_path = Path(directory) / "content-pack.json"
        try:
            validation_path.write_bytes(pack_bytes(plan))
        except OSError as error:
            raise LauncherError(
                "cannot write generated import pack for validation: {}".format(
                    error
                )
            )
        merged_catalog, identifiers = load_content_packs(
            catalog, (validation_path,)
        )
        if identifiers != (plan.bundle_identifier,):
            raise LauncherError(
                "generated import pack did not select exactly its own bundle"
            )
        if arguments.dry_run:
            install_arguments = _remote_import_install_arguments(
                arguments, validation_path, pack_path
            )
            return _command_content_install(
                install_arguments, catalog
            )

        def persist_pack() -> None:
            created = save_pack(pack_path, plan)
            print()
            print(
                "{} {}".format(
                    style(
                        (
                            "Saved local pack:"
                            if created
                            else "Reused local pack:"
                        ),
                        "success",
                    ),
                    pack_path,
                )
            )

        result = _command_content_install(
            _remote_import_install_arguments(
                arguments, validation_path, pack_path
            ),
            catalog,
            before_mutation=persist_pack,
        )

    if result != 0:
        return result
    if kind.application == "llama-cpp":
        data_dir = _content_data_dir(arguments.data_dir, prepare=False)
        model_path = artifact_path(
            data_dir,
            merged_catalog.artifact(plan.artifact_identifier),
        )
        next_step(
            shlex.join(
                (
                    "./rocmplete",
                    "run",
                    "llama-cpp",
                    "server",
                    "--model",
                    str(model_path),
                )
            )
        )
    else:
        next_step("./rocmplete run {}".format(kind.application))
    return 0


def command_content(arguments: argparse.Namespace, catalog: Catalog) -> int:
    action = arguments.content_command
    if action is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose list, status, install, import, or workflows",
            CONTENT_EXAMPLES,
        )
    if action == "list":
        if arguments.application and not (
            arguments.bundles or arguments.models
        ):
            raise LauncherError("--application requires --bundles or --models")
        if (
            arguments.data_dir is not None
            or arguments.details
            or arguments.scan
        ) and not arguments.models:
            raise LauncherError(
                "--data-dir, --details, and --scan require --models"
            )
        if arguments.models:
            return _print_model_inventory(arguments, catalog)
        if arguments.bundles:
            bundles = tuple(
                bundle
                for bundle in catalog.bundles.values()
                if (
                    arguments.application is None
                    or bundle.application == arguments.application
                )
            )
            heading = (
                "{} bundles:".format(
                    dict(CONTENT_APPLICATIONS)[arguments.application]
                )
                if arguments.application
                else "Exact bundles:"
            )
            print(style(heading, "heading"))
            rows = []
            for bundle in bundles:
                artifacts = catalog.bundle_artifacts(bundle)
                states = []
                if catalog.bundle_agreements(bundle):
                    states.append("TERMS")
                if not all(
                    item.license.status == "verified"
                    for item in artifacts
                ):
                    states.append("UNVERIFIED")
                license_state = "+".join(states) if states else "verified"
                rows.append(
                    (
                        bundle.identifier,
                        bundle.application,
                        human_size(catalog.bundle_size(bundle)),
                        format_state(license_state),
                        bundle.description,
                    )
                )
            print_columns(
                rows,
                columns=(
                    ColumnSpec(role="command"),
                    ColumnSpec(),
                    ColumnSpec(align=">"),
                    ColumnSpec(),
                    ColumnSpec(),
                ),
                indent="  ",
            )
            return 0
        if arguments.families:
            print(style("Model families:", "heading"))
            rows = []
            for family, description in CONTENT_FAMILIES:
                bundles = _resolve_content_bundles(
                    catalog, "family", family
                )
                rows.append(
                    (
                        "family {}".format(family),
                        len(bundles),
                        "bundle" if len(bundles) == 1 else "bundles",
                        description,
                    )
                )
            print_columns(
                rows,
                columns=(
                    ColumnSpec(role="command"),
                    ColumnSpec(align=">"),
                    ColumnSpec(),
                    ColumnSpec(),
                ),
                indent="  ",
            )
            print()
            print(style("Global:", "heading"))
            print_columns(
                (
                    (
                        "all",
                        len(catalog.bundles),
                        "bundles",
                        "every cataloged content bundle",
                    ),
                ),
                columns=(
                    ColumnSpec(role="command"),
                    ColumnSpec(align=">"),
                    ColumnSpec(),
                    ColumnSpec(),
                ),
                indent="  ",
            )
            return 0

        print(style("Applications:", "heading"))
        for application, label in CONTENT_APPLICATIONS:
            print("  {}".format(style(label, "heading")))
            rows = []
            for recipe in CONTENT_APPLICATION_RECIPES[application]:
                bundles = _resolve_content_bundles(
                    catalog, application, recipe.identifier
                )
                command = "{} {}".format(application, recipe.identifier)
                rows.append(
                    (
                        command,
                        len(bundles),
                        "bundle" if len(bundles) == 1 else "bundles",
                        recipe.description,
                    )
                )
            print_columns(
                rows,
                columns=(
                    ColumnSpec(role="command"),
                    ColumnSpec(align=">"),
                    ColumnSpec(),
                    ColumnSpec(),
                ),
                indent="    ",
            )
            print()
        print(
            style(
                "More views: use 'content list --models' for runnable models, "
                "'content list --bundles' for exact content, 'content install "
                "APPLICATION all' for every bundle owned by one application, "
                "or 'content list --families' for broader aggregates.",
                "muted",
            )
        )
        return 0

    if action == "status":
        data_dir = _content_data_dir(arguments.data_dir, prepare=False)
        bundles = tuple(
            _resolve_content_bundles(
                catalog, arguments.target, arguments.selection
            )
            if arguments.target
            else catalog.bundles.values()
        )
        complete = True
        verified_states = {}
        verification_store = VerificationStore.load(data_dir)
        for bundle in bundles:
            statuses = tuple(
                inspect_bundle(
                    catalog, bundle, data_dir, verification_store
                )
            )
            reported = []
            if arguments.details or arguments.verify:
                print(
                    "{}:".format(
                        style(bundle.identifier, "heading")
                    )
                )
            for status in statuses:
                state = content_status_state(status)
                if arguments.verify and status.state == "installed":
                    path = (
                        status.path
                        if isinstance(status, ArtifactStatus)
                        else status.blob_path
                    )
                    destination = (
                        status.artifact.destination
                        if isinstance(status, ArtifactStatus)
                        else "{}/{}".format(
                            status.tree.destination, status.file.path
                        )
                    )
                    if path not in verified_states:
                        print(
                            "  {} {}".format(
                                style("Hashing:", "info"), destination
                            ),
                            flush=True,
                        )
                        verified_states[path] = verify_status(status)
                    state = verified_states[path]
                destination = (
                    status.artifact.destination
                    if isinstance(status, ArtifactStatus)
                    else "{}/{}".format(status.tree.destination, status.file.path)
                )
                reported.append((state, destination))
            if bundle.workflow:
                workflow = catalog.workflow(bundle.workflow)
                state = workflow_state(data_dir, workflow)
                reported.append(
                    (state, "workflow/{}".format(workflow.destination))
                )

            ready_count = sum(
                state in ("installed", "verified")
                for state, _ in reported
            )
            states = {state for state, _ in reported}
            if states & {"size-mismatch", "user-file", "hash-mismatch"}:
                summary = "conflict"
            elif ready_count == len(reported):
                summary = "ready"
            elif states == {"missing"}:
                summary = "missing"
            else:
                summary = "partial"
            complete = complete and summary == "ready"
            if arguments.details or arguments.verify:
                for state, destination in reported:
                    print(
                        "  {} {}".format(
                            format_state(state, 14), destination
                        )
                    )
            else:
                print(
                    "{} {} {}/{} items".format(
                        format_state(summary, 10),
                        style(
                            "{:<42}".format(bundle.identifier),
                            "command",
                        ),
                        ready_count,
                        len(reported),
                    )
                )
        return 0 if complete else 1
    if action == "install":
        return _command_content_install(arguments, catalog)
    if action == "import":
        return _command_content_import(arguments, catalog)
    if action == "workflows":
        return command_workflows(arguments, catalog)
    raise LauncherError("unknown content command: {}".format(action))


def _llama_model_label(model: LlamaModel) -> str:
    if model.presets:
        return ", ".join(model.presets)
    if model.expected_shards > 1:
        return "local ({}/{} shards)".format(
            model.shard_count, model.expected_shards
        )
    return "local"


def _llama_model_path(model: LlamaModel, root: Path) -> str:
    if model.source == "catalog":
        try:
            return str(model.path.relative_to(root))
        except ValueError:
            pass
    return str(model.path)


def _llama_template_policy(preset: LlamaPreset) -> str:
    if preset.chat_template:
        return "managed {}".format(preset.chat_template)
    if preset.jinja:
        return "model metadata; Jinja enabled"
    return "model metadata; llama.cpp automatic"


def _llama_speculation_policy(preset: LlamaPreset) -> str:
    if not preset.speculative_type:
        return "off"
    strategy = {
        "draft-mtp": "MTP",
        "draft-dflash": "DFlash",
    }[preset.speculative_type]
    backend_policy = ""
    if preset.draft_tokens_by_backend:
        backend_policy = " ({})".format(
            ", ".join(
                "{}={}".format(backend, tokens)
                for backend, tokens in sorted(
                    preset.draft_tokens_by_backend.items()
                )
            )
        )
    if preset.draft_artifact:
        return "{}, {} draft tokens{}; draft {}".format(
            strategy,
            preset.draft_tokens,
            backend_policy,
            preset.draft_artifact,
        )
    return "{}, {} draft tokens{} from model heads".format(
        strategy, preset.draft_tokens, backend_policy
    )


def _llama_context_policy(preset: LlamaPreset) -> str:
    if not preset.context_override_architectures:
        return "model metadata"
    return "forced for {}; automatic fitting disabled".format(
        ", ".join(preset.context_override_architectures)
    )


def _llama_flash_attention_policy(preset: LlamaPreset) -> str:
    policies = [
        "{}={}".format(profile, preset.flash_attention[profile])
        for profile in GPU_PROFILES
        if profile in preset.flash_attention
    ]
    if not policies:
        return "llama.cpp default"
    return "{}; otherwise llama.cpp default".format(", ".join(policies))


def _llama_kv_cache_policy(preset: LlamaPreset) -> str:
    policies = [
        "{}={}".format(profile, preset.kv_cache[profile])
        for profile in GPU_PROFILES
        if profile in preset.kv_cache
    ]
    if not policies:
        return "llama.cpp default"
    return "{}; otherwise llama.cpp default".format(", ".join(policies))


def _llama_reasoning_policy(preset: LlamaPreset) -> str:
    if not preset.reasoning_control:
        return "not exposed"
    levels = (
        ["on"]
        if preset.reasoning_control == "toggle"
        else list(preset.reasoning_levels)
    )
    if preset.reasoning_off:
        levels.insert(0, "off")
    return "{}; {}; default {}".format(
        preset.reasoning_control,
        ", ".join(levels),
        preset.reasoning_default,
    )


def _llama_sampling_policy(preset: LlamaPreset) -> str:
    if not preset.sampling_policy:
        return "request or llama.cpp default"
    return "catalog {} (thinking/non-thinking)".format(
        preset.sampling_policy
    )


def _print_llama_model_details(
    catalog: Catalog, models: Sequence[LlamaModel], root: Path
) -> None:
    managed = tuple(model for model in models if model.presets)
    if not managed:
        return
    print()
    print(style("Managed preset details", "heading"))
    for model in managed:
        for identifier in model.presets:
            preset = catalog.llama_preset(identifier)
            bundle = catalog.bundle(preset.bundle)
            model_artifacts = tuple(
                artifact
                for artifact in catalog.bundle_artifacts(bundle)
                if artifact.target == "llama-models"
            )
            print()
            print("  {}".format(style(identifier, "command")))
            print_columns(
                (
                    ("Model", _llama_model_path(model, root)),
                    ("Bundle", bundle.identifier),
                    ("Catalog size", human_size(catalog.bundle_size(bundle))),
                    ("Files", len(model_artifacts)),
                    (
                        "Default context",
                        "{} tokens".format(preset.default_context),
                    ),
                    ("Context policy", _llama_context_policy(preset)),
                    ("Template", _llama_template_policy(preset)),
                    (
                        "Speculation",
                        _llama_speculation_policy(preset),
                    ),
                    ("Reasoning", _llama_reasoning_policy(preset)),
                    ("Sampling", _llama_sampling_policy(preset)),
                    (
                        "Flash Attention",
                        _llama_flash_attention_policy(preset),
                    ),
                    ("K/V cache", _llama_kv_cache_policy(preset)),
                ),
                columns=(ColumnSpec(role="label"), ColumnSpec()),
                indent="    ",
            )


def _print_llama_model_inventory(
    arguments: argparse.Namespace, catalog: Catalog, data_dir: Path
) -> None:
    root = StorageLayout(data_dir).llama_models
    models = llama_models(catalog, data_dir, arguments.scan)

    print(style("llama.cpp models", "heading"))
    print_columns(
        (("Model root", root),),
        columns=(ColumnSpec(role="label"), ColumnSpec()),
        indent="  ",
    )
    if arguments.scan:
        print_columns(
            tuple(
                ("Also scanned", path.expanduser())
                for path in arguments.scan
            ),
            columns=(ColumnSpec(role="label"), ColumnSpec()),
            indent="  ",
        )
    print()
    if not models:
        print(style("No managed or local GGUF models found.", "muted"))
        print(
            "Add a managed model to the catalog, or scan an external "
            "directory with --scan PATH."
        )
        return

    rows = [
        (
            style("Status", "label"),
            style("Size", "label"),
            style("Preset or model", "label"),
        )
    ]
    rows.extend(
        (
            format_state(model.state),
            human_size(model.size) if model.size else "—",
            (
                style(_llama_model_label(model), "command")
                if model.presets
                else "{}  {}".format(
                    _llama_model_label(model),
                    _llama_model_path(model, root),
                )
            ),
        )
        for model in models
    )
    print_columns(
        rows,
        columns=(
            ColumnSpec(),
            ColumnSpec(align=">"),
            ColumnSpec(),
        ),
        indent="  ",
    )
    if arguments.details:
        _print_llama_model_details(catalog, models, root)


def _print_dwarfstar_model_inventory(
    arguments: argparse.Namespace, catalog: Catalog, data_dir: Path
) -> None:
    root = StorageLayout(data_dir).dwarfstar_models
    bundles = tuple(
        bundle
        for bundle in catalog.bundles.values()
        if bundle.application == "dwarfstar"
    )

    print(style("DwarfStar models", "heading"))
    print_columns(
        (("Model root", root),),
        columns=(ColumnSpec(role="label"), ColumnSpec()),
        indent="  ",
    )
    print()
    rows = [
        (
            style("Status", "label"),
            style("Size", "label"),
            style("Bundle", "label"),
        )
    ]
    details = []
    for bundle in bundles:
        artifacts = tuple(
            catalog.artifact(identifier) for identifier in bundle.artifacts
        )
        statuses = inspect_bundle(catalog, bundle, data_dir)
        if not statuses or not all(
            isinstance(status, ArtifactStatus) for status in statuses
        ):
            raise LauncherError(
                "DwarfStar bundle {} did not resolve to direct GGUF "
                "artifacts".format(bundle.identifier)
            )
        failure = next(
            (
                status
                for status in statuses
                if not content_status_ready(status)
            ),
            None,
        )
        state = "ready" if failure is None else content_status_state(failure)
        rows.append(
            (
                format_state(state),
                human_size(catalog.bundle_size(bundle)),
                style(bundle.identifier, "command"),
            )
        )
        details.append((bundle, artifacts))
    print_columns(
        rows,
        columns=(
            ColumnSpec(),
            ColumnSpec(align=">"),
            ColumnSpec(),
        ),
        indent="  ",
    )
    if arguments.details:
        print()
        print(style("Managed DwarfStar model details", "heading"))
        for bundle, artifacts in details:
            print()
            print("  {}".format(style(bundle.identifier, "command")))
            artifact_rows = [("Model", artifacts[0].destination)]
            artifact_rows.extend(
                ("Support", artifact.destination)
                for artifact in artifacts[1:]
            )
            print_columns(
                tuple(artifact_rows)
                + (
                    (
                        "Catalog size",
                        human_size(catalog.bundle_size(bundle)),
                    ),
                    ("Files", len(bundle.artifacts)),
                    (
                        "Install",
                        "./rocmplete content install {}".format(
                            bundle.identifier
                        ),
                    ),
                    (
                        "Run",
                        "./rocmplete run dwarfstar server{}".format(
                            " --dspark" if len(artifacts) > 1 else ""
                        ),
                    ),
                ),
                columns=(ColumnSpec(role="label"), ColumnSpec()),
                indent="    ",
            )


def _print_model_inventory(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    supported = ("llama-cpp", "dwarfstar")
    if arguments.application and arguments.application not in supported:
        raise LauncherError(
            "model inventory does not support {}; choose {}".format(
                arguments.application, " or ".join(supported)
            )
        )
    if arguments.scan and arguments.application == "dwarfstar":
        raise LauncherError("--scan applies only to llama.cpp GGUF models")

    data_dir = _content_data_dir(arguments.data_dir, prepare=False)
    applications = (
        (arguments.application,) if arguments.application else supported
    )
    for index, application in enumerate(applications):
        if index:
            print()
        if application == "llama-cpp":
            _print_llama_model_inventory(arguments, catalog, data_dir)
        else:
            _print_dwarfstar_model_inventory(arguments, catalog, data_dir)
    print()
    print(
        style(
            "Use --details to find install commands and managed runtime policy.",
            "muted",
        )
    )
    if "llama-cpp" in applications:
        print(
            style(
                "Run a ready llama.cpp preset with --preset; use a local "
                "GGUF row's absolute path with --model.",
                "muted",
            )
        )
    if "dwarfstar" in applications:
        print(
            style(
                "Run the ready DwarfStar model with 'run dwarfstar server'.",
                "muted",
            )
        )
    return 0


def command_workflows(arguments: argparse.Namespace, catalog: Catalog) -> int:
    action = arguments.workflows_command
    if action is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose list, status, or install",
            WORKFLOW_EXAMPLES,
        )
    if action == "list":
        print(style("Available workflows:", "heading"))
        for pack in catalog.workflow_packs.values():
            print(
                "  {} {}".format(
                    style("{:<28}".format(pack.identifier), "command"),
                    pack.description,
                )
            )
        return 0

    data_dir = _content_data_dir(
        arguments.data_dir, prepare=action == "install"
    )
    if action == "status":
        packs = (
            [catalog.workflow(arguments.workflow)]
            if arguments.workflow
            else list(catalog.workflow_packs.values())
        )
        complete = True
        for pack in packs:
            destination = workflow_destination(data_dir, pack)
            state = workflow_state(data_dir, pack)
            print(
                "{} {}  {}".format(
                    format_state(state, 14),
                    style(pack.identifier, "command"),
                    destination,
                )
            )
            complete = complete and state == "installed"
        return 0 if complete else 1
    if action == "install":
        install_workflow(
            catalog.workflow(arguments.workflow),
            data_dir,
            _comfyui_image(arguments.image),
            force=arguments.force,
        )
        return 0
    raise LauncherError("unknown workflows command: {}".format(action))


def _acknowledge_unverified_downloads(
    statuses: Sequence[object],
    already_acknowledged: bool,
    non_interactive: bool = False,
) -> bool:
    risky = missing_unverified(statuses)
    if not risky:
        return False
    if already_acknowledged:
        return True
    warnings = sorted(
        {
            (
                status.artifact.license.warning
                if hasattr(status, "artifact")
                else status.tree.license.warning
            )
            for status in risky
        }
    )
    print(file=sys.stderr)
    print(
        style("License provenance warning:", "warning", sys.stderr),
        file=sys.stderr,
    )
    for warning in warnings:
        print("  {}".format(warning), file=sys.stderr)
    print(
        "ROCmplete does not redistribute these files and cannot establish "
        "your right to use them.",
        file=sys.stderr,
    )
    if non_interactive or not sys.stdin.isatty():
        raise LauncherError(
            "noninteractive content installation requires "
            "--acknowledge-license-risk "
            "for unverified artifacts"
        )
    try:
        response = input(
            prompt("Continue with the direct download? [y/N] ")
        )
    except EOFError:
        response = ""
    if response.strip().lower() not in ("y", "yes"):
        raise LauncherError("license-risk acknowledgment declined")
    return True


def _selection_agreements(
    catalog: Catalog, bundles: Sequence[Bundle]
) -> Tuple[Agreement, ...]:
    identifiers = set()
    agreements = []
    for bundle in bundles:
        for agreement in catalog.bundle_agreements(bundle):
            if agreement.identifier not in identifiers:
                identifiers.add(agreement.identifier)
                agreements.append(agreement)
    return tuple(agreements)


def _print_selection_agreements(
    catalog: Catalog, bundles: Sequence[Bundle]
) -> None:
    agreements = _selection_agreements(catalog, bundles)
    if not agreements:
        return
    print(style("Model terms:", "heading"))
    for agreement in agreements:
        print(
            "  {}: {}".format(
                style(agreement.name, "warning"), agreement.url
            )
        )
        print("    {}".format(agreement.summary))


def _print_agreements(catalog: Catalog, bundle: Bundle) -> None:
    _print_selection_agreements(catalog, [bundle])


def _require_selection_license_acceptance(
    catalog: Catalog,
    bundles: Sequence[Bundle],
    accepted: bool,
    non_interactive: bool = False,
) -> None:
    agreements = _selection_agreements(catalog, bundles)
    if not agreements or accepted:
        return
    names = ", ".join(item.name for item in agreements)
    if not non_interactive and sys.stdin.isatty():
        print(
            "\n{} The selected content is governed by {}.".format(
                style("Terms:", "warning", sys.stderr), names
            ),
            file=sys.stderr,
        )
        try:
            response = input(
                prompt(
                    "Confirm that you accept these terms and are permitted "
                    "to use the models in your location? [y/N] "
                )
            )
        except EOFError:
            response = ""
        if response.strip().lower() in ("y", "yes"):
            return
        raise LauncherError("model license acceptance declined")
    raise LauncherError(
        "selection is governed by {}. Review the URLs in the plan and repeat "
        "with --accept-license to confirm that you accept those terms and "
        "are permitted to use the models in your location".format(names)
    )


def _require_license_acceptance(
    catalog: Catalog,
    bundle: Bundle,
    accepted: bool,
    non_interactive: bool = False,
) -> None:
    _require_selection_license_acceptance(
        catalog,
        [bundle],
        accepted,
        non_interactive=non_interactive,
    )


def _resolve_content_bundles(
    catalog: Catalog, target: str, selection: Optional[str] = None
) -> Tuple[Bundle, ...]:
    if target == "all":
        if selection is not None:
            raise LauncherError("'all' does not accept a content selection")
        return tuple(catalog.bundles.values())
    if target == "family":
        families = dict(CONTENT_FAMILIES)
        if selection is None:
            raise LauncherError(
                "family requires qwen or wan; for example 'family qwen'"
            )
        if selection not in families:
            raise LauncherError(
                "unknown content family {!r}; choose {}".format(
                    selection, " or ".join(families)
                )
            )
        bundles = tuple(
            bundle
            for bundle in catalog.bundles.values()
            if selection in bundle.groups
        )
        return bundles
    if target in CONTENT_APPLICATION_RECIPES:
        if selection is None:
            examples = " or ".join(
                "'{} {}'".format(target, recipe.identifier)
                for recipe in application_recipes(target)
            )
            raise LauncherError(
                "{} requires a content recipe or all; choose {} or "
                "'{} all'".format(
                    target, examples, target
                )
            )
        if selection == "all":
            return tuple(
                bundle
                for bundle in catalog.bundles.values()
                if bundle.application == target
            )
        return recipe_bundles(catalog, content_recipe(target, selection))
    if selection is not None:
        raise LauncherError(
            "exact bundle {!r} does not accept a content selection".format(
                target
            )
        )
    return (catalog.bundle(target),)


def _interactive_menu_choice(
    heading: str,
    choices: Sequence[Tuple[str, str]],
    subject: str = "content selection",
) -> str:
    print(style(heading, "heading"))
    print_numbered_choices(choices)
    try:
        raw = input(prompt("Choose [1-{}]: ".format(len(choices))))
    except EOFError:
        raw = ""
    try:
        choice = int(raw)
    except ValueError:
        raise LauncherError("{} must be a menu number".format(subject))
    if not 1 <= choice <= len(choices):
        raise LauncherError("{} is out of range".format(subject))
    return choices[choice - 1][0]


def _exact_bundle_category(bundle: Bundle) -> str:
    if bundle.application != "comfyui":
        if bundle.application in ("llama-cpp", "dwarfstar"):
            return bundle.application
        raise LauncherError(
            "exact bundle {!r} has no browser category".format(
                bundle.identifier
            )
        )

    groups = set(bundle.groups)
    matches = []
    if groups.intersection(("qwen", "krea")):
        matches.append("comfyui-images")
    if groups.intersection(("wan", "ltx", "hunyuan")):
        matches.append("comfyui-videos")
    if "ltx-camera" in groups:
        matches.append("comfyui-addons")
    if len(matches) != 1:
        raise LauncherError(
            "exact bundle {!r} must have exactly one browser category".format(
                bundle.identifier
            )
        )
    return matches[0]


def _exact_bundles(
    catalog: Catalog, category: str
) -> Tuple[Bundle, ...]:
    return tuple(
        bundle
        for bundle in catalog.bundles.values()
        if _exact_bundle_category(bundle) == category
    )


def _exact_categories(
    catalog: Catalog, application: Optional[str] = None
) -> Tuple[Tuple[str, str], ...]:
    allowed = {
        "comfyui": {"comfyui-images", "comfyui-videos", "comfyui-addons"},
        "llama-cpp": {"llama-cpp"},
        "dwarfstar": {"dwarfstar"},
    }
    if application is None:
        selected = None
    else:
        try:
            selected = allowed[application]
        except KeyError:
            raise LauncherError(
                "application {!r} has no exact-bundle browser".format(
                    application
                )
            )
    categories = []
    for identifier, description in EXACT_BUNDLE_CATEGORIES:
        if selected is not None and identifier not in selected:
            continue
        count = len(_exact_bundles(catalog, identifier))
        if count:
            categories.append(
                (
                    identifier,
                    "{} ({} {})".format(
                        description,
                        count,
                        "bundle" if count == 1 else "bundles",
                    ),
                )
            )
    return tuple(categories)


def _exact_bundle_display_identifier(
    bundle: Bundle, category: str
) -> str:
    prefixes = {
        "llama-cpp": "llama-",
        "dwarfstar": "dwarfstar-",
    }
    prefix = prefixes.get(category, "")
    if prefix and bundle.identifier.startswith(prefix):
        return bundle.identifier[len(prefix) :]
    return bundle.identifier


def _interactive_exact_bundle_selection(
    catalog: Catalog, application: Optional[str] = None
) -> str:
    categories = _exact_categories(catalog, application)
    if not categories:
        raise LauncherError("no exact bundles are available")
    if len(categories) == 1:
        category = categories[0][0]
    else:
        print()
        category = _interactive_menu_choice(
            "Browse exact bundles:",
            categories,
            subject="bundle category",
        )
    bundles = _exact_bundles(catalog, category)
    description = dict(EXACT_BUNDLE_CATEGORIES)[category]
    print()
    print(style("{}:".format(description), "heading"))
    print_numbered_choices(
        (
            (
                _exact_bundle_display_identifier(bundle, category),
                human_size(catalog.bundle_size(bundle)),
                bundle.description,
            )
            for bundle in bundles
        ),
        columns=(
            ColumnSpec(role="command"),
            ColumnSpec(align=">"),
            ColumnSpec(),
        ),
    )
    try:
        raw = input(
            prompt("Choose bundle [1-{}]: ".format(len(bundles)))
        )
    except EOFError:
        raw = ""
    try:
        choice = int(raw)
    except ValueError:
        raise LauncherError("bundle selection must be a menu number")
    if not 1 <= choice <= len(bundles):
        raise LauncherError("bundle selection is out of range")
    return bundles[choice - 1].identifier


def _interactive_application_selection(
    catalog: Catalog, application: str
) -> Tuple[str, Optional[str]]:
    choices = tuple(
        (recipe.identifier, recipe.description)
        for recipe in CONTENT_APPLICATION_RECIPES[application]
    ) + (
        (
            "browse-bundles",
            "browse all exact {} bundles".format(
                dict(CONTENT_APPLICATIONS)[application]
            ),
        ),
    )
    selection = _interactive_menu_choice(
        "{} content:".format(dict(CONTENT_APPLICATIONS)[application]),
        choices,
    )
    if selection == "browse-bundles":
        return (
            _interactive_exact_bundle_selection(catalog, application),
            None,
        )
    return application, selection


def _interactive_content_target(
    catalog: Catalog, target: Optional[str] = None
) -> Tuple[str, Optional[str]]:
    if not sys.stdin.isatty():
        raise LauncherError(
            "content install requires TARGET in noninteractive use; "
            "run './rocmplete content list' to inspect choices"
        )
    if target in CONTENT_APPLICATION_RECIPES:
        print()
        return _interactive_application_selection(catalog, target)
    if target == "family":
        print()
        selection = _interactive_menu_choice(
            "Model families:", CONTENT_FAMILIES
        )
        return target, selection
    if target is not None:
        return target, None

    top_level = tuple(
        (application, label) for application, label in CONTENT_APPLICATIONS
    ) + (("exact-bundles", "browse exact bundles by category"),)
    target = _interactive_menu_choice("Install content for:", top_level)
    if target in CONTENT_APPLICATION_RECIPES:
        print()
        return _interactive_application_selection(catalog, target)
    return _interactive_exact_bundle_selection(catalog), None


def _command_content_install(
    arguments: argparse.Namespace,
    catalog: Catalog,
    before_mutation: Optional[Callable[[], None]] = None,
) -> int:
    if arguments.local_mirror_move and arguments.local_mirror is None:
        raise LauncherError(
            "--local-mirror-move requires --local-mirror PATH"
        )
    pack_paths = tuple(arguments.from_file)
    if pack_paths and (arguments.target or arguments.selection):
        raise LauncherError(
            "--from-file cannot be combined with an explicit TARGET"
        )
    if pack_paths and arguments.interactive:
        raise LauncherError(
            "--from-file already selects its bundles and cannot be combined "
            "with --interactive"
        )
    if arguments.interactive and arguments.target:
        raise LauncherError(
            "--interactive cannot be combined with an explicit TARGET"
        )
    guided = (
        arguments.interactive
        or arguments.target is None
        or (
            arguments.selection is None
            and arguments.target
            in tuple(CONTENT_APPLICATION_RECIPES) + ("family",)
        )
    )
    if pack_paths:
        catalog, identifiers = load_content_packs(catalog, pack_paths)
        bundles = tuple(catalog.bundle(identifier) for identifier in identifiers)
        aggregate = True
        selection_name = "local content packs"
        print(style("Content packs:", "heading"))
        for path in getattr(arguments, "display_pack_paths", pack_paths):
            print("  {}".format(path))
        print()
    else:
        if guided:
            if arguments.non_interactive:
                raise LauncherError(
                    "noninteractive content installation requires a complete "
                    "application selection, family, or exact bundle; "
                    "run './rocmplete content list' to inspect choices"
                )
            arguments.target, arguments.selection = _interactive_content_target(
                catalog, arguments.target
            )
        bundles = _resolve_content_bundles(
            catalog, arguments.target, arguments.selection
        )
        aggregate = (
            arguments.target == "all"
            or arguments.target == "family"
            or arguments.target in CONTENT_APPLICATION_RECIPES
        )
        selection_name = " ".join(
            item
            for item in (arguments.target, arguments.selection)
            if item is not None
        )
    workflows = tuple(
        catalog.workflow(identifier)
        for identifier in dict.fromkeys(
            bundle.workflow for bundle in bundles if bundle.workflow
        )
    )
    data_dir = _content_data_dir(
        arguments.data_dir, prepare=not arguments.dry_run
    )
    local_mirror = (
        LocalMirror(arguments.local_mirror, move=arguments.local_mirror_move)
        if arguments.local_mirror is not None
        else None
    )
    if local_mirror is not None:
        local_mirror.validate_destination(data_dir)
        print(
            "{} {} ({})".format(
                style("Local mirror:", "label"),
                local_mirror.root,
                "move verified files"
                if local_mirror.move
                else "copy verified files",
            )
        )
        if arguments.dry_run:
            print(
                style(
                    "Mirror candidates are not hashed during a dry run; "
                    "the download total is the worst case.",
                    "muted",
                )
            )
        elif local_mirror.move:
            print(
                "{} verified source files will be removed from the mirror.".format(
                    style("WARNING:", "warning")
                )
            )
    download_image = _content_tools_image(arguments.image)
    # The content-tools override belongs only to download execution. Curated
    # sources are resources of the managed ComfyUI application image.
    workflow_image = APPLICATIONS["comfyui"].image
    summary_only = getattr(arguments, "summary_only", False)
    if summary_only:
        statuses = print_selection_summary(catalog, bundles, data_dir)
    elif aggregate:
        statuses = print_selection_plan(
            catalog, bundles, data_dir, selection_name
        )
    else:
        statuses = print_plan(catalog, bundles[0], data_dir)
    if any(
        item.state in ("size-mismatch", "user-file") for item in statuses
    ):
        raise LauncherError(
            "selection contains existing files with unexpected sizes; "
            "move or remove them explicitly first"
        )
    if summary_only and workflows:
        print(
            "{} {}".format(
                style(
                    "Workflow:" if len(workflows) == 1 else "Workflows:",
                    "label",
                ),
                ", ".join(workflow.identifier for workflow in workflows),
            )
        )
    elif aggregate and workflows:
        print(style("Workflows:", "heading"))
        for workflow in workflows:
            print(
                "  {} -> {}".format(
                    workflow.identifier,
                    workflow_destination(data_dir, workflow),
                )
            )
    elif workflows:
        print(
            "Workflow:    {} -> {}".format(
                workflows[0].identifier,
                workflow_destination(data_dir, workflows[0]),
            )
        )
    _print_selection_agreements(catalog, bundles)
    if arguments.dry_run:
        return 0
    _require_selection_license_acceptance(
        catalog,
        bundles,
        arguments.accept_license,
        non_interactive=arguments.non_interactive,
    )
    acknowledged = _acknowledge_unverified_downloads(
        statuses,
        arguments.acknowledge_license_risk,
        non_interactive=arguments.non_interactive,
    )
    with content_install_lock(data_dir):
        if before_mutation is not None:
            before_mutation()
        if aggregate:
            result = install_artifacts(
                selection_artifacts(catalog, bundles),
                data_dir,
                download_image,
                acknowledge_license_risk=acknowledged,
                local_mirror=local_mirror,
            )
        else:
            result = install_bundle(
                catalog,
                bundles[0],
                data_dir,
                download_image,
                acknowledge_license_risk=acknowledged,
                local_mirror=local_mirror,
            )
        if result != 0:
            return result
        for workflow in workflows:
            install_workflow(
                workflow,
                data_dir,
                workflow_image,
                force=arguments.force_workflow,
            )
    if aggregate:
        print(
            "\n{} {} {} and {} {}.".format(
                style("Content ready:", "success"),
                len(bundles),
                "bundle" if len(bundles) == 1 else "bundles",
                len(workflows),
                "workflow" if len(workflows) == 1 else "workflows",
            )
        )
    elif workflows:
        print(
            "\n{} Start ComfyUI and open workflow {!r}.".format(
                style("Content ready.", "success"),
                workflows[0].identifier,
            )
        )
    else:
        print(
            "\n{} Native application models are installed.".format(
                style("Content ready.", "success")
            )
        )
    if getattr(arguments, "suppress_next_actions", False):
        return 0
    applications = tuple(dict.fromkeys(
        bundle.application for bundle in bundles
    ))
    selected_bundle_ids = {bundle.identifier for bundle in bundles}
    selected_recipe = None
    if (
        not pack_paths
        and arguments.target in CONTENT_APPLICATION_RECIPES
        and arguments.selection not in (None, "all")
    ):
        selected_recipe = content_recipe(
            arguments.target, arguments.selection
        )
    actions = []
    for application in applications:
        if application == "llama-cpp":
            presets = tuple(
                preset
                for preset in catalog.llama_presets.values()
                if preset.bundle in selected_bundle_ids
            )
            if presets:
                actions.append(
                    (
                        "./rocmplete run llama-cpp server "
                        "--router --models-max 1",
                        "Start the managed API router for installed presets.",
                    )
                )
            if any(preset.agent_tools for preset in presets):
                actions.extend(
                    (
                        (
                            "./rocmplete agent opencode",
                            "Start the guarded OpenCode client after the "
                            "router is ready.",
                        ),
                        (
                            "./rocmplete agent pi",
                            "Or start the guarded Pi client against the same "
                            "router.",
                        ),
                        (
                            "./rocmplete agent omp",
                            "Or start the guarded OMP client against the same "
                            "router.",
                        ),
                        (
                            "./rocmplete agent maki",
                            "Or start the guarded Maki client against the same "
                            "router.",
                        ),
                    )
                )
            actions.extend(
                (
                    "./rocmplete run llama-cpp cli --preset {}".format(
                        preset.identifier
                    ),
                    "Or chat with this preset directly in the terminal.",
                )
                for preset in presets
            )
            continue
        if (
            selected_recipe is not None
            and selected_recipe.application == application
        ):
            actions.append((selected_recipe.next_command, ""))
            continue
        command = APPLICATIONS[application].after_content
        if command:
            actions.append((command, ""))
    next_actions(actions)
    return 0


def _acceptance_build_arguments(target: str) -> argparse.Namespace:
    return argparse.Namespace(
        application=target,
        no_layer_cache=False,
        no_cache=False,
        image=None,
    )


def _acceptance_content_arguments(
    bundle: Bundle, arguments: argparse.Namespace
) -> argparse.Namespace:
    return argparse.Namespace(
        target=bundle.identifier,
        selection=None,
        interactive=False,
        non_interactive=arguments.non_interactive,
        from_file=[],
        local_mirror=None,
        local_mirror_move=False,
        data_dir=arguments.data_dir,
        image=None,
        dry_run=False,
        force_workflow=False,
        accept_license=arguments.accept_license,
        acknowledge_license_risk=arguments.acknowledge_license_risk,
        suppress_next_actions=True,
        summary_only=True,
    )


def _acceptance_bundle_ready(
    catalog: Catalog,
    bundle: Bundle,
    data_dir: Path,
    verification_store: Optional[VerificationStore] = None,
) -> bool:
    return all(
        content_status_ready(status)
        for status in inspect_bundle(
            catalog, bundle, data_dir, verification_store
        )
    )


def _confirm_acceptance_preparation(
    arguments: argparse.Namespace, description: str
) -> None:
    if arguments.prepare:
        return
    if arguments.non_interactive or not sys.stdin.isatty():
        raise LauncherError(
            "{}; rerun with --prepare to authorize builds and downloads".format(
                description
            )
        )
    try:
        answer = input(prompt("{} Continue? [y/N] ".format(description)))
    except EOFError:
        answer = ""
    if answer.strip().lower() not in ("y", "yes"):
        raise LauncherError("acceptance preparation was declined")


def _print_acceptance_plan(
    catalog: Catalog,
    cases: Sequence[Tuple[AcceptanceCase, str]],
    image_states: Sequence[Tuple[str, str, bool]],
    bundle_states: Sequence[Tuple[Bundle, bool]],
    *,
    profile: str,
    architecture: str,
    render_node: str,
) -> None:
    print(style("Smoke acceptance plan:", "heading"))
    print("  {} {}".format(style("Architecture:", "label"), architecture))
    print("  {} {}".format(style("Profile:", "label"), profile))
    print("  {} {}".format(style("Render node:", "label"), render_node))
    print()
    print(style("Cases:", "heading"))
    for case, reason in cases:
        state = "N/P" if reason else "run"
        print(
            "  {}  {}{}".format(
                format_state(state, 8),
                case.description,
                " — {}".format(reason) if reason else "",
            )
        )
    print()
    print(style("Images:", "heading"))
    for target, image, ready in image_states:
        print(
            "  {}  {} ({})".format(
                format_state("ready" if ready else "build", 8),
                target,
                image,
            )
        )
    print()
    print(style("Content:", "heading"))
    if not bundle_states:
        print("  none required")
    for bundle, ready in bundle_states:
        print(
            "  {}  {} ({})".format(
                format_state("ready" if ready else "install", 8),
                bundle.identifier,
                human_size(catalog.bundle_size(bundle)),
            )
        )
    missing_size = sum(
        catalog.bundle_size(bundle)
        for bundle, ready in bundle_states
        if not ready
    )
    if missing_size:
        print(
            "  {}  {} {}".format(
                " " * 8,
                style("Worst-case download:", "label"),
                human_size(missing_size),
            )
        )


def _acceptance_result_path(
    arguments: argparse.Namespace, data_dir: Path
) -> Path:
    if arguments.resume:
        try:
            path = Path(arguments.resume).expanduser().resolve(strict=True)
        except OSError as error:
            raise LauncherError(
                "cannot resolve acceptance result: {}".format(error)
            )
        if not path.is_file():
            raise LauncherError("acceptance result is not a regular file")
        if path.suffix != ".json":
            raise LauncherError("acceptance result must use a .json suffix")
        return path
    if arguments.output:
        path = Path(arguments.output).expanduser().resolve(strict=False)
        if path.suffix != ".json":
            raise LauncherError("acceptance result must use a .json suffix")
        if path.exists() or path.is_symlink():
            raise LauncherError(
                "refusing to replace acceptance result: {}".format(path)
            )
        report = path.with_suffix(".md")
        if report.exists() or report.is_symlink():
            raise LauncherError(
                "refusing to replace acceptance report: {}".format(report)
            )
        if not path.parent.is_dir():
            raise LauncherError(
                "acceptance result parent is not a directory: {}".format(
                    path.parent
                )
            )
        return path
    return default_acceptance_result_path(data_dir)


def _review_acceptance_entry(
    entry: MutableMapping[str, object],
    *,
    non_interactive: bool,
) -> None:
    if entry.get("status") != "blocked" or entry.get("visual") is not True:
        return
    artifacts = entry.get("artifacts", [])
    print()
    print(
        "{} {}".format(
            style("Review:", "heading"),
            entry.get("description", entry.get("identifier", "output")),
        )
    )
    if isinstance(artifacts, list):
        print(style("Artifacts:", "label"))
        for artifact in artifacts:
            print("  {}".format(artifact))
    criteria = entry.get("review_criteria", [])
    if isinstance(criteria, list) and criteria:
        print(style("Smoke pass criteria:", "label"))
        for criterion in criteria:
            print("  - {}".format(criterion))
    if non_interactive:
        print(
            style(
                "Visual sanity remains BLOCKED; resume interactively to review.",
                "warning",
            )
        )
        return
    if not sys.stdin.isatty():
        print(
            style(
                "Visual sanity remains BLOCKED because stdin is not a terminal.",
                "warning",
            )
        )
        return
    try:
        answer = input(
            prompt("Output sanity [p]ass, [f]ail, [d]efer: ")
        ).strip().lower()
    except EOFError:
        answer = ""
    if answer in ("p", "pass"):
        entry["status"] = "pass"
        entry["reason"] = None
        entry["reviewed_at"] = datetime.now(timezone.utc).isoformat()
    elif answer in ("f", "fail"):
        entry["status"] = "fail"
        entry["reason"] = "generated output failed human sanity review"
        entry["reviewed_at"] = datetime.now(timezone.utc).isoformat()
    elif answer not in ("", "d", "defer"):
        print(style("Unrecognized review choice; leaving BLOCKED.", "warning"))


def _print_acceptance_review_summary(
    result: Mapping[str, object], result_path: Path
) -> None:
    entries = result.get("cases")
    if not isinstance(entries, list):
        return
    reviewed = [
        entry
        for entry in entries
        if isinstance(entry, dict)
        and entry.get("visual") is True
        and isinstance(entry.get("artifacts"), list)
        and entry.get("artifacts")
    ]
    if not reviewed:
        return
    groups = (
        ("pass", "pass"),
        ("fail", "fail"),
        ("blocked", "deferred"),
    )
    print()
    print(style("Visual reviews:", "heading"))
    for status, label in groups:
        identifiers = [
            str(entry.get("identifier", "unknown"))
            for entry in reviewed
            if entry.get("status") == status
        ]
        print(
            "  {}  {}".format(
                format_state(label, 10),
                ", ".join(identifiers) if identifiers else "none",
            )
        )
    if any(entry.get("status") == "blocked" for entry in reviewed):
        next_step(
            "./rocmplete acceptance run --resume {}".format(
                shlex.quote(str(result_path))
            )
        )


def command_acceptance(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    if arguments.acceptance_command is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose run",
            ACCEPTANCE_EXAMPLES,
        )

    data_dir = _content_data_dir(arguments.data_dir, prepare=False)
    result_path = _acceptance_result_path(arguments, data_dir)
    resumed_acceptance = (
        load_acceptance_result(result_path)
        if arguments.resume
        else None
    )

    podman.require_rootless()
    render_nodes = select_render_nodes(
        requested_render_nodes(arguments.render_node)
    )
    if len(render_nodes) != 1:
        raise LauncherError(
            "acceptance currently requires exactly one --render-node"
        )
    render_node = render_nodes[0]
    check_gpu_device_access(render_nodes)
    requested_profile = validate_profile(arguments.profile)
    if requested_profile == "cpu":
        raise LauncherError("acceptance requires a supported ROCm GPU profile")
    port = validate_port(arguments.port)
    if arguments.dry_run:
        verification_store = VerificationStore.load(data_dir)
        provisional_profile = requested_profile
        cases = selected_acceptance_cases(
            provisional_profile, arguments.application
        )
        images = acceptance_required_images(cases)
        bundles = acceptance_required_bundles(catalog, cases)
        image_states = tuple(
            (target, image, podman.image_exists(image))
            for target, image in images
        )
        bundle_states = tuple(
            (
                bundle,
                _acceptance_bundle_ready(
                    catalog, bundle, data_dir, verification_store
                ),
            )
            for bundle in bundles
        )
        _print_acceptance_plan(
            catalog,
            cases,
            image_states,
            bundle_states,
            profile=provisional_profile,
            architecture=(
                "detected at execution"
                if provisional_profile == "auto"
                else "validated at execution"
            ),
            render_node=render_node,
        )
        print()
        print(style("No image, content, container, or result was changed.", "muted"))
        return 0

    if not podman.image_exists(ROCM_BASE_IMAGE):
        _confirm_acceptance_preparation(
            arguments,
            "The diagnostic base image is missing and must be built first.",
        )
        result = command_build(_acceptance_build_arguments("base"))
        if result != 0:
            return result

    hardware = probe_hardware(ROCM_BASE_IMAGE, render_node)
    profile = hardware["Profile"]
    architecture = hardware["Architecture"]
    if requested_profile != "auto" and requested_profile != profile:
        raise LauncherError(
            "requested profile {!r} does not match detected architecture "
            "{!r} ({})".format(
                requested_profile,
                architecture,
                profile,
            )
        )

    cases = selected_acceptance_cases(profile, arguments.application)
    images = acceptance_required_images(cases)
    bundles = acceptance_required_bundles(catalog, cases)
    verification_store = VerificationStore.load(data_dir)
    image_states = tuple(
        (target, image, podman.image_exists(image))
        for target, image in images
    )
    bundle_states = tuple(
        (
            bundle,
            _acceptance_bundle_ready(
                catalog, bundle, data_dir, verification_store
            ),
        )
        for bundle in bundles
    )
    _print_acceptance_plan(
        catalog,
        cases,
        image_states,
        bundle_states,
        profile=profile,
        architecture=architecture,
        render_node=render_node,
    )

    missing_images = tuple(
        (target, image)
        for target, image, ready in image_states
        if not ready
    )
    missing_bundles = tuple(
        bundle for bundle, ready in bundle_states if not ready
    )
    if missing_images or missing_bundles:
        descriptions = []
        if missing_images:
            descriptions.append(
                "{} image{}".format(
                    len(missing_images),
                    "" if len(missing_images) == 1 else "s",
                )
            )
        if missing_bundles:
            descriptions.append(
                "{} content bundle{}".format(
                    len(missing_bundles),
                    "" if len(missing_bundles) == 1 else "s",
                )
            )
        _confirm_acceptance_preparation(
            arguments,
            "Preparation requires {}.".format(" and ".join(descriptions)),
        )

    for target, _ in missing_images:
        result = command_build(_acceptance_build_arguments(target))
        if result != 0:
            return result

    if missing_bundles and not podman.image_exists(CONTENT_TOOLS_IMAGE):
        result = command_build(_acceptance_build_arguments("content-tools"))
        if result != 0:
            return result
    for bundle in missing_bundles:
        result = _command_content_install(
            _acceptance_content_arguments(bundle, arguments), catalog
        )
        if result != 0:
            return result

    unavailable_images = [
        image for _, image in images if not podman.image_exists(image)
    ]
    verification_store = VerificationStore.load(data_dir)
    unavailable_bundles = [
        bundle.identifier
        for bundle in bundles
        if not _acceptance_bundle_ready(
            catalog,
            bundle,
            data_dir,
            verification_store,
        )
    ]
    if unavailable_images or unavailable_bundles:
        raise LauncherError(
            "acceptance preparation did not produce all prerequisites; "
            "images: {}; content: {}".format(
                ", ".join(unavailable_images) or "ready",
                ", ".join(unavailable_bundles) or "ready",
            )
        )

    data_dir = prepare_data_dir(data_dir)
    StorageLayout(data_dir).acceptance_results.mkdir(
        parents=True, exist_ok=True
    )
    for case, reason in cases:
        if reason or case.application is None:
            continue
        container_name = APPLICATIONS[case.application].container_name
        if podman.container_exists(container_name):
            raise LauncherError(
                "managed container {!r} is running; stop {} before "
                "acceptance".format(container_name, case.application)
            )

    image_ids = {
        image: podman.image_id(image) for _, image in images
    }
    identity = source_identity()
    definition = acceptance_definition(
        catalog,
        cases,
        profile=profile,
        architecture=architecture,
        render_node=render_node,
        image_ids=image_ids,
        source_identity=identity,
        memory_policy=arguments.memory_policy,
        kernel_policy=arguments.kernel_policy,
    )
    fingerprint = acceptance_fingerprint(definition)
    if arguments.resume:
        if resumed_acceptance is None:
            raise LauncherError("acceptance resume state was not loaded")
        validate_acceptance_result_fingerprint(
            resumed_acceptance, fingerprint
        )
        acceptance = resumed_acceptance
        acceptance["finished_at"] = None
    else:
        acceptance = create_acceptance_result(
            definition, cases, hardware=hardware
        )
    checkpoint_acceptance(
        result_path, acceptance, create=not bool(arguments.resume)
    )

    pending_identifiers = pending_case_identifiers(acceptance)
    case_map = {case.identifier: case for case, _ in cases}
    for identifier in pending_identifiers:
        entry = case_entry(acceptance, identifier)
        attempt = begin_case(entry)
        checkpoint_acceptance(result_path, acceptance)
        started = time.monotonic()
        print()
        print(
            "{} {}".format(
                style("Running:", "heading"),
                entry.get("description", identifier),
            ),
            flush=True,
        )
        try:
            if identifier == "host-gpu":
                outcome = run_host_case(ROCM_BASE_IMAGE, render_node)
            else:
                case = case_map.get(identifier)
                if case is None:
                    raise LauncherError(
                        "result contains an unknown acceptance case {!r}".format(
                            identifier
                        )
                    )
                outcome = run_application_case(
                    identifier,
                    catalog,
                    data_dir=data_dir,
                    profile=profile,
                    render_node=render_node,
                    port=port,
                    suite_id=str(acceptance["suite_id"]),
                    attempt=attempt,
                    memory_policy=arguments.memory_policy,
                    kernel_policy=arguments.kernel_policy,
                )
            complete_case(entry, outcome, started=started)
            checkpoint_acceptance(result_path, acceptance)
        except KeyboardInterrupt as error:
            fail_case(
                entry,
                error,
                started=started,
                interrupted=True,
            )
            checkpoint_acceptance(result_path, acceptance)
            raise
        except LauncherError as error:
            fail_case(entry, error, started=started)
            print(
                style(
                    "{} failed: {}".format(identifier, error),
                    "error",
                    sys.stderr,
                ),
                file=sys.stderr,
            )
        checkpoint_acceptance(result_path, acceptance)

    for identifier in blocked_visual_identifiers(acceptance):
        entry = case_entry(acceptance, identifier)
        _review_acceptance_entry(
            entry, non_interactive=arguments.non_interactive
        )
        checkpoint_acceptance(result_path, acceptance)

    report_path = finish_acceptance(
        result_path,
        acceptance,
        create_report=not bool(arguments.resume),
    )
    final_status = str(acceptance["status"])
    print()
    print(
        "{} {}".format(
            style(
                "Acceptance {}:".format(final_status.upper()),
                "success" if final_status == "pass" else (
                    "error" if final_status == "fail" else "warning"
                ),
            ),
            result_path,
        )
    )
    print("{} {}".format(style("Report:", "label"), report_path))
    _print_acceptance_review_summary(acceptance, result_path)
    if final_status == "pass":
        return 0
    if final_status == "blocked":
        return 2
    return 1


def command_benchmark(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    action = arguments.benchmark_command
    if action is None:
        return _print_incomplete_command(
            arguments.command_parser,
            "choose run, suite, agent, llama-cpp, "
            "llama-cpp-speculative, or report",
            BENCHMARK_EXAMPLES,
        )
    if action == "agent":
        suite = load_coding_suite()
        if arguments.list_tasks:
            print("Frozen coding-agent tasks")
            for task in suite.tasks:
                print(
                    "  {:30} {:14} {:15} {:6} {}".format(
                        task.identifier,
                        task.kind,
                        task.toolchain,
                        task.difficulty,
                        task.repository,
                    )
                )
            return 0
        if not arguments.preset and not arguments.dwarfstar:
            raise LauncherError(
                "benchmark agent requires --preset PRESET or --dwarfstar"
            )
        data_dir = _content_data_dir(arguments.data_dir, prepare=False)
        output = (
            Path(arguments.output).expanduser().resolve()
            if arguments.output
            else None
        )
        selected_preset = (
            catalog.llama_preset(arguments.preset)
            if arguments.preset
            else None
        )
        result_path, _ = run_agent_evaluation(
            catalog,
            AgentEvaluationOptions(
                data_dir=data_dir,
                preset=arguments.preset or "",
                dwarfstar=arguments.dwarfstar,
                tasks=tuple(arguments.task),
                repetitions=arguments.repetitions,
                context=arguments.context or AGENT_EVALUATION_DEFAULT_CONTEXT,
                thinking=arguments.thinking or (
                    reasoning_client_default(selected_preset)
                    if selected_preset is not None
                    else "high"
                ),
                profile=arguments.profile,
                backend=arguments.backend,
                port=validate_port(arguments.port),
                render_nodes=tuple(arguments.render_node or ()),
                output=output,
                keep_going=arguments.keep_going,
                dry_run=arguments.dry_run,
            ),
        )
        if arguments.dry_run:
            print(style("No source was fetched and no process was started.", "muted"))
        else:
            print(
                "{} {}".format(
                    style("Coding-agent evaluation complete:", "success"),
                    result_path,
                )
            )
            print("{} {}".format(style("Report:", "label"), result_path.with_suffix(".md")))
        return 0
    if action == "llama-cpp":
        return _command_llama_benchmark(arguments, catalog)
    if action == "llama-cpp-speculative":
        return _command_llama_speculative_benchmark(arguments, catalog)
    if action == "report":
        if arguments.output and arguments.report_format == "both":
            raise LauncherError(
                "--output requires --report-format markdown or html"
            )
        try:
            suite = json.loads(Path(arguments.subject).read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise LauncherError(
                "cannot read benchmark suite {}: {}".format(
                    arguments.subject, error
                )
            )
        if not isinstance(suite, dict):
            raise LauncherError("benchmark suite must contain a JSON object")
        formats = (
            ("markdown", "html")
            if arguments.report_format == "both"
            else (arguments.report_format,)
        )
        for report_format in formats:
            rendered = (
                render_suite_markdown(suite)
                if report_format == "markdown"
                else render_suite_html(suite)
            )
            if arguments.output:
                output = Path(arguments.output)
            else:
                suffix = ".md" if report_format == "markdown" else ".html"
                output = Path(arguments.subject).with_suffix(suffix)
            try:
                output.write_text(rendered)
            except OSError as error:
                raise LauncherError(
                    "cannot write benchmark report {}: {}".format(output, error)
                )
            print(
                "{} {}".format(
                    style("Benchmark report:", "success"), output
                )
            )
        return 0

    data_dir = _content_data_dir(
        arguments.data_dir, prepare=not arguments.dry_run
    )
    if not arguments.dry_run:
        StorageLayout(data_dir).prepare_runtime("comfyui")
    profile = validate_profile(
        arguments.profile
        or environment_value(os.environ, "PROFILE", "auto")
    )
    if profile == "cpu":
        raise LauncherError(
            "managed benchmarks require a supported ROCm GPU profile"
        )
    if arguments.runs < 1:
        raise LauncherError("--runs must be at least 1")
    if arguments.seed < 0 or arguments.seed > 2**53 - 1:
        raise LauncherError("--seed must be between 0 and 2^53-1")
    port = validate_port(
        arguments.port
        or environment_value(
            os.environ, "BENCHMARK_PORT", str(DEFAULT_BENCHMARK_PORT)
        )
    )
    render_nodes = select_render_nodes(
        requested_render_nodes(arguments.render_node)
    )
    if len(render_nodes) != 1:
        raise LauncherError(
            "managed ComfyUI benchmarks currently require exactly one "
            "--render-node"
        )
    render_node = render_nodes[0]
    check_gpu_device_access(render_nodes)
    options = BenchmarkOptions(
        image=_comfyui_image(arguments.image),
        profile=profile,
        port=port,
        data_dir=data_dir,
        render_node=render_node,
        runs=arguments.runs,
        seed=arguments.seed,
        unconfined=arguments.unconfined,
        dry_run=arguments.dry_run,
        memory_policy=validate_memory_policy(
            arguments.memory_policy
            or environment_value(
                os.environ, "MEMORY_POLICY", "balanced"
            )
        ),
        kernel_policy=validate_kernel_policy(
            arguments.kernel_policy
            or environment_value(
                os.environ, "KERNEL_POLICY", "default"
            )
        ),
        cache_mode=arguments.cache_mode,
    )
    if action == "suite":
        selected = []
        for bundle in catalog.bundles.values():
            if bundle.identifier not in catalog.benchmarks:
                continue
            family_match = (
                arguments.family is not None
                and arguments.family in bundle.groups
            )
            explicit_match = bundle.identifier in arguments.include
            if arguments.family and arguments.include:
                wanted = family_match or explicit_match
            elif arguments.family:
                wanted = family_match
            elif arguments.include:
                wanted = explicit_match
            else:
                wanted = True
            if not wanted:
                continue
            selected.append(bundle)
        unknown = sorted(set(arguments.include) - set(catalog.bundles))
        if unknown:
            raise LauncherError(
                "unknown suite bundles: {}".format(", ".join(unknown))
            )
        bundles = tuple(selected)
        if not bundles:
            raise LauncherError("benchmark suite selection is empty")
        _print_selection_agreements(catalog, bundles)
        if not arguments.dry_run:
            _require_selection_license_acceptance(
                catalog,
                bundles,
                arguments.accept_license,
                non_interactive=arguments.non_interactive,
            )
        result = run_benchmark_suite(
            catalog,
            bundles,
            options,
            podman.selinux_volume_suffix(),
            resume_path=Path(arguments.resume) if arguments.resume else None,
            keep_going=arguments.keep_going,
            report_format=arguments.report_format,
        )
        if arguments.dry_run:
            print(style("No container was started.", "muted"))
        else:
            print(
                "{} {}".format(
                    style("Benchmark suite complete:", "success"),
                    result,
                )
            )
        return 0

    bundle = catalog.bundle(arguments.bundle)
    _print_agreements(catalog, bundle)
    if not arguments.dry_run:
        _require_license_acceptance(
            catalog,
            bundle,
            arguments.accept_license,
            non_interactive=arguments.non_interactive,
        )
    result = run_benchmark(
        catalog,
        bundle,
        options,
        podman.selinux_volume_suffix(),
    )
    if arguments.dry_run:
        print(style("No container was started.", "muted"))
    else:
        print(
            "{} {}".format(
                style("Benchmark complete:", "success"), result
            )
        )
    return 0


def _command_llama_benchmark(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    if bool(arguments.model) == bool(arguments.preset):
        raise LauncherError("choose exactly one of --model or --preset")
    for name, value in (
        ("--repetitions", arguments.repetitions),
        ("--prompt-tokens", arguments.prompt_tokens),
        ("--generation-tokens", arguments.generation_tokens),
        ("--batch-size", arguments.batch_size),
        ("--ubatch-size", arguments.ubatch_size),
    ):
        if value < 1:
            raise LauncherError("{} must be at least 1".format(name))
    if arguments.context_depth < 0:
        raise LauncherError("--context-depth must be zero or positive")
    if arguments.ubatch_size > arguments.batch_size:
        raise LauncherError("--ubatch-size must not exceed --batch-size")
    if arguments.cache_type_v != "f16" and arguments.flash_attn != "on":
        raise LauncherError(
            "a quantized value cache requires --flash-attn on"
        )
    profile = validate_profile(
        arguments.profile
        or environment_value(os.environ, "PROFILE", "auto")
    )
    if arguments.compare_backends and profile == "cpu":
        raise LauncherError("--compare-backends requires a GPU profile")
    render_nodes = requested_render_nodes(arguments.render_node)
    if profile != "cpu":
        render_nodes = select_render_nodes(render_nodes)
        check_gpu_device_access(render_nodes)
    else:
        render_nodes = ()
    requested_data = selected_data_dir(arguments.data_dir)
    data_dir = inspect_data_path(requested_data)
    output = (
        Path(arguments.output).expanduser().resolve(strict=False)
        if arguments.output
        else None
    )
    if output is not None and output.exists():
        raise LauncherError(
            "refusing to replace existing benchmark result: {}".format(output)
        )

    model = None
    managed_model = ""
    if arguments.model:
        model = _resolved_regular_file(arguments.model, "GGUF model")
        if model.suffix.lower() != ".gguf":
            raise LauncherError("--model must name a .gguf file")
        status = model.stat()
        model_metadata = {
            "kind": "local",
            "path": str(model),
            "size": status.st_size,
            "mtime_ns": status.st_mtime_ns,
        }
    else:
        preset = catalog.llama_preset(arguments.preset)
        if preset.speculative_type:
            raise LauncherError(
                (
                    "preset '{}' enables {}, but llama-bench does not "
                    "exercise ROCmplete's speculative runtime policy; "
                    "benchmark a non-speculative preset or measure the "
                    "preset through the llama.cpp server API"
                ).format(preset.identifier, preset.speculative_type)
            )
        managed_model, installed = _llama_preset_status(
            catalog, preset.identifier, data_dir
        )
        artifact = catalog.artifact(preset.artifact)
        model_metadata = {
            "kind": "catalog",
            "preset": preset.identifier,
            "path": str(installed),
            "repository": artifact.source.repository,
            "revision": artifact.source.revision,
            "source_path": artifact.source.path,
            "size": artifact.size,
            "sha256": artifact.sha256,
        }
    if not arguments.dry_run:
        data_dir = prepare_data_dir(requested_data)
        StorageLayout(data_dir).prepare_runtime("llama-cpp")
    image = arguments.image or APPLICATIONS["llama-cpp"].image
    backends = (
        ("rocm", "vulkan")
        if arguments.compare_backends
        else (arguments.backend,)
    )
    commands = {}
    for backend in backends:
        options = LlamaBenchmarkOptions(
            image=image,
            profile=profile,
            data_dir=data_dir,
            backend=backend,
            model=model,
            managed_model=managed_model,
            render_nodes=render_nodes,
            repetitions=arguments.repetitions,
            prompt_tokens=arguments.prompt_tokens,
            generation_tokens=arguments.generation_tokens,
            context_depth=arguments.context_depth,
            batch_size=arguments.batch_size,
            ubatch_size=arguments.ubatch_size,
            cache_type_k=arguments.cache_type_k,
            cache_type_v=arguments.cache_type_v,
            flash_attention=arguments.flash_attn,
            unconfined=arguments.unconfined,
        )
        commands[backend] = llama_benchmark_command(
            options, podman.selinux_volume_suffix()
        )
    print("{} {}".format(style("Model:", "label"), model_metadata["path"]))
    if arguments.compare_backends:
        print("{} ROCm, Vulkan".format(style("Backends:", "label")))
    else:
        print("{} {}".format(style("Backend:", "label"), arguments.backend))
    print(
        "{} depth {}, pp{}, tg{}, batch {}/{}, KV {}/{}, FA {}, "
        "{} repetitions".format(
            style("Parameters:", "label"),
            arguments.context_depth,
            arguments.prompt_tokens,
            arguments.generation_tokens,
            arguments.batch_size,
            arguments.ubatch_size,
            arguments.cache_type_k,
            arguments.cache_type_v,
            arguments.flash_attn,
            arguments.repetitions,
        )
    )
    if arguments.dry_run:
        for backend in backends:
            print(
                "\n{} {}".format(
                    style("Backend:", "label"), _llama_backend_name(backend)
                )
            )
            print(
                "{}\n  {}".format(
                    style("Resolved command:", "heading"),
                    style(shlex.join(commands[backend]), "command"),
                )
            )
        print(style("No container was started.", "muted"))
        return 0
    podman.require_rootless()
    if not podman.image_exists(image):
        raise LauncherError(
            "image not found: {} (run './rocmplete build llama-cpp')".format(
                image
            )
        )
    parameters = {
        "repetitions": arguments.repetitions,
        "prompt_tokens": arguments.prompt_tokens,
        "generation_tokens": arguments.generation_tokens,
        "context_depth": arguments.context_depth,
        "batch_size": arguments.batch_size,
        "ubatch_size": arguments.ubatch_size,
        "cache_type_k": arguments.cache_type_k,
        "cache_type_v": arguments.cache_type_v,
        "flash_attention": arguments.flash_attn,
    }
    if not arguments.compare_backends:
        result = run_llama_benchmark(
            commands[arguments.backend],
            data_dir=data_dir,
            image=image,
            profile=profile,
            backend=arguments.backend,
            render_nodes=render_nodes,
            model=model_metadata,
            parameters=parameters,
            output=output,
        )
        print(
            "{} {}".format(style("Benchmark complete:", "success"), result)
        )
        return 0

    results = {}
    errors = {}
    for backend in backends:
        print(
            "\n{} {}".format(
                style("Running backend:", "heading"),
                _llama_backend_name(backend),
            )
        )
        try:
            result = run_llama_benchmark(
                commands[backend],
                data_dir=data_dir,
                image=image,
                profile=profile,
                backend=backend,
                render_nodes=render_nodes,
                model=model_metadata,
                parameters=parameters,
            )
        except LauncherError as error:
            errors[backend] = str(error)
            print(
                style(
                    "{} failed: {}".format(
                        _llama_backend_name(backend), error
                    ),
                    "error",
                    sys.stderr,
                ),
                file=sys.stderr,
            )
            continue
        results[backend] = result
        print(
            "{} {}".format(style("Benchmark complete:", "success"), result)
        )
    comparison_path, comparison = write_backend_comparison(
        data_dir=data_dir,
        image=image,
        profile=profile,
        render_nodes=render_nodes,
        model=model_metadata,
        parameters=parameters,
        results=results,
        errors=errors,
        output=output,
    )
    _print_llama_backend_comparison(comparison)
    print(
        "\n{} {}".format(
            style("Comparison complete:", "success"), comparison_path
        )
    )
    return 1 if errors else 0


def _print_llama_speculative_summary(value: Mapping[str, object]) -> None:
    summary = value.get("summary")
    if not isinstance(summary, dict):
        return
    depths = summary.get("depths")
    if not isinstance(depths, dict) or not depths:
        return
    print("\n{}".format(style("Speculative depth screen", "heading")))
    print(
        "  {:>5} {:>10} {:>11} {:>12} {:>7}".format(
            "Depth", "Generate", "Acceptance", "Prompt", "Trials"
        )
    )
    for raw_depth, raw_metrics in sorted(
        depths.items(), key=lambda item: int(item[0])
    ):
        if not isinstance(raw_metrics, dict):
            continue
        print(
            "  {:>5} {:>7.2f} t/s {:>9.1f}% {:>9.2f} t/s {:>7}".format(
                raw_depth,
                float(raw_metrics["generation_tokens_per_second"]),
                float(raw_metrics["acceptance_percent"]),
                float(raw_metrics["prompt_tokens_per_second"]),
                int(raw_metrics["complete_trials"]),
            )
        )
    winner = summary.get("winner_depth")
    improvement = summary.get("winner_improvement_percent")
    decision = summary.get("screening_decision")
    if winner is not None and improvement is not None:
        print(
            "\n{} depth {} ({:+.2f}% versus incumbent depth {})".format(
                style("Screening winner:", "label"),
                winner,
                float(improvement),
                summary.get("incumbent_depth"),
            )
        )
    if decision == "retest-candidate":
        print(
            style(
                "The winner cleared the screening gate; repeat it against "
                "the incumbent before changing the managed preset.",
                "warning",
            )
        )
    elif decision == "keep-incumbent":
        print("The incumbent remains the supported draft depth.")
    digest_counts = summary.get("response_digest_counts")
    if isinstance(digest_counts, dict):
        divergent = [
            key
            for key, count in digest_counts.items()
            if isinstance(count, int) and count > 1
        ]
        if divergent:
            print(
                style(
                    "WARNING: output diverged across depths for {} paired "
                    "context/seed conditions; inspect responses before "
                    "treating throughput as actionable.".format(
                        len(divergent)
                    ),
                    "warning",
                )
            )


def _command_llama_speculative_benchmark(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    preset = catalog.llama_preset(arguments.preset)
    if not preset.speculative_type:
        raise LauncherError(
            "preset '{}' does not enable speculative decoding".format(
                preset.identifier
            )
        )
    if not is_agent_capable(preset):
        raise LauncherError(
            "preset '{}' has no reviewed Chat Completions policy".format(
                preset.identifier
            )
        )
    maximum_depth = 8 if preset.speculative_type == "draft-mtp" else 15
    depths = tuple(
        sorted(
            set(arguments.draft_depth or range(1, maximum_depth + 1))
        )
    )
    if not depths or any(depth < 1 or depth > maximum_depth for depth in depths):
        raise LauncherError(
            "--draft-depth must be between 1 and {} for {}".format(
                maximum_depth, preset.speculative_type
            )
        )
    if len(depths) != len(arguments.draft_depth or depths):
        raise LauncherError("--draft-depth values must be unique")
    context_depths = tuple(
        sorted(set(arguments.context_depth or LLAMA_SPECULATIVE_CONTEXT_DEPTHS))
    )
    if not context_depths or any(depth < 1 for depth in context_depths):
        raise LauncherError("--context-depth must be positive")
    if len(context_depths) != len(arguments.context_depth or context_depths):
        raise LauncherError("--context-depth values must be unique")
    if arguments.repetitions < 1:
        raise LauncherError("--repetitions must be at least 1")
    if arguments.generation_tokens < 1:
        raise LauncherError("--generation-tokens must be at least 1")
    if not 0.0 <= arguments.draft_probability_min <= 1.0:
        raise LauncherError(
            "--draft-probability-min must be between 0 and 1"
        )
    if arguments.poll is not None and not 0 <= arguments.poll <= 100:
        raise LauncherError("--poll must be between 0 and 100")
    if arguments.batch_size < 1:
        raise LauncherError("--batch-size must be at least 1")
    if arguments.ubatch_size < 1:
        raise LauncherError("--ubatch-size must be at least 1")
    if arguments.ubatch_size > arguments.batch_size:
        raise LauncherError("--ubatch-size must not exceed --batch-size")
    if arguments.cache_type_v not in ("preset", "f16") and (
        arguments.flash_attn != "on"
    ):
        raise LauncherError(
            "a quantized value cache requires --flash-attn on"
        )
    if (
        arguments.seed < 0
        or arguments.seed + arguments.repetitions - 1 > 2**31 - 1
    ):
        raise LauncherError(
            "--seed and repetition-derived seeds must be between 0 and 2^31-1"
        )
    if arguments.context < 1 or arguments.context > preset.default_context:
        raise LauncherError(
            "--context must be between 1 and the preset limit {}".format(
                preset.default_context
            )
        )
    if (
        max(context_depths) + arguments.generation_tokens + 1024
        > arguments.context
    ):
        raise LauncherError(
            "--context must leave at least 1024 tokens beyond the largest "
            "--context-depth and generation allowance"
        )
    profile = validate_profile(arguments.profile)
    if profile == "cpu":
        raise LauncherError("speculative depth sweeps require a GPU profile")
    render_nodes = select_render_nodes(
        requested_render_nodes(arguments.render_node)
    )
    check_gpu_device_access(render_nodes)
    thinking = arguments.thinking or reasoning_client_default(preset)
    native_reasoning = reasoning_native_value(preset, thinking)
    requested_data = selected_data_dir(arguments.data_dir)
    data_dir = inspect_data_path(requested_data)
    managed_model, installed = _llama_preset_status(
        catalog, preset.identifier, data_dir
    )
    managed_draft = (
        catalog.artifact(preset.draft_artifact).destination
        if preset.draft_artifact
        else ""
    )
    artifact = catalog.artifact(preset.artifact)
    model_metadata = {
        "kind": "catalog",
        "preset": preset.identifier,
        "bundle": preset.bundle,
        "path": str(installed),
        "artifact": artifact.identifier,
        "repository": artifact.source.repository,
        "revision": artifact.source.revision,
        "source_path": artifact.source.path,
        "size": artifact.size,
        "sha256": artifact.sha256,
    }
    if preset.draft_artifact:
        draft_artifact = catalog.artifact(preset.draft_artifact)
        model_metadata["draft"] = {
            "artifact": draft_artifact.identifier,
            "repository": draft_artifact.source.repository,
            "revision": draft_artifact.source.revision,
            "source_path": draft_artifact.source.path,
            "size": draft_artifact.size,
            "sha256": draft_artifact.sha256,
        }
    image = arguments.image or APPLICATIONS["llama-cpp"].image
    port = validate_port(arguments.port)
    try:
        output = (
            Path(arguments.output).expanduser().resolve(strict=False)
            if arguments.output
            else None
        )
        resume = (
            Path(arguments.resume).expanduser().resolve(strict=True)
            if arguments.resume
            else None
        )
    except OSError as error:
        raise LauncherError(
            "cannot resolve speculative benchmark result path: {}".format(
                error
            )
        )
    if resume is not None and not resume.is_file():
        raise LauncherError(
            "speculative benchmark resume is not a regular file: {}".format(
                resume
            )
        )
    if output is not None and output.exists():
        raise LauncherError(
            "refusing to replace existing speculative benchmark: {}".format(
                output
            )
        )
    profile_flash_attention = (
        preset.flash_attention
        if arguments.flash_attn == "preset"
        else {
            candidate: arguments.flash_attn for candidate in GPU_PROFILES
        }
    )
    if (arguments.cache_type_k == "preset") != (
        arguments.cache_type_v == "preset"
    ):
        raise LauncherError(
            "--cache-type-k and --cache-type-v must both use preset or "
            "both select an explicit type"
        )
    if (
        arguments.cache_type_k != "preset"
        and arguments.cache_type_k != arguments.cache_type_v
    ):
        raise LauncherError(
            "the managed server currently requires matching key and value "
            "cache types"
        )
    profile_kv_cache = (
        preset.kv_cache
        if arguments.cache_type_k == "preset"
        else {
            candidate: arguments.cache_type_k for candidate in GPU_PROFILES
        }
    )
    commands = {}
    for depth in depths:
        command = llama_command(
            LlamaOptions(
                image=image,
                profile=profile,
                mode="server",
                data_dir=data_dir,
                backend=arguments.backend,
                managed_model=managed_model,
                managed_draft=managed_draft,
                speculative_type=preset.speculative_type,
                draft_tokens=depth,
                context_override_architectures=(
                    preset.context_override_architectures
                ),
                jinja=preset.jinja,
                reasoning_preserve=preset.reasoning_preserve,
                chat_template=preset.chat_template,
                sampling_defaults=_llama_sampling_defaults_json(
                    catalog, preset
                ),
                profile_flash_attention=profile_flash_attention,
                profile_kv_cache=profile_kv_cache,
                render_nodes=render_nodes,
                listen="127.0.0.1",
                port=port,
                context=arguments.context,
                detach=True,
                unconfined=arguments.unconfined,
                container_name=LLAMA_SPECULATIVE_BENCHMARK_CONTAINER_NAME,
                container_role="benchmark",
                auto_remove=False,
            ),
            podman.selinux_volume_suffix(),
        )
        if arguments.graph_optimization:
            image_position = command.index(image)
            command[image_position:image_position] = [
                "--env",
                "GGML_CUDA_GRAPH_OPT=1",
            ]
        if arguments.disable_graphs:
            image_position = command.index(image)
            command[image_position:image_position] = [
                "--env",
                "GGML_CUDA_DISABLE_GRAPHS=1",
            ]
        command.extend(
            (
                "--parallel",
                "1",
                "--spec-draft-p-min",
                str(arguments.draft_probability_min),
                "--batch-size",
                str(arguments.batch_size),
                "--ubatch-size",
                str(arguments.ubatch_size),
            )
        )
        if arguments.draft_backend_sampling == "off":
            command.append("--no-spec-draft-backend-sampling")
        if arguments.ngram_simple:
            # llama.cpp permits repeated --spec-type options and gives
            # draftless implementations priority before falling back to MTP
            # or DFlash. Keep this benchmark-only instead of widening the
            # managed preset policy before target-hardware measurements.
            command.extend(("--spec-type", "ngram-simple"))
        if arguments.poll is not None:
            command.extend(("--poll", str(arguments.poll)))
        if arguments.no_host:
            command.append("--no-host")
        commands[depth] = tuple(command)
    request_count = len(depths) * len(context_depths) * arguments.repetitions
    print("{} {}".format(style("Model:", "label"), preset.identifier))
    print(
        "{} {}, depths {}, targets {}, {} repetitions ({} requests)".format(
            style("Parameters:", "label"),
            preset.speculative_type,
            ",".join(str(depth) for depth in depths),
            ",".join(str(depth) for depth in context_depths),
            arguments.repetitions,
            request_count,
        )
    )
    print(
        "{} {}, seed {}, {} generated tokens, context {}, parallel 1".format(
            style("Condition:", "label"),
            thinking,
            arguments.seed,
            arguments.generation_tokens,
            arguments.context,
        )
    )
    if arguments.dry_run:
        for depth in depths:
            print(
                "\n{} {}\n  {}".format(
                    style("Draft depth:", "label"),
                    depth,
                    style(shlex.join(commands[depth]), "command"),
                )
            )
        print(
            style(
                "No container was started and no checkpoint was written.",
                "muted",
            )
        )
        return 0
    data_dir = prepare_data_dir(requested_data)
    StorageLayout(data_dir).prepare_runtime("llama-cpp")
    podman.require_rootless()
    if not podman.image_exists(image):
        raise LauncherError(
            "image not found: {} (run './rocmplete build llama-cpp')".format(
                image
            )
        )
    if podman.container_exists(LLAMA_SPECULATIVE_BENCHMARK_CONTAINER_NAME):
        raise LauncherError(
            "benchmark container {!r} already exists".format(
                LLAMA_SPECULATIVE_BENCHMARK_CONTAINER_NAME
            )
        )
    # The data path was only inspected while commands were constructed. Its
    # resolved value is unchanged after preparation, so the mount commands
    # remain valid and dry-run never creates it.
    result_path, result = run_speculative_benchmark(
        SpeculativeBenchmarkOptions(
            data_dir=data_dir,
            preset=preset.identifier,
            image=image,
            image_id=podman.image_id(image),
            source_identity=source_identity(),
            profile=profile,
            backend=arguments.backend,
            render_nodes=render_nodes,
            port=port,
            context=arguments.context,
            thinking=thinking,
            native_reasoning=native_reasoning,
            speculative_type=preset.speculative_type,
            incumbent_depth=preset.draft_tokens_for_backend(arguments.backend),
            depths=depths,
            context_depths=context_depths,
            repetitions=arguments.repetitions,
            generation_tokens=arguments.generation_tokens,
            seed=arguments.seed,
            draft_probability_min=arguments.draft_probability_min,
            draft_backend_sampling=(
                arguments.draft_backend_sampling == "on"
            ),
            ngram_simple=arguments.ngram_simple,
            graph_optimization=arguments.graph_optimization,
            disable_graphs=arguments.disable_graphs,
            poll=arguments.poll,
            no_host=arguments.no_host,
            flash_attention=arguments.flash_attn,
            cache_type_k=arguments.cache_type_k,
            cache_type_v=arguments.cache_type_v,
            batch_size=arguments.batch_size,
            ubatch_size=arguments.ubatch_size,
            sampling=agent_sampling_parameters(
                catalog, preset.identifier, thinking
            ),
            model=model_metadata,
            commands=commands,
            output=output,
            resume=resume,
            keep_going=arguments.keep_going,
        )
    )
    _print_llama_speculative_summary(result)
    print(
        "\n{} {}".format(
            style("Speculative benchmark checkpoint:", "success"), result_path
        )
    )
    return 0 if result.get("status") == "complete" else 1


def _llama_backend_name(backend: str) -> str:
    return "ROCm" if backend == "rocm" else "Vulkan"


def _print_llama_backend_comparison(
    value: Mapping[str, object],
) -> None:
    entries = cast(
        Mapping[str, Mapping[str, object]], value["backends"]
    )
    parameters = cast(Mapping[str, int], value["parameters"])
    prompt_tokens = parameters["prompt_tokens"]
    generation_tokens = parameters["generation_tokens"]
    print("\n{}".format(style("Backend comparison", "heading")))
    print(
        "  {} {} {} {}".format(
            style("{:<8}".format("Backend"), "label"),
            style(
                "{:>16}".format("pp{}".format(prompt_tokens)),
                "label",
            ),
            style(
                "{:>16}".format("tg{}".format(generation_tokens)),
                "label",
            ),
            style("{:>16}".format("Estimated"), "label"),
        )
    )
    for backend in ("rocm", "vulkan"):
        entry = entries[backend]
        if entry["status"] == "pass":
            prompt_rate = float(entry["prompt_tokens_per_second"])
            generation_rate = float(
                entry["generation_tokens_per_second"]
            )
            estimated = float(entry["estimated_inference_seconds"])
            row = (
                "{:>12.2f} t/s".format(prompt_rate),
                "{:>12.2f} t/s".format(generation_rate),
                "{:>13.3f} s".format(estimated),
            )
        else:
            row = ("failed", "failed", "failed")
        print(
            "  {:<8} {:>16} {:>16} {:>16}".format(
                _llama_backend_name(backend), *row
            )
        )
    comparison = value["comparison"]
    if not isinstance(comparison, dict):
        print(
            "\n{}".format(
                style(
                    "Both backends must pass before their rates can be "
                    "compared.",
                    "warning",
                )
            )
        )
    else:
        labels = (
            ("Prompt processing", "prompt_processing", "faster_percent"),
            ("Token generation", "token_generation", "faster_percent"),
        )
        print()
        for label, key, percent_key in labels:
            result = cast(Mapping[str, object], comparison[key])
            winner = result["winner"]
            if winner == "tie":
                detail = "tie"
            else:
                detail = "{} is {:.1f}% faster".format(
                    _llama_backend_name(str(winner)),
                    float(result[percent_key]),
                )
            print("{} {}".format(style(label + ":", "label"), detail))
        estimated = cast(
            Mapping[str, object],
            comparison["estimated_inference_time"],
        )
        winner = estimated["winner"]
        if winner == "tie":
            detail = "tie"
        else:
            detail = "{} has {:.1f}% lower estimated inference time".format(
                _llama_backend_name(str(winner)),
                float(estimated["lower_percent"]),
            )
        print(
            "{} {}".format(
                style(
                    "For this pp{}/tg{} workload:".format(
                        prompt_tokens, generation_tokens
                    ),
                    "label",
                ),
                detail,
            )
        )
    print(
        style(
            "Estimated time combines the measured pp/tg rates and excludes "
            "model loading and warmups.",
            "muted",
        )
    )
    for backend in ("rocm", "vulkan"):
        entry = entries[backend]
        if entry["status"] == "pass":
            print(
                "{} {}".format(
                    style(
                        "{} result:".format(
                            _llama_backend_name(backend)
                        ),
                        "label",
                    ),
                    entry["result"],
                )
            )
        else:
            print(
                "{} {}".format(
                    style(
                        "{} error:".format(
                            _llama_backend_name(backend)
                        ),
                        "label",
                    ),
                    entry["error"],
                )
            )


def _resolved_regular_file(value: str, description: str) -> Path:
    try:
        path = Path(value).expanduser().resolve(strict=True)
    except OSError as error:
        raise LauncherError(
            "cannot resolve {}: {}".format(description, error)
        )
    if not path.is_file():
        raise LauncherError("{} is not a regular file: {}".format(
            description, path
        ))
    return path


def _llama_preset_status(
    catalog: Catalog, identifier: str, data_dir: Path
) -> Tuple[str, Path]:
    preset = catalog.llama_preset(identifier)
    bundle = catalog.bundle(preset.bundle)
    statuses = inspect_bundle(catalog, bundle, data_dir)
    failures = [status for status in statuses if not content_status_ready(status)]
    if failures:
        states = ", ".join(
            "{} ({})".format(status.path, content_status_state(status))
            for status in failures
            if isinstance(status, ArtifactStatus)
        )
        raise LauncherError(
            "llama.cpp preset {!r} is not installed: {}"
            "\n  Install content: ./rocmplete content install {}".format(
                identifier,
                states or "managed content is incomplete",
                preset.bundle,
            )
        )
    artifact = catalog.artifact(preset.artifact)
    return artifact.destination, artifact_path(data_dir, artifact)


def _llama_sampling_defaults_json(
    catalog: Catalog, preset: LlamaPreset
) -> str:
    if not preset.sampling_policy:
        return ""
    policy = catalog.llama_sampling_policy(preset.sampling_policy)
    return json.dumps(
        policy.modes(),
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    )


def _render_llama_router_preset(
    catalog: Catalog, data_dir: Path, backend: str = "rocm"
) -> Tuple[str, Tuple[str, ...]]:
    sections = ["version = 1", ""]
    installed = []
    broken = []
    verification_store = VerificationStore.load(data_dir)
    for identifier, preset in catalog.llama_presets.items():
        artifact = catalog.artifact(preset.artifact)
        statuses = inspect_bundle(
            catalog,
            catalog.bundle(preset.bundle),
            data_dir,
            verification_store,
        )
        states = {content_status_state(status) for status in statuses}
        if states == {"missing"}:
            continue
        if not all(content_status_ready(status) for status in statuses):
            broken.append(
                "{} ({})".format(identifier, ", ".join(sorted(states)))
            )
            continue
        destination = artifact.destination
        draft_destination = ""
        if preset.draft_artifact:
            draft_destination = catalog.artifact(
                preset.draft_artifact
            ).destination
        if any(
            character in value
            for value in (destination, draft_destination)
            for character in "\r\n"
        ):
            raise LauncherError(
                "llama.cpp preset {} has an unsafe model path".format(identifier)
            )
        section = [
            "[{}]".format(identifier),
            "model = /content/models/{}".format(destination),
            "c = {}".format(preset.default_context),
        ]
        if preset.context_override_architectures:
            section.extend(
                [
                    "fit = off",
                    "override-kv = {}".format(
                        ",".join(
                            "{}.context_length=int:{}".format(
                                architecture, preset.default_context
                            )
                            for architecture in (
                                preset.context_override_architectures
                            )
                        )
                    ),
                ]
            )
        if preset.jinja:
            section.append("jinja = true")
        if preset.reasoning_preserve:
            section.append("reasoning-preserve = true")
        if preset.chat_template:
            section.extend(
                [
                    "jinja = true",
                    "chat-template-file = "
                    "/usr/local/share/rocmplete/llama-chat-templates/"
                    "{}.jinja".format(preset.chat_template),
                ]
            )
        sampling_defaults = _llama_sampling_defaults_json(catalog, preset)
        if sampling_defaults:
            # This dedicated server policy remains separate from the Jinja
            # context. The patched server selects one mode after resolving
            # the request's reasoning control.
            section.append(
                "sampling-defaults-by-reasoning = {}".format(
                    sampling_defaults
                )
            )
        # Auto profile resolution happens inside the container. These private
        # keys are replaced in the entrypoint's tmpfs copy before llama.cpp
        # sees the generated preset.
        for profile in GPU_PROFILES:
            flash_attention = preset.flash_attention.get(profile, "")
            if flash_attention:
                section.append(
                    "rocmplete-flash-attn-{} = {}".format(
                        profile, flash_attention
                    )
                )
            kv_cache = preset.kv_cache.get(profile, "")
            if kv_cache:
                section.append(
                    "rocmplete-kv-cache-{} = {}".format(
                        profile, kv_cache
                    )
                )
        if preset.speculative_type:
            section.extend(
                [
                    "spec-type = {}".format(preset.speculative_type),
                    "spec-draft-n-max = {}".format(
                        preset.draft_tokens_for_backend(backend)
                    ),
                ]
            )
            if draft_destination:
                section.append(
                    "model-draft = /content/models/{}".format(
                        draft_destination
                    )
                )
        section.extend(["load-on-startup = false", ""])
        sections.extend(section)
        installed.append(identifier)
    if broken:
        raise LauncherError(
            "managed llama.cpp content is incomplete: {}".format(
                ", ".join(broken)
            )
        )
    if not installed:
        raise LauncherError(
            "no managed llama.cpp presets are installed; run "
            "'./rocmplete content install llama-cpp qwen3.6'"
        )
    return "\n".join(sections), tuple(installed)


def _write_llama_router_preset(path: Path, contents: str) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        if path.exists() and (path.is_symlink() or not path.is_file()):
            raise LauncherError(
                "refusing unexpected llama.cpp router preset: {}".format(path)
            )
        temporary = path.with_name(
            ".{}.{}.tmp".format(path.name, uuid.uuid4().hex)
        )
        try:
            temporary.write_text(contents)
            temporary.chmod(0o600)
            os.replace(str(temporary), str(path))
        finally:
            try:
                temporary.unlink()
            except FileNotFoundError:
                pass
    except LauncherError:
        raise
    except OSError as error:
        raise LauncherError(
            "cannot write llama.cpp router preset {}: {}".format(path, error)
        )


def command_llama(arguments: argparse.Namespace, catalog: Catalog) -> int:
    selected = sum(
        bool(value)
        for value in (arguments.model, arguments.preset, arguments.router)
    )
    if selected != 1:
        raise LauncherError(
            "choose exactly one of --model, --preset, or --router"
        )
    if arguments.context is not None and arguments.context < 0:
        raise LauncherError("--context must be zero or positive")
    if arguments.models_max is not None and not arguments.router:
        raise LauncherError("--models-max is only valid with --router")
    models_max = arguments.models_max or 2
    if models_max < 1:
        raise LauncherError("--models-max must be at least 1")
    model = None
    if arguments.model:
        model = _resolved_regular_file(arguments.model, "GGUF model")
        if model.suffix.lower() != ".gguf":
            raise LauncherError("--model must name a .gguf file")
    profile = validate_profile(
        arguments.profile
        or environment_value(os.environ, "PROFILE", "auto")
    )
    render_nodes = requested_render_nodes(arguments.render_node)
    if profile != "cpu":
        render_nodes = select_render_nodes(render_nodes)
        check_gpu_device_access(render_nodes)
    else:
        render_nodes = ()
    requested_data = selected_data_dir(arguments.data_dir)
    data_dir = (
        inspect_data_path(requested_data)
        if arguments.dry_run
        else prepare_data_dir(requested_data)
    )
    if not arguments.dry_run:
        StorageLayout(data_dir).prepare_runtime("llama-cpp")
    managed_model = ""
    managed_draft = ""
    speculative_type = ""
    draft_tokens = 0
    context_override_architectures = ()
    jinja = False
    reasoning_preserve = False
    chat_template = ""
    sampling_defaults = ""
    profile_flash_attention = {}
    profile_kv_cache = {}
    display_model = str(model) if model is not None else ""
    context = arguments.context
    router_preset = None
    router_models = ()
    if arguments.preset:
        preset = catalog.llama_preset(arguments.preset)
        managed_model, managed_path = _llama_preset_status(
            catalog, arguments.preset, data_dir
        )
        display_model = "{} ({})".format(arguments.preset, managed_path)
        if preset.draft_artifact:
            managed_draft = catalog.artifact(
                preset.draft_artifact
            ).destination
        speculative_type = preset.speculative_type
        draft_tokens = preset.draft_tokens_for_backend(arguments.backend)
        context_override_architectures = (
            preset.context_override_architectures
        )
        jinja = preset.jinja
        reasoning_preserve = preset.reasoning_preserve
        chat_template = preset.chat_template
        sampling_defaults = _llama_sampling_defaults_json(catalog, preset)
        profile_flash_attention = preset.flash_attention
        profile_kv_cache = preset.kv_cache
        if context is None:
            context = preset.default_context
        if preset.context_override_architectures and context == 0:
            raise LauncherError(
                "--context 0 cannot disable the managed context metadata "
                "override for preset '{}'".format(preset.identifier)
            )
    elif arguments.router:
        contents, router_models = _render_llama_router_preset(
            catalog, data_dir, arguments.backend
        )
        router_preset = (
            StorageLayout(data_dir).application("llama-cpp") / "models.ini"
        )
        if not arguments.dry_run:
            _write_llama_router_preset(router_preset, contents)
        display_model = "router: {}".format(", ".join(router_models))
    if context is None:
        context = 0
    application = APPLICATIONS["llama-cpp"]
    image = arguments.image or application.image
    listen = validate_listen_address(
        arguments.listen
        if arguments.listen is not None
        else environment_value(os.environ, "LISTEN", DEFAULT_LISTEN)
    )
    port = validate_port(
        arguments.port
        or environment_value(os.environ, "PORT", str(application.port))
    )
    api_key_file = (
        _resolved_regular_file(arguments.api_key_file, "API-key file")
        if arguments.api_key_file
        else None
    )
    interactive = arguments.mode == "cli" and arguments.prompt is None
    if interactive and not arguments.dry_run and not sys.stdin.isatty():
        raise LauncherError(
            "interactive llama.cpp CLI requires a terminal; pass --prompt "
            "for a non-interactive invocation"
        )
    options = LlamaOptions(
        image=image,
        profile=profile,
        mode=arguments.mode,
        data_dir=data_dir,
        backend=arguments.backend,
        source_revision=_project_revision(),
        model=model,
        managed_model=managed_model,
        managed_draft=managed_draft,
        speculative_type=speculative_type,
        draft_tokens=draft_tokens,
        context_override_architectures=context_override_architectures,
        jinja=jinja,
        reasoning_preserve=reasoning_preserve,
        chat_template=chat_template,
        sampling_defaults=sampling_defaults,
        profile_flash_attention=profile_flash_attention,
        profile_kv_cache=profile_kv_cache,
        router_preset=router_preset,
        models_max=models_max,
        render_nodes=render_nodes,
        listen=listen,
        port=port,
        context=context,
        prompt=arguments.prompt,
        api_key_file=api_key_file,
        detach=arguments.detach,
        interactive=interactive,
        unconfined=arguments.unconfined,
    )
    command = llama_command(options, podman.selinux_volume_suffix())
    if (
        arguments.mode == "server"
        and not is_loopback_address(listen)
        and api_key_file is None
    ):
        print(
            "{} llama.cpp is published on {}:{} without "
            "authentication.".format(
                style("WARNING:", "warning"), listen, port
            ),
            flush=True,
        )
    print(
        "{} {}".format(
            style("Application data:", "label"),
            StorageLayout(data_dir).application("llama-cpp"),
        )
    )
    print("{} {}".format(style("Backend:", "label"), arguments.backend))
    print("{} {}".format(style("Model:", "label"), display_model))
    if arguments.mode == "server" and not arguments.dry_run:
        status_command = ["./rocmplete", "status", "llama-cpp"]
        if arguments.preset:
            status_command.extend(("--model", arguments.preset))
        print(
            "{} {}".format(
                style("Configuration:", "label"),
                style(shlex.join(status_command), "command"),
            )
        )
    if arguments.dry_run:
        print(
            "{}\n  {}".format(
                style("Resolved command:", "heading"),
                style(shlex.join(command), "command"),
            )
        )
        return 0
    podman.require_rootless()
    if not podman.image_exists(image):
        if arguments.image:
            raise LauncherError("image not found: {}".format(image))
        raise LauncherError(
            "image not found: {}"
            "\n  Build image: ./rocmplete build llama-cpp".format(image)
        )
    if podman.container_exists(application.container_name):
        raise LauncherError(
            "container {!r} already exists; use logs or stop".format(
                application.container_name
            )
        )
    if arguments.mode == "server":
        print(
            "{} {}".format(
                style("Open:", "heading"),
                style(_browser_url(listen, port), "command"),
            )
        )
        print(
            "{} {}".format(
                style("Logs:", "label"),
                style("./rocmplete logs llama-cpp", "command"),
            )
        )
        print(
            "{} {}".format(
                style("Stop:", "label"),
                style("./rocmplete stop llama-cpp", "command"),
            )
        )
    if arguments.detach:
        return podman.run(command)
    return podman.run_managed_foreground(
        command,
        application.container_name,
        "llama.cpp {} failed".format(arguments.mode),
    )


def _managed_dwarfstar_artifacts(
    catalog: Catalog,
    data_dir: Path,
    bundle_identifier: str,
    recipe_identifier: str,
) -> Tuple[Path, ...]:
    bundle = catalog.bundle(bundle_identifier)
    statuses = inspect_bundle(catalog, bundle, data_dir)
    failures = tuple(
        status for status in statuses if not content_status_ready(status)
    )
    if failures:
        states = ", ".join(
            "{} ({})".format(status.path, content_status_state(status))
            for status in failures
            if isinstance(status, ArtifactStatus)
        )
        raise LauncherError(
            "DwarfStar model is not installed: {}\n"
            "  Install content: ./rocmplete content install dwarfstar "
            "{}".format(
                states or "managed content is incomplete",
                recipe_identifier,
            )
        )
    return tuple(
        artifact_path(data_dir, catalog.artifact(identifier))
        for identifier in bundle.artifacts
    )


def _managed_dwarfstar_model(catalog: Catalog, data_dir: Path) -> Path:
    return _managed_dwarfstar_artifacts(
        catalog,
        data_dir,
        DWARFSTAR_DEFAULT_MODEL_BUNDLE,
        "flash-0731-q2-imatrix",
    )[0]


def _managed_dwarfstar_dspark_models(
    catalog: Catalog, data_dir: Path
) -> Tuple[Path, Path]:
    paths = _managed_dwarfstar_artifacts(
        catalog,
        data_dir,
        DWARFSTAR_DSPARK_MODEL_BUNDLE,
        "flash-0731-q2-imatrix-dspark",
    )
    if len(paths) != 2:
        raise LauncherError(
            "managed DwarfStar DSpark bundle must contain target and "
            "support GGUFs"
        )
    return paths[0], paths[1]


def command_dwarfstar(
    arguments: argparse.Namespace, catalog: Catalog
) -> int:
    if arguments.context < 4096 or arguments.context > 1048576:
        raise LauncherError(
            "--context must be between 4096 and 1048576 tokens"
        )
    if (
        arguments.output_tokens < 1
        or arguments.output_tokens >= arguments.context
    ):
        raise LauncherError(
            "--output-tokens must be positive and smaller than --context"
        )
    profile = arguments.profile or environment_value(
        os.environ, "PROFILE", "auto"
    )
    profile = validate_profile(profile)
    if profile == "cpu":
        raise LauncherError("DwarfStar requires a supported ROCm GPU profile")
    render_nodes = select_render_nodes(
        requested_render_nodes(arguments.render_node)
    )
    if len(render_nodes) != 1:
        raise LauncherError(
            "DwarfStar requires exactly one selected render node"
        )
    check_gpu_device_access(render_nodes)

    requested_data = selected_data_dir(arguments.data_dir)
    data_dir = (
        inspect_data_path(requested_data)
        if arguments.dry_run
        else prepare_data_dir(requested_data)
    )
    if not arguments.dry_run:
        StorageLayout(data_dir).prepare_runtime("dwarfstar")
    support_model = None
    if arguments.dspark and arguments.model:
        raise LauncherError(
            "--dspark uses the exact managed 0731 target and support "
            "GGUFs and cannot be combined with --model"
        )
    if arguments.dspark:
        model, support_model = _managed_dwarfstar_dspark_models(
            catalog, data_dir
        )
    elif arguments.model:
        model = _resolved_regular_file(
            arguments.model, "DwarfStar GGUF model"
        )
        if model.suffix.lower() != ".gguf":
            raise LauncherError("--model must name a .gguf file")
    else:
        model = _managed_dwarfstar_model(catalog, data_dir)
    application = APPLICATIONS["dwarfstar"]
    image = arguments.image or application.image
    listen = validate_listen_address(
        arguments.listen
        if arguments.listen is not None
        else environment_value(os.environ, "LISTEN", DEFAULT_LISTEN)
    )
    port = validate_port(
        arguments.port
        or environment_value(os.environ, "PORT", str(application.port))
    )
    interactive = arguments.mode == "cli" and arguments.prompt is None
    if interactive and not arguments.dry_run and not sys.stdin.isatty():
        raise LauncherError(
            "interactive DwarfStar CLI requires a terminal; pass --prompt "
            "for a non-interactive invocation"
        )
    options = DwarfStarOptions(
        image=image,
        mode=arguments.mode,
        data_dir=data_dir,
        model=model,
        support_model=support_model,
        dspark=arguments.dspark,
        render_nodes=render_nodes,
        profile=profile,
        listen=listen,
        port=port,
        context=arguments.context,
        output_tokens=arguments.output_tokens,
        prompt=arguments.prompt,
        no_thinking=arguments.no_thinking,
        detach=arguments.detach,
        interactive=interactive,
        unconfined=arguments.unconfined,
    )
    command = dwarfstar_command(options, podman.selinux_volume_suffix())
    if arguments.mode == "server" and not is_loopback_address(listen):
        print(
            "{} DwarfStar is published on {}:{} without authentication.".format(
                style("WARNING:", "warning"), listen, port
            ),
            flush=True,
        )
    print(
        "{} {}".format(
            style("Application data:", "label"),
            StorageLayout(data_dir).application("dwarfstar"),
        )
    )
    print("{} {}".format(style("Model:", "label"), model))
    if support_model is not None:
        print(
            "{} {}".format(
                style("DSpark support:", "label"), support_model
            )
        )
    if arguments.dry_run:
        print(
            "{}\n  {}".format(
                style("Resolved command:", "heading"),
                style(shlex.join(command), "command"),
            )
        )
        return 0

    podman.require_rootless()
    if not podman.image_exists(image):
        if arguments.image:
            raise LauncherError("image not found: {}".format(image))
        raise LauncherError(
            "image not found: {}\n"
            "  Build image: ./rocmplete build dwarfstar".format(image)
        )
    if podman.container_exists(application.container_name):
        raise LauncherError(
            "container {!r} already exists; use logs or stop".format(
                application.container_name
            )
        )
    if arguments.mode == "server":
        print(
            "{} {}".format(
                style("Open:", "heading"),
                style(_browser_url(listen, port), "command"),
            )
        )
        print(
            "{} {}".format(
                style("Logs:", "label"),
                style("./rocmplete logs dwarfstar", "command"),
            )
        )
        print(
            "{} {}".format(
                style("Stop:", "label"),
                style("./rocmplete stop dwarfstar", "command"),
            )
        )
    if arguments.detach:
        return podman.run(command)
    return podman.run_managed_foreground(
        command,
        application.container_name,
        "DwarfStar {} failed".format(arguments.mode),
    )


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser, arguments = parse_arguments(
        sys.argv[1:] if argv is None else argv
    )
    if arguments.command is None:
        parser.print_help()
        return 2
    try:
        if arguments.command == "build":
            return command_build(arguments)
        if arguments.command == "guide":
            return print_application_guide(arguments.application)
        if arguments.command == "agent":
            return command_agent(arguments)
        if arguments.command == "images":
            return command_images(arguments)
        if arguments.command == "doctor":
            return command_doctor(arguments)
        if arguments.command == "status":
            return command_status(arguments)
        if arguments.command == "run":
            return command_run(arguments)
        if arguments.command == "shell":
            return command_shell(arguments)
        if arguments.command == "logs":
            return command_logs(arguments)
        if arguments.command == "stop":
            return command_stop(arguments)
        if arguments.command == "cleanup":
            return command_cleanup(arguments)
        if arguments.command == "content":
            return command_content(arguments, load_catalog())
        if arguments.command == "acceptance":
            return command_acceptance(arguments, load_catalog())
        if arguments.command == "benchmark":
            return command_benchmark(arguments, load_catalog())
        raise LauncherError("unknown command: {}".format(arguments.command))
    except LauncherError as error:
        print(
            style("error: {}".format(error), "error", sys.stderr),
            file=sys.stderr,
        )
        return 1
    except KeyboardInterrupt:
        print(style("interrupted", "warning", sys.stderr), file=sys.stderr)
        return 130
