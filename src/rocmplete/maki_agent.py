"""Launch Maki against ROCmplete's managed local model servers."""

from __future__ import annotations

import json
import os
import shlex
import stat
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Optional, Sequence, Tuple

from .agent_models import (
    DWARFSTAR_MODEL,
    DWARFSTAR_PROVIDER_ID,
    PROVIDER_ID,
    agent_output_limit,
    default_agent_model,
    is_agent_capable,
    recommended_agent_model,
)
from .agent_sandbox import (
    AgentSandboxPaths as MakiSandboxPaths,
    AgentSandboxPlan as MakiSandboxPlan,
    create_sandbox_plan as create_agent_sandbox_plan,
    find_real_executable,
    prepare_sandbox_paths as prepare_agent_sandbox_paths,
    sandbox_paths as agent_sandbox_paths,
)
from .catalog import Catalog, LlamaPreset
from .config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from .errors import LauncherError
from .layout import validate_managed_parent
from .project import PROJECT_ROOT


WRAPPER_PATH = PROJECT_ROOT / "bin" / "maki"
_MANAGEMENT_COMMANDS = frozenset(("auth", "models", "index", "mcp", "prompt"))
_PASSTHROUGH_COMMANDS = frozenset(("update", "rollback", "migrate"))
_INFORMATION_ARGUMENTS = frozenset(("--help", "-h", "--version", "-V"))


@dataclass(frozen=True)
class MakiLaunchPlan:
    command: Tuple[str, ...]
    default_provider: Optional[str]
    default_model: Optional[str]
    default_thinking: Optional[str]
    endpoint: str
    dwarfstar_endpoint: str
    init_content: bytes
    provider_contents: Mapping[str, bytes]
    tier_content: bytes
    mode: str


def _native_thinking_fields(preset: LlamaPreset) -> Mapping[str, object]:
    reasoning_control = preset.reasoning_control
    if not reasoning_control:
        return {}
    if reasoning_control == "toggle":
        return {
            "thinking_fields": {
                "off": {
                    "chat_template_kwargs": {"enable_thinking": False}
                },
                "adaptive": {
                    "chat_template_kwargs": {"enable_thinking": True}
                },
            }
        }

    field = "reasoning_{}".format(reasoning_control)
    modes = {
        "adaptive": {field: preset.reasoning_default},
        **{
            level: {field: level}
            for level in preset.reasoning_levels
        },
    }
    if preset.reasoning_off:
        modes["off"] = {field: "none"}
    return {
        "thinking_fields": modes,
        **({"requires_thinking": True} if not preset.reasoning_off else {}),
    }


def _models(catalog: Catalog) -> Tuple[Mapping[str, object], ...]:
    return tuple(
        {
            "id": identifier,
            "tier": "medium",
            "context_window": preset.default_context,
            "max_output_tokens": agent_output_limit(
                preset.default_context
            ),
            "supports_thinking": bool(preset.reasoning_control),
            **_native_thinking_fields(preset),
        }
        for identifier, preset in catalog.llama_presets.items()
        if is_agent_capable(preset)
    )


def _provider_script(
    display_name: str,
    endpoint: str,
    models: Sequence[Mapping[str, object]],
) -> bytes:
    info = json.dumps(
        {
            "display_name": display_name,
            "base": "llama-cpp",
            "has_auth": False,
        },
        separators=(",", ":"),
    )
    listed = json.dumps(list(models), separators=(",", ":"))
    resolved = json.dumps(
        {"base_url": endpoint, "headers": {}}, separators=(",", ":")
    )
    content = """#!/bin/sh
set -eu
case "${{1:-}}" in
  info) printf '%s\\n' {info} ;;
  models) printf '%s\\n' {models} ;;
  resolve|refresh|reload) printf '%s\\n' {resolved} ;;
  *) printf '%s\\n' 'unsupported ROCmplete provider command' >&2; exit 2 ;;
esac
""".format(
        info=shlex.quote(info),
        models=shlex.quote(listed),
        resolved=shlex.quote(resolved),
    )
    return content.encode("utf-8")


def _default_model(
    catalog: Catalog, data_dir: Path
) -> Tuple[str, str, str]:
    return default_agent_model(catalog, data_dir, "Maki")


def _render_init(provider: str, model: str) -> bytes:
    model_spec = json.dumps("{}/{}".format(provider, model))
    # Each generated model maps adaptive onto its native default. A single
    # provider-wide named level would be wrong after switching families.
    content = """maki.setup({{
  always_thinking = "adaptive",
  provider = {{ default_model = {model} }},
  plugins = {{ task = {{ max_concurrent = 1 }} }},
}})
""".format(model=model_spec)
    return content.encode("utf-8")


def _render_tiers(provider: str, model: str) -> bytes:
    model_spec = "{}/{}".format(provider, model)
    contents = {
        "compaction": model_spec,
        "weak": model_spec,
        "medium": model_spec,
        "strong": model_spec,
    }
    return (json.dumps(contents, indent=2) + "\n").encode("utf-8")


def create_launch_plan(
    catalog: Catalog,
    data_dir: Path,
    port: int,
    arguments: Sequence[str],
    environ: Optional[Mapping[str, str]] = None,
    *,
    dwarfstar_port: int = 8000,
) -> MakiLaunchPlan:
    env = os.environ if environ is None else environ
    endpoint = "http://127.0.0.1:{}/v1".format(port)
    dwarfstar_endpoint = "http://127.0.0.1:{}/v1".format(dwarfstar_port)
    forwarded = tuple(arguments)
    if forwarded[:1] == ("--",):
        forwarded = forwarded[1:]
    executable = find_real_executable("maki", WRAPPER_PATH, env, "Maki")

    passthrough = (
        bool(forwarded) and forwarded[0] in _INFORMATION_ARGUMENTS
    ) or (forwarded[:1] and forwarded[0] in _PASSTHROUGH_COMMANDS)
    management = forwarded[:1] and forwarded[0] in _MANAGEMENT_COMMANDS
    if passthrough or management:
        provider, model, thinking = recommended_agent_model(catalog)
        mode = "passthrough" if passthrough else "management"
        defaults = (None, None, None)
    else:
        provider, model, thinking = _default_model(catalog, data_dir)
        mode = "session"
        defaults = (provider, model, thinking)

    dwarfstar_models = (
        {
            "id": DWARFSTAR_MODEL,
            "tier": "medium",
            "context_window": DWARFSTAR_DEFAULT_CONTEXT,
            "max_output_tokens": DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
            "supports_thinking": True,
            "thinking_fields": {
                "off": {"reasoning_effort": "none"},
                "adaptive": {"reasoning_effort": "high"},
                "high": {"reasoning_effort": "high"},
            },
        },
    )
    return MakiLaunchPlan(
        command=(executable, *forwarded),
        default_provider=defaults[0],
        default_model=defaults[1],
        default_thinking=defaults[2],
        endpoint=endpoint,
        dwarfstar_endpoint=dwarfstar_endpoint,
        init_content=_render_init(provider, model),
        provider_contents={
            PROVIDER_ID: _provider_script(
                "ROCmplete llama.cpp", endpoint, _models(catalog)
            ),
            DWARFSTAR_PROVIDER_ID: _provider_script(
                "ROCmplete DwarfStar",
                dwarfstar_endpoint,
                dwarfstar_models,
            ),
        },
        tier_content=_render_tiers(provider, model),
        mode=mode,
    )


def sandbox_paths(data_dir: Path) -> MakiSandboxPaths:
    return agent_sandbox_paths(data_dir, "maki")


def _secure_directory(path: Path, display_name: str) -> None:
    try:
        status = path.lstat()
    except FileNotFoundError:
        try:
            path.mkdir(mode=0o700)
        except OSError as error:
            raise LauncherError(
                "cannot create {} directory {}: {}".format(
                    display_name, path, error
                )
            )
        return
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} directory {}: {}".format(
                display_name, path, error
            )
        )
    if stat.S_ISLNK(status.st_mode) or not stat.S_ISDIR(status.st_mode):
        raise LauncherError(
            "{} path is not a real directory: {}".format(display_name, path)
        )
    try:
        path.chmod(0o700)
    except OSError as error:
        raise LauncherError(
            "cannot secure {} directory {}: {}".format(
                display_name, path, error
            )
        )


def _write_managed_file(
    path: Path,
    content: bytes,
    mode: int,
    data_dir: Path,
    root: Path,
    display_name: str,
    *,
    preserve_existing: bool = False,
) -> None:
    validate_managed_parent(path, root, data_dir, display_name)
    try:
        status = path.lstat()
    except FileNotFoundError:
        status = None
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} {}: {}".format(display_name, path, error)
        )
    if status is not None:
        if not stat.S_ISREG(status.st_mode) or status.st_nlink != 1:
            raise LauncherError(
                "{} is not a private regular file: {}".format(
                    display_name, path
                )
            )
        if preserve_existing:
            try:
                path.chmod(mode)
            except OSError as error:
                raise LauncherError(
                    "cannot secure {} {}: {}".format(
                        display_name, path, error
                    )
                )
            return
        try:
            if path.read_bytes() == content:
                path.chmod(mode)
                return
        except OSError as error:
            raise LauncherError(
                "cannot read {} {}: {}".format(display_name, path, error)
            )

    temporary = None
    try:
        descriptor, temporary = tempfile.mkstemp(
            prefix=".{}-".format(path.name), suffix=".tmp", dir=str(path.parent)
        )
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, mode)
        os.replace(temporary, path)
        temporary = None
    except OSError as error:
        raise LauncherError(
            "cannot write {} {}: {}".format(display_name, path, error)
        )
    finally:
        if temporary is not None:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass


def _read_private_file(path: Path, display_name: str) -> Optional[bytes]:
    try:
        status = path.lstat()
    except FileNotFoundError:
        return None
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} {}: {}".format(display_name, path, error)
        )
    if not stat.S_ISREG(status.st_mode) or status.st_nlink != 1:
        raise LauncherError(
            "{} is not a private regular file: {}".format(
                display_name, path
            )
        )
    try:
        return path.read_bytes()
    except OSError as error:
        raise LauncherError(
            "cannot read {} {}: {}".format(display_name, path, error)
        )


def prepare_state(
    plan: MakiLaunchPlan,
    paths: MakiSandboxPaths,
    data_dir: Path,
) -> None:
    """Prepare private Maki configuration while preserving user tier picks."""

    prepare_agent_sandbox_paths(paths, data_dir, "Maki")
    config_dir = paths.config / "maki"
    providers_dir = config_dir / "providers"
    state_dir = paths.state / "maki"
    for path in (config_dir, providers_dir, state_dir):
        _secure_directory(path, "Maki state")
    try:
        unexpected = sorted(
            entry.name
            for entry in providers_dir.iterdir()
            if entry.name not in plan.provider_contents
        )
    except OSError as error:
        raise LauncherError(
            "cannot inspect Maki provider directory {}: {}".format(
                providers_dir, error
            )
        )
    if unexpected:
        raise LauncherError(
            "Maki private provider directory contains unmanaged entries: "
            "{}".format(", ".join(unexpected))
        )
    _write_managed_file(
        config_dir / "init.lua",
        plan.init_content,
        0o600,
        data_dir,
        paths.root,
        "Maki generated configuration",
    )
    for name, content in plan.provider_contents.items():
        _write_managed_file(
            providers_dir / name,
            content,
            0o700,
            data_dir,
            paths.root,
            "Maki provider configuration",
        )
    tiers = state_dir / "model-tiers"
    tier_seed = state_dir / "rocmplete-tier-seed"
    current_tiers = _read_private_file(tiers, "Maki model tiers")
    previous_seed = _read_private_file(tier_seed, "Maki tier seed")
    preserve_tiers = current_tiers is not None and (
        previous_seed is None or current_tiers != previous_seed
    )
    _write_managed_file(
        tiers,
        plan.tier_content,
        0o600,
        data_dir,
        paths.root,
        "Maki model tiers",
        preserve_existing=preserve_tiers,
    )
    _write_managed_file(
        tier_seed,
        plan.tier_content,
        0o600,
        data_dir,
        paths.root,
        "Maki tier seed",
    )


def launch_environment(
    paths: MakiSandboxPaths,
    environ: Optional[Mapping[str, str]] = None,
) -> Mapping[str, str]:
    child = dict(os.environ if environ is None else environ)
    home = Path(child.get("HOME", str(Path.home())))
    legacy = home / ".maki"
    if legacy.is_dir():
        raise LauncherError(
            "Maki would ignore ROCmplete's private XDG state while {} exists; "
            "run the real 'maki migrate xdg' first".format(legacy)
        )
    child.update(
        {
            "XDG_CONFIG_HOME": str(paths.config),
            "XDG_DATA_HOME": str(paths.data),
            "XDG_STATE_HOME": str(paths.state),
            "XDG_CACHE_HOME": str(paths.cache),
        }
    )
    return child


def create_sandbox_plan(
    plan: MakiLaunchPlan,
    data_dir: Path,
    workdir: Path,
    environ: Optional[Mapping[str, str]] = None,
) -> MakiSandboxPlan:
    return create_agent_sandbox_plan(
        plan.command,
        data_dir,
        workdir,
        "maki",
        "Maki",
        {},
        environ,
    )
