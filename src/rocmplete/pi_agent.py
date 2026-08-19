"""Launch Pi against ROCmplete's managed local model servers."""

from __future__ import annotations

import json
import os
import stat
import tempfile
import urllib.error
import urllib.parse
import urllib.request
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
    reasoning_client_default,
)
from .agent_sandbox import (
    AgentSandboxPaths as PiSandboxPaths,
    AgentSandboxPlan as PiSandboxPlan,
    SANDBOX_HOME,
    create_sandbox_plan as create_agent_sandbox_plan,
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
from .pi_runtime import PiRuntime, resolve_pi_runtime
from .project import PROJECT_ROOT


WRAPPER_PATH = PROJECT_ROOT / "bin" / "pi"
MODEL_PICKER_EXTENSION_SOURCE = (
    PROJECT_ROOT
    / "agent-clients"
    / "pi"
    / "extensions"
    / "rocmplete-model-picker.ts"
)
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
_REMOTE_MODELS_LIMIT = 1024 * 1024
_REMOTE_MODELS_TIMEOUT = 5


@dataclass(frozen=True)
class PiLaunchPlan:
    command: Tuple[str, ...]
    runtime_root: Path
    default_provider: Optional[str]
    default_model: Optional[str]
    default_thinking: Optional[str]
    endpoint: str
    dwarfstar_endpoint: str
    config_content: bytes
    model_picker_extension: bytes
    mode: str
    remote_llama: bool


def load_model_picker_extension(
    path: Path = MODEL_PICKER_EXTENSION_SOURCE,
) -> bytes:
    """Read the repository-owned Pi model-picker extension."""

    try:
        status = path.lstat()
        if stat.S_ISLNK(status.st_mode) or not stat.S_ISREG(status.st_mode):
            raise LauncherError(
                "Pi model-picker extension is not a regular file: {}".format(
                    path
                )
            )
        return path.read_bytes()
    except FileNotFoundError:
        raise LauncherError(
            "Pi model-picker extension is missing: {}".format(path)
        )
    except OSError as error:
        raise LauncherError(
            "cannot read Pi model-picker extension {}: {}".format(path, error)
        )


def normalize_llama_url(value: str) -> str:
    """Validate and normalize an OpenAI-compatible llama.cpp base URL."""

    if not value or any(ord(character) <= 32 for character in value):
        raise LauncherError(
            "Pi llama.cpp URL must not be empty or contain whitespace"
        )
    try:
        parsed = urllib.parse.urlsplit(value)
        port = parsed.port
    except ValueError as error:
        raise LauncherError("invalid Pi llama.cpp URL: {}".format(error))
    if parsed.scheme not in ("http", "https"):
        raise LauncherError("Pi llama.cpp URL must use http or https")
    if not parsed.hostname:
        raise LauncherError("Pi llama.cpp URL must include a host")
    if parsed.username is not None or parsed.password is not None:
        raise LauncherError("Pi llama.cpp URL must not contain credentials")
    if parsed.query or parsed.fragment:
        raise LauncherError(
            "Pi llama.cpp URL must not contain a query or fragment"
        )
    if port is not None and not 1 <= port <= 65535:
        raise LauncherError("Pi llama.cpp URL port must be between 1 and 65535")
    endpoint = value.rstrip("/")
    if not urllib.parse.urlsplit(endpoint).path.endswith("/v1"):
        raise LauncherError("Pi llama.cpp URL path must end with /v1")
    return endpoint


def discover_remote_models(endpoint: str) -> Tuple[str, ...]:
    """Read the bounded standard model inventory from a remote router."""

    url = "{}/models".format(endpoint)
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/json"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(
            request, timeout=_REMOTE_MODELS_TIMEOUT
        ) as response:
            raw = response.read(_REMOTE_MODELS_LIMIT + 1)
    except urllib.error.HTTPError as error:
        raise LauncherError(
            "remote llama.cpp model discovery at {} returned HTTP {}".format(
                url, error.code
            )
        )
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        raise LauncherError(
            "cannot reach remote llama.cpp model inventory at {}: {}".format(
                url, error
            )
        )
    if len(raw) > _REMOTE_MODELS_LIMIT:
        raise LauncherError(
            "remote llama.cpp model inventory at {} exceeds {} bytes".format(
                url, _REMOTE_MODELS_LIMIT
            )
        )
    try:
        document = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise LauncherError(
            "remote llama.cpp returned invalid model inventory JSON at "
            "{}: {}".format(url, error)
        )
    if not isinstance(document, dict) or not isinstance(
        document.get("data"), list
    ):
        raise LauncherError(
            "remote llama.cpp returned an invalid model inventory at {}".format(
                url
            )
        )
    identifiers = []
    seen = set()
    for item in document["data"]:
        if not isinstance(item, dict) or not isinstance(item.get("id"), str):
            raise LauncherError(
                "remote llama.cpp returned an invalid model inventory at "
                "{}".format(url)
            )
        identifier = item["id"]
        if not identifier or identifier in seen:
            continue
        seen.add(identifier)
        identifiers.append(identifier)
    return tuple(identifiers)


def _remote_default_model(
    catalog: Catalog, endpoint: str, identifiers: Sequence[str]
) -> Tuple[str, str, str]:
    advertised = set(identifiers)
    maintained = tuple(
        identifier
        for identifier, preset in catalog.llama_presets.items()
        if identifier in advertised and is_agent_capable(preset)
    )
    if RECOMMENDED_MODEL in maintained:
        identifier = RECOMMENDED_MODEL
    elif maintained:
        identifier = maintained[0]
    else:
        raise LauncherError(
            "remote llama.cpp router at {} advertises no model maintained "
            "for Pi; start the managed router with an installed agent model".format(
                endpoint
            )
        )
    preset = catalog.llama_preset(identifier)
    return PROVIDER_ID, identifier, reasoning_client_default(preset)


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
    runtime: Optional[PiRuntime] = None,
    llama_url: Optional[str] = None,
) -> PiLaunchPlan:
    env = os.environ if environ is None else environ
    remote_llama = llama_url is not None
    endpoint = (
        normalize_llama_url(llama_url)
        if llama_url is not None
        else "http://127.0.0.1:{}/v1".format(port)
    )
    dwarfstar_endpoint = "http://127.0.0.1:{}/v1".format(dwarfstar_port)
    forwarded = tuple(arguments)
    if forwarded[:1] == ("--",):
        forwarded = forwarded[1:]
    managed_runtime = runtime or resolve_pi_runtime(data_dir, env)
    model_picker_extension = load_model_picker_extension()
    command_prefix = (
        str(managed_runtime.node),
        str(managed_runtime.entrypoint),
    )
    self_update = forwarded[:1] == ("update",) and (
        len(forwarded) == 1
        or "--self" in forwarded[1:]
        or "--all" in forwarded[1:]
        or forwarded[1:2] in (("self",), ("pi",))
    )
    if self_update:
        raise LauncherError(
            "Pi is managed by ROCmplete; update the checkout and run "
            "./rocmplete agent install pi. Use pi update --extensions for "
            "Pi packages."
        )
    if (
        bool(forwarded) and forwarded[0] in _INFORMATION_ARGUMENTS
    ):
        return PiLaunchPlan(
            command=(*command_prefix, *forwarded),
            runtime_root=managed_runtime.root,
            default_provider=None,
            default_model=None,
            default_thinking=None,
            endpoint=endpoint,
            dwarfstar_endpoint=dwarfstar_endpoint,
            config_content=render_config(
                catalog, endpoint, dwarfstar_endpoint
            ),
            model_picker_extension=model_picker_extension,
            mode="passthrough",
            remote_llama=remote_llama,
        )
    if forwarded[:1] and forwarded[0] in _MANAGEMENT_COMMANDS:
        return PiLaunchPlan(
            command=(*command_prefix, *forwarded),
            runtime_root=managed_runtime.root,
            default_provider=None,
            default_model=None,
            default_thinking=None,
            endpoint=endpoint,
            dwarfstar_endpoint=dwarfstar_endpoint,
            config_content=render_config(
                catalog, endpoint, dwarfstar_endpoint
            ),
            model_picker_extension=model_picker_extension,
            mode="management",
            remote_llama=remote_llama,
        )

    if remote_llama:
        provider, model, thinking = _remote_default_model(
            catalog, endpoint, discover_remote_models(endpoint)
        )
    else:
        provider, model, thinking = _default_model(catalog, data_dir)
    command = (
        *command_prefix,
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
        runtime_root=managed_runtime.root,
        default_provider=provider,
        default_model=model,
        default_thinking=thinking,
        endpoint=endpoint,
        dwarfstar_endpoint=dwarfstar_endpoint,
        config_content=render_config(catalog, endpoint, dwarfstar_endpoint),
        model_picker_extension=model_picker_extension,
        mode="session",
        remote_llama=remote_llama,
    )


def sandbox_paths(data_dir: Path) -> PiSandboxPaths:
    return agent_sandbox_paths(data_dir, "pi")


def _agent_dir(paths: PiSandboxPaths) -> Path:
    return paths.data / "pi" / "agent"


def _refresh_private_file(
    path: Path,
    contents: bytes,
    description: str,
) -> None:
    """Atomically refresh one ROCmplete-owned file below Pi's private state."""

    try:
        current = path.lstat()
    except FileNotFoundError:
        current = None
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} {}: {}".format(description, path, error)
        )
    if current is not None:
        if not stat.S_ISREG(current.st_mode) or current.st_nlink != 1:
            raise LauncherError(
                "{} is not a private regular file: {}".format(
                    description, path
                )
            )
        try:
            unchanged = path.read_bytes() == contents
        except OSError as error:
            raise LauncherError(
                "cannot read {} {}: {}".format(description, path, error)
            )
        if unchanged:
            try:
                path.chmod(0o600)
            except OSError as error:
                raise LauncherError(
                    "cannot secure {} {}: {}".format(
                        description, path, error
                    )
                )
            return

    temporary = None
    try:
        descriptor, temporary = tempfile.mkstemp(
            prefix=".{}-".format(path.name),
            suffix=".tmp",
            dir=str(path.parent),
        )
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(contents)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
        temporary = None
    except OSError as error:
        raise LauncherError(
            "cannot write {} {}: {}".format(description, path, error)
        )
    finally:
        if temporary is not None:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass


def prepare_state(
    plan: PiLaunchPlan,
    paths: PiSandboxPaths,
    data_dir: Path,
) -> Path:
    """Prepare Pi's private state and atomically refresh managed resources."""

    prepare_agent_sandbox_paths(paths, data_dir, "Pi")
    agent_dir = _agent_dir(paths)
    models = agent_dir / "models.json"
    extensions = agent_dir / "extensions"
    model_picker = extensions / "rocmplete-model-picker.ts"
    validate_managed_parent(models, paths.root, data_dir, "Pi model config")
    validate_managed_parent(
        model_picker, paths.root, data_dir, "Pi model-picker extension"
    )
    for path in (agent_dir.parent, agent_dir, extensions):
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
    _refresh_private_file(
        models, plan.config_content, "Pi model configuration"
    )
    _refresh_private_file(
        model_picker,
        plan.model_picker_extension,
        "Pi model-picker extension",
    )
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
        read_only_mounts=(
            (plan.runtime_root, plan.runtime_root),
            *read_only_mounts,
        ),
    )
