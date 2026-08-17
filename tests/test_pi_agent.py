import io
import json
import os
import stat
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

from rocmplete.agent_models import agent_client_sampling_parameters
from rocmplete.bundles import artifact_path
from rocmplete.catalog import load_catalog
from rocmplete.cli import command_pi, main
from rocmplete.cli_parser import parse_arguments
from rocmplete.config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from rocmplete.content_verification import VerificationStore
from rocmplete.errors import LauncherError
from rocmplete.pi_agent import (
    RECOMMENDED_MODEL,
    SANDBOX_AGENT_DIR,
    WRAPPER_PATH,
    create_launch_plan,
    create_sandbox_plan,
    launch_environment,
    prepare_state,
    render_config,
    sandbox_paths,
)


class PiLauncherTests(unittest.TestCase):
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

    def _fake_pi(self, root):
        executable = root / "pi"
        executable.write_text("#!/bin/sh\nexit 0\n")
        executable.chmod(0o755)
        return executable

    def test_config_uses_chat_completions_and_exact_model_limits(self):
        config = json.loads(
            render_config(
                self.catalog,
                "http://127.0.0.1:9090/v1",
                "http://127.0.0.1:8001/v1",
            )
        )
        provider = config["providers"]["rocmplete"]
        self.assertEqual(provider["api"], "openai-completions")
        self.assertEqual(provider["baseUrl"], "http://127.0.0.1:9090/v1")
        self.assertEqual(provider["apiKey"], "rocmplete-local")
        self.assertFalse(provider["authHeader"])
        self.assertEqual(
            provider["compat"],
            {
                "supportsDeveloperRole": False,
                "supportsReasoningEffort": True,
            },
        )
        models = {model["id"]: model for model in provider["models"]}
        self.assertEqual(len(models), 13)
        self.assertNotIn("qwen3-0.6b-q8-0", models)
        self.assertNotIn("translategemma-27b-it-q8-0", models)
        self.assertIn("qwen3.6-35b-a3b-ud-q8-k-xl", models)
        self.assertIn("qwen3.6-35b-a3b-mtp-ud-q8-k-xl", models)
        self.assertIn("qwen3.8-27b-mtp-ud-q8-k-xl", models)
        self.assertIn("qwen3.8-27b-mtp-ud-q4-k-xl", models)
        self.assertEqual(
            models["qwen3.8-27b-mtp-ud-q4-k-xl"]["contextWindow"], 65536
        )
        self.assertEqual(
            models[self.default_model]["contextWindow"], 262144
        )
        self.assertEqual(models[self.default_model]["maxTokens"], 16384)
        self.assertEqual(
            models[self.default_model]["thinkingLevelMap"],
            {
                "off": "none",
                "minimal": None,
                "low": "low",
                "medium": "medium",
                "high": None,
                "xhigh": "xhigh",
                "max": None,
            },
        )
        muse = models[
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
        ]
        self.assertEqual(muse["contextWindow"], 262144)
        self.assertTrue(muse["reasoning"])
        self.assertEqual(muse["thinkingLevelMap"]["xhigh"], "xhigh")
        self.assertIsNone(muse["thinkingLevelMap"]["off"])
        self.assertEqual(
            muse["compat"],
            {
                "thinkingFormat": "chat-template",
                "supportsReasoningEffort": False,
                "chatTemplateKwargs": {
                    "reasoning_strength": {"$var": "thinking.effort"},
                    "preserve_thinking": True,
                },
            },
        )
        qwen = models["qwen3.6-27b-mtp-q8-0"]
        self.assertEqual(qwen["thinkingLevelMap"]["off"], "none")
        self.assertEqual(qwen["thinkingLevelMap"]["high"], "high")
        self.assertIsNone(qwen["thinkingLevelMap"]["low"])
        self.assertIsNone(qwen["thinkingLevelMap"]["xhigh"])
        self.assertEqual(
            qwen["compat"],
            {
                "thinkingFormat": "qwen-chat-template",
                "supportsReasoningEffort": False,
            },
        )
        qwen38 = models["qwen3.8-27b-mtp-ud-q8-k-xl"]
        self.assertEqual(qwen38["thinkingLevelMap"]["off"], "none")
        self.assertEqual(qwen38["thinkingLevelMap"]["medium"], "medium")
        self.assertEqual(qwen38["thinkingLevelMap"]["xhigh"], "xhigh")
        self.assertIsNone(qwen38["thinkingLevelMap"]["high"])
        self.assertEqual(
            qwen38["compat"],
            {
                "thinkingFormat": "openai",
                "supportsReasoningEffort": True,
            },
        )
        for identifier, model in models.items():
            if identifier.startswith("qwen3.6-"):
                self.assertEqual(
                    model["compat"]["thinkingFormat"],
                    "qwen-chat-template",
                )
            if identifier.startswith("qwen3.8-"):
                self.assertEqual(
                    model["compat"]["thinkingFormat"], "openai"
                )
                self.assertEqual(model["thinkingLevelMap"]["off"], "none")
        self.assertFalse(models["kat-coder-v2.5-dev-q8-0"]["reasoning"])
        for identifier, model in models.items():
            expected_sampling = agent_client_sampling_parameters(
                self.catalog, identifier
            )
            if expected_sampling:
                self.assertEqual(model["samplingParams"], expected_sampling)
            else:
                self.assertNotIn("samplingParams", model)

        dwarfstar = config["providers"]["dwarfstar"]
        self.assertEqual(
            dwarfstar["baseUrl"], "http://127.0.0.1:8001/v1"
        )
        model = dwarfstar["models"][0]
        self.assertEqual(
            model["id"], "deepseek-v4-flash-0731-q2-imatrix"
        )
        self.assertEqual(model["contextWindow"], DWARFSTAR_DEFAULT_CONTEXT)
        self.assertEqual(
            model["maxTokens"], DWARFSTAR_DEFAULT_OUTPUT_TOKENS
        )
        self.assertEqual(
            model["thinkingLevelMap"],
            {
                "off": "none",
                "minimal": None,
                "low": None,
                "medium": None,
                "high": "high",
                "xhigh": None,
                "max": None,
            },
        )

    def test_launch_selects_installed_model_and_forwards_overrides_last(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_pi(binary_dir)
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
            self.assertIn("--offline", plan.command)
            self.assertIn("--no-approve", plan.command)
            self.assertNotIn("--no-extensions", plan.command)
            self.assertNotIn("--no-skills", plan.command)
            self.assertNotIn("--no-prompt-templates", plan.command)
            self.assertNotIn("--no-themes", plan.command)
            self.assertEqual(plan.mode, "session")
            self.assertEqual(plan.default_provider, "rocmplete")
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
            self._fake_pi(binary_dir)
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

    def test_launch_requires_a_maintained_installed_model(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_pi(binary_dir)
            with self.assertRaisesRegex(
                LauncherError, "no installed model is maintained for Pi"
            ):
                create_launch_plan(
                    self.catalog,
                    root / "data",
                    8080,
                    (),
                    {"PATH": str(binary_dir)},
                )

    def test_management_commands_do_not_require_a_model_or_session_flags(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_pi(binary_dir)

            for arguments, expected_mode in (
                (("install", "npm:pi-code-indexer"), "management"),
                (("remove", "npm:pi-code-indexer"), "management"),
                (("uninstall", "npm:pi-code-indexer"), "management"),
                (("update", "--extensions"), "management"),
                (("update", "npm:pi-code-indexer"), "management"),
                (("list",), "management"),
                (("config",), "management"),
                (("auth", "check"), "management"),
                (("--help",), "passthrough"),
                (("--version",), "passthrough"),
                (("update",), "passthrough"),
                (("update", "self"), "passthrough"),
                (("update", "pi"), "passthrough"),
                (("update", "--self"), "passthrough"),
                (("update", "--all"), "passthrough"),
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
                    self.assertIsNone(plan.default_provider)
                    self.assertIsNone(plan.default_model)
                    self.assertIsNone(plan.default_thinking)

    def test_launch_requires_real_pi_outside_wrapper_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "data"
            self._mark_installed(data_dir, self.default_model)
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

    def test_prepare_state_writes_private_atomic_model_config(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_pi(binary_dir)
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
            models = agent_dir / "models.json"
            self.assertEqual(models.read_bytes(), plan.config_content)
            self.assertEqual(stat.S_IMODE(models.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(agent_dir.stat().st_mode), 0o700)
            self.assertEqual(list(agent_dir.glob(".models.*.tmp")), [])

            first_inode = models.stat().st_ino
            self.assertEqual(prepare_state(plan, paths, data_dir), agent_dir)
            self.assertEqual(models.stat().st_ino, first_inode)

    def test_prepare_state_refuses_a_symlinked_model_config(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_pi(binary_dir)
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
            models = agent_dir / "models.json"
            models.unlink()
            target = root / "outside.json"
            target.write_bytes(plan.config_content)
            models.symlink_to(target)

            with self.assertRaisesRegex(
                LauncherError, "not a private regular file"
            ):
                prepare_state(plan, paths, data_dir)
            self.assertEqual(target.read_bytes(), plan.config_content)

    def test_sandbox_keeps_pi_state_private_and_hides_host_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project"
            binary_dir.mkdir()
            workdir.mkdir()
            executable = self._fake_pi(binary_dir)
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
            resolver = Path("/run/systemd/resolve/stub-resolv.conf")
            with patch(
                "rocmplete.agent_sandbox._runtime_resolver_target",
                return_value=resolver,
            ):
                sandbox = create_sandbox_plan(
                    plan,
                    data_dir,
                    workdir,
                    {
                        "PATH": str(binary_dir),
                        "TERM": "xterm-256color",
                        "HF_TOKEN": "must-not-leak",
                        "SSH_AUTH_SOCK": "/run/ssh-agent",
                    },
                )
            command = list(sandbox.command)
            self.assertEqual(command[0], str(bwrap.resolve()))
            self.assertIn("--clearenv", command)
            self.assertIn("--share-net", command)
            self.assertIn(str(paths.data), command)
            self.assertNotIn("must-not-leak", command)
            self.assertNotIn("/run/ssh-agent", command)
            self.assertIn(str(SANDBOX_AGENT_DIR), command)
            triples = [
                command[index : index + 3]
                for index in range(len(command) - 2)
            ]
            self.assertIn(
                ["--ro-bind", str(resolver), str(resolver)], triples
            )
            self.assertEqual(
                command[-4:],
                ["--thinking", "medium", "--print", "ping"],
            )
            self.assertEqual(sandbox.environment, {"PATH": str(binary_dir)})
            self.assertEqual(sandbox.state_root, paths.root)
            self.assertIn(str(executable.resolve()), command)

    def test_management_sandbox_keeps_command_first_and_allows_network(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project"
            data_dir.mkdir()
            binary_dir.mkdir()
            workdir.mkdir()
            executable = self._fake_pi(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                ("install", "npm:pi-code-indexer"),
                {"PATH": str(binary_dir)},
            )
            paths = sandbox_paths(data_dir)
            prepare_state(plan, paths, data_dir)
            sandbox = create_sandbox_plan(
                plan,
                data_dir,
                workdir,
                {"PATH": str(binary_dir)},
            )
            command = list(sandbox.command)
            self.assertEqual(
                command[-3:],
                [str(executable.resolve()), "install", "npm:pi-code-indexer"],
            )
            self.assertNotIn("PI_OFFLINE", command)
            self.assertIn(str(SANDBOX_AGENT_DIR), command)

    def test_sandbox_accepts_explicit_read_only_cache_and_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project"
            module_cache = root / "module-cache"
            binary_dir.mkdir()
            workdir.mkdir()
            module_cache.mkdir()
            executable = self._fake_pi(binary_dir)
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
            destination = Path("/run/rocmplete/modules")
            sandbox = create_sandbox_plan(
                plan,
                data_dir,
                workdir,
                {"PATH": str(binary_dir)},
                read_only_mounts=((module_cache, destination),),
                extra_environment={"GOMODCACHE": str(destination)},
            )
            command = list(sandbox.command)
            mount = command.index(str(module_cache))
            self.assertEqual(command[mount - 1], "--ro-bind")
            self.assertEqual(command[mount + 1], str(destination))
            setting = command.index("GOMODCACHE")
            self.assertEqual(command[setting - 1], "--setenv")
            self.assertEqual(command[setting + 1], str(destination))
            self.assertIn(str(executable.resolve()), command)

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_executes_pi_with_private_state_without_sandbox(self, execute):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_pi(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "pi",
                    "--data-dir",
                    str(data_dir),
                    "--no-sandbox",
                    "--",
                    "--list-models",
                ]
            )
            with patch.dict(
                os.environ,
                {
                    "PATH": str(binary_dir),
                    "ROCMLETE_PI_PORT": "9090",
                    "ROCMLETE_PI_DWARFSTAR_PORT": "8001",
                },
                clear=True,
            ):
                self.assertEqual(command_pi(arguments, self.catalog), 0)

            command, argv, child = execute.call_args.args
            self.assertEqual(command, str(executable))
            self.assertEqual(argv[-1], "--list-models")
            self.assertEqual(child["PI_OFFLINE"], "1")
            self.assertEqual(child["PI_TELEMETRY"], "0")
            models = Path(child["PI_CODING_AGENT_DIR"]) / "models.json"
            config = json.loads(models.read_text())
            self.assertEqual(
                config["providers"]["rocmplete"]["baseUrl"],
                "http://127.0.0.1:9090/v1",
            )
            self.assertEqual(
                config["providers"]["dwarfstar"]["baseUrl"],
                "http://127.0.0.1:8001/v1",
            )

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_executes_management_command_in_private_online_state(
        self, execute
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_pi(binary_dir)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "pi",
                    "--data-dir",
                    str(data_dir),
                    "--no-sandbox",
                    "--",
                    "install",
                    "npm:pi-code-indexer",
                ]
            )
            with patch.dict(
                os.environ,
                {"PATH": str(binary_dir)},
                clear=True,
            ):
                self.assertEqual(command_pi(arguments, self.catalog), 0)

            command, argv, child = execute.call_args.args
            self.assertEqual(command, str(executable))
            self.assertEqual(
                argv,
                [str(executable), "install", "npm:pi-code-indexer"],
            )
            self.assertNotIn("PI_OFFLINE", child)
            self.assertEqual(child["PI_TELEMETRY"], "0")
            self.assertTrue(
                Path(child["PI_CODING_AGENT_DIR"]).is_dir()
            )

    def test_launch_environment_replaces_inherited_pi_state(self):
        child = launch_environment(
            Path("/private/pi"),
            {
                "PATH": "/bin",
                "PI_CODING_AGENT_DIR": "/host/pi",
                "PI_OFFLINE": "0",
                "TOKEN": "kept-without-sandbox",
            },
        )
        self.assertEqual(child["PI_CODING_AGENT_DIR"], "/private/pi")
        self.assertEqual(child["PI_OFFLINE"], "1")
        self.assertEqual(child["PI_SKIP_VERSION_CHECK"], "1")
        self.assertEqual(child["PI_TELEMETRY"], "0")
        self.assertEqual(child["TOKEN"], "kept-without-sandbox")

    def test_parser_help_and_wrapper_expose_pi_launcher(self):
        _, arguments = parse_arguments(
            [
                "agent",
                "pi",
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
        self.assertEqual(arguments.pi_arguments, ["--", "--model", "other"])
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaisesRegex(SystemExit, "0"):
                main(["--help"])
        self.assertIn("agent", output.getvalue())
        self.assertNotIn("    pi", output.getvalue())

        _, arguments = parse_arguments(
            ["agent", "pi", "--no-sandbox", "--"]
        )
        self.assertFalse(arguments.sandbox)
        self.assertTrue(stat.S_IMODE(WRAPPER_PATH.stat().st_mode) & 0o111)
        contents = WRAPPER_PATH.read_text()
        self.assertIn('Path(__file__).resolve()', contents)
        self.assertIn(
            '[str(launcher), "agent", "pi", "--", *sys.argv[1:]]',
            contents,
        )


if __name__ == "__main__":
    unittest.main()
