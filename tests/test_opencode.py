import io
import json
import os
import stat
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

from rocmplete.agent_models import (
    agent_client_sampling_parameters,
    reasoning_client_default,
)
from rocmplete.bundles import artifact_path
from rocmplete.catalog import load_catalog
from rocmplete.cli import command_opencode, main
from rocmplete.cli_parser import parse_arguments
from rocmplete.content_verification import VerificationStore
from rocmplete.config import (
    DWARFSTAR_DEFAULT_CONTEXT,
    DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
)
from rocmplete.errors import LauncherError
from rocmplete.opencode import (
    DWARFSTAR_MODEL,
    DWARFSTAR_PROVIDER_ID,
    RECOMMENDED_MODEL,
    SANDBOX_HOME,
    SANDBOX_TUI_CONFIG,
    TUI_CONFIG_PATH,
    WRAPPER_PATH,
    _home_alias_arguments,
    create_launch_plan,
    create_sandbox_plan,
    launch_environment,
    passthrough_command,
    prepare_sandbox_paths,
    render_config,
    sandbox_paths,
)


class OpenCodeLauncherTests(unittest.TestCase):
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

    def _fake_opencode(self, root):
        executable = root / "opencode"
        executable.write_text("#!/bin/sh\nexit 0\n")
        executable.chmod(0o755)
        return executable

    def test_home_alias_preserves_fedora_var_home_layout(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "var" / "home"
            target.mkdir(parents=True)
            alias = root / "home"
            alias.symlink_to("var/home")

            self.assertEqual(
                _home_alias_arguments(alias, target),
                ("--symlink", "var/home", str(alias)),
            )

    def test_home_alias_ignores_an_ordinary_home_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            home = root / "home"
            target = root / "var" / "home"
            home.mkdir()
            target.mkdir(parents=True)

            self.assertEqual(_home_alias_arguments(home, target), ())

    def test_config_uses_chat_completions_provider_and_guarded_agents(self):
        config = json.loads(
            render_config(
                self.catalog,
                self.default_model,
                "http://127.0.0.1:9090/v1",
            )
        )
        self.assertEqual(config["$schema"], "https://opencode.ai/config.json")
        self.assertEqual(
            config["model"], "rocmplete/{}".format(self.default_model)
        )
        self.assertEqual(config["default_agent"], "investigate")
        self.assertEqual(
            config["permission"],
            {"edit": "ask", "bash": "ask", "task": "ask"},
        )
        investigate = config["agent"]["investigate"]
        self.assertEqual(investigate["mode"], "primary")
        self.assertNotIn("temperature", investigate)
        self.assertNotIn("options", investigate)
        self.assertEqual(
            investigate["permission"],
            {
                "*": "deny",
                "read": {
                    "*": "allow",
                    "*.env": "deny",
                    "*.env.*": "deny",
                    "*.env.example": "allow",
                },
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
        )
        self.assertIn("generated summary", investigate["prompt"])
        self.assertIn("no more than 500 words", investigate["prompt"])
        local = config["agent"]["investigate-local"]
        self.assertEqual(local["mode"], "subagent")
        self.assertTrue(local["hidden"])
        self.assertNotIn("temperature", local)
        self.assertNotIn("options", local)
        self.assertNotIn("task", local["permission"])
        web = config["agent"]["investigate-web"]
        self.assertEqual(web["mode"], "subagent")
        self.assertTrue(web["hidden"])
        self.assertNotIn("temperature", web)
        self.assertNotIn("options", web)
        self.assertEqual(
            web["permission"],
            {"*": "deny", "webfetch": "allow", "websearch": "allow"},
        )

        provider = config["provider"]["rocmplete"]
        self.assertEqual(provider["npm"], "@ai-sdk/openai-compatible")
        self.assertEqual(
            provider["options"]["baseURL"],
            "http://127.0.0.1:9090/v1",
        )
        self.assertEqual(len(provider["models"]), 13)
        self.assertNotIn("qwen3-0.6b-q8-0", provider["models"])
        self.assertNotIn("translategemma-27b-it-q8-0", provider["models"])
        self.assertIn("qwen3.6-27b-mtp-q8-0", provider["models"])
        self.assertIn(
            "qwen3.6-35b-a3b-mtp-ud-q8-k-xl", provider["models"]
        )
        self.assertIn("qwen3.8-27b-mtp-ud-q8-k-xl", provider["models"])
        self.assertIn("qwen3.8-27b-mtp-ud-q4-k-xl", provider["models"])
        self.assertEqual(
            provider["models"]["qwen3.8-27b-mtp-ud-q4-k-xl"]["limit"],
            {"context": 131072, "output": 16384},
        )
        self.assertIn("kat-coder-v2.5-dev-q8-0", provider["models"])
        self.assertEqual(
            provider["models"][
                "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
            ]["limit"],
            {"context": 262144, "output": 16384},
        )
        self.assertEqual(
            provider["models"][self.default_model]["limit"],
            {"context": 262144, "output": 16384},
        )
        for identifier, model in provider["models"].items():
            preset = self.catalog.llama_preset(identifier)
            expected_options = dict(
                agent_client_sampling_parameters(self.catalog, identifier)
            )
            if preset.sampling_policy:
                expected_options.update(
                    {"temperature": None, "top_p": None}
                )
            if preset.reasoning_control:
                self.assertTrue(model["reasoning"])
                expected_options["reasoningEffort"] = (
                    reasoning_client_default(preset)
                )
                enabled = (
                    {"thinking"}
                    if preset.reasoning_control == "toggle"
                    else set(preset.reasoning_levels)
                )
                if preset.reasoning_off:
                    enabled.add("instant")
                for name, variant in model["variants"].items():
                    if name in enabled:
                        self.assertEqual(
                            variant["reasoningEffort"],
                            "none"
                            if name == "instant"
                            else "high"
                            if name == "thinking"
                            else name,
                        )
                    else:
                        self.assertEqual(variant, {"disabled": True})
            else:
                self.assertNotIn("variants", model)
            self.assertEqual(model["options"], expected_options)

        dwarfstar = config["provider"][DWARFSTAR_PROVIDER_ID]
        self.assertEqual(dwarfstar["npm"], "@ai-sdk/openai-compatible")
        self.assertEqual(
            dwarfstar["options"]["baseURL"],
            "http://127.0.0.1:8000/v1",
        )
        model = dwarfstar["models"][DWARFSTAR_MODEL]
        self.assertEqual(
            model["limit"],
            {
                "context": DWARFSTAR_DEFAULT_CONTEXT,
                "output": DWARFSTAR_DEFAULT_OUTPUT_TOKENS,
            },
        )
        self.assertEqual(model["options"], {"reasoningEffort": "high"})
        self.assertEqual(
            model["variants"],
            {
                "instant": {"reasoningEffort": "none"},
                "thinking": {"reasoningEffort": "high"},
                "low": {"disabled": True},
                "medium": {"disabled": True},
                "high": {"disabled": True},
                "max": {"disabled": True},
            },
        )

    def test_static_tui_config_has_managed_agent_order(self):
        config = json.loads(TUI_CONFIG_PATH.read_text())
        self.assertEqual(config["$schema"], "https://opencode.ai/tui.json")
        self.assertEqual(
            config["keybinds"],
            {
                "agent_cycle": "shift+tab",
                "agent_cycle_reverse": "tab",
            },
        )

    def test_launch_uses_recommended_installed_model_and_forwards_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_opencode(binary_dir)
            self._mark_installed(data_dir, self.default_model)

            plan = create_launch_plan(
                self.catalog,
                data_dir,
                9090,
                ("--", "-m", "rocmplete/other", "repo"),
                {"PATH": "{}:{}".format(WRAPPER_PATH.parent, binary_dir)},
            )

            self.assertEqual(plan.command[0], str(executable))
            self.assertEqual(
                plan.command[1:],
                ("-m", "rocmplete/other", "repo"),
            )
            self.assertEqual(plan.default_model, self.default_model)
            self.assertEqual(plan.default_provider, "rocmplete")
            self.assertEqual(plan.endpoint, "http://127.0.0.1:9090/v1")
            self.assertEqual(
                plan.dwarfstar_endpoint, "http://127.0.0.1:8000/v1"
            )
            self.assertEqual(
                json.loads(plan.config_content)["model"],
                "rocmplete/{}".format(self.default_model),
            )

    def test_launch_falls_back_to_an_installed_agent_model(self):
        fallback = "qwen3.6-27b-q8-0"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_opencode(binary_dir)
            self._mark_installed(data_dir, fallback)

            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            self.assertEqual(plan.default_model, fallback)
            self.assertEqual(plan.default_provider, "rocmplete")

    def test_launch_uses_dwarfstar_when_no_agent_llama_model_is_installed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_opencode(binary_dir)
            self._mark_bundle_installed(
                data_dir,
                "dwarfstar-deepseek-v4-flash-0731-q2-imatrix",
            )

            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )

            self.assertEqual(plan.default_provider, DWARFSTAR_PROVIDER_ID)
            self.assertEqual(plan.default_model, DWARFSTAR_MODEL)
            self.assertEqual(
                json.loads(plan.config_content)["model"],
                "dwarfstar/deepseek-v4-flash-0731-q2-imatrix",
            )

    def test_launch_requires_an_installed_agent_model(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_opencode(binary_dir)
            self._mark_installed(root / "data", "qwen3-0.6b-q8-0")
            with self.assertRaisesRegex(
                LauncherError, "no installed model"
            ) as raised:
                create_launch_plan(
                    self.catalog,
                    root / "data",
                    8080,
                    (),
                    {"PATH": str(binary_dir)},
                )
            self.assertIn("content install llama-cpp", str(raised.exception))
            self.assertIn("content install dwarfstar", str(raised.exception))

    def test_launch_requires_the_real_opencode_executable(self):
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

    def test_information_and_self_management_commands_bypass_managed_state(self):
        with tempfile.TemporaryDirectory() as directory:
            binary_dir = Path(directory) / "bin"
            binary_dir.mkdir()
            executable = self._fake_opencode(binary_dir)
            environment = {"PATH": str(binary_dir)}

            for arguments in (
                ("--help",),
                ("--version",),
                ("completion", "zsh"),
                ("plugin", "example-plugin"),
                ("plug", "example-plugin"),
                ("upgrade",),
                ("uninstall",),
            ):
                with self.subTest(arguments=arguments):
                    self.assertEqual(
                        passthrough_command(("--", *arguments), environment),
                        (str(executable), *arguments),
                    )
            self.assertIsNone(
                passthrough_command(("session", "--help"), environment)
            )

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_passthrough_needs_no_model_or_private_state(self, execute):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            executable = self._fake_opencode(binary_dir)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "opencode",
                    "--data-dir",
                    str(data_dir),
                    "--",
                    "--version",
                ]
            )
            environment = {"PATH": str(binary_dir), "TOKEN": "preserved"}
            with patch.dict(os.environ, environment, clear=True):
                self.assertEqual(command_opencode(arguments, self.catalog), 0)

            self.assertEqual(
                execute.call_args.args,
                (
                    str(executable),
                    [str(executable), "--version"],
                    environment,
                ),
            )
            self.assertFalse(data_dir.exists())

    def test_child_environment_uses_inline_config_and_static_tui_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_opencode(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            child = launch_environment(
                plan,
                {
                    "PATH": str(binary_dir),
                    "HOME": "/home/test",
                    "OPENCODE_CONFIG": "/stale/config.json",
                    "OPENCODE_CONFIG_CONTENT": "stale",
                    "OPENCODE_TUI_CONFIG": "/stale/tui.json",
                },
            )
            self.assertNotIn("OPENCODE_CONFIG", child)
            self.assertEqual(
                child["OPENCODE_CONFIG_CONTENT"], plan.config_content
            )
            self.assertEqual(
                child["OPENCODE_TUI_CONFIG"], str(TUI_CONFIG_PATH)
            )
            self.assertEqual(child["HOME"], "/home/test")

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_executes_opencode_without_writing_configuration(self, execute):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            config_home = root / "configuration"
            binary_dir.mkdir()
            executable = self._fake_opencode(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "opencode",
                    "--data-dir",
                    str(data_dir),
                    "--no-sandbox",
                    "--",
                    "--print-logs",
                ]
            )
            with patch.dict(
                os.environ,
                {
                    "PATH": str(binary_dir),
                    "XDG_CONFIG_HOME": str(config_home),
                    "ROCMLETE_OPENCODE_PORT": "9090",
                    "ROCMLETE_OPENCODE_DWARFSTAR_PORT": "8001",
                },
                clear=True,
            ):
                self.assertEqual(command_opencode(arguments, self.catalog), 0)

            command, argv, child = execute.call_args.args
            self.assertEqual(command, str(executable))
            self.assertEqual(argv, [str(executable), "--print-logs"])
            self.assertEqual(
                json.loads(child["OPENCODE_CONFIG_CONTENT"])["provider"]
                ["rocmplete"]["options"]["baseURL"],
                "http://127.0.0.1:9090/v1",
            )
            self.assertEqual(
                json.loads(child["OPENCODE_CONFIG_CONTENT"])["provider"]
                ["dwarfstar"]["options"]["baseURL"],
                "http://127.0.0.1:8001/v1",
            )
            self.assertFalse(config_home.exists())

    @patch("rocmplete.cli.os.execvpe")
    def test_cli_uses_sandbox_by_default(self, execute):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_opencode(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            self._mark_installed(data_dir, self.default_model)
            _, arguments = parse_arguments(
                [
                    "agent",
                    "opencode",
                    "--data-dir",
                    str(data_dir),
                    "--",
                    "--print-logs",
                ]
            )
            with patch.dict(
                os.environ,
                {"PATH": str(binary_dir), "HF_TOKEN": "must-not-leak"},
                clear=True,
            ):
                with redirect_stderr(io.StringIO()) as output:
                    self.assertEqual(
                        command_opencode(arguments, self.catalog), 0
                    )

            command, argv, child = execute.call_args.args
            self.assertEqual(command, str(bwrap.resolve()))
            self.assertEqual(argv[0], str(bwrap.resolve()))
            self.assertIn("--clearenv", argv)
            self.assertNotIn("must-not-leak", argv)
            self.assertEqual(child, {"PATH": str(binary_dir)})
            self.assertIn("Writable project", output.getvalue())
            paths = sandbox_paths(data_dir)
            self.assertTrue(paths.root.is_dir())

    def test_sandbox_hides_host_environment_and_keeps_private_state(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project with spaces"
            binary_dir.mkdir()
            workdir.mkdir()
            executable = self._fake_opencode(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                ("--help",),
                {"PATH": str(binary_dir)},
            )
            paths = sandbox_paths(data_dir)
            prepare_sandbox_paths(paths, data_dir)
            sandbox = create_sandbox_plan(
                plan,
                data_dir,
                workdir,
                {
                    "PATH": str(binary_dir),
                    "HOME": "/home/test",
                    "TERM": "xterm-256color",
                    "HF_TOKEN": "must-not-leak",
                    "SSH_AUTH_SOCK": "/run/ssh-agent",
                },
            )

            command = list(sandbox.command)
            self.assertEqual(command[0], str(bwrap.resolve()))
            self.assertIn("--unshare-all", command)
            self.assertIn("--share-net", command)
            self.assertIn("--die-with-parent", command)
            self.assertIn("--new-session", command)
            self.assertIn("--clearenv", command)
            self.assertIn(str(workdir.resolve()), command)
            self.assertIn(str(paths.data), command)
            self.assertIn(str(TUI_CONFIG_PATH), command)
            triples = [
                command[index : index + 3]
                for index in range(len(command) - 2)
            ]
            self.assertIn(
                [
                    "--ro-bind",
                    str(TUI_CONFIG_PATH),
                    str(SANDBOX_TUI_CONFIG),
                ],
                triples,
            )
            self.assertIn(
                [
                    "--bind",
                    str(workdir.resolve()),
                    str(workdir.resolve()),
                ],
                triples,
            )
            self.assertNotIn("must-not-leak", command)
            self.assertNotIn("/run/ssh-agent", command)
            self.assertNotIn("/home/test", command)
            self.assertEqual(
                command[-3:], [str(executable.resolve()), "--pure", "--help"]
            )
            self.assertEqual(
                sandbox.environment, {"PATH": str(binary_dir)}
            )
            self.assertEqual(sandbox.state_root, paths.root)
            for path in (
                paths.root,
                paths.config,
                paths.data,
                paths.state,
                paths.cache,
            ):
                self.assertTrue(path.is_dir())
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o700)
            home_index = command.index("HOME")
            self.assertEqual(command[home_index + 1], str(SANDBOX_HOME))
            tui_index = command.index("OPENCODE_TUI_CONFIG")
            self.assertEqual(
                command[tui_index + 1], str(SANDBOX_TUI_CONFIG)
            )
            chdir_index = command.index("--chdir")
            self.assertEqual(
                command[chdir_index + 1], str(workdir.resolve())
            )

    def test_sandbox_refuses_state_inside_writable_project(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "project" / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            (root / "project").mkdir()
            self._fake_opencode(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            paths = sandbox_paths(data_dir)
            prepare_sandbox_paths(paths, data_dir)
            with self.assertRaisesRegex(LauncherError, "overlaps"):
                create_sandbox_plan(
                    plan,
                    data_dir,
                    root / "project",
                    {"PATH": str(binary_dir)},
                )

    def test_sandbox_state_rejects_symlinked_owned_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            paths = sandbox_paths(data_dir)
            paths.root.mkdir(parents=True)
            redirect = root / "redirect"
            redirect.mkdir()
            paths.config.symlink_to(redirect, target_is_directory=True)
            with self.assertRaisesRegex(LauncherError, "not a real directory"):
                prepare_sandbox_paths(paths, data_dir)

    def test_sandbox_refuses_host_home_as_writable_project(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            binary_dir.mkdir()
            self._fake_opencode(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            with self.assertRaisesRegex(
                LauncherError, "contains the host home"
            ):
                create_sandbox_plan(
                    plan,
                    data_dir,
                    Path.home(),
                    {"PATH": str(binary_dir)},
                )

    def test_sandbox_requires_bubblewrap(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project"
            binary_dir.mkdir()
            workdir.mkdir()
            self._fake_opencode(binary_dir)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                (),
                {"PATH": str(binary_dir)},
            )
            paths = sandbox_paths(data_dir)
            with self.assertRaisesRegex(
                LauncherError, "bubblewrap executable.*not found"
            ):
                create_sandbox_plan(
                    plan,
                    data_dir,
                    workdir,
                    {"PATH": str(binary_dir)},
                )
            self.assertFalse(paths.root.exists())

    def test_sandbox_preserves_upstream_subcommand_position(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "data"
            binary_dir = root / "bin"
            workdir = root / "project"
            binary_dir.mkdir()
            workdir.mkdir()
            executable = self._fake_opencode(binary_dir)
            bwrap = binary_dir / "bwrap"
            bwrap.write_text("#!/bin/sh\nexit 0\n")
            bwrap.chmod(0o755)
            self._mark_installed(data_dir, self.default_model)
            plan = create_launch_plan(
                self.catalog,
                data_dir,
                8080,
                ("session", "--help"),
                {"PATH": str(binary_dir)},
            )
            paths = sandbox_paths(data_dir)
            prepare_sandbox_paths(paths, data_dir)
            sandbox = create_sandbox_plan(
                plan,
                data_dir,
                workdir,
                {"PATH": str(binary_dir)},
            )
            self.assertEqual(
                sandbox.command[-4:],
                (str(executable.resolve()), "--pure", "session", "--help"),
            )

    def test_parser_and_help_expose_launcher_instead_of_installer(self):
        _, arguments = parse_arguments(
            [
                "agent",
                "opencode",
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
            arguments.opencode_arguments,
            ["--", "-m", "rocmplete/model"],
        )
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaisesRegex(SystemExit, "0"):
                main(["--help"])
        rendered = output.getvalue()
        self.assertIn("agent", rendered)
        self.assertNotIn("    opencode", rendered)
        self.assertNotIn("integration", rendered)

        _, arguments = parse_arguments(
            ["agent", "opencode", "--no-sandbox", "--"]
        )
        self.assertFalse(arguments.sandbox)

    def test_wrapper_is_executable_and_delegates_to_rocmplete(self):
        self.assertTrue(stat.S_IMODE(WRAPPER_PATH.stat().st_mode) & 0o111)
        contents = WRAPPER_PATH.read_text()
        self.assertIn('Path(__file__).resolve()', contents)
        self.assertIn(
            '[str(launcher), "agent", "opencode", "--", '
            '*sys.argv[1:]]',
            contents,
        )


if __name__ == "__main__":
    unittest.main()
