import io
import json
import os
import stat
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

from rocmplete.agent_models import (
    RECOMMENDED_MODEL,
    agent_client_sampling_parameters,
)
from rocmplete.bundles import artifact_path
from rocmplete.catalog import load_catalog
from rocmplete.cli import command_omp, main
from rocmplete.cli_parser import parse_arguments
from rocmplete.config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from rocmplete.content_verification import VerificationStore
from rocmplete.errors import LauncherError
from rocmplete.omp_agent import (
    SANDBOX_AGENT_DIR,
    SANDBOX_OVERLAY_PATH,
    WRAPPER_PATH,
    create_launch_plan,
    create_sandbox_plan,
    launch_environment,
    prepare_state,
    render_models,
    render_overlay,
    sandbox_paths,
)


class OmpLauncherTests(unittest.TestCase):
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

    def _fake_omp(self, root):
        executable = root / "omp"
        executable.write_text("#!/bin/sh\nexit 0\n")
        executable.chmod(0o755)
        return executable

    def test_models_use_llama_discovery_and_exact_model_policy(self):
        config = json.loads(
            render_models(
                self.catalog,
                "http://127.0.0.1:9090/v1",
                "http://127.0.0.1:8001/v1",
            )
        )
        provider = config["providers"]["rocmplete-llama-cpp"]
        self.assertEqual(provider["api"], "openai-completions")
        self.assertEqual(provider["auth"], "none")
        self.assertEqual(provider["discovery"], {"type": "llama.cpp"})
        self.assertEqual(provider["baseUrl"], "http://127.0.0.1:9090/v1")
        models = {model["id"]: model for model in provider["models"]}
        self.assertEqual(len(models), 13)
        self.assertNotIn("qwen3-0.6b-q8-0", models)
        self.assertNotIn("translategemma-27b-it-q8-0", models)
        self.assertIn("qwen3.6-35b-a3b-ud-q8-k-xl", models)
        self.assertIn("qwen3.6-35b-a3b-mtp-ud-q8-k-xl", models)
        self.assertIn("qwen3.8-27b-mtp-ud-q8-k-xl", models)
        self.assertIn("qwen3.8-27b-mtp-ud-q4-k-xl", models)
        self.assertEqual(
            models["qwen3.8-27b-mtp-ud-q4-k-xl"]["contextWindow"], 131072
        )
        qwen = models["qwen3.6-27b-mtp-q8-0"]
        self.assertEqual(qwen["contextWindow"], 262144)
        self.assertEqual(qwen["maxTokens"], 16384)
        self.assertTrue(qwen["supportsTools"])
        self.assertEqual(
            qwen["thinking"],
            {
                "mode": "effort",
                "efforts": ["high"],
                "defaultLevel": "high",
            },
        )
        self.assertEqual(qwen["compat"]["thinkingFormat"], "qwen-chat-template")
        self.assertFalse(qwen["compat"]["supportsReasoningEffort"])
        qwen38 = models["qwen3.8-27b-mtp-ud-q8-k-xl"]
        self.assertEqual(
            qwen38["thinking"],
            {
                "mode": "effort",
                "efforts": ["low", "medium", "xhigh"],
                "defaultLevel": "medium",
            },
        )
        self.assertEqual(qwen38["compat"]["thinkingFormat"], "openai")
        self.assertTrue(qwen38["compat"]["supportsReasoningEffort"])
        muse = models[
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
        ]
        self.assertEqual(muse["contextWindow"], 262144)
        self.assertTrue(muse["reasoning"])
        self.assertEqual(
            muse["thinking"],
            {
                "mode": "effort",
                "efforts": ["low", "medium", "high", "xhigh"],
                "defaultLevel": "high",
            },
        )
        self.assertEqual(muse["compat"]["thinkingFormat"], "openai")
        self.assertTrue(muse["compat"]["supportsReasoningEffort"])
        for identifier, model in models.items():
            expected_sampling = agent_client_sampling_parameters(
                self.catalog, identifier
            )
            if expected_sampling:
                self.assertEqual(
                    model["compat"]["extraBody"], expected_sampling
                )
            else:
                self.assertNotIn("extraBody", model["compat"])
            self.assertFalse(model["compat"]["supportsDeveloperRole"])

        dwarfstar = config["providers"]["rocmplete-dwarfstar"]
        self.assertEqual(
            dwarfstar["baseUrl"], "http://127.0.0.1:8001/v1"
        )
        model = dwarfstar["models"][0]
        self.assertEqual(model["contextWindow"], DWARFSTAR_DEFAULT_CONTEXT)
        self.assertEqual(
            model["maxTokens"], DWARFSTAR_DEFAULT_OUTPUT_TOKENS
        )
        self.assertEqual(model["thinking"]["efforts"], ["high"])
        self.assertTrue(model["supportsTools"])

    def test_overlay_keeps_every_role_and_model_local(self):
        overlay = json.loads(
            render_overlay(
                self.catalog, "rocmplete-llama-cpp", self.default_model
            )
        )
        reference = "rocmplete-llama-cpp/{}".format(self.default_model)
        self.assertEqual(
            overlay["modelRoles"],
            {
                "default": reference,
                "smol": "@default",
                "slow": "@default",
                "vision": "@default",
                "plan": "@default",
                "designer": "@default",
                "commit": "@default",
                "tiny": "@default",
                "task": "@default",
                "advisor": "@default",
            },
        )
        self.assertEqual(len(overlay["enabledModels"]), 14)
        self.assertIn(
            "rocmplete-dwarfstar/deepseek-v4-flash-0731-q2-imatrix",
            overlay["enabledModels"],
        )
        self.assertEqual(overlay["disabledProviders"], ["llama.cpp"])
        self.assertEqual(overlay["tools"]["approvalMode"], "yolo")
        self.assertFalse(overlay["startup"]["checkUpdate"])
        self.assertFalse(overlay["startup"]["setupWizard"])
        self.assertEqual(overlay["marketplace"]["autoUpdate"], "off")

    def test_launch_selects_installed_model_and_forwards_overrides_last(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_omp(binary_dir)
            self._mark_installed(data_dir, self.default_model)

            plan = create_launch_plan(
                self.catalog,
                data_dir,
                9090,
                ("--", "--model", "other", "--thinking", "high"),
                {"PATH": "{}:{}".format(WRAPPER_PATH.parent, binary_dir)},
                dwarfstar_port=8001,
            )

            self.assertEqual(plan.command[0], str(executable))
            self.assertEqual(plan.mode, "session")
            self.assertEqual(plan.default_provider, "rocmplete-llama-cpp")
            self.assertEqual(plan.default_model, self.default_model)
            self.assertEqual(plan.default_thinking, "medium")
            self.assertEqual(plan.endpoint, "http://127.0.0.1:9090/v1")
            self.assertEqual(
                plan.dwarfstar_endpoint, "http://127.0.0.1:8001/v1"
            )
            self.assertEqual(
                plan.command[-4:],
                ("--model", "other", "--thinking", "high"),
            )

    def test_launch_falls_back_to_dwarfstar(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_omp(binary_dir)
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
            self.assertEqual(plan.default_provider, "rocmplete-dwarfstar")
            self.assertEqual(
                plan.default_model,
                "deepseek-v4-flash-0731-q2-imatrix",
            )
            self.assertEqual(plan.default_thinking, "high")

    def test_launch_requires_a_maintained_installed_model(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_omp(binary_dir)
            with self.assertRaisesRegex(
                LauncherError, "no installed model is maintained for OMP"
            ):
                create_launch_plan(
                    self.catalog,
                    root / "data",
                    8080,
                    (),
                    {"PATH": str(binary_dir)},
                )

    def test_commands_do_not_require_an_installed_model(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_omp(binary_dir)
            for arguments, expected_mode in (
                (("models", "rocmplete"), "management"),
                (("config", "get", "tools.approvalMode"), "management"),
                (("plugin", "list"), "management"),
                (("acp",), "management"),
                (("--help",), "passthrough"),
                (("--version",), "passthrough"),
                (("completions", "zsh"), "passthrough"),
                (("update",), "passthrough"),
            ):
                with self.subTest(arguments=arguments):
                    plan = create_launch_plan(
                        self.catalog,
                        root / "empty-data",
                        8080,
                        ("--", *arguments),
                        {"PATH": str(binary_dir)},
                    )
                    self.assertEqual(
                        plan.command, (str(executable), *arguments)
                    )
                    self.assertEqual(plan.mode, expected_mode)
                    self.assertIsNone(plan.default_model)

    def test_launch_rejects_omp_profiles_and_ignores_wrapper(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_omp(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            for argument in ("--profile", "--profile=work", "--alias=test"):
                with self.subTest(argument=argument):
                    with self.assertRaisesRegex(
                        LauncherError, "profiles cannot be combined"
                    ):
                        create_launch_plan(
                            self.catalog,
                            data_dir,
                            8080,
                            (argument,),
                            {"PATH": str(binary_dir)},
                        )

            with self.assertRaisesRegex(
                LauncherError, "executable not found outside"
            ):
                create_launch_plan(
                    self.catalog,
                    data_dir,
                    8080,
                    (),
                    {"PATH": str(WRAPPER_PATH.parent)},
                )

    def test_prepare_state_writes_only_private_owned_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_omp(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            paths = sandbox_paths(data_dir)
            agent_dir = prepare_state(plan, paths, data_dir)
            models = agent_dir / "models.yml"
            overlay = agent_dir / "rocmplete.json"
            user_config = agent_dir / "config.yml"
            user_config.write_text("user: true\n")
            self.assertEqual(models.read_bytes(), plan.models_content)
            self.assertEqual(overlay.read_bytes(), plan.overlay_content)
            self.assertEqual(stat.S_IMODE(models.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(overlay.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(agent_dir.stat().st_mode), 0o700)
            self.assertEqual(prepare_state(plan, paths, data_dir), agent_dir)
            self.assertEqual(user_config.read_text(), "user: true\n")

            models.unlink()
            target = root / "outside.yml"
            target.write_bytes(plan.models_content)
            models.symlink_to(target)
            with self.assertRaisesRegex(
                LauncherError, "not a private regular file"
            ):
                prepare_state(plan, paths, data_dir)

    def test_environment_replaces_profiles_and_host_state(self):
        paths = sandbox_paths(Path("/private/data"))
        child = launch_environment(
            paths,
            {
                "PATH": "/bin",
                "OMP_PROFILE": "work",
                "PI_PROFILE": "work",
                "PI_CODING_AGENT_DIR": "/host/omp",
                "PI_CONFIG_FILES": "/host/config.yml",
                "TOKEN": "kept-without-sandbox",
            },
        )
        self.assertNotIn("OMP_PROFILE", child)
        self.assertNotIn("PI_PROFILE", child)
        self.assertEqual(
            child["PI_CODING_AGENT_DIR"],
            str(paths.data / "omp" / "agent"),
        )
        self.assertEqual(
            child["PI_CONFIG_FILES"],
            str(paths.data / "omp" / "agent" / "rocmplete.json"),
        )
        self.assertEqual(child["XDG_CONFIG_HOME"], str(paths.config))
        self.assertEqual(child["TOKEN"], "kept-without-sandbox")

    def test_sandbox_keeps_omp_state_private_and_hides_host_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project"
            binary_dir.mkdir()
            workdir.mkdir()
            executable = self._fake_omp(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                ("--print", "ping"),
                {"PATH": str(binary_dir)},
            )
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
            self.assertIn(str(SANDBOX_AGENT_DIR), command)
            self.assertIn(str(SANDBOX_OVERLAY_PATH), command)
            self.assertEqual(command[-2:], ["--print", "ping"])
            self.assertEqual(sandbox.environment, {"PATH": str(binary_dir)})
            self.assertIn(str(executable.resolve()), command)

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_executes_omp_with_private_state_without_sandbox(
        self, execute
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_omp(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "omp",
                    "--data-dir",
                    str(data_dir),
                    "--no-sandbox",
                    "--",
                    "--no-session",
                ]
            )
            with patch.dict(
                os.environ,
                {
                    "PATH": str(binary_dir),
                    "ROCMLETE_OMP_PORT": "9090",
                    "ROCMLETE_OMP_DWARFSTAR_PORT": "8001",
                    "OMP_PROFILE": "host",
                },
                clear=True,
            ):
                self.assertEqual(command_omp(arguments, self.catalog), 0)

            command, argv, child = execute.call_args.args
            self.assertEqual(command, str(executable))
            self.assertEqual(argv[-1], "--no-session")
            self.assertNotIn("OMP_PROFILE", child)
            models = Path(child["PI_CODING_AGENT_DIR"]) / "models.yml"
            config = json.loads(models.read_text())
            self.assertEqual(
                config["providers"]["rocmplete-llama-cpp"]["baseUrl"],
                "http://127.0.0.1:9090/v1",
            )
            self.assertEqual(
                config["providers"]["rocmplete-dwarfstar"]["baseUrl"],
                "http://127.0.0.1:8001/v1",
            )

    def test_parser_help_and_wrapper_expose_omp_launcher(self):
        _, arguments = parse_arguments(
            [
                "agent",
                "omp",
                "--port",
                "9090",
                "--dwarfstar-port",
                "8001",
                "--",
                "--model",
                "other",
            ]
        )
        self.assertEqual(arguments.port, "9090")
        self.assertEqual(arguments.dwarfstar_port, "8001")
        self.assertTrue(arguments.sandbox)
        self.assertEqual(arguments.omp_arguments, ["--", "--model", "other"])
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaisesRegex(SystemExit, "0"):
                main(["agent", "--help"])
        self.assertIn("omp", output.getvalue())

        _, arguments = parse_arguments(
            ["agent", "omp", "--no-sandbox", "--"]
        )
        self.assertFalse(arguments.sandbox)
        self.assertTrue(stat.S_IMODE(WRAPPER_PATH.stat().st_mode) & 0o111)
        self.assertIn(
            '[str(launcher), "agent", "omp", "--", *sys.argv[1:]]',
            WRAPPER_PATH.read_text(),
        )


if __name__ == "__main__":
    unittest.main()
