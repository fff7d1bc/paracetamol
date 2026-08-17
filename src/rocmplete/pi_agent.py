"""Launch Pi against ROCmplete's managed local model servers."""

from __future__ import annotations

import json
import os
import stat
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Optional, Sequence, Tuple

from .agent_models import (
    DWARFSTAR_MODEL,
    DWARFSTAR_PROVIDER_ID,
    PROVIDER_ID,
    RECOMMENDED_MODEL,
    agent_output_limit,
    agent_client_sampling_parameters,
    default_agent_model,
    is_agent_capable,
)
from .agent_sandbox import (
    AgentSandboxPaths as PiSandboxPaths,
    AgentSandboxPlan as PiSandboxPlan,
    SANDBOX_HOME,
    create_sandbox_plan as create_agent_sandbox_plan,
    find_real_executable,
    prepare_sandbox_paths as prepare_agent_sandbox_paths,
    sandbox_paths as agent_sandbox_paths,
)
from .catalog import Catalog
from .config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from .errors import LauncherError
from .layout import validate_managed_parent
from .project import PROJECT_ROOT


WRAPPER_PATH = PROJECT_ROOT / "bin" / "pi"
SANDBOX_AGENT_DIR = SANDBOX_HOME / ".local" / "share" / "pi" / "agent"
_THINKING_LEVELS = ("minimal", "low", "medium", "high", "xhigh", "max")
_DWARFSTAR_REASONING_LEVELS = {
    "off": "none",
    "minimal": None,
    "low": None,
    "medium": None,
    "high": "high",
    "xhigh": None,
    "max": None,
}
_COST = {
    "input": 0,
    "output": 0,
    "cacheRead": 0,
    "cacheWrite": 0,
}
_CLIENT_ARGUMENTS = (
    "--offline",
    "--no-approve",
)
_MANAGEMENT_COMMANDS = frozenset(
    (
        "install",
        "remove",
        "uninstall",
        "update",
        "list",
        "config",
        "auth",
    )
)
_INFORMATION_ARGUMENTS = frozenset(("--help", "-h", "--version", "-v"))


@dataclass(frozen=True)
class PiLaunchPlan:
    command: Tuple[str, ...]
    default_provider: Optional[str]
    default_model: Optional[str]
    default_thinking: Optional[str]
    endpoint: str
    dwarfstar_endpoint: str
    config_content: bytes
    mode: str


def _provider(
    name: str,
    endpoint: str,
    models: Sequence[Mapping[str, object]],
) -> Mapping[str, object]:
    return {
        "name": name,
        "baseUrl": endpoint,
        "api": "openai-completions",
        # Pi requires a value before it exposes a custom provider. The local
        # servers need no credential and authHeader keeps this literal local.
        "apiKey": "rocmplete-local",
        "authHeader": False,
        "compat": {
            "supportsDeveloperRole": False,
            "supportsReasoningEffort": True,
        },
        "models": list(models),
    }


def render_config(
    catalog: Catalog,
    endpoint: str,
    dwarfstar_endpoint: str = "http://127.0.0.1:8000/v1",
) -> bytes:
    models = []
    for identifier, preset in catalog.llama_presets.items():
        if not is_agent_capable(preset):
            continue
        model = {
            "id": identifier,
            "name": identifier,
            "reasoning": bool(preset.reasoning_control),
            "input": ["text"],
            "contextWindow": preset.default_context,
            "maxTokens": agent_output_limit(preset.default_context),
            "cost": _COST,
        }
        sampling = agent_client_sampling_parameters(catalog, identifier)
        if sampling:
            model["samplingParams"] = sampling
        if preset.reasoning_control:
            exposed = (
                {"high"}
                if preset.reasoning_control == "toggle"
                else set(preset.reasoning_levels)
            )
            model["thinkingLevelMap"] = {
                "off": "none" if preset.reasoning_off else None,
                **{
                    level: level if level in exposed else None
                    for level in _THINKING_LEVELS
                },
            }
            if preset.reasoning_control == "toggle":
                model["compat"] = {
                    "thinkingFormat": "qwen-chat-template",
                    "supportsReasoningEffort": False,
                }
            elif preset.reasoning_control == "effort":
                # Pi's `qwen` transport is the DashScope-style top-level
                # enable_thinking field, which llama.cpp does not consume.
                # Its OpenAI transport sends reasoning_effort for both named
                # levels and the mapped `none` value used by off.
                model["compat"] = {
                    "thinkingFormat": "openai",
                    "supportsReasoningEffort": True,
                }
            else:
                model["compat"] = {
                    "thinkingFormat": "chat-template",
                    "supportsReasoningEffort": False,
                    "chatTemplateKwargs": {
                        "reasoning_strength": {
                            "$var": "thinking.effort"
                        },
                        "preserve_thinking": True,
                    },
                }
        models.append(model)
    dwarfstar_model = {
        "id": DWARFSTAR_MODEL,
        "name": "DeepSeek V4 Flash 0731",
        "reasoning": True,
        "thinkingLevelMap": _DWARFSTAR_REASONING_LEVELS,
        "input": ["text"],
        "contextWindow": DWARFSTAR_DEFAULT_CONTEXT,
        "maxTokens": DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
        "cost": _COST,
    }
    contents = {
        "providers": {
            PROVIDER_ID: _provider(
                "ROCmplete llama.cpp", endpoint, models
            ),
            DWARFSTAR_PROVIDER_ID: _provider(
                "ROCmplete DwarfStar",
                dwarfstar_endpoint,
                (dwarfstar_model,),
            ),
        }
    }
    return (json.dumps(contents, indent=2) + "\n").encode("utf-8")


def _default_model(catalog: Catalog, data_dir: Path) -> Tuple[str, str, str]:
    return default_agent_model(catalog, data_dir, "Pi")


def create_launch_plan(
    catalog: Catalog,
    data_dir: Path,
    port: int,
    arguments: Sequence[str],
    environ: Optional[Mapping[str, str]] = None,
    *,
    dwarfstar_port: int = 8000,
) -> PiLaunchPlan:
    env = os.environ if environ is None else environ
    endpoint = "http://127.0.0.1:{}/v1".format(port)
    dwarfstar_endpoint = "http://127.0.0.1:{}/v1".format(dwarfstar_port)
    forwarded = tuple(arguments)
    if forwarded[:1] == ("--",):
        forwarded = forwarded[1:]
    executable = find_real_executable("pi", WRAPPER_PATH, env, "Pi")
    self_update = forwarded[:1] == ("update",) and (
        len(forwarded) == 1
        or "--self" in forwarded[1:]
        or "--all" in forwarded[1:]
        or forwarded[1:2] in (("self",), ("pi",))
    )
    if (
        bool(forwarded) and forwarded[0] in _INFORMATION_ARGUMENTS
    ) or self_update:
        return PiLaunchPlan(
            command=(executable, *forwarded),
            default_provider=None,
            default_model=None,
            default_thinking=None,
            endpoint=endpoint,
            dwarfstar_endpoint=dwarfstar_endpoint,
            config_content=render_config(
                catalog, endpoint, dwarfstar_endpoint
            ),
            mode="passthrough",
        )
    if forwarded[:1] and forwarded[0] in _MANAGEMENT_COMMANDS:
        return PiLaunchPlan(
            command=(executable, *forwarded),
            default_provider=None,
            default_model=None,
            default_thinking=None,
            endpoint=endpoint,
            dwarfstar_endpoint=dwarfstar_endpoint,
            config_content=render_config(
                catalog, endpoint, dwarfstar_endpoint
            ),
            mode="management",
        )

    provider, model, thinking = _default_model(catalog, data_dir)
    command = (
        executable,
        *_CLIENT_ARGUMENTS,
        "--provider",
        provider,
        "--model",
        model,
        "--thinking",
        thinking,
        *forwarded,
    )
    return PiLaunchPlan(
        command=command,
        default_provider=provider,
        default_model=model,
        default_thinking=thinking,
        endpoint=endpoint,
        dwarfstar_endpoint=dwarfstar_endpoint,
        config_content=render_config(catalog, endpoint, dwarfstar_endpoint),
        mode="session",
    )


def sandbox_paths(data_dir: Path) -> PiSandboxPaths:
    return agent_sandbox_paths(data_dir, "pi")


def _agent_dir(paths: PiSandboxPaths) -> Path:
    return paths.data / "pi" / "agent"


def prepare_state(
    plan: PiLaunchPlan,
    paths: PiSandboxPaths,
    data_dir: Path,
) -> Path:
    """Prepare Pi's private state and atomically refresh models.json."""

    prepare_agent_sandbox_paths(paths, data_dir, "Pi")
    agent_dir = _agent_dir(paths)
    models = agent_dir / "models.json"
    validate_managed_parent(models, paths.root, data_dir, "Pi model config")
    for path in (agent_dir.parent, agent_dir):
        try:
            status = path.lstat()
        except FileNotFoundError:
            try:
                path.mkdir(mode=0o700)
            except OSError as error:
                raise LauncherError(
                    "cannot create Pi state directory {}: {}".format(
                        path, error
                    )
                )
            continue
        except OSError as error:
            raise LauncherError(
                "cannot inspect Pi state directory {}: {}".format(
                    path, error
                )
            )
        if stat.S_ISLNK(status.st_mode) or not stat.S_ISDIR(status.st_mode):
            raise LauncherError(
                "Pi state path is not a real directory: {}".format(path)
            )
        try:
            path.chmod(0o700)
        except OSError as error:
            raise LauncherError(
                "cannot secure Pi state directory {}: {}".format(path, error)
            )
    try:
        model_status = models.lstat()
    except FileNotFoundError:
        model_status = None
    except OSError as error:
        raise LauncherError(
            "cannot inspect Pi model configuration {}: {}".format(
                models, error
            )
        )
    if model_status is not None:
        if (
            not stat.S_ISREG(model_status.st_mode)
            or model_status.st_nlink != 1
        ):
            raise LauncherError(
                "Pi model configuration is not a private regular file: "
                "{}".format(models)
            )
        try:
            if models.read_bytes() == plan.config_content:
                models.chmod(0o600)
                return agent_dir
        except OSError as error:
            raise LauncherError(
                "cannot read Pi model configuration {}: {}".format(
                    models, error
                )
            )

    temporary = None
    try:
        descriptor, temporary = tempfile.mkstemp(
            prefix=".models.", suffix=".tmp", dir=str(agent_dir)
        )
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(plan.config_content)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, models)
        temporary = None
    except OSError as error:
        raise LauncherError(
            "cannot write Pi model configuration {}: {}".format(models, error)
        )
    finally:
        if temporary is not None:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass
    return agent_dir


def _runtime_environment(
    agent_dir: Path, *, offline: bool = True
) -> Mapping[str, str]:
    environment = {
        "PI_CODING_AGENT_DIR": str(agent_dir),
        "PI_SKIP_VERSION_CHECK": "1",
        "PI_TELEMETRY": "0",
    }
    if offline:
        environment["PI_OFFLINE"] = "1"
    return environment


def launch_environment(
    agent_dir: Path,
    environ: Optional[Mapping[str, str]] = None,
    *,
    offline: bool = True,
) -> Mapping[str, str]:
    child = dict(os.environ if environ is None else environ)
    child.update(_runtime_environment(agent_dir, offline=offline))
    return child


def create_sandbox_plan(
    plan: PiLaunchPlan,
    data_dir: Path,
    workdir: Path,
    environ: Optional[Mapping[str, str]] = None,
    *,
    read_only_mounts: Sequence[Tuple[Path, Path]] = (),
    extra_environment: Optional[Mapping[str, str]] = None,
) -> PiSandboxPlan:
    runtime_environment = dict(
        _runtime_environment(
            SANDBOX_AGENT_DIR, offline=plan.mode == "session"
        )
    )
    if extra_environment is not None:
        runtime_environment.update(extra_environment)
    return create_agent_sandbox_plan(
        plan.command,
        data_dir,
        workdir,
        "pi",
        "Pi",
        runtime_environment,
        environ,
        read_only_mounts=read_only_mounts,
    )
