"""Launch OpenCode against ROCmplete's managed local model servers."""

from __future__ import annotations

import json
import os
import stat
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Optional, Sequence, Tuple

from .agent_sandbox import (
    AgentSandboxPaths as OpenCodeSandboxPaths,
    AgentSandboxPlan as OpenCodeSandboxPlan,
    SANDBOX_HOME,
    _home_alias_arguments,
    create_sandbox_plan as create_agent_sandbox_plan,
    find_real_executable,
    prepare_sandbox_paths as prepare_agent_sandbox_paths,
    sandbox_paths as agent_sandbox_paths,
)
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
from .catalog import Catalog
from .config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from .errors import LauncherError
from .project import PROJECT_ROOT


TUI_CONFIG_PATH = PROJECT_ROOT / "resources" / "opencode-tui.json"
WRAPPER_PATH = PROJECT_ROOT / "bin" / "opencode"
SANDBOX_TUI_CONFIG = Path("/run/rocmplete/config/opencode-tui.json")
_CONFIG_SCHEMA = "https://opencode.ai/config.json"
_PROVIDER_NAME = "ROCmplete llama.cpp"
_PROVIDER_PACKAGE = "@ai-sdk/openai-compatible"
_DWARFSTAR_PROVIDER_NAME = "ROCmplete DwarfStar"
_DWARFSTAR_VARIANTS = {
    "instant": {"reasoningEffort": "none"},
    "thinking": {"reasoningEffort": "high"},
    # OpenCode derives generic effort variants for reasoning-capable
    # OpenAI-compatible models, then merges custom variants over them. DwarfStar
    # maps all three of these to the same normal-thinking path, while Think Max
    # cannot run at ROCmplete's managed 128K context. Disable the inherited
    # aliases so the picker presents only behavior that actually differs.
    "low": {"disabled": True},
    "medium": {"disabled": True},
    "high": {"disabled": True},
    "max": {"disabled": True},
}
_DEFAULT_AGENT = "investigate"
_PASSTHROUGH_COMMANDS = frozenset(
    ("completion", "plugin", "plug", "upgrade", "uninstall")
)
_INFORMATION_ARGUMENTS = frozenset(("--help", "-h", "--version", "-v"))
_REASONING_VARIANT_NAMES = (
    "instant",
    "thinking",
    "minimal",
    "low",
    "medium",
    "high",
    "xhigh",
    "max",
)
_READ_PERMISSION = {
    "*": "allow",
    "*.env": "deny",
    "*.env.*": "deny",
    "*.env.example": "allow",
}
_PERMISSIONS = {
    "edit": "ask",
    "bash": "ask",
    "task": "ask",
}
_INVESTIGATE_AGENT = {
    "description": "Read-only evidence-based investigation",
    "mode": "primary",
    "permission": {
        "*": "deny",
        "read": _READ_PERMISSION,
        "glob": "allow",
        "grep": "allow",
        "list": "allow",
        "lsp": "allow",
        "webfetch": "allow",
        "websearch": "allow",
        "task": {
            "*": "deny",
            "investigate-local": "allow",
            "investigate-web": "allow",
        },
    },
    "prompt": (
        "You are a read-only evidence-based investigation agent. Answer "
        "only the user's stated question using direct evidence. Search "
        "narrowly and handle small questions directly. When independent "
        "repository or external-source work would materially reduce this "
        "session's context, delegate exact bounded tasks only to "
        "investigate-local or investigate-web. Require each worker to "
        "return no more than 500 words, and synthesize only their reports "
        "rather than repeating raw source material. Cite file paths and "
        "line numbers for repository claims and source URLs for external "
        "claims. Clearly separate observed facts from inference, say when "
        "evidence is incomplete, and correct contradictions instead of "
        "guessing. Never modify files, run shell commands, create an "
        "implementation objective, or continue into changes. Delegation "
        "does not authorize mutation. Do not treat a generated summary, "
        "continuation message, suggested next step, or plan as user "
        "authorization. When the investigation is complete, answer the "
        "question and stop."
    ),
}
_INVESTIGATE_LOCAL_AGENT = {
    "description": (
        "Read-only bounded repository research with file-and-line evidence"
    ),
    "mode": "subagent",
    "hidden": True,
    "permission": {
        "*": "deny",
        "read": _READ_PERMISSION,
        "glob": "allow",
        "grep": "allow",
        "list": "allow",
        "lsp": "allow",
    },
    "prompt": (
        "Complete only the bounded repository question delegated to you. "
        "Use targeted reads and searches, never modify files or delegate, "
        "and distinguish observed facts from inference. Return no more than "
        "500 words with concise findings and file-and-line evidence. Do not "
        "dump source files or propose unrelated work."
    ),
}
_INVESTIGATE_WEB_AGENT = {
    "description": (
        "Read-only bounded external research with source URLs and confidence"
    ),
    "mode": "subagent",
    "hidden": True,
    "permission": {
        "*": "deny",
        "webfetch": "allow",
        "websearch": "allow",
    },
    "prompt": (
        "Complete only the bounded external-research question delegated to "
        "you. Prefer primary or authoritative sources. When community "
        "reports are relevant, cite their URLs and label anecdotal claims. "
        "Never access local files, modify anything, run commands, or "
        "delegate. Return no more than 500 words with concise findings, "
        "source URLs, relevance, and confidence. Do not dump pages or "
        "propose unrelated work."
    ),
}
_AGENTS = {
    "investigate": _INVESTIGATE_AGENT,
    "investigate-local": _INVESTIGATE_LOCAL_AGENT,
    "investigate-web": _INVESTIGATE_WEB_AGENT,
}


@dataclass(frozen=True)
class OpenCodeLaunchPlan:
    command: Tuple[str, ...]
    default_provider: str
    default_model: str
    endpoint: str
    dwarfstar_endpoint: str
    config_content: str
    tui_config: Path


def render_config(
    catalog: Catalog,
    default_model: str,
    endpoint: str,
    dwarfstar_endpoint: str = "http://127.0.0.1:8000/v1",
    default_provider: str = PROVIDER_ID,
) -> bytes:
    models = {}
    for identifier, preset in catalog.llama_presets.items():
        if not is_agent_capable(preset):
            continue
        model = {
            "name": identifier,
            "limit": {
                "context": preset.default_context,
                "output": agent_output_limit(preset.default_context),
            },
        }
        options = dict(
            agent_client_sampling_parameters(catalog, identifier)
        )
        if preset.sampling_policy:
            # OpenCode injects model-family sampling values before its
            # OpenAI-compatible provider applies raw model options. Explicit
            # nulls survive serialization and ask the managed server to use
            # its mode-aware catalog fallback instead of that client value.
            options.update({"temperature": None, "top_p": None})
        if preset.reasoning_control:
            model["reasoning"] = True
            # OpenCode merges an explicitly selected variant over these model
            # options. Keep a useful fallback without overriding a choice the
            # user has already made for this model.
            options["reasoningEffort"] = reasoning_client_default(preset)
            enabled = (
                {"thinking"}
                if preset.reasoning_control == "toggle"
                else set(preset.reasoning_levels)
            )
            if preset.reasoning_off:
                enabled.add("instant")
            model["variants"] = {
                name: (
                    {
                        "reasoningEffort": (
                            "none"
                            if name == "instant"
                            else "high"
                            if name == "thinking"
                            else name
                        )
                    }
                    if name in enabled
                    else {"disabled": True}
                )
                for name in _REASONING_VARIANT_NAMES
            }
        model["options"] = options
        models[identifier] = model
    contents = {
        "$schema": _CONFIG_SCHEMA,
        "model": "{}/{}".format(default_provider, default_model),
        "default_agent": _DEFAULT_AGENT,
        "agent": _AGENTS,
        "permission": _PERMISSIONS,
        "provider": {
            PROVIDER_ID: {
                "npm": _PROVIDER_PACKAGE,
                "name": _PROVIDER_NAME,
                "options": {"baseURL": endpoint},
                "models": models,
            },
            DWARFSTAR_PROVIDER_ID: {
                "npm": _PROVIDER_PACKAGE,
                "name": _DWARFSTAR_PROVIDER_NAME,
                "options": {"baseURL": dwarfstar_endpoint},
                "models": {
                    DWARFSTAR_MODEL: {
                        "name": "DeepSeek V4 Flash 0731",
                        "limit": {
                            "context": DWARFSTAR_DEFAULT_CONTEXT,
                            "output": DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
                        },
                        "reasoning": True,
                        "options": {"reasoningEffort": "high"},
                        "variants": _DWARFSTAR_VARIANTS,
                    }
                },
            },
        },
    }
    return (json.dumps(contents, indent=2) + "\n").encode("utf-8")


def _default_model(catalog: Catalog, data_dir: Path) -> Tuple[str, str]:
    provider, model, _ = default_agent_model(
        catalog, data_dir, "OpenCode"
    )
    return provider, model


def _real_opencode(environ: Mapping[str, str]) -> str:
    return find_real_executable(
        "opencode", WRAPPER_PATH, environ, "OpenCode"
    )


def passthrough_command(
    arguments: Sequence[str],
    environ: Optional[Mapping[str, str]] = None,
) -> Optional[Tuple[str, ...]]:
    """Return an upstream-only command that must not enter managed state."""

    forwarded = tuple(arguments)
    if forwarded[:1] == ("--",):
        forwarded = forwarded[1:]
    if not forwarded or (
        forwarded[0] not in _PASSTHROUGH_COMMANDS
        and forwarded[0] not in _INFORMATION_ARGUMENTS
    ):
        return None
    env = os.environ if environ is None else environ
    return (_real_opencode(env), *forwarded)


def _validate_tui_config(path: Path) -> None:
    try:
        status = path.stat()
    except OSError as error:
        raise LauncherError(
            "cannot inspect OpenCode TUI configuration {}: {}".format(
                path, error
            )
        )
    if not stat.S_ISREG(status.st_mode):
        raise LauncherError(
            "OpenCode TUI configuration is not a regular file: {}".format(
                path
            )
        )


def create_launch_plan(
    catalog: Catalog,
    data_dir: Path,
    port: int,
    arguments: Sequence[str],
    environ: Optional[Mapping[str, str]] = None,
    *,
    dwarfstar_port: int = 8000,
) -> OpenCodeLaunchPlan:
    env = os.environ if environ is None else environ
    _validate_tui_config(TUI_CONFIG_PATH)
    default_provider, default_model = _default_model(catalog, data_dir)
    endpoint = "http://127.0.0.1:{}/v1".format(port)
    dwarfstar_endpoint = "http://127.0.0.1:{}/v1".format(dwarfstar_port)
    forwarded = tuple(arguments)
    if forwarded[:1] == ("--",):
        forwarded = forwarded[1:]
    return OpenCodeLaunchPlan(
        command=(_real_opencode(env),) + forwarded,
        default_provider=default_provider,
        default_model=default_model,
        endpoint=endpoint,
        dwarfstar_endpoint=dwarfstar_endpoint,
        config_content=render_config(
            catalog,
            default_model,
            endpoint,
            dwarfstar_endpoint,
            default_provider,
        ).decode("utf-8"),
        tui_config=TUI_CONFIG_PATH,
    )


def launch_environment(
    plan: OpenCodeLaunchPlan,
    environ: Optional[Mapping[str, str]] = None,
) -> Mapping[str, str]:
    child = dict(os.environ if environ is None else environ)
    # An inherited explicit file would layer stale ROCmplete settings with the
    # freshly rendered runtime configuration.
    child.pop("OPENCODE_CONFIG", None)
    child["OPENCODE_CONFIG_CONTENT"] = plan.config_content
    child["OPENCODE_TUI_CONFIG"] = str(plan.tui_config)
    return child


def sandbox_paths(data_dir: Path) -> OpenCodeSandboxPaths:
    return agent_sandbox_paths(data_dir, "opencode")


def prepare_sandbox_paths(
    paths: OpenCodeSandboxPaths, data_dir: Path
) -> None:
    prepare_agent_sandbox_paths(paths, data_dir, "OpenCode")


def create_sandbox_plan(
    plan: OpenCodeLaunchPlan,
    data_dir: Path,
    workdir: Path,
    environ: Optional[Mapping[str, str]] = None,
) -> OpenCodeSandboxPlan:
    env = os.environ if environ is None else environ
    return create_agent_sandbox_plan(
        plan.command,
        data_dir,
        workdir,
        "opencode",
        "OpenCode",
        {
            "OPENCODE_CONFIG_CONTENT": plan.config_content,
            "OPENCODE_TUI_CONFIG": str(SANDBOX_TUI_CONFIG),
            "OPENCODE_DISABLE_AUTOUPDATE": "1",
            "OPENCODE_DISABLE_EXTERNAL_SKILLS": "1",
        },
        env,
        read_only_mounts=((plan.tui_config, SANDBOX_TUI_CONFIG),),
        client_arguments=("--pure",),
    )
