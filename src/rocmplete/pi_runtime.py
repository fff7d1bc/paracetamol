"""Pinned, host-side Pi runtime installation and resolution."""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import tempfile
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Mapping, Optional, Tuple

from .errors import LauncherError
from .layout import StorageLayout, validate_managed_parent
from .project import PROJECT_ROOT


PI_RUNTIME_SOURCE = PROJECT_ROOT / "agent-clients" / "pi"
PI_PACKAGE = "@earendil-works/pi-coding-agent"
_RECEIPT_SCHEMA_VERSION = 1
_NODE_REQUIREMENT = re.compile(r"^>=(\d+)\.(\d+)\.(\d+)$")
_NODE_VERSION = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$")


@dataclass(frozen=True)
class PiRuntimeSource:
    root: Path
    package_json: bytes
    package_lock: bytes
    package_version: str
    minimum_node: Tuple[int, int, int]
    lock_sha256: str


@dataclass(frozen=True)
class PiRuntime:
    root: Path
    node: Path
    entrypoint: Path
    package_version: str
    node_version: str
    lock_sha256: str


@dataclass(frozen=True)
class PiRuntimeInstallResult:
    runtime: PiRuntime
    installed: bool


def runtime_root(data_dir: Path) -> Path:
    return StorageLayout(data_dir).pi_runtime


def _read_regular_file(path: Path, description: str) -> bytes:
    try:
        status = path.lstat()
        if stat.S_ISLNK(status.st_mode) or not stat.S_ISREG(status.st_mode):
            raise LauncherError(
                "{} is not a regular file: {}".format(description, path)
            )
        return path.read_bytes()
    except FileNotFoundError:
        raise LauncherError("{} is missing: {}".format(description, path))
    except OSError as error:
        raise LauncherError(
            "cannot read {} {}: {}".format(description, path, error)
        )


def _json_object(contents: bytes, path: Path, description: str):
    try:
        value = json.loads(contents)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise LauncherError(
            "invalid {} {}: {}".format(description, path, error)
        )
    if not isinstance(value, dict):
        raise LauncherError("{} must contain an object: {}".format(
            description, path
        ))
    return value


def load_runtime_source(
    source_dir: Path = PI_RUNTIME_SOURCE,
) -> PiRuntimeSource:
    package_path = source_dir / "package.json"
    lock_path = source_dir / "package-lock.json"
    package_contents = _read_regular_file(
        package_path, "Pi runtime package manifest"
    )
    lock_contents = _read_regular_file(
        lock_path, "Pi runtime package lock"
    )
    package = _json_object(
        package_contents, package_path, "Pi runtime package manifest"
    )
    dependencies = package.get("dependencies")
    if not isinstance(dependencies, dict) or set(dependencies) != {PI_PACKAGE}:
        raise LauncherError(
            "Pi runtime package manifest must have exactly one dependency: "
            "{}".format(PI_PACKAGE)
        )
    version = dependencies.get(PI_PACKAGE)
    if not isinstance(version, str) or not re.fullmatch(
        r"\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", version
    ):
        raise LauncherError(
            "Pi runtime package version must be an exact release"
        )
    engines = package.get("engines")
    requirement = engines.get("node") if isinstance(engines, dict) else None
    match = _NODE_REQUIREMENT.fullmatch(requirement or "")
    if match is None:
        raise LauncherError(
            "Pi runtime Node requirement must have the form >=MAJOR.MINOR.PATCH"
        )
    minimum_node = tuple(int(part) for part in match.groups())

    lock = _json_object(lock_contents, lock_path, "Pi runtime package lock")
    packages = lock.get("packages")
    root_package = packages.get("") if isinstance(packages, dict) else None
    locked_dependencies = (
        root_package.get("dependencies")
        if isinstance(root_package, dict)
        else None
    )
    locked_pi = (
        packages.get("node_modules/{}".format(PI_PACKAGE))
        if isinstance(packages, dict)
        else None
    )
    if (
        lock.get("lockfileVersion") != 3
        or not isinstance(locked_dependencies, dict)
        or locked_dependencies.get(PI_PACKAGE) != version
        or not isinstance(locked_pi, dict)
        or locked_pi.get("version") != version
        or not isinstance(locked_pi.get("integrity"), str)
    ):
        raise LauncherError(
            "Pi runtime package lock does not pin the package manifest"
        )
    for package_path, metadata in packages.items():
        if package_path == "":
            continue
        if (
            not isinstance(metadata, dict)
            or not isinstance(metadata.get("version"), str)
            or not isinstance(metadata.get("license"), str)
            or not isinstance(metadata.get("resolved"), str)
            or not metadata["resolved"].startswith(
                "https://registry.npmjs.org/"
            )
            or not isinstance(metadata.get("integrity"), str)
            or not metadata["integrity"].startswith("sha512-")
        ):
            raise LauncherError(
                "Pi runtime package lock has incomplete immutable metadata "
                "for {}".format(package_path)
            )
    digest = hashlib.sha256()
    digest.update(package_contents)
    digest.update(b"\0")
    digest.update(lock_contents)
    return PiRuntimeSource(
        root=source_dir,
        package_json=package_contents,
        package_lock=lock_contents,
        package_version=version,
        minimum_node=minimum_node,
        lock_sha256=digest.hexdigest(),
    )


def _system_executable(name: str, system_path: str) -> Path:
    executable = shutil.which(name, path=system_path)
    if executable is None:
        raise LauncherError(
            "system {} is required for the managed Pi runtime; install a "
            "distribution package that provides it in /bin or /usr/bin".format(
                name
            )
        )
    try:
        resolved = Path(executable).resolve(strict=True)
        status = resolved.stat()
    except OSError as error:
        raise LauncherError(
            "cannot inspect system {} executable {}: {}".format(
                name, executable, error
            )
        )
    if not stat.S_ISREG(status.st_mode) or not os.access(resolved, os.X_OK):
        raise LauncherError(
            "system {} is not an executable regular file: {}".format(
                name, resolved
            )
        )
    return resolved


def _node_runtime(
    minimum: Tuple[int, int, int],
    environ: Mapping[str, str],
    system_path: str,
    runner: Callable[..., subprocess.CompletedProcess],
) -> Tuple[Path, str]:
    node = _system_executable("node", system_path)
    try:
        probe = runner(
            [str(node), "--version"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            check=False,
            env=dict(environ),
        )
    except OSError as error:
        raise LauncherError("cannot run system Node.js: {}".format(error))
    output = probe.stdout.strip() if probe.returncode == 0 else ""
    match = _NODE_VERSION.fullmatch(output)
    if match is None:
        detail = probe.stderr.strip() or output or "exit {}".format(
            probe.returncode
        )
        raise LauncherError(
            "cannot determine system Node.js version: {}".format(detail)
        )
    actual = tuple(int(part) for part in match.groups())
    if actual < minimum:
        raise LauncherError(
            "managed Pi requires Node.js >= {}, but system Node.js is {}".format(
                ".".join(str(part) for part in minimum), output.lstrip("v")
            )
        )
    return node, output.lstrip("v")


def _installation_path(root: Path, source: PiRuntimeSource) -> Path:
    return root / "installations" / source.lock_sha256


def _runtime_from_installation(
    installation: Path,
    source: PiRuntimeSource,
    node: Path,
    node_version: str,
) -> PiRuntime:
    receipt_path = installation / "receipt.json"
    receipt_contents = _read_regular_file(
        receipt_path, "Pi runtime receipt"
    )
    receipt = _json_object(
        receipt_contents, receipt_path, "Pi runtime receipt"
    )
    if (
        receipt.get("schema_version") != _RECEIPT_SCHEMA_VERSION
        or receipt.get("package") != PI_PACKAGE
        or receipt.get("package_version") != source.package_version
        or receipt.get("lock_sha256") != source.lock_sha256
    ):
        raise LauncherError(
            "Pi runtime receipt does not match this checkout; rerun "
            "./rocmplete agent install pi"
        )
    relative = receipt.get("entrypoint")
    if not isinstance(relative, str):
        raise LauncherError("Pi runtime receipt has no entrypoint")
    entrypoint = installation / relative
    try:
        resolved_installation = installation.resolve(strict=True)
        resolved_entrypoint = entrypoint.resolve(strict=True)
        resolved_entrypoint.relative_to(resolved_installation)
        status = resolved_entrypoint.stat()
    except (OSError, ValueError) as error:
        raise LauncherError(
            "cannot resolve managed Pi entrypoint {}: {}".format(
                entrypoint, error
            )
        )
    if not stat.S_ISREG(status.st_mode):
        raise LauncherError(
            "managed Pi entrypoint is not a regular file: {}".format(
                resolved_entrypoint
            )
        )
    return PiRuntime(
        root=resolved_installation,
        node=node,
        entrypoint=resolved_entrypoint,
        package_version=source.package_version,
        node_version=node_version,
        lock_sha256=source.lock_sha256,
    )


def resolve_pi_runtime(
    data_dir: Path,
    environ: Optional[Mapping[str, str]] = None,
    *,
    source_dir: Path = PI_RUNTIME_SOURCE,
    system_path: str = os.defpath,
    runner: Callable[..., subprocess.CompletedProcess] = subprocess.run,
) -> PiRuntime:
    source = load_runtime_source(source_dir)
    installation = _installation_path(runtime_root(data_dir), source)
    try:
        status = installation.lstat()
    except FileNotFoundError:
        raise LauncherError(
            "managed Pi {} is not installed for this checkout; run "
            "./rocmplete agent install pi".format(source.package_version)
        )
    except OSError as error:
        raise LauncherError(
            "cannot inspect managed Pi runtime {}: {}".format(
                installation, error
            )
        )
    if stat.S_ISLNK(status.st_mode) or not stat.S_ISDIR(status.st_mode):
        raise LauncherError(
            "managed Pi runtime is not a real directory: {}".format(
                installation
            )
        )
    env = os.environ if environ is None else environ
    node, node_version = _node_runtime(
        source.minimum_node, env, system_path, runner
    )
    return _runtime_from_installation(
        installation, source, node, node_version
    )


def _secure_directory(
    path: Path, description: str, *, enforce_private: bool = True
) -> None:
    try:
        path.mkdir(mode=0o700)
        return
    except FileExistsError:
        pass
    except OSError as error:
        raise LauncherError(
            "cannot create {} {}: {}".format(description, path, error)
        )
    try:
        status = path.lstat()
    except OSError as error:
        raise LauncherError(
            "cannot inspect {} {}: {}".format(description, path, error)
        )
    if stat.S_ISLNK(status.st_mode) or not stat.S_ISDIR(status.st_mode):
        raise LauncherError(
            "{} is not a real directory: {}".format(description, path)
        )
    if enforce_private:
        try:
            path.chmod(0o700)
        except OSError as error:
            raise LauncherError(
                "cannot secure {} {}: {}".format(description, path, error)
            )


@contextmanager
def _runtime_lock(root: Path):
    lock_path = root / ".install.lock"
    flags = os.O_RDWR | os.O_CREAT | getattr(os, "O_CLOEXEC", 0)
    flags |= getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(str(lock_path), flags, 0o600)
    except OSError as error:
        raise LauncherError(
            "cannot open Pi runtime installation lock {}: {}".format(
                lock_path, error
            )
        )
    try:
        status = os.fstat(descriptor)
        if (
            not stat.S_ISREG(status.st_mode)
            or status.st_uid != os.geteuid()
            or stat.S_IMODE(status.st_mode) & 0o077
        ):
            raise LauncherError(
                "Pi runtime installation lock is not a private owned file: "
                "{}".format(lock_path)
            )
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise LauncherError(
                "another Pi runtime installation is active for {}".format(
                    root
                )
            )
        yield
    except OSError as error:
        raise LauncherError(
            "cannot use Pi runtime installation lock {}: {}".format(
                lock_path, error
            )
        )
    finally:
        try:
            os.close(descriptor)
        except OSError:
            pass


def _write_private(path: Path, contents: bytes) -> None:
    try:
        with path.open("xb") as handle:
            handle.write(contents)
            handle.flush()
            os.fsync(handle.fileno())
        path.chmod(0o600)
    except OSError as error:
        raise LauncherError("cannot write {}: {}".format(path, error))


def _installed_entrypoint(staging: Path) -> Tuple[Path, str]:
    package_root = staging / "node_modules" / PI_PACKAGE
    package_path = package_root / "package.json"
    contents = _read_regular_file(package_path, "installed Pi package manifest")
    package = _json_object(
        contents, package_path, "installed Pi package manifest"
    )
    binary = package.get("bin")
    relative = binary.get("pi") if isinstance(binary, dict) else None
    if not isinstance(relative, str) or not relative:
        raise LauncherError("installed Pi package does not declare its pi binary")
    entrypoint = package_root / relative
    try:
        resolved_root = package_root.resolve(strict=True)
        resolved_entrypoint = entrypoint.resolve(strict=True)
        resolved_entrypoint.relative_to(resolved_root)
        status = resolved_entrypoint.stat()
    except (OSError, ValueError) as error:
        raise LauncherError(
            "cannot validate installed Pi entrypoint {}: {}".format(
                entrypoint, error
            )
        )
    if not stat.S_ISREG(status.st_mode):
        raise LauncherError(
            "installed Pi entrypoint is not a regular file: {}".format(
                resolved_entrypoint
            )
        )
    return resolved_entrypoint, str(resolved_entrypoint.relative_to(staging))


def install_pi_runtime(
    data_dir: Path,
    environ: Optional[Mapping[str, str]] = None,
    *,
    source_dir: Path = PI_RUNTIME_SOURCE,
    system_path: str = os.defpath,
    runner: Callable[..., subprocess.CompletedProcess] = subprocess.run,
) -> PiRuntimeInstallResult:
    env = dict(os.environ if environ is None else environ)
    source = load_runtime_source(source_dir)
    node, node_version = _node_runtime(
        source.minimum_node, env, system_path, runner
    )
    npm = _system_executable("npm", system_path)
    root = runtime_root(data_dir)
    validate_managed_parent(
        root / ".boundary", root, data_dir, "Pi runtime"
    )
    for directory, description, enforce_private in (
        (root.parent.parent, "Pi application parent", False),
        (root.parent, "Pi application directory", True),
        (root, "Pi runtime directory", True),
        (
            root / "installations",
            "Pi runtime installations directory",
            True,
        ),
    ):
        _secure_directory(
            directory, description, enforce_private=enforce_private
        )
    installation = _installation_path(root, source)

    with _runtime_lock(root):
        try:
            installation_status = installation.lstat()
        except FileNotFoundError:
            installation_status = None
        except OSError as error:
            raise LauncherError(
                "cannot inspect managed Pi installation {}: {}".format(
                    installation, error
                )
            )
        if installation_status is not None:
            if stat.S_ISLNK(installation_status.st_mode) or not stat.S_ISDIR(
                installation_status.st_mode
            ):
                raise LauncherError(
                    "managed Pi installation is not a real directory: "
                    "{}".format(installation)
                )
            try:
                runtime = _runtime_from_installation(
                    installation, source, node, node_version
                )
            except LauncherError as error:
                raise LauncherError(
                    "managed Pi installation is incomplete or modified: {}; "
                    "remove only {} and retry".format(error, installation)
                )
            return PiRuntimeInstallResult(runtime=runtime, installed=False)

        try:
            staging_value = tempfile.mkdtemp(
                prefix=".install-", dir=str(root / "installations")
            )
        except OSError as error:
            raise LauncherError(
                "cannot create Pi runtime staging directory: {}".format(
                    error
                )
            )
        staging = Path(staging_value)
        try:
            try:
                staging.chmod(0o700)
            except OSError as error:
                raise LauncherError(
                    "cannot secure Pi runtime staging directory {}: {}".format(
                        staging, error
                    )
                )
            _write_private(staging / "package.json", source.package_json)
            _write_private(staging / "package-lock.json", source.package_lock)
            child = dict(env)
            child["PATH"] = system_path
            child["npm_config_update_notifier"] = "false"
            try:
                process = runner(
                    [
                        str(npm),
                        "ci",
                        "--prefix",
                        str(staging),
                        "--omit=dev",
                        "--ignore-scripts",
                        "--no-audit",
                        "--no-fund",
                    ],
                    stdin=None,
                    check=False,
                    env=child,
                )
            except OSError as error:
                raise LauncherError(
                    "cannot run npm for the managed Pi runtime: {}".format(
                        error
                    )
                )
            if process.returncode != 0:
                raise LauncherError(
                    "npm failed to install managed Pi {} (exit {})".format(
                        source.package_version, process.returncode
                    )
                )
            entrypoint, relative_entrypoint = _installed_entrypoint(staging)
            try:
                probe = runner(
                    [str(node), str(entrypoint), "--version"],
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    text=True,
                    check=False,
                    env=child,
                )
            except OSError as error:
                raise LauncherError(
                    "cannot start the staged Pi runtime: {}".format(error)
                )
            reported = probe.stdout.strip()
            if probe.returncode != 0 or reported != source.package_version:
                detail = probe.stderr.strip() or reported or "no output"
                raise LauncherError(
                    "staged Pi version check failed: expected {}, got {}".format(
                        source.package_version, detail
                    )
                )
            receipt = {
                "schema_version": _RECEIPT_SCHEMA_VERSION,
                "package": PI_PACKAGE,
                "package_version": source.package_version,
                "lock_sha256": source.lock_sha256,
                "node_version": node_version,
                "entrypoint": relative_entrypoint,
            }
            _write_private(
                staging / "receipt.json",
                (json.dumps(receipt, indent=2) + "\n").encode("utf-8"),
            )
            try:
                os.replace(staging, installation)
                staging = None
            except OSError as error:
                raise LauncherError(
                    "cannot activate managed Pi runtime {}: {}".format(
                        installation, error
                    )
                )
        finally:
            if staging is not None:
                try:
                    shutil.rmtree(staging)
                except OSError:
                    pass

        runtime = _runtime_from_installation(
            installation, source, node, node_version
        )
        return PiRuntimeInstallResult(runtime=runtime, installed=True)
