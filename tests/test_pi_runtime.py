import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from rocmplete.errors import LauncherError
from rocmplete.pi_runtime import (
    PI_PACKAGE,
    install_pi_runtime,
    load_runtime_source,
    resolve_pi_runtime,
    runtime_root,
)


class PiRuntimeTests(unittest.TestCase):
    def test_repository_runtime_lock_is_complete(self):
        source = load_runtime_source()
        self.assertEqual(source.package_version, "0.84.2")
        self.assertEqual(source.minimum_node, (22, 19, 0))

    def _source(self, root, version="1.2.3", node=">=22.19.0"):
        source = root / "source"
        source.mkdir()
        package = {
            "name": "rocmplete-pi-runtime",
            "private": True,
            "version": "1.0.0",
            "dependencies": {PI_PACKAGE: version},
            "engines": {"node": node},
        }
        lock = {
            "name": "rocmplete-pi-runtime",
            "version": "1.0.0",
            "lockfileVersion": 3,
            "requires": True,
            "packages": {
                "": {
                    "dependencies": {PI_PACKAGE: version},
                    "engines": {"node": node},
                },
                "node_modules/{}".format(PI_PACKAGE): {
                    "version": version,
                    "resolved": (
                        "https://registry.npmjs.org/test/-/test-{}.tgz".format(
                            version
                        )
                    ),
                    "integrity": "sha512-test",
                    "license": "MIT",
                },
            },
        }
        (source / "package.json").write_text(
            json.dumps(package) + "\n"
        )
        (source / "package-lock.json").write_text(
            json.dumps(lock) + "\n"
        )
        return source

    def _system_bin(self, root):
        binary = root / "system-bin"
        binary.mkdir()
        for name in ("node", "npm"):
            path = binary / name
            path.write_text("#!/bin/sh\nexit 0\n")
            path.chmod(0o755)
        return binary

    def _runner(self, version, calls, *, npm_returncode=0):
        def run(command, **kwargs):
            calls.append((tuple(command), kwargs))
            if command[1:] == ["--version"]:
                return subprocess.CompletedProcess(
                    command, 0, stdout="v24.1.0\n", stderr=""
                )
            if command[0].endswith("npm"):
                if npm_returncode:
                    return subprocess.CompletedProcess(
                        command, npm_returncode
                    )
                prefix = Path(command[command.index("--prefix") + 1])
                package = prefix / "node_modules" / PI_PACKAGE
                entrypoint = package / "dist" / "cli.js"
                entrypoint.parent.mkdir(parents=True)
                (package / "package.json").write_text(
                    json.dumps(
                        {
                            "name": PI_PACKAGE,
                            "version": version,
                            "bin": {"pi": "dist/cli.js"},
                        }
                    )
                    + "\n"
                )
                entrypoint.write_text("export {};\n")
                return subprocess.CompletedProcess(command, 0)
            if command[-1:] == ["--version"]:
                return subprocess.CompletedProcess(
                    command, 0, stdout=version + "\n", stderr=""
                )
            raise AssertionError("unexpected command: {!r}".format(command))

        return run

    def test_source_requires_an_exact_locked_pi_and_node_minimum(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = self._source(root)
            loaded = load_runtime_source(source)
            self.assertEqual(loaded.package_version, "1.2.3")
            self.assertEqual(loaded.minimum_node, (22, 19, 0))
            self.assertRegex(loaded.lock_sha256, r"^[0-9a-f]{64}$")

            package_path = source / "package.json"
            package = json.loads(package_path.read_text())
            package["dependencies"][PI_PACKAGE] = "^1.2.3"
            package_path.write_text(json.dumps(package) + "\n")
            with self.assertRaisesRegex(
                LauncherError, "exact release"
            ):
                load_runtime_source(source)

    def test_install_is_atomic_verified_and_idempotent(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            data_dir.mkdir()
            source = self._source(root)
            system_bin = self._system_bin(root)
            calls = []
            runner = self._runner("1.2.3", calls)

            result = install_pi_runtime(
                data_dir,
                {},
                source_dir=source,
                system_path=str(system_bin),
                runner=runner,
            )
            self.assertTrue(result.installed)
            self.assertEqual(result.runtime.package_version, "1.2.3")
            self.assertEqual(result.runtime.node_version, "24.1.0")
            self.assertTrue(result.runtime.entrypoint.is_file())
            receipt = json.loads(
                (result.runtime.root / "receipt.json").read_text()
            )
            self.assertEqual(receipt["lock_sha256"], result.runtime.lock_sha256)
            npm = [call for call, _ in calls if call[0].endswith("npm")]
            self.assertEqual(len(npm), 1)
            self.assertIn("--ignore-scripts", npm[0])
            self.assertIn("--omit=dev", npm[0])
            self.assertEqual(
                list((runtime_root(data_dir) / "installations").glob(".install-*")),
                [],
            )

            calls.clear()
            repeated = install_pi_runtime(
                data_dir,
                {},
                source_dir=source,
                system_path=str(system_bin),
                runner=runner,
            )
            self.assertFalse(repeated.installed)
            self.assertFalse(
                any(command[0].endswith("npm") for command, _ in calls)
            )
            resolved = resolve_pi_runtime(
                data_dir,
                {},
                source_dir=source,
                system_path=str(system_bin),
                runner=runner,
            )
            self.assertEqual(resolved.root, result.runtime.root)

    def test_failed_install_removes_staging_without_looking_installed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            data_dir.mkdir()
            source = self._source(root)
            system_bin = self._system_bin(root)
            runner = self._runner("1.2.3", [], npm_returncode=7)

            with self.assertRaisesRegex(LauncherError, "npm failed"):
                install_pi_runtime(
                    data_dir,
                    {},
                    source_dir=source,
                    system_path=str(system_bin),
                    runner=runner,
                )
            installations = runtime_root(data_dir) / "installations"
            self.assertEqual(list(installations.iterdir()), [])
            with self.assertRaisesRegex(LauncherError, "is not installed"):
                resolve_pi_runtime(
                    data_dir,
                    {},
                    source_dir=source,
                    system_path=str(system_bin),
                    runner=runner,
                )

    def test_install_rejects_an_old_system_node(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            data_dir.mkdir()
            source = self._source(root)
            system_bin = self._system_bin(root)

            def runner(command, **kwargs):
                return subprocess.CompletedProcess(
                    command, 0, stdout="v22.18.0\n", stderr=""
                )

            with self.assertRaisesRegex(
                LauncherError, "requires Node.js >= 22.19.0"
            ):
                install_pi_runtime(
                    data_dir,
                    {},
                    source_dir=source,
                    system_path=str(system_bin),
                    runner=runner,
                )
            self.assertFalse(runtime_root(data_dir).exists())

    def test_resolve_refuses_a_symlinked_installation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            data_dir.mkdir()
            source = load_runtime_source(self._source(root))
            system_bin = self._system_bin(root)
            installation = (
                runtime_root(data_dir)
                / "installations"
                / source.lock_sha256
            )
            installation.parent.mkdir(parents=True)
            outside = root / "outside"
            outside.mkdir()
            installation.symlink_to(outside, target_is_directory=True)

            with self.assertRaisesRegex(
                LauncherError, "not a real directory"
            ):
                resolve_pi_runtime(
                    data_dir,
                    {},
                    source_dir=source.root,
                    system_path=str(system_bin),
                    runner=self._runner("1.2.3", []),
                )


if __name__ == "__main__":
    unittest.main()
