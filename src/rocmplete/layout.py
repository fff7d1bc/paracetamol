"""Persistent host storage layout and managed-path boundaries.

Application state is writable and private to one runtime. Managed content is
shared, reproducible, and mounted read-only. Staging is tool-owned scratch
space that can be removed without affecting either.
"""

import os
import stat
from pathlib import Path

from .errors import LauncherError


def validate_managed_parent(
    path: Path,
    managed_root: Path,
    data_dir: Path,
    description: str,
) -> None:
    """Require a prospective file's parent to stay in one owned partition.

    Resolving only against the overall data directory is insufficient: a
    staging symlink into application state would still be "inside" that
    directory. Inspect every existing component without following symlinks so
    callers can reject pre-existing redirects before creating or replacing a
    final file below ``managed_root``.
    """
    data = Path(os.path.abspath(str(data_dir)))
    root = Path(os.path.abspath(str(managed_root)))
    candidate = Path(os.path.abspath(str(path)))
    try:
        root.relative_to(data)
        candidate.relative_to(root)
        relative_parent = candidate.parent.relative_to(data)
    except ValueError:
        raise LauncherError(
            "{} path escapes its managed root: {}".format(
                description, path
            )
        )

    components = [data]
    current = data
    for part in relative_parent.parts:
        current = current / part
        components.append(current)
    for current in components:
        try:
            status = current.lstat()
        except FileNotFoundError:
            break
        except OSError as error:
            raise LauncherError(
                "cannot inspect {} path component {}: {}".format(
                    description, current, error
                )
            )
        if stat.S_ISLNK(status.st_mode):
            raise LauncherError(
                "refusing symlinked {} path component: {}".format(
                    description, current
                )
            )
        if not stat.S_ISDIR(status.st_mode):
            raise LauncherError(
                "{} path component is not a directory: {}".format(
                    description, current
                )
            )


class StorageLayout:
    def __init__(self, root: Path) -> None:
        self.root = root

    def application(self, name: str) -> Path:
        return self.root / "apps" / name

    @property
    def comfyui(self) -> Path:
        return self.application("comfyui")

    @property
    def comfy_models(self) -> Path:
        return self.root / "content" / "comfyui" / "models"

    @property
    def llama_models(self) -> Path:
        return self.root / "content" / "llama-cpp" / "models"

    @property
    def dwarfstar_models(self) -> Path:
        return self.root / "content" / "dwarfstar" / "models"

    @property
    def content_verification(self) -> Path:
        return self.root / "content" / ".rocmplete" / "verification.json"

    @property
    def staging(self) -> Path:
        return self.root / "staging"

    def staging_for(self, name: str) -> Path:
        return self.staging / name

    @property
    def imported_workflows(self) -> Path:
        return (
            self.comfyui
            / "user"
            / "default"
            / "workflows"
            / "imported"
        )

    @property
    def curated_workflows(self) -> Path:
        return (
            self.comfyui
            / "user"
            / "default"
            / "workflows"
            / "curated"
        )

    @property
    def benchmarks(self) -> Path:
        return self.comfyui / "benchmarks"

    @property
    def llama_benchmarks(self) -> Path:
        return self.application("llama-cpp") / "benchmarks"

    @property
    def pi_runtime(self) -> Path:
        return self.application("pi") / "runtime"

    @property
    def agent_evaluations(self) -> Path:
        return self.application("agent-evaluation")

    @property
    def acceptance(self) -> Path:
        return self.application("acceptance")

    @property
    def acceptance_results(self) -> Path:
        return self.acceptance / "results"

    def prepare_runtime(self, application: str) -> None:
        app = self.application(application)
        app.mkdir(parents=True, exist_ok=True)
        if application == "comfyui":
            self.comfy_models.mkdir(parents=True, exist_ok=True)
        if application == "llama-cpp":
            self.llama_models.mkdir(parents=True, exist_ok=True)
        if application == "dwarfstar":
            self.dwarfstar_models.mkdir(parents=True, exist_ok=True)

    def prepare_downloads(self) -> None:
        (self.staging / ".home").mkdir(parents=True, exist_ok=True)
        (self.staging / ".cache" / "huggingface").mkdir(
            parents=True, exist_ok=True
        )
