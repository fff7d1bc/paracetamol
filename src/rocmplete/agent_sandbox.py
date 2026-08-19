"""Shared bubblewrap boundary for local tool-using agent clients."""

from __future__ import annotations

import os
import pwd
import shutil
import stat
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Mapping, Optional, Sequence, Tuple

from .errors import LauncherError
from .layout import StorageLayout, validate_managed_parent


SANDBOX_HOME = Path("/run/rocmplete/home")
SANDBOX_RUNTIME = Path("/run/rocmplete/runtime")


@dataclass(frozen=True)
class AgentSandboxPaths:
    root: Path
    config: Path
    data: Path
    state: Path
    cache: Path


@dataclass(frozen=True)
class AgentSandboxPlan:
    command: Tuple[str, ...]
    environment: Mapping[str, str]
    workdir: Path
    state_root: Path


def find_real_executable(
    name: str,
    wrapper: Path,
    environ: Mapping[str, str],
    display_name: str,
) -> str:
    """Find a client executable while excluding ROCmplete's PATH wrapper."""

    path_value = environ.get("PATH", os.defpath)
    try:
        resolved_wrapper = wrapper.resolve(strict=False)
    except OSError as error:
        raise LauncherError(
            "cannot resolve {} wrapper: {}".format(display_name, error)
        )
    search = []
    for entry in path_value.split(os.pathsep):
        directory = Path(entry or os.curdir)
        try:
            candidate = (directory / name).resolve(strict=False)
        except OSError:
            candidate = directory / name
        if candidate == resolved_wrapper:
            continue
        search.append(entry)
    executable = shutil.which(name, path=os.pathsep.join(search))
    if executable is None:
        raise LauncherError(
            "{} executable not found outside ROCmplete's bin directory".format(
                display_name
            )
        )
    return executable


def sandbox_paths(data_dir: Path, application: str) -> AgentSandboxPaths:
    root = StorageLayout(data_dir).application(application) / "sandbox"
    return AgentSandboxPaths(
        root=root,
        config=root / "config",
        data=root / "data",
        state=root / "state",
        cache=root / "cache",
    )


def prepare_sandbox_paths(
    paths: AgentSandboxPaths,
    data_dir: Path,
    display_name: str,
) -> None:
    validate_managed_parent(
        paths.cache / ".boundary",
        paths.root,
        data_dir,
        "{} sandbox state".format(display_name),
    )
    shared_parent = paths.root.parent.parent
    for path in (
        shared_parent,
        paths.root.parent,
        paths.root,
        paths.config,
        paths.data,
        paths.state,
        paths.cache,
    ):
        try:
            status = path.lstat()
        except FileNotFoundError:
            try:
                path.mkdir(mode=0o700)
            except OSError as error:
                raise LauncherError(
                    "cannot create {} sandbox directory {}: {}".format(
                        display_name, path, error
                    )
                )
            continue
        except OSError as error:
            raise LauncherError(
                "cannot inspect {} sandbox directory {}: {}".format(
                    display_name, path, error
                )
            )
        if stat.S_ISLNK(status.st_mode) or not stat.S_ISDIR(status.st_mode):
            raise LauncherError(
                "{} sandbox path is not a real directory: {}".format(
                    display_name, path
                )
            )
        if path != shared_parent:
            try:
                path.chmod(0o700)
            except OSError as error:
                raise LauncherError(
                    "cannot secure {} sandbox directory {}: {}".format(
                        display_name, path, error
                    )
                )


def _resolved_executable(name: str, environ: Mapping[str, str]) -> Path:
    executable = shutil.which(name, path=environ.get("PATH", os.defpath))
    if executable is None:
        if name == "bwrap":
            raise LauncherError(
                "bubblewrap executable 'bwrap' not found on PATH; install "
                "the bubblewrap package or rerun with --no-sandbox"
            )
        raise LauncherError("{} executable not found on PATH".format(name))
    try:
        resolved = Path(executable).resolve(strict=True)
        status = resolved.stat()
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} executable {}: {}".format(
                name, executable, error
            )
        )
    if not stat.S_ISREG(status.st_mode) or not os.access(resolved, os.X_OK):
        raise LauncherError(
            "{} executable is not an executable regular file: {}".format(
                name, resolved
            )
        )
    return resolved


def _linuxbrew_prefix(executable: Path) -> Optional[Path]:
    for parent in executable.parents:
        if parent.name == ".linuxbrew":
            return parent
    return None


def _sandbox_path(executable: Path) -> str:
    entries = []
    prefix = _linuxbrew_prefix(executable)
    if prefix is not None:
        entries.extend((str(prefix / "bin"), str(prefix / "sbin")))
    entries.extend(
        (
            "/usr/local/sbin",
            "/usr/local/bin",
            "/usr/sbin",
            "/usr/bin",
            "/sbin",
            "/bin",
        )
    )
    return os.pathsep.join(entries)


def _git_identity(environ: Mapping[str, str]) -> Mapping[str, str]:
    result: Dict[str, str] = {}
    configured = (
        ("GIT_AUTHOR_NAME", "user.name"),
        ("GIT_AUTHOR_EMAIL", "user.email"),
    )
    git = shutil.which("git", path=environ.get("PATH", os.defpath))
    for variable, key in configured:
        value = environ.get(variable)
        if value is None and git is not None:
            try:
                probe = subprocess.run(
                    [git, "config", "--global", "--get", key],
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.DEVNULL,
                    text=True,
                    check=False,
                    timeout=5,
                )
            except (OSError, subprocess.TimeoutExpired):
                probe = None
            if probe is not None and probe.returncode == 0:
                value = probe.stdout.rstrip("\r\n")
        if value and "\x00" not in value and "\n" not in value:
            result[variable] = value
    if "GIT_AUTHOR_NAME" in result:
        result["GIT_COMMITTER_NAME"] = result["GIT_AUTHOR_NAME"]
    if "GIT_AUTHOR_EMAIL" in result:
        result["GIT_COMMITTER_EMAIL"] = result["GIT_AUTHOR_EMAIL"]
    return result


def _terminal_environment(environ: Mapping[str, str]) -> Mapping[str, str]:
    allowed = {
        "COLORTERM",
        "LANG",
        "LANGUAGE",
        "LC_ALL",
        "NO_COLOR",
        "TERM",
        "TZ",
    }
    return {
        key: value
        for key, value in environ.items()
        if key in allowed or key.startswith("LC_")
    }


def _directory_arguments(paths: Sequence[Path]) -> Tuple[str, ...]:
    directories = set()
    for path in paths:
        for parent in reversed(path.parents):
            if parent != Path("/"):
                directories.add(parent)
        directories.add(path)
    arguments = []
    for directory in sorted(
        directories, key=lambda item: (len(item.parts), str(item))
    ):
        if directory == Path("/usr") or Path("/usr") in directory.parents:
            continue
        if directory == Path("/etc") or Path("/etc") in directory.parents:
            continue
        arguments.extend(("--dir", str(directory)))
    return tuple(arguments)


def _runtime_resolver_target(
    resolv_conf: Path = Path("/etc/resolv.conf"),
    runtime_root: Path = Path("/run"),
) -> Optional[Path]:
    """Return a dynamic resolver target hidden by the private /run mount."""

    try:
        link_status = resolv_conf.lstat()
    except FileNotFoundError:
        return None
    except OSError as error:
        raise LauncherError(
            "cannot inspect host resolver {}: {}".format(
                resolv_conf, error
            )
        )
    if not stat.S_ISLNK(link_status.st_mode):
        return None
    try:
        target = resolv_conf.resolve(strict=True)
        target_status = target.stat()
    except (OSError, RuntimeError) as error:
        raise LauncherError(
            "cannot resolve host resolver symlink {}: {}".format(
                resolv_conf, error
            )
        )
    try:
        target.relative_to(runtime_root)
    except ValueError:
        return None
    if target == runtime_root or not stat.S_ISREG(target_status.st_mode):
        raise LauncherError(
            "host resolver target is not a regular file below {}: {}".format(
                runtime_root, target
            )
        )
    return target


def _runtime_mdns_socket(
    socket_path: Path = Path("/run/avahi-daemon/socket"),
) -> Optional[Path]:
    """Return the exact host Avahi socket needed for `.local` lookups."""

    try:
        status = socket_path.lstat()
    except FileNotFoundError:
        return None
    except OSError as error:
        raise LauncherError(
            "cannot inspect host mDNS socket {}: {}".format(
                socket_path, error
            )
        )
    if not stat.S_ISSOCK(status.st_mode):
        raise LauncherError(
            "host mDNS path is not a Unix socket: {}".format(socket_path)
        )
    return socket_path


def _home_alias_arguments(
    home: Path = Path("/home"),
    expected_target: Path = Path("/var/home"),
) -> Tuple[str, ...]:
    """Preserve Fedora's stable home alias without exposing either tree."""

    try:
        if not stat.S_ISLNK(home.lstat().st_mode):
            return ()
        target = os.readlink(str(home))
        if home.resolve(strict=True) != expected_target.resolve(strict=True):
            return ()
    except OSError:
        return ()
    return ("--symlink", target, str(home))


def _validated_workdir(
    workdir: Path,
    paths: AgentSandboxPaths,
    display_name: str,
) -> Path:
    try:
        working = workdir.resolve(strict=True)
        status = working.stat()
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} working directory {}: {}".format(
                display_name, workdir, error
            )
        )
    if not stat.S_ISDIR(status.st_mode):
        raise LauncherError(
            "{} working directory is not a directory: {}".format(
                display_name, working
            )
        )
    try:
        host_home = Path(pwd.getpwuid(os.getuid()).pw_dir).resolve(strict=True)
        host_home.relative_to(working)
        contains_home = True
    except ValueError:
        contains_home = False
    except OSError as error:
        raise LauncherError(
            "cannot inspect the host home directory: {}".format(error)
        )
    if contains_home:
        raise LauncherError(
            "refusing a {} sandbox working directory that contains the host "
            "home: {}".format(display_name, working)
        )
    try:
        working.relative_to(paths.root)
        overlaps = True
    except ValueError:
        try:
            paths.root.relative_to(working)
            overlaps = True
        except ValueError:
            overlaps = False
    if overlaps:
        raise LauncherError(
            "{} working directory overlaps its private sandbox state: "
            "{}".format(display_name, paths.root)
        )
    return working


def create_sandbox_plan(
    command: Sequence[str],
    data_dir: Path,
    workdir: Path,
    application: str,
    display_name: str,
    child_environment: Mapping[str, str],
    environ: Optional[Mapping[str, str]] = None,
    *,
    read_only_mounts: Sequence[Tuple[Path, Path]] = (),
    client_arguments: Sequence[str] = (),
) -> AgentSandboxPlan:
    env = os.environ if environ is None else environ
    paths = sandbox_paths(data_dir, application)
    working = _validated_workdir(workdir, paths, display_name)
    bwrap = _resolved_executable("bwrap", env)
    executable = Path(command[0]).resolve(strict=True)
    prefix = _linuxbrew_prefix(executable)
    resolver_target = _runtime_resolver_target()
    mdns_socket = _runtime_mdns_socket()
    mount_paths = [
        SANDBOX_HOME,
        SANDBOX_HOME / ".config",
        SANDBOX_HOME / ".local",
        SANDBOX_HOME / ".local" / "share",
        SANDBOX_HOME / ".local" / "state",
        SANDBOX_HOME / ".cache",
        SANDBOX_RUNTIME,
        working.parent,
    ]
    if resolver_target is not None:
        mount_paths.append(resolver_target.parent)
    if mdns_socket is not None:
        mount_paths.append(mdns_socket.parent)
    mount_paths.extend(
        destination.parent for _, destination in read_only_mounts
    )
    if prefix is not None:
        mount_paths.append(prefix.parent)
    else:
        mount_paths.append(executable.parent)

    arguments = [
        str(bwrap),
        "--unshare-all",
        "--share-net",
        "--die-with-parent",
        "--new-session",
        "--hostname",
        "rocmplete",
        "--cap-drop",
        "ALL",
        "--clearenv",
        "--proc",
        "/proc",
        "--dev",
        "/dev",
        "--tmpfs",
        "/tmp",
        "--tmpfs",
        "/run",
        "--ro-bind",
        "/usr",
        "/usr",
        "--ro-bind",
        "/etc",
        "/etc",
        "--symlink",
        "usr/bin",
        "/bin",
        "--symlink",
        "usr/sbin",
        "/sbin",
        "--symlink",
        "usr/lib",
        "/lib",
    ]
    if Path("/usr/lib64").exists():
        arguments.extend(("--symlink", "usr/lib64", "/lib64"))
    arguments.extend(_home_alias_arguments())
    arguments.extend(_directory_arguments(mount_paths))
    if resolver_target is not None:
        # /etc is visible read-only, but its resolv.conf symlink commonly
        # points into /run. Recreate only that exact target after /run becomes
        # a private tmpfs so shared host networking retains working DNS.
        arguments.extend(
            (
                "--ro-bind",
                str(resolver_target),
                str(resolver_target),
            )
        )
    if mdns_socket is not None:
        # NSS mDNS modules ask Avahi over this Unix socket. Expose that one
        # endpoint rather than the host /run tree so LAN `.local` names work
        # without weakening the rest of the runtime-state boundary.
        arguments.extend(
            ("--ro-bind", str(mdns_socket), str(mdns_socket))
        )
    if prefix is not None:
        arguments.extend(("--ro-bind", str(prefix), str(prefix)))
    elif not (
        executable == Path("/usr") or Path("/usr") in executable.parents
    ):
        arguments.extend(("--ro-bind", str(executable), str(executable)))
    for source, destination in read_only_mounts:
        arguments.extend(("--ro-bind", str(source), str(destination)))
    arguments.extend(("--bind", str(working), str(working)))
    state_mounts = (
        (paths.config, SANDBOX_HOME / ".config"),
        (paths.data, SANDBOX_HOME / ".local" / "share"),
        (paths.state, SANDBOX_HOME / ".local" / "state"),
        (paths.cache, SANDBOX_HOME / ".cache"),
    )
    for source, destination in state_mounts:
        arguments.extend(("--bind", str(source), str(destination)))

    username = pwd.getpwuid(os.getuid()).pw_name
    child = {
        "HOME": str(SANDBOX_HOME),
        "PATH": _sandbox_path(executable),
        "SHELL": "/bin/sh",
        "USER": username,
        "LOGNAME": username,
        "TMPDIR": "/tmp",
        "XDG_CONFIG_HOME": str(SANDBOX_HOME / ".config"),
        "XDG_DATA_HOME": str(SANDBOX_HOME / ".local" / "share"),
        "XDG_STATE_HOME": str(SANDBOX_HOME / ".local" / "state"),
        "XDG_CACHE_HOME": str(SANDBOX_HOME / ".cache"),
        "XDG_RUNTIME_DIR": str(SANDBOX_RUNTIME),
    }
    child.update(child_environment)
    child.update(_terminal_environment(env))
    child.update(_git_identity(env))
    for key, value in child.items():
        arguments.extend(("--setenv", key, value))
    arguments.extend(("--chdir", str(working), "--", str(executable)))
    arguments.extend(client_arguments)
    arguments.extend(command[1:])
    return AgentSandboxPlan(
        command=tuple(arguments),
        environment={"PATH": env.get("PATH", os.defpath)},
        workdir=working,
        state_root=paths.root,
    )
