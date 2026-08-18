import io
import json
import os
import stat
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch

from rocmplete.agent_models import RECOMMENDED_MODEL
from rocmplete.bundles import artifact_path
from rocmplete.catalog import load_catalog
from rocmplete.cli import command_maki, main
from rocmplete.cli_parser import parse_arguments
from rocmplete.config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from rocmplete.content_verification import VerificationStore
from rocmplete.errors import LauncherError
from rocmplete.maki_agent import (
    WRAPPER_PATH,
    create_launch_plan,
    create_sandbox_plan,
    launch_environment,
    prepare_state,
    sandbox_paths,
)


class MakiLauncherTests(unittest.TestCase):
    default_model = RECOMMENDED_MODEL

    def setUp(self):
        self.catalog = load_catalog()

    def _mark_bundle_installed(self, data_dir, bundle_identifier):
        bundle = self.catalog.bundle(bundle_identifier)
        store = VerificationStore.load(data_dir)
        for artifact in self.catalog.bundle_artifacts(bundle):
            path = artifact_path(data_dir, artifact)
            path.parent.mkdir(parents=True, exist_ok=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            store.record(path, artifact.size, artifact.sha256)
        store.save()

    def _mark_installed(self, data_dir, identifier):
        preset = self.catalog.llama_preset(identifier)
        self._mark_bundle_installed(data_dir, preset.bundle)

    def _fake_maki(self, root):
        executable = root / "maki"
        executable.write_text("#!/bin/sh\nexit 0\n")
        executable.chmod(0o755)
        return executable

    def _plan(self, root, arguments=()):
        data_dir = root / "data"
        binary_dir = root / "bin"
        binary_dir.mkdir()
        executable = self._fake_maki(binary_dir)
        self._mark_installed(data_dir, self.default_model)
        plan = create_launch_plan(
            self.catalog,
            data_dir,
            9090,
            arguments,
            {"PATH": str(binary_dir)},
            dwarfstar_port=8001,
        )
        return data_dir, executable, plan

    def test_provider_scripts_publish_exact_catalog_and_endpoints(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir, _, plan = self._plan(root)
            paths = sandbox_paths(data_dir)
            prepare_state(plan, paths, data_dir)

            provider = paths.config / "maki" / "providers" / "rocmplete"
            info = json.loads(
                subprocess.check_output([provider, "info"], text=True)
            )
            models = json.loads(
                subprocess.check_output([provider, "models"], text=True)
            )
            resolved = json.loads(
                subprocess.check_output([provider, "resolve"], text=True)
            )
            self.assertEqual(
                info,
                {
                    "display_name": "ROCmplete llama.cpp",
                    "base": "llama-cpp",
                    "has_auth": False,
                },
            )
            self.assertEqual(len(models), 13)
            by_id = {model["id"]: model for model in models}
            self.assertNotIn("qwen3-0.6b-q8-0", by_id)
            self.assertIn("qwen3.6-35b-a3b-ud-q8-k-xl", by_id)
            self.assertIn("qwen3.6-35b-a3b-mtp-ud-q8-k-xl", by_id)
            self.assertIn("qwen3.8-27b-mtp-ud-q8-k-xl", by_id)
            self.assertIn("qwen3.8-27b-mtp-ud-q4-k-xl", by_id)
            self.assertEqual(
                by_id["qwen3.8-27b-mtp-ud-q4-k-xl"]["context_window"],
                131072,
            )
            self.assertEqual(
                by_id[self.default_model]["context_window"], 262144
            )
            self.assertEqual(
                by_id[self.default_model]["max_output_tokens"], 16384
            )
            self.assertTrue(
                by_id[self.default_model]["supports_thinking"]
            )
            muse = by_id[
                "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
            ]
            self.assertEqual(muse["context_window"], 262144)
            self.assertTrue(muse["supports_thinking"])
            self.assertTrue(
                by_id["qwen3.8-27b-mtp-ud-q8-k-xl"]["supports_thinking"]
            )
            self.assertFalse(
                by_id["kat-coder-v2.5-dev-q8-0"]["supports_thinking"]
            )
            self.assertEqual(
                resolved,
                {"base_url": "http://127.0.0.1:9090/v1", "headers": {}},
            )

            dwarfstar = paths.config / "maki" / "providers" / "dwarfstar"
            model = json.loads(
                subprocess.check_output([dwarfstar, "models"], text=True)
            )[0]
            self.assertEqual(
                model["id"], "deepseek-v4-flash-0731-q2-imatrix"
            )
            self.assertEqual(
                model["context_window"], DWARFSTAR_DEFAULT_CONTEXT
            )
            self.assertEqual(
                model["max_output_tokens"],
                DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
            )
            self.assertFalse(model["supports_thinking"])

    def test_launch_preserves_maki_arguments_and_selects_installed_default(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir, executable, plan = self._plan(
                Path(directory),
                ("--", "-p", "ping", "--max-turns", "2"),
            )
            self.assertTrue(data_dir.is_dir())
            self.assertEqual(
                plan.command,
                (str(executable), "-p", "ping", "--max-turns", "2"),
            )
            self.assertEqual(plan.mode, "session")
            self.assertEqual(plan.default_provider, "rocmplete")
            self.assertEqual(plan.default_model, self.default_model)
            self.assertEqual(plan.default_thinking, "medium")
            self.assertIn(
                'default_model = "rocmplete/{}"'.format(self.default_model),
                plan.init_content.decode(),
            )
            self.assertIn(
                'always_thinking = "medium"', plan.init_content.decode()
            )
            self.assertIn(
                "max_concurrent = 1", plan.init_content.decode()
            )

    def test_launch_falls_back_to_dwarfstar(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_maki(binary_dir)
            self._mark_bundle_installed(
                root / "data",
                "dwarfstar-deepseek-v4-flash-0731-q2-imatrix",
            )
            plan = create_launch_plan(
                self.catalog,
                root / "data",
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            self.assertEqual(plan.default_provider, "dwarfstar")
            self.assertEqual(
                plan.default_model,
                "deepseek-v4-flash-0731-q2-imatrix",
            )
            self.assertEqual(plan.default_thinking, "high")

    def test_session_requires_a_maintained_installed_model(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_maki(binary_dir)
            with self.assertRaisesRegex(
                LauncherError, "no installed model is maintained for Maki"
            ):
                create_launch_plan(
                    self.catalog,
                    root / "data",
                    8080,
                    (),
                    {"PATH": str(binary_dir)},
                )

    def test_management_and_passthrough_commands_need_no_model(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_maki(binary_dir)
            cases = (
                (("models",), "management"),
                (("index", "file.py"), "management"),
                (("prompt", "tools"), "management"),
                (("auth", "status"), "management"),
                (("--help",), "passthrough"),
                (("-V",), "passthrough"),
                (("update",), "passthrough"),
                (("rollback",), "passthrough"),
                (("migrate", "xdg"), "passthrough"),
            )
            for arguments, mode in cases:
                with self.subTest(arguments=arguments):
                    plan = create_launch_plan(
                        self.catalog,
                        root / "empty-data",
                        8080,
                        arguments,
                        {"PATH": str(binary_dir)},
                    )
                    self.assertEqual(
                        plan.command, (str(executable), *arguments)
                    )
                    self.assertEqual(plan.mode, mode)
                    self.assertIsNone(plan.default_model)
                    self.assertIn(
                        'default_model = "rocmplete/{}"'.format(
                            self.default_model
                        ),
                        plan.init_content.decode(),
                    )
                    self.assertIn(
                        'always_thinking = "medium"',
                        plan.init_content.decode(),
                    )

    def test_prepare_state_refreshes_managed_files_but_preserves_tiers(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir, _, plan = self._plan(root)
            paths = sandbox_paths(data_dir)
            prepare_state(plan, paths, data_dir)
            config = paths.config / "maki" / "init.lua"
            provider = paths.config / "maki" / "providers" / "rocmplete"
            tiers = paths.state / "maki" / "model-tiers"
            self.assertEqual(stat.S_IMODE(config.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(provider.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE(tiers.stat().st_mode), 0o600)
            self.assertEqual(
                set(json.loads(tiers.read_text())),
                {"compaction", "weak", "medium", "strong"},
            )

            tiers.write_text('{"strong":"rocmplete/custom"}\n')
            config.write_text("stale\n")
            prepare_state(plan, paths, data_dir)
            self.assertEqual(
                tiers.read_text(),
                '{"strong":"rocmplete/custom"}\n',
            )
            self.assertEqual(config.read_bytes(), plan.init_content)

            tiers.write_bytes(plan.tier_content)
            seed = paths.state / "maki" / "rocmplete-tier-seed"
            seed.write_bytes(plan.tier_content)
            changed = replace(
                plan,
                tier_content=b'{"strong":"rocmplete/new"}\n',
            )
            prepare_state(changed, paths, data_dir)
            self.assertEqual(tiers.read_bytes(), changed.tier_content)
            self.assertEqual(seed.read_bytes(), changed.tier_content)

    def test_prepare_state_refuses_symlinked_managed_provider(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir, _, plan = self._plan(root)
            paths = sandbox_paths(data_dir)
            prepare_state(plan, paths, data_dir)
            provider = paths.config / "maki" / "providers" / "rocmplete"
            provider.unlink()
            outside = root / "outside"
            outside.write_text("safe")
            provider.symlink_to(outside)
            with self.assertRaisesRegex(
                LauncherError, "not a private regular file"
            ):
                prepare_state(plan, paths, data_dir)
            self.assertEqual(outside.read_text(), "safe")

    def test_prepare_state_refuses_unmanaged_provider(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir, _, plan = self._plan(root)
            paths = sandbox_paths(data_dir)
            prepare_state(plan, paths, data_dir)
            unexpected = paths.config / "maki" / "providers" / "surprise"
            unexpected.write_text("#!/bin/sh\n")
            with self.assertRaisesRegex(
                LauncherError, "contains unmanaged entries: surprise"
            ):
                prepare_state(plan, paths, data_dir)

    def test_sandbox_keeps_state_private_and_preserves_exact_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir, executable, plan = self._plan(
                root, ("-p", "ping", "--max-turns", "2")
            )
            binary_dir = root / "bin"
            workdir = root / "project"
            workdir.mkdir()
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            paths = sandbox_paths(data_dir)
            prepare_state(plan, paths, data_dir)
            sandbox = create_sandbox_plan(
                plan,
                data_dir,
                workdir,
                {
                    "PATH": str(binary_dir),
                    "TERM": "xterm-256color",
                    "HF_TOKEN": "must-not-leak",
                },
            )
            command = list(sandbox.command)
            self.assertEqual(command[0], str(bwrap.resolve()))
            self.assertIn("--clearenv", command)
            self.assertIn("--share-net", command)
            self.assertNotIn("must-not-leak", command)
            self.assertEqual(
                command[-5:],
                [str(executable.resolve()), "-p", "ping", "--max-turns", "2"],
            )
            self.assertEqual(sandbox.state_root, paths.root)

    def test_no_sandbox_environment_is_private_and_rejects_legacy_state(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = sandbox_paths(root / "data")
            child = launch_environment(
                paths,
                {"HOME": str(root / "home"), "TOKEN": "kept"},
            )
            self.assertEqual(child["XDG_CONFIG_HOME"], str(paths.config))
            self.assertEqual(child["XDG_STATE_HOME"], str(paths.state))
            self.assertEqual(child["TOKEN"], "kept")

            legacy = root / "home" / ".maki"
            legacy.mkdir(parents=True)
            with self.assertRaisesRegex(LauncherError, "maki migrate xdg"):
                launch_environment(paths, {"HOME": str(root / "home")})

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_executes_with_private_state_without_sandbox(self, execute):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir, executable, _ = self._plan(root)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "maki",
                    "--data-dir",
                    str(data_dir),
                    "--no-sandbox",
                    "--",
                    "-p",
                    "ping",
                ]
            )
            with patch.dict(
                os.environ,
                {
                    "PATH": str(root / "bin"),
                    "HOME": str(root / "home"),
                    "ROCMLETE_MAKI_PORT": "9090",
                    "ROCMLETE_MAKI_DWARFSTAR_PORT": "8001",
                },
                clear=True,
            ):
                self.assertEqual(command_maki(arguments, self.catalog), 0)
            command, argv, child = execute.call_args.args
            self.assertEqual(command, str(executable))
            self.assertEqual(argv[-2:], ["-p", "ping"])
            config = Path(child["XDG_CONFIG_HOME"]) / "maki" / "init.lua"
            self.assertTrue(config.is_file())
            provider = (
                Path(child["XDG_CONFIG_HOME"])
                / "maki"
                / "providers"
                / "rocmplete"
            )
            resolved = json.loads(
                subprocess.check_output([provider, "resolve"], text=True)
            )
            self.assertEqual(
                resolved["base_url"], "http://127.0.0.1:9090/v1"
            )

    def test_parser_and_wrapper_expose_maki_launcher(self):
        _, arguments = parse_arguments(
            [
                "agent",
                "maki",
                "--port",
                "9090",
                "--dwarfstar-port",
                "8001",
                "--",
                "-m",
                "rocmplete/model",
            ]
        )
        self.assertEqual(arguments.port, "9090")
        self.assertEqual(arguments.dwarfstar_port, "8001")
        self.assertTrue(arguments.sandbox)
        self.assertEqual(
            arguments.maki_arguments,
            ["--", "-m", "rocmplete/model"],
        )
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaisesRegex(SystemExit, "0"):
                main(["--help"])
        self.assertNotIn("    maki", output.getvalue())
        self.assertTrue(stat.S_IMODE(WRAPPER_PATH.stat().st_mode) & 0o111)
        self.assertIn(
            '[str(launcher), "agent", "maki", "--", *sys.argv[1:]]',
            WRAPPER_PATH.read_text(),
        )


if __name__ == "__main__":
    unittest.main()
