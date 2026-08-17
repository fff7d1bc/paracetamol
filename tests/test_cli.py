import io
import json
import os
import subprocess
import tempfile
import unittest
from contextlib import ExitStack, redirect_stderr, redirect_stdout
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import ANY, call, patch

from rocmplete.cli import (
    _acknowledge_unverified_downloads,
    _acceptance_result_path,
    _command_content_import,
    _command_content_install,
    _exact_bundle_category,
    _exact_bundles,
    _exact_categories,
    _interactive_build_target,
    _interactive_content_target,
    _llama_context_policy,
    _llama_flash_attention_policy,
    _llama_kv_cache_policy,
    _llama_reasoning_policy,
    _llama_sampling_policy,
    _llama_speculation_policy,
    _llama_template_policy,
    _parse_gpu_diagnostic_output,
    _print_doctor_apparmor_userns_policy,
    _print_doctor_devices,
    _print_doctor_selinux_device_policy,
    _print_rdna35_memory_guidance,
    _read_ttm_state,
    _rdna35_ttm_target_gib,
    _require_license_acceptance,
    _require_selection_license_acceptance,
    _render_llama_router_preset,
    _resolve_content_bundles,
    _remote_import_url,
    _remote_import_kind,
    _strix_halo_kfd_warning,
    _uses_grub_drop_in,
    _uses_grubby,
    _uses_rpm_ostree_boot,
    command_build,
    command_acceptance,
    command_benchmark,
    command_cleanup,
    command_content,
    command_doctor,
    command_logs,
    command_images,
    command_run,
    command_status,
    command_stop,
    main,
    requested_render_nodes,
    resolve_run_options,
    select_render_nodes,
)
from rocmplete.cli_parser import parse_arguments
from rocmplete.catalog import (
    Agreement,
    Artifact,
    ArtifactSource,
    Bundle,
    Catalog,
    LicenseInfo,
    load_catalog,
)
from rocmplete.config import (
    APPLICATIONS,
    CONTENT_TOOLS_BUILD_TARGET,
    CONTENT_TOOLS_IMAGE,
    ROCM_BASE_BUILD_TARGET,
    ROCM_BASE_IMAGE,
    ROCM_RUNTIME_BUILD_TARGET,
    ROCM_RUNTIME_IMAGE,
)
from rocmplete.content_verification import VerificationStore
from rocmplete.errors import LauncherError
from rocmplete.image_archive import (
    ArchivedImage,
    ImageArchive,
    selected_image_references,
)
from rocmplete.remote_import import RemoteDiscovery, RemoteFile


def _record_managed_file(data_dir, path, artifact):
    store = VerificationStore.load(data_dir)
    store.record(path, artifact.size, artifact.sha256)
    store.save()


def _qwen_sampling_defaults(thinking_presence=0.0):
    return json.dumps(
        {
            "thinking": {
                "temperature": 1.0,
                "top_p": 0.95,
                "top_k": 20,
                "min_p": 0.0,
                "presence_penalty": thinking_presence,
                "repeat_penalty": 1.0,
            },
            "non_thinking": {
                "temperature": 0.7,
                "top_p": 0.8,
                "top_k": 20,
                "min_p": 0.0,
                "presence_penalty": 1.5,
                "repeat_penalty": 1.0,
            },
        },
        sort_keys=True,
        separators=(",", ":"),
    )


class CliTests(unittest.TestCase):
    def _archive(self, path, references):
        return ImageArchive(
            path=path,
            size=1024,
            images=tuple(
                ArchivedImage(reference, "sha256:" + str(index) * 64, "amd64", "linux")
                for index, reference in enumerate(references, 1)
            ),
        )

    def test_root_version_is_available_without_podman(self):
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaises(SystemExit) as raised:
                parse_arguments(["--version"])
        self.assertEqual(raised.exception.code, 0)
        self.assertEqual(output.getvalue(), "ROCmplete 0.1.0\n")

    def test_path_launcher_delegates_to_checkout_outside_project(self):
        root = Path(__file__).resolve().parents[1]
        launcher = root / "bin" / "rocmplete"
        self.assertTrue(os.access(launcher, os.X_OK))
        with tempfile.TemporaryDirectory() as directory:
            alias = Path(directory) / "rocmplete"
            alias.symlink_to(launcher)
            completed = subprocess.run(
                [str(alias), "--version"],
                cwd=directory,
                check=True,
                capture_output=True,
                text=True,
            )
        self.assertEqual(completed.stdout, "ROCmplete 0.1.0\n")
        self.assertEqual(completed.stderr, "")

    def test_strix_halo_kfd_warning_only_applies_below_upstream_baseline(
        self,
    ):
        self.assertEqual(
            _strix_halo_kfd_warning("6.18.3-generic"),
            "kernel 6.18.3; verify gfx1151 queue/context-save backports "
            "(upstream 6.18.4+)",
        )
        self.assertIsNone(_strix_halo_kfd_warning("6.18.4-test"))
        self.assertIsNone(_strix_halo_kfd_warning("7.0.0-28-generic"))

    def test_images_without_operation_prints_copyable_examples(self):
        _, arguments = parse_arguments(["images"])
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(command_images(arguments), 2)
        self.assertIn("./rocmplete images export all", output.getvalue())
        self.assertIn("./rocmplete images import", output.getvalue())

    @patch("rocmplete.cli.podman.image_id")
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_export_dry_run_does_not_create_archive(
        self, require_rootless, run, image_exists, image_id
    ):
        image_id.return_value = "sha256:" + "a" * 64
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "images.tar"
            _, arguments = parse_arguments(
                [
                    "images",
                    "export",
                    "comfyui",
                    "--output",
                    str(output),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as rendered:
                self.assertEqual(command_images(arguments), 0)
            self.assertFalse(output.exists())
        run.assert_not_called()
        self.assertIn("podman save", rendered.getvalue())
        self.assertIn(ROCM_BASE_IMAGE, rendered.getvalue())

    @patch("rocmplete.cli.inspect_archive")
    @patch("rocmplete.cli.podman.image_id")
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_export_is_atomic_and_verifies_saved_ids(
        self,
        require_rootless,
        run,
        image_exists,
        image_id,
        inspect,
    ):
        references = selected_image_references("comfyui")
        archive_ids = {
            reference: "sha256:" + str(index) * 64
            for index, reference in enumerate(references, 1)
        }
        image_id.side_effect = lambda reference: archive_ids[reference]

        def create_archive(command):
            output = Path(command[command.index("--output") + 1])
            output.write_bytes(b"archive")
            inspect.return_value = ImageArchive(
                output,
                len(b"archive"),
                tuple(
                    ArchivedImage(
                        reference,
                        archive_ids[reference],
                        "amd64",
                        "linux",
                    )
                    for reference in references
                ),
            )
            return 0

        run.side_effect = create_archive
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "images.tar"
            _, arguments = parse_arguments(
                [
                    "images",
                    "export",
                    "comfyui",
                    "--output",
                    str(output),
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(command_images(arguments), 0)
            self.assertEqual(output.read_bytes(), b"archive")
            self.assertEqual(
                list(Path(directory).glob("*.partial-*")), []
            )

    @patch("rocmplete.cli.podman.image_id")
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_failed_image_export_removes_partial_archive(
        self, require_rootless, run, image_exists, image_id
    ):
        image_id.return_value = "sha256:" + "a" * 64

        def fail_after_writing(command):
            partial = Path(command[command.index("--output") + 1])
            partial.write_bytes(b"partial")
            return 2

        run.side_effect = fail_after_writing
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "images.tar"
            _, arguments = parse_arguments(
                [
                    "images",
                    "export",
                    "comfyui",
                    "--output",
                    str(output),
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(command_images(arguments), 2)
            self.assertFalse(output.exists())
            self.assertEqual(tuple(Path(directory).iterdir()), ())

    @patch("rocmplete.cli.inspect_archive")
    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch("rocmplete.cli.podman.run")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_import_dry_run_validates_without_loading(
        self, require_rootless, run, image_exists, inspect
    ):
        references = selected_image_references("comfyui")
        inspect.return_value = self._archive(
            Path("/backup/images.tar"), references
        )
        _, arguments = parse_arguments(
            ["images", "import", "/backup/images.tar", "--dry-run"]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_images(arguments), 0)
        run.assert_not_called()
        self.assertIn("podman load --input", output.getvalue())

    @patch("rocmplete.cli.inspect_archive")
    @patch("rocmplete.cli.podman.image_id")
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_import_is_idempotent_when_ids_match(
        self, require_rootless, run, image_exists, image_id, inspect
    ):
        references = selected_image_references("comfyui")
        archive = self._archive(Path("/backup/images.tar"), references)
        inspect.return_value = archive
        ids = {item.reference: item.image_id for item in archive.images}
        image_id.side_effect = lambda reference: ids[reference]
        _, arguments = parse_arguments(
            ["images", "import", "/backup/images.tar"]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_images(arguments), 0)
        run.assert_not_called()
        self.assertIn("already present", output.getvalue())

    @patch("rocmplete.cli.inspect_archive")
    @patch(
        "rocmplete.cli.podman.image_id",
        return_value="sha256:" + "f" * 64,
    )
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_import_refuses_conflicting_current_tags(
        self, require_rootless, image_exists, image_id, inspect
    ):
        references = selected_image_references("comfyui")
        inspect.return_value = self._archive(
            Path("/backup/images.tar"), references
        )
        _, arguments = parse_arguments(
            ["images", "import", "/backup/images.tar"]
        )
        with self.assertRaisesRegex(LauncherError, "different images"):
            with redirect_stdout(io.StringIO()):
                command_images(arguments)

    @patch("rocmplete.cli.inspect_archive")
    @patch(
        "rocmplete.cli.podman.image_id",
        return_value="sha256:" + "1" * 64,
    )
    @patch(
        "rocmplete.cli.podman.image_exists",
        side_effect=(False, True),
    )
    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_import_loads_and_verifies_missing_images(
        self,
        require_rootless,
        run,
        image_exists,
        image_id,
        inspect,
    ):
        archive = self._archive(
            Path("/backup/images.tar"), (CONTENT_TOOLS_IMAGE,)
        )
        inspect.return_value = archive
        _, arguments = parse_arguments(
            ["images", "import", "/backup/images.tar"]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_images(arguments), 0)
        self.assertEqual(
            run.call_args.args[0],
            ["podman", "load", "--input", "/backup/images.tar"],
        )
        self.assertIn("Imported 1 managed images", output.getvalue())

    @patch("rocmplete.cli.inspect_archive")
    @patch(
        "rocmplete.cli.podman.image_id",
        return_value="sha256:" + "1" * 64,
    )
    @patch(
        "rocmplete.cli.podman.image_exists",
        side_effect=(False, True),
    )
    @patch(
        "rocmplete.cli.podman.run",
        side_effect=KeyboardInterrupt,
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_image_import_reports_partial_success_after_interruption(
        self,
        require_rootless,
        run,
        image_exists,
        image_id,
        inspect,
    ):
        inspect.return_value = self._archive(
            Path("/backup/images.tar"), (CONTENT_TOOLS_IMAGE,)
        )
        _, arguments = parse_arguments(
            ["images", "import", "/backup/images.tar"]
        )
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaises(KeyboardInterrupt):
                command_images(arguments)
        self.assertIn("Imported before interruption", output.getvalue())

    def test_restricted_bundle_requires_explicit_license_acceptance(self):
        artifact = Artifact(
            identifier="restricted",
            description="Restricted model",
            source=ArtifactSource("owner/repo", "a" * 40, "model.bin"),
            destination="checkpoints/model.bin",
            size=1,
            sha256="b" * 64,
            license=LicenseInfo(
                "LicenseRef-Test",
                "verified",
                "https://example.invalid/license",
            ),
            agreements=("test-terms",),
        )
        bundle = Bundle(
            "test-bundle",
            "Test bundle",
            "comfyui",
            ("restricted",),
            "test-workflow",
        )
        catalog = Catalog(
            agreements={
                "test-terms": Agreement(
                    "test-terms",
                    "Test Terms",
                    "https://example.invalid/terms",
                    "Review the terms.",
                )
            },
            artifacts={"restricted": artifact},
            bundles={"test-bundle": bundle},
            workflow_packs={},
            benchmarks={},
        )
        with self.assertRaisesRegex(LauncherError, "--accept-license"):
            _require_license_acceptance(catalog, bundle, False)
        _require_license_acceptance(catalog, bundle, True)

    @patch("rocmplete.cli.podman.container_exists", return_value=False)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_without_scope_prints_copyable_examples(
        self, require_rootless, container_exists
    ):
        _, arguments = parse_arguments(["cleanup"])
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(command_cleanup(arguments), 2)
        self.assertIn("./rocmplete cleanup containers", output.getvalue())
        require_rootless.assert_not_called()

    def test_every_cleanup_scope_accepts_the_same_confirmation_flags(self):
        for scope in (
            "containers",
            "images",
            "build-cache",
            "caches",
            "downloads",
            "data",
        ):
            with self.subTest(scope=scope):
                _, arguments = parse_arguments(
                    [
                        "cleanup",
                        scope,
                        "--yes",
                        "--non-interactive",
                    ]
                )
                self.assertTrue(arguments.yes)
                self.assertTrue(arguments.non_interactive)

    @patch("rocmplete.cli.sys.stdin")
    @patch("builtins.input", return_value="no")
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run_quiet_stdout")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_image_decline_preserves_the_planned_image(
        self, require_rootless, run, image_exists, user_input, stdin
    ):
        stdin.isatty.return_value = True
        _, arguments = parse_arguments(
            [
                "cleanup",
                "images",
                "--image-tag",
                "localhost/test:latest",
            ]
        )
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaisesRegex(LauncherError, "declined"):
                command_cleanup(arguments)
        self.assertIn(
            "image: localhost/test:latest", output.getvalue()
        )
        self.assertIn("images cleanup", user_input.call_args.args[0])
        run.assert_not_called()

    @patch("builtins.input")
    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch("rocmplete.cli.podman.run_quiet_stdout")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_empty_cleanup_plan_needs_no_confirmation(
        self, require_rootless, run, image_exists, user_input
    ):
        _, arguments = parse_arguments(
            [
                "cleanup",
                "images",
                "--image-tag",
                "localhost/test:missing",
                "--non-interactive",
            ]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_cleanup(arguments), 0)
        self.assertEqual(
            output.getvalue(),
            "Image not present: localhost/test:missing\n",
        )
        user_input.assert_not_called()
        run.assert_not_called()

    @patch(
        "rocmplete.cli._existing_managed_containers",
        return_value=("rocmplete-comfyui",),
    )
    @patch("rocmplete.cli.podman.run_quiet_stdout")
    @patch("rocmplete.cli.podman.require_rootless")
    def test_noninteractive_container_cleanup_requires_yes(
        self, require_rootless, run, existing_containers
    ):
        _, arguments = parse_arguments(
            [
                "cleanup",
                "containers",
                "comfyui",
                "--non-interactive",
            ]
        )
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaisesRegex(LauncherError, "--yes"):
                command_cleanup(arguments)
        self.assertIn(
            "container: rocmplete-comfyui", output.getvalue()
        )
        run.assert_not_called()

    @patch(
        "rocmplete.cli.podman.image_exists",
        return_value=True,
    )
    @patch(
        "rocmplete.cli.podman.container_exists",
        return_value=False,
    )
    @patch("rocmplete.cli.podman.run_quiet_stdout", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_removes_requested_custom_image(
        self, require_rootless, run, container_exists, image_exists
    ):
        _, arguments = parse_arguments(
            [
                "cleanup",
                "images",
                "--image-tag",
                "localhost/test:latest",
                "--yes",
            ]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_cleanup(arguments), 0)
        self.assertEqual(
            run.call_args_list[0].args[0],
            ["podman", "image", "rm", "localhost/test:latest"],
        )
        self.assertEqual(
            output.getvalue(),
            "Cleanup plan:\n"
            "  image: localhost/test:latest\n"
            "Removed image: localhost/test:latest\n",
        )

    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli._existing_managed_containers", return_value=())
    @patch("rocmplete.cli.podman.run_quiet_stdout", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_all_images_removes_shared_base_last(
        self, require_rootless, run, existing_containers, image_exists
    ):
        _, arguments = parse_arguments(["cleanup", "images", "--yes"])
        with redirect_stdout(io.StringIO()):
            self.assertEqual(command_cleanup(arguments), 0)
        removed = [call.args[0][-1] for call in run.call_args_list]
        self.assertEqual(
            removed,
            [spec.image for spec in APPLICATIONS.values()]
            + [
                ROCM_BASE_IMAGE,
                ROCM_RUNTIME_IMAGE,
                CONTENT_TOOLS_IMAGE,
            ],
        )

    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_build_cache_removes_only_owned_build_downloads(
        self, require_rootless
    ):
        with tempfile.TemporaryDirectory() as directory:
            cache_home = Path(directory) / "cache"
            build_cache = cache_home / "rocmplete/build"
            pip_file = build_cache / "pip/http-v2/wheel"
            pip_file.parent.mkdir(parents=True)
            pip_file.write_bytes(b"cached wheel")
            sibling = cache_home / "keep"
            sibling.write_bytes(b"keep")
            _, arguments = parse_arguments(
                ["cleanup", "build-cache", "--yes"]
            )
            with patch.dict(
                os.environ, {"XDG_CACHE_HOME": str(cache_home)}
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_cleanup(arguments), 0)
            self.assertFalse(build_cache.exists())
            self.assertEqual(sibling.read_bytes(), b"keep")
            self.assertIn("Removed build cache:", output.getvalue())
        require_rootless.assert_not_called()

    @patch("rocmplete.cli.sys.stdin")
    @patch("builtins.input", return_value="yes")
    def test_cleanup_build_cache_shows_plan_before_prompt(
        self, user_input, stdin
    ):
        stdin.isatty.return_value = True
        with tempfile.TemporaryDirectory() as directory:
            cache_home = Path(directory) / "cache"
            build_cache = cache_home / "rocmplete/build"
            build_cache.mkdir(parents=True)
            (build_cache / "package.whl").write_bytes(b"wheel")
            _, arguments = parse_arguments(
                ["cleanup", "build-cache"]
            )
            with patch.dict(
                os.environ, {"XDG_CACHE_HOME": str(cache_home)}
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_cleanup(arguments), 0)
            self.assertIn(
                "build cache: {} (5.00 B)".format(build_cache),
                output.getvalue(),
            )
            self.assertIn(
                "reusable build downloads",
                user_input.call_args.args[0],
            )
            self.assertFalse(build_cache.exists())

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_all_builds_every_application_target(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(["build", "all"])
        with patch(
            "rocmplete.cli.podman.image_exists", return_value=False
        ), patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw"),
        ), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 0)
        self.assertEqual(run.call_count, 6)
        targets = [
            call.args[0][call.args[0].index("--target") + 1]
            for call in run.call_args_list
        ]
        self.assertEqual(
            targets,
            [
                CONTENT_TOOLS_BUILD_TARGET,
                ROCM_RUNTIME_BUILD_TARGET,
                ROCM_BASE_BUILD_TARGET,
                "comfyui",
                "llama-cpp",
                "dwarfstar",
            ],
        )
        runtime_command = run.call_args_list[1].args[0]
        self.assertIn(ROCM_RUNTIME_IMAGE, runtime_command)
        base_command = run.call_args_list[2].args[0]
        self.assertIn(ROCM_BASE_IMAGE, base_command)
        self.assertIn(
            "ROCM_RUNTIME_IMAGE={}".format(ROCM_RUNTIME_IMAGE),
            base_command,
        )
        comfy_build = run.call_args_list[3].args[0]
        self.assertIn(
            "ROCM_BASE_IMAGE={}".format(ROCM_BASE_IMAGE), comfy_build
        )
        self.assertIn("--pull=never", comfy_build)
        llama_build = run.call_args_list[-2].args[0]
        self.assertIn("--pull=never", llama_build)
        self.assertNotIn(
            "ROCM_BASE_IMAGE={}".format(ROCM_BASE_IMAGE), llama_build
        )
        self.assertIn(
            "ROCM_RUNTIME_IMAGE={}".format(ROCM_RUNTIME_IMAGE),
            llama_build,
        )
        dwarfstar_build = run.call_args_list[-1].args[0]
        self.assertIn("--pull=never", dwarfstar_build)
        self.assertNotIn(
            "ROCM_BASE_IMAGE={}".format(ROCM_BASE_IMAGE),
            dwarfstar_build,
        )
        self.assertIn(
            "ROCM_RUNTIME_IMAGE={}".format(ROCM_RUNTIME_IMAGE),
            dwarfstar_build,
        )
        self.assertTrue(
            all(
                "/cache/pip:/var/cache/rocmplete/pip:rw" in command
                for command in (call.args[0] for call in run.call_args_list)
            )
        )
        self.assertTrue(
            all(
                "--no-cache" not in command
                for command in (call.args[0] for call in run.call_args_list)
            )
        )
        text = output.getvalue()
        self.assertIn("\nBuilt images:\n", text)
        self.assertIn(ROCM_BASE_IMAGE, text)
        self.assertIn(ROCM_RUNTIME_IMAGE, text)
        self.assertIn(CONTENT_TOOLS_IMAGE, text)
        for application in targets[3:]:
            self.assertIn(application, text)
            self.assertIn(APPLICATIONS[application].image, text)
        self.assertIn(
            "\nNext:\n"
            "    ./rocmplete content install\n"
            "        Choose and install content interactively.\n"
            "    ./rocmplete content list\n"
            "        Or inspect available selections without installing.\n",
            text,
        )

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_base_builds_runtime_and_shared_pytorch_image(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(["build", "base", "--no-cache"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 0)
        self.assertEqual(run.call_count, 2)
        runtime_command = run.call_args_list[0].args[0]
        self.assertEqual(
            runtime_command[runtime_command.index("--target") + 1],
            ROCM_RUNTIME_BUILD_TARGET,
        )
        self.assertIn(ROCM_RUNTIME_IMAGE, runtime_command)
        command = run.call_args_list[1].args[0]
        self.assertEqual(
            command[command.index("--target") + 1],
            ROCM_BASE_BUILD_TARGET,
        )
        self.assertIn(ROCM_BASE_IMAGE, command)
        self.assertIn(
            "ROCM_RUNTIME_IMAGE={}".format(ROCM_RUNTIME_IMAGE), command
        )
        self.assertIn("--no-cache", command)
        self.assertNotIn(CONTENT_TOOLS_IMAGE, command)
        text = output.getvalue()
        self.assertIn("Built images:", text)
        self.assertIn(ROCM_RUNTIME_IMAGE, text)
        self.assertIn(ROCM_BASE_IMAGE, text)
        self.assertIn("./rocmplete doctor", text)
        self.assertNotIn("--gpu", text)

    @patch("rocmplete.cli.podman.run", side_effect=(0, 2))
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_base_reports_runtime_when_pytorch_base_fails(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(["build", "base"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 2)
        text = output.getvalue()
        self.assertIn("Built before failure:", text)
        self.assertIn(ROCM_RUNTIME_IMAGE, text)
        self.assertNotIn("\nNext:\n", text)

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_content_tools_builds_only_download_prerequisite(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(
            ["build", "content-tools", "--no-layer-cache"]
        )
        with patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw"),
        ), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 0)
        run.assert_called_once()
        command = run.call_args.args[0]
        self.assertEqual(
            command[command.index("--target") + 1],
            CONTENT_TOOLS_BUILD_TARGET,
        )
        self.assertIn(CONTENT_TOOLS_IMAGE, command)
        self.assertIn("--no-cache", command)
        self.assertNotIn(ROCM_BASE_IMAGE, command)
        self.assertIn("./rocmplete content list", output.getvalue())

    @patch("rocmplete.cli.podman.run", side_effect=(0, 0, 0, 0, 2))
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_all_summarizes_partial_success_without_next_step(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(["build", "all"])
        with patch(
            "rocmplete.cli.podman.image_exists", return_value=False
        ), patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw"),
        ), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 2)
        text = output.getvalue()
        self.assertIn("\nBuilt before failure:\n", text)
        summary = text.split("Built before failure:\n", 1)[1]
        self.assertIn(ROCM_BASE_IMAGE, summary)
        self.assertIn(APPLICATIONS["comfyui"].image, summary)
        self.assertNotIn(APPLICATIONS["llama-cpp"].image, summary)
        self.assertNotIn("\nNext:\n", text)

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_all_no_cache_refreshes_base_once_and_every_application(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(["build", "all", "--no-cache"])
        with redirect_stdout(io.StringIO()):
            self.assertEqual(command_build(arguments), 0)
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual(
            [
                command[command.index("--target") + 1]
                for command in commands
            ],
            [
                CONTENT_TOOLS_BUILD_TARGET,
                ROCM_RUNTIME_BUILD_TARGET,
                ROCM_BASE_BUILD_TARGET,
                "comfyui",
                "llama-cpp",
                "dwarfstar",
            ],
        )
        self.assertTrue(all("--no-cache" in command for command in commands))
        self.assertEqual(
            sum(
                command[command.index("--target") + 1]
                == ROCM_BASE_BUILD_TARGET
                for command in commands
            ),
            1,
        )
        self.assertTrue(
            all(
                "PIP_CACHE_DIR=/var/cache/rocmplete/pip" not in command
                for command in commands
            )
        )

    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_no_layer_cache_checks_cached_prerequisites(
        self, require_rootless, run, image_exists
    ):
        _, arguments = parse_arguments(
            ["build", "comfyui", "--no-layer-cache"]
        )
        with patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw,Z"),
        ), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 0)
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual(
            [
                command[command.index("--target") + 1]
                for command in commands
            ],
            [
                CONTENT_TOOLS_BUILD_TARGET,
                ROCM_RUNTIME_BUILD_TARGET,
                ROCM_BASE_BUILD_TARGET,
                "comfyui",
            ],
        )
        self.assertTrue(
            all("--no-cache" not in command for command in commands[:3])
        )
        self.assertIn("--no-cache", commands[3])
        self.assertTrue(
            all(
                "/cache/pip:/var/cache/rocmplete/pip:rw,Z" in command
                for command in commands
            )
        )
        self.assertIn("Building", output.getvalue())
        image_exists.assert_not_called()

    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_normal_build_checks_existing_prerequisite_layers(
        self, require_rootless, run, image_exists
    ):
        _, arguments = parse_arguments(["build", "comfyui"])
        with patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw"),
        ), redirect_stdout(io.StringIO()):
            self.assertEqual(command_build(arguments), 0)
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual(
            [
                command[command.index("--target") + 1]
                for command in commands
            ],
            [
                CONTENT_TOOLS_BUILD_TARGET,
                ROCM_RUNTIME_BUILD_TARGET,
                ROCM_BASE_BUILD_TARGET,
                "comfyui",
            ],
        )
        self.assertTrue(
            all("--no-cache" not in command for command in commands)
        )
        image_exists.assert_not_called()

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_build_llama_cpp_skips_pytorch_base(
        self, require_rootless, run
    ):
        _, arguments = parse_arguments(["build", "llama-cpp"])
        with patch(
            "rocmplete.cli.podman.image_exists", return_value=False
        ), patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw"),
        ), redirect_stdout(io.StringIO()):
            self.assertEqual(command_build(arguments), 0)
        self.assertEqual(run.call_count, 3)
        self.assertEqual(
            run.call_args_list[0].args[0][
                run.call_args_list[0].args[0].index("--target") + 1
            ],
            CONTENT_TOOLS_BUILD_TARGET,
        )
        runtime_command = run.call_args_list[1].args[0]
        self.assertEqual(
            runtime_command[runtime_command.index("--target") + 1],
            ROCM_RUNTIME_BUILD_TARGET,
        )
        command = run.call_args_list[2].args[0]
        self.assertEqual(
            command[command.index("--target") + 1], "llama-cpp"
        )
        self.assertIn("--pull=never", command)
        self.assertIn(
            "ROCM_RUNTIME_IMAGE={}".format(ROCM_RUNTIME_IMAGE), command
        )

    def test_guide_subcommand_parses_an_application(self):
        _, arguments = parse_arguments(["guide", "llama-cpp"])

        self.assertEqual(arguments.command, "guide")
        self.assertEqual(arguments.application, "llama-cpp")

    def test_bare_guide_prints_the_application_index(self):
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(main(["guide"]), 0)

        text = output.getvalue()
        self.assertIn("Application guides", text)
        self.assertIn("comfyui", text)
        self.assertIn("llama-cpp", text)
        self.assertIn("dwarfstar", text)
        self.assertIn("./rocmplete guide APPLICATION", text)

    def test_llama_guide_explains_cli_server_and_router_selection(self):
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(main(["guide", "llama-cpp"]), 0)

        text = output.getvalue()
        normalized = " ".join(text.split())
        self.assertIn("./rocmplete build llama-cpp", text)
        self.assertIn("./rocmplete content install llama-cpp qwen3.8", text)
        self.assertIn("./rocmplete content list --models", text)
        self.assertIn("run llama-cpp cli --preset", text)
        self.assertIn("run llama-cpp server --preset", text)
        self.assertIn("run llama-cpp server --router", text)
        self.assertIn(
            "API model name is the ROCmplete preset name", normalized
        )
        self.assertIn("A model is the GGUF weight file", normalized)
        self.assertIn(
            "qwen3.6 recipe installs dense 27B MTP Q8_0 and sparse "
            "35B-A3B MTP Dynamic Q8_K_XL together",
            normalized,
        )
        self.assertIn(
            "Presets do not store a general system prompt", normalized
        )
        self.assertIn("reviewed Jinja templates", normalized)
        self.assertIn("fixed managed template", normalized)
        self.assertIn("not a dependable repository agent", normalized)
        self.assertIn("./rocmplete agent opencode", text)
        self.assertIn("bin/opencode", text)
        self.assertIn("./rocmplete agent pi", text)
        self.assertIn("bin/pi", text)
        self.assertIn("./rocmplete agent omp", text)
        self.assertIn("bin/omp", text)
        self.assertIn("./rocmplete agent maki", text)
        self.assertIn("bin/maki", text)
        self.assertNotIn("OPENCODE_CONFIG", text)
        self.assertNotIn("OPENCODE_TUI_CONFIG", text)
        self.assertIn("TranslateGemma has one preset", normalized)
        self.assertIn("--compare-backends", text)
        self.assertIn("guide/applications.md#llamacpp", text)

    def test_dwarfstar_guide_explains_source_build_and_memory_starting_point(
        self,
    ):
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(main(["guide", "dwarfstar"]), 0)

        text = " ".join(output.getvalue().split())
        self.assertIn("compiled locally from one source commit", text)
        self.assertIn("112 GiB shared-memory starting point", text)
        self.assertIn("run dwarfstar server --context 32768", text)
        self.assertIn("DSpark is an optional, exact managed pair", text)
        self.assertIn("run dwarfstar server --dspark", text)
        self.assertIn("guide/applications.md#dwarfstar", text)

    @patch("builtins.input", return_value="2")
    @patch("rocmplete.cli.sys.stdin")
    def test_guided_build_selects_base(self, stdin, input_mock):
        stdin.isatty.return_value = True
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(_interactive_build_target(), "base")
        text = output.getvalue()
        self.assertIn("Build targets:", text)
        base_line = next(
            line for line in text.splitlines() if ") base " in line
        )
        content_line = next(
            line for line in text.splitlines() if ") content-tools " in line
        )
        self.assertEqual(
            base_line.index("shared"),
            content_line.index("shared"),
        )
        self.assertIn("Choose build target", input_mock.call_args.args[0])

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.require_rootless")
    @patch("builtins.input", return_value="4")
    @patch("rocmplete.cli.sys.stdin")
    def test_bare_build_uses_guided_target_on_a_terminal(
        self, stdin, input_mock, require_rootless, run
    ):
        stdin.isatty.return_value = True
        _, arguments = parse_arguments(["build"])
        with patch(
            "rocmplete.cli.podman.image_exists", return_value=True
        ), patch(
            "rocmplete.cli._pip_build_cache",
            return_value=(Path("/cache/pip"), ":rw"),
        ), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 0)
        command = run.call_args.args[0]
        self.assertEqual(
            command[command.index("--target") + 1],
            APPLICATIONS["comfyui"].build_target,
        )
        self.assertIn("Built images:", output.getvalue())

    @patch("rocmplete.cli.podman.require_rootless")
    @patch("rocmplete.cli.sys.stdin")
    def test_bare_build_keeps_guidance_for_noninteractive_use(
        self, stdin, require_rootless
    ):
        stdin.isatty.return_value = False
        _, arguments = parse_arguments(["build"])
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(command_build(arguments), 2)
        self.assertIn("./rocmplete build all", output.getvalue())
        self.assertIn("./rocmplete build base", output.getvalue())
        require_rootless.assert_not_called()

    @patch("rocmplete.cli._existing_managed_containers", return_value=())
    @patch("rocmplete.cli.podman.require_rootless")
    def test_noninteractive_cleanup_requires_yes_before_mutation(
        self, require_rootless, existing_containers
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "data"
            data_dir.mkdir()
            _, arguments = parse_arguments(
                [
                    "cleanup",
                    "data",
                    "--data-dir",
                    str(data_dir),
                    "--non-interactive",
                ]
            )
            with self.assertRaisesRegex(LauncherError, "--yes"):
                command_cleanup(arguments)
        require_rootless.assert_called_once_with()

    @patch("rocmplete.cli._existing_managed_containers", return_value=())
    @patch("rocmplete.cli.podman.require_rootless")
    @patch("builtins.input", return_value="yes")
    @patch("rocmplete.cli.sys.stdin")
    def test_cleanup_data_can_be_confirmed_interactively(
        self, stdin, user_input, require_rootless, existing_containers
    ):
        stdin.isatty.return_value = True
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "data"
            data_dir.mkdir()
            (data_dir / "model.bin").write_bytes(b"test")
            _, arguments = parse_arguments(
                ["cleanup", "data", "--data-dir", str(data_dir)]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(command_cleanup(arguments), 0)
            self.assertFalse(data_dir.exists())
        self.assertIn(
            "Permanently remove all ROCmplete data",
            user_input.call_args.args[0],
        )

    @patch("rocmplete.cli._existing_managed_containers", return_value=())
    @patch("rocmplete.cli.podman.require_rootless")
    @patch("builtins.input", return_value="no")
    @patch("rocmplete.cli.sys.stdin")
    def test_cleanup_data_decline_preserves_data(
        self, stdin, user_input, require_rootless, existing_containers
    ):
        stdin.isatty.return_value = True
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "data"
            data_dir.mkdir()
            preserved = data_dir / "model.bin"
            preserved.write_bytes(b"test")
            _, arguments = parse_arguments(
                ["cleanup", "data", "--data-dir", str(data_dir)]
            )
            with self.assertRaisesRegex(LauncherError, "declined"):
                command_cleanup(arguments)
            self.assertEqual(preserved.read_bytes(), b"test")
        require_rootless.assert_called_once_with()

    @patch("rocmplete.cli._existing_managed_containers", return_value=())
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_removes_confirmed_data(
        self, require_rootless, existing_containers
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = tempfile.mkdtemp(dir=directory)
            with open(data_dir + "/model.bin", "wb") as model:
                model.write(b"test")
            _, arguments = parse_arguments(
                ["cleanup", "data", "--yes", "--data-dir", data_dir]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_cleanup(arguments), 0)
            self.assertFalse(os.path.exists(data_dir))
            self.assertIn(
                "persistent data: {} (4.00 B)\n".format(data_dir),
                output.getvalue(),
            )
            self.assertTrue(
                output.getvalue().endswith(
                    "Removed persistent data: {}\n".format(data_dir)
                )
            )

    @patch("rocmplete.cli._existing_managed_containers", return_value=())
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_removes_only_generated_cache_and_staging(
        self, require_rootless, existing_containers
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "rocmplete"
            (data_dir / "apps/comfyui/cache/huggingface").mkdir(parents=True)
            (data_dir / "apps/comfyui/cache/huggingface/blob").write_bytes(
                b"cache"
            )
            (data_dir / "apps/comfyui/home/.triton").mkdir(parents=True)
            (data_dir / "apps/comfyui/home/.triton/kernel").write_bytes(
                b"kernel"
            )
            (data_dir / "staging/comfyui/tree").mkdir(parents=True)
            (data_dir / "staging/comfyui/tree/part").write_bytes(b"part")
            (data_dir / "content/comfyui/models/checkpoints").mkdir(
                parents=True
            )
            model = (
                data_dir
                / "content/comfyui/models/checkpoints/keep.safetensors"
            )
            model.write_bytes(b"keep")
            _, cache_arguments = parse_arguments(
                [
                    "cleanup",
                    "caches",
                    "--yes",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_cleanup(cache_arguments), 0)
            self.assertFalse((data_dir / "apps/comfyui/cache").exists())
            self.assertFalse((data_dir / "apps/comfyui/home/.triton").exists())
            _, download_arguments = parse_arguments(
                [
                    "cleanup",
                    "downloads",
                    "--yes",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            with redirect_stdout(output):
                self.assertEqual(command_cleanup(download_arguments), 0)
            self.assertFalse((data_dir / "staging").exists())
            self.assertEqual(model.read_bytes(), b"keep")
            self.assertIn("Removed generated data:", output.getvalue())

    @patch("rocmplete.cli.podman.container_exists", return_value=False)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_refuses_symlinked_parent(
        self, require_rootless, container_exists
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "rocmplete"
            external_home = root / "external-home"
            (external_home / ".cache").mkdir(parents=True)
            preserved = external_home / ".cache/preserved"
            preserved.write_bytes(b"preserve")
            data_dir.mkdir()
            (data_dir / "apps/comfyui").mkdir(parents=True)
            (data_dir / "apps/comfyui/home").symlink_to(
                external_home, target_is_directory=True
            )
            _, arguments = parse_arguments(
                [
                    "cleanup",
                    "caches",
                    "--yes",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            with redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(LauncherError, "symlinked path"):
                    command_cleanup(arguments)
            self.assertEqual(preserved.read_bytes(), b"preserve")

    @patch("rocmplete.cli.podman.managed_download_container_names")
    @patch("rocmplete.cli.podman.managed_container_names", return_value=())
    @patch(
        "rocmplete.cli.podman.remove_container",
        side_effect=LauncherError("container is still stopping"),
    )
    @patch(
        "rocmplete.cli.podman.container_exists",
        side_effect=[True, True],
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_propagates_container_removal_failure(
        self,
        require_rootless,
        container_exists,
        remove_container,
        managed_containers,
        download_containers,
    ):
        download_containers.return_value = ()
        _, arguments = parse_arguments(
            ["cleanup", "containers", "comfyui", "--yes"]
        )
        with self.assertRaisesRegex(
            LauncherError, "container is still stopping"
        ):
            command_cleanup(arguments)
        remove_container.assert_called_once_with(
            "rocmplete-comfyui", stop_timeout=0
        )

    @patch("rocmplete.cli.podman.managed_download_container_names")
    @patch("rocmplete.cli.podman.managed_container_names", return_value=())
    @patch("rocmplete.cli.podman.remove_container")
    @patch(
        "rocmplete.cli.podman.container_exists",
        side_effect=[True, False, False, False, False, False],
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_containers_reports_removed_and_absent_consistently(
        self,
        require_rootless,
        container_exists,
        remove_container,
        managed_containers,
        download_containers,
    ):
        download_containers.return_value = ()
        _, arguments = parse_arguments(
            ["cleanup", "containers", "--yes"]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_cleanup(arguments), 0)
        self.assertEqual(
            output.getvalue().splitlines(),
            [
                "Container not present: rocmplete-llama-cpp",
                "Container not present: rocmplete-dwarfstar",
                "Cleanup plan:",
                "  container: rocmplete-comfyui",
                "Removed container: rocmplete-comfyui",
            ],
        )
        remove_container.assert_called_once_with(
            "rocmplete-comfyui", stop_timeout=0
        )

    @patch(
        "rocmplete.cli.podman.managed_download_container_names",
        return_value=("rocmplete-download-0123456789ab",),
    )
    @patch(
        "rocmplete.cli.podman.managed_container_names",
        return_value=(),
    )
    @patch("rocmplete.cli.podman.remove_container")
    @patch(
        "rocmplete.cli.podman.container_exists",
        side_effect=lambda name: name == "rocmplete-llama-benchmark",
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_cleanup_all_includes_legacy_benchmark_and_downloader_containers(
        self,
        require_rootless,
        container_exists,
        remove_container,
        managed_containers,
        download_containers,
    ):
        _, arguments = parse_arguments(
            ["cleanup", "containers", "--yes"]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_cleanup(arguments), 0)
        self.assertIn(
            "container: rocmplete-llama-benchmark", output.getvalue()
        )
        self.assertIn(
            "container: rocmplete-download-0123456789ab",
            output.getvalue(),
        )
        self.assertEqual(
            remove_container.call_args_list,
            [
                call("rocmplete-llama-benchmark", stop_timeout=0),
                call(
                    "rocmplete-download-0123456789ab", stop_timeout=0
                ),
            ],
        )

    @patch("rocmplete.cli.podman.remove_container")
    @patch(
        "rocmplete.cli.podman.container_exists",
        return_value=True,
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_stop_removes_a_retained_container(
        self, require_rootless, container_exists, remove_container
    ):
        _, arguments = parse_arguments(["stop", "comfyui"])
        self.assertEqual(command_stop(arguments), 0)
        remove_container.assert_called_once_with(
            "rocmplete-comfyui", stop_timeout=2
        )

    @patch("rocmplete.cli.podman.remove_container")
    @patch(
        "rocmplete.cli.podman.container_exists",
        return_value=False,
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_stop_is_idempotent_for_an_absent_container(
        self, require_rootless, container_exists, remove_container
    ):
        _, arguments = parse_arguments(["stop", "comfyui"])
        self.assertEqual(command_stop(arguments), 0)
        remove_container.assert_not_called()

    @patch(
        "rocmplete.cli.podman.remove_container",
        side_effect=LauncherError("container still exists"),
    )
    @patch(
        "rocmplete.cli.podman.container_exists",
        return_value=True,
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_stop_propagates_failure_for_a_remaining_container(
        self, require_rootless, container_exists, remove_container
    ):
        _, arguments = parse_arguments(["stop", "comfyui"])
        with self.assertRaisesRegex(LauncherError, "container still exists"):
            command_stop(arguments)

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.container_exists", return_value=True)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_logs_are_bounded_by_default(
        self, require_rootless, container_exists, run
    ):
        _, arguments = parse_arguments(["logs", "comfyui"])
        self.assertEqual(command_logs(arguments), 0)
        run.assert_called_once_with(
            ["podman", "logs", "--tail", "200", "rocmplete-comfyui"]
        )

        _, arguments = parse_arguments(
            ["logs", "comfyui", "--all", "--follow"]
        )
        self.assertEqual(command_logs(arguments), 0)
        self.assertEqual(
            run.call_args.args[0],
            ["podman", "logs", "--follow", "rocmplete-comfyui"],
        )

    @patch("rocmplete.cli.podman.container_exists", return_value=False)
    @patch(
        "rocmplete.cli.podman.image_exists",
        side_effect=[True, True, True, True, False, True, False],
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_status_summarizes_local_state_without_creating_data(
        self, require_rootless, image_exists, container_exists
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                ["status", "--data-dir", str(data_dir)]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_status(arguments), 0)
            self.assertFalse(data_dir.exists())
        text = output.getvalue()
        self.assertIn("Persistent data", text)
        self.assertIn("missing", text)
        self.assertIn("Images", text)
        self.assertIn(ROCM_BASE_IMAGE, text)
        self.assertIn(ROCM_RUNTIME_IMAGE, text)
        self.assertIn("Managed containers", text)
        self.assertIn("rocmplete-comfyui", text)

    @patch("rocmplete.cli.podman.container_exists", return_value=False)
    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_status_uses_toml_data_directory_without_creating_it(
        self, require_rootless, image_exists, container_exists
    ):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            data_dir = home / "large-storage" / "rocmplete"
            config = home / ".config" / "rocmplete" / "config.toml"
            config.parent.mkdir(parents=True)
            config.write_text(
                '[storage]\ndata_dir = "{}"\n'.format(data_dir)
            )
            _, arguments = parse_arguments(["status"])
            with patch.dict(
                os.environ,
                {
                    "HOME": str(home),
                    "XDG_CONFIG_HOME": "",
                    "XDG_DATA_HOME": "",
                    "ROCMLETE_DATA_DIR": "",
                },
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_status(arguments), 0)

            self.assertFalse(data_dir.exists())
        self.assertIn(str(data_dir), output.getvalue())

    def test_status_llama_cpp_prints_shareable_direct_runtime_report(self):
        image = APPLICATIONS["llama-cpp"].image
        runtime_report = "\n".join(
            (
                "schema=1",
                "mode=server",
                "profile=strix-halo",
                "backend=rocm",
                "device=ROCm0: AMD Radeon 8060S Graphics",
                "backend_devices=ROCm0",
                "architecture=gfx1151",
                "gpu_count=1",
                "router=0",
                "models_max=2",
                "context=262144",
                "listen=0.0.0.0",
                "host_listen=127.0.0.1",
                "port=8080",
                "api_key=1",
                "unified_memory=1",
                "vulkan_f16_kv_contiguize=0",
            )
        )
        environment = "\n".join(
            (
                "ROCMLETE_LLAMA_MODEL=/content/models/qwen3.6-27b-mtp/"
                "Qwen3.6-27B-Q8_0.gguf",
                "ROCMLETE_SOURCE_REVISION={}".format("c" * 40),
                "ROCMLETE_PROFILE=auto",
                "ROCMLETE_LLAMA_DRAFT_MODEL=",
                "ROCMLETE_LLAMA_SPECULATIVE_TYPE=draft-mtp",
                "ROCMLETE_LLAMA_DRAFT_TOKENS=3",
                "ROCMLETE_LLAMA_CONTEXT_OVERRIDE=",
                "ROCMLETE_LLAMA_JINJA=0",
                "ROCMLETE_LLAMA_REASONING_PRESERVE=0",
                "ROCMLETE_LLAMA_CHAT_TEMPLATE=qwen3.6",
                "ROCMLETE_LLAMA_SAMPLING_DEFAULTS={}".format(
                    _qwen_sampling_defaults()
                ),
                "ROCMLETE_LLAMA_FLASH_ATTN_STRIX_HALO=on",
                "ROCMLETE_LLAMA_KV_CACHE_STRIX_HALO=q8_0",
                "ROCMLETE_RENDER_NODES=/dev/dri/renderD128",
            )
        )

        def capture(command, error):
            if command == [
                "podman", "inspect", "--format", "{{.State.Status}}",
                "rocmplete-llama-cpp",
            ]:
                return "running"
            if command == [
                "podman", "exec", "rocmplete-llama-cpp", "cat",
                "/tmp/rocmplete-llama-runtime",
            ]:
                return runtime_report
            if command == [
                "podman", "inspect", "--format",
                "{{range .Config.Env}}{{println .}}{{end}}",
                "rocmplete-llama-cpp",
            ]:
                return environment
            if command == [
                "podman", "inspect", "--format", "{{.Config.Image}}",
                "rocmplete-llama-cpp",
            ]:
                return image
            if command == [
                "podman", "inspect", "--format", "{{.Image}}",
                "rocmplete-llama-cpp",
            ]:
                return "sha256:" + "b" * 64
            if command == [
                "podman", "image", "inspect", "--format",
                "{{json .Labels}}", "sha256:" + "b" * 64,
            ]:
                return json.dumps(
                    {
                        "org.opencontainers.image.revision": "a" * 40,
                        "io.github.fff7d1bc.rocmplete.rocm.version": "7.14.0",
                        "io.github.fff7d1bc.rocmplete.llama-cpp.patches": (
                            "hip-apu-host-buffer,reasoning-controls"
                        ),
                    }
                )
            self.fail("unexpected capture command: {!r}".format(command))

        _, arguments = parse_arguments(
            [
                "status",
                "llama-cpp",
                "--model",
                "qwen3.6-27b-mtp-q8-0",
            ]
        )
        with patch("rocmplete.cli.podman.require_rootless"), patch(
            "rocmplete.cli.podman.container_exists", return_value=True
        ), patch("rocmplete.cli.podman.capture", side_effect=capture), patch(
            "rocmplete.cli.podman.capture_bytes",
            return_value=(
                b"llama-server\0--sampling-defaults-by-reasoning\0{}\0"
                b"--api-key-file\0/run/secrets/llama-api-key\0"
            ),
        ), patch(
            "rocmplete.cli._project_revision", return_value="c" * 40
        ), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_status(arguments, load_catalog()), 0)

        text = output.getvalue()
        self.assertIn("ROCmplete llama.cpp runtime", text)
        self.assertIn("sha256:" + "b" * 64, text)
        self.assertIn("qwen3.6-27b-mtp-q8-0", text)
        self.assertIn("AMD Radeon 8060S Graphics", text)
        self.assertIn("toggle; off, on; default on", text)
        self.assertIn("temp 1.0, top-p 0.95", text)
        self.assertIn("temp 0.7, top-p 0.8", text)
        self.assertIn("explicit request values override", text)
        self.assertIn("q8_0", text)
        self.assertIn("API_KEY_FILE", text)
        self.assertIn("configured (value redacted)", text)
        self.assertIn("--sampling-defaults-by-reasoning", text)
        self.assertIn("hip-apu-host-buffer,reasoning-controls", text)
        self.assertNotIn("/run/secrets", text)

    def test_status_llama_cpp_selects_one_running_router_model(self):
        image = APPLICATIONS["llama-cpp"].image
        runtime_report = "\n".join(
            (
                "schema=1",
                "mode=server",
                "profile=strix-halo",
                "backend=rocm",
                "device=ROCm0: AMD Radeon 8060S Graphics",
                "backend_devices=ROCm0",
                "architecture=gfx1151",
                "gpu_count=1",
                "router=1",
                "models_max=2",
                "context=",
                "listen=0.0.0.0",
                "host_listen=192.0.2.10",
                "port=8080",
                "api_key=0",
                "unified_memory=1",
                "vulkan_f16_kv_contiguize=0",
            )
        )
        router = "\n".join(
            (
                "version = 1",
                "",
                "[qwen3.8-27b-mtp-ud-q8-k-xl]",
                "model = /content/models/qwen3.8-27b/Qwen3.8-27B-UD-Q8_K_XL.gguf",
                "c = 262144",
                "jinja = true",
                "reasoning-preserve = true",
                "chat-template-file = /usr/local/share/rocmplete/"
                "llama-chat-templates/qwen3.8.jinja",
                "sampling-defaults-by-reasoning = {}".format(
                    _qwen_sampling_defaults()
                ),
                "spec-type = draft-mtp",
                "spec-draft-n-max = 3",
                "rocmplete-flash-attn-strix-halo = on",
                "rocmplete-kv-cache-strix-halo = q8_0",
                "load-on-startup = false",
                "",
            )
        )
        environment = "\n".join(
            (
                "ROCMLETE_LLAMA_ROUTER=1",
                "ROCMLETE_SOURCE_REVISION={}".format("c" * 40),
                "ROCMLETE_PROFILE=auto",
                "ROCMLETE_RENDER_NODES=/dev/dri/renderD128",
            )
        )

        def capture(command, error):
            if command[0:4] == [
                "podman", "inspect", "--format", "{{.State.Status}}",
            ]:
                return "running"
            if command[-1:] == ["/tmp/rocmplete-llama-runtime"]:
                return runtime_report
            if command[0:4] == [
                "podman", "inspect", "--format",
                "{{range .Config.Env}}{{println .}}{{end}}",
            ]:
                return environment
            if command[0:4] == [
                "podman", "inspect", "--format", "{{.Config.Image}}",
            ]:
                return image
            if command[0:4] == [
                "podman", "inspect", "--format", "{{.Image}}",
            ]:
                return "sha256:" + "b" * 64
            if command[0:4] == [
                "podman", "image", "inspect", "--format",
            ]:
                return json.dumps(
                    {
                        "org.opencontainers.image.revision": "a" * 40,
                        "io.github.fff7d1bc.rocmplete.rocm.version": "7.14.0",
                        "io.github.fff7d1bc.rocmplete.llama-cpp.patches": (
                            "hip-apu-host-buffer,reasoning-controls"
                        ),
                    }
                )
            if command[-1:] == ["/run/rocmplete/models.ini"]:
                return router
            self.fail("unexpected capture command: {!r}".format(command))

        def run_status(parameters):
            _, arguments = parse_arguments(parameters)
            with patch("rocmplete.cli.podman.require_rootless"), patch(
                "rocmplete.cli.podman.container_exists", return_value=True
            ), patch(
                "rocmplete.cli.podman.capture", side_effect=capture
            ), patch(
                "rocmplete.cli.podman.capture_bytes",
                return_value=(
                    b"llama-server\0--models-preset\0"
                    b"/tmp/rocmplete-models.ini\0--models-max\0"
                    b"2\0"
                ),
            ), patch(
                "rocmplete.cli._project_revision", return_value="c" * 40
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_status(arguments, load_catalog()), 0)
            return output.getvalue()

        text = run_status(
            [
                "status",
                "llama-cpp",
                "--model",
                "qwen3.8-27b-mtp-ud-q8-k-xl",
            ]
        )
        self.assertIn("Mode", text)
        self.assertIn("router", text)
        self.assertIn("qwen3.8-27b-mtp-ud-q8-k-xl", text)
        self.assertIn("effort; off, low, medium, xhigh; default medium", text)
        self.assertIn("Flash Attention", text)
        self.assertIn("q8_0", text)
        self.assertIn("Exact running llama.cpp command", text)
        self.assertIn("--models-preset /tmp/rocmplete-models.ini", text)
        self.assertIn("192.0.2.10:8080", text)

        overview = run_status(["status", "llama-cpp"])
        self.assertIn("Router models", overview)
        self.assertIn("qwen3.8-27b-mtp-ud-q8-k-xl", overview)
        self.assertIn("--model MODEL", overview)

    @patch("rocmplete.cli.podman.require_rootless")
    def test_status_model_requires_llama_cpp_scope(self, require_rootless):
        _, arguments = parse_arguments(["status", "--model", "qwen"])
        with self.assertRaisesRegex(
            LauncherError, "--model requires 'status llama-cpp'"
        ):
            command_status(arguments)

    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch(
        "rocmplete.cli.podman.capture",
        return_value="podman version test",
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_doctor_does_not_create_missing_data_directory(
        self, require_rootless, capture, image_exists
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                ["doctor", "--data-dir", str(data_dir)]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_doctor(arguments), 0)
            self.assertFalse(data_dir.exists())
        text = output.getvalue()
        self.assertIn("not created", text)
        self.assertIn("GPU probe", text)
        self.assertIn("not built", text)
        self.assertIn("./rocmplete build base", text)

    def test_doctor_device_report_makes_missing_requirements_explicit(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with redirect_stdout(io.StringIO()) as output:
                _print_doctor_devices(root / "kfd", ())
        text = output.getvalue()
        self.assertIn(str(root / "kfd"), text)
        self.assertIn("/dev/dri/renderD*", text)
        self.assertIn("KFD", text)
        self.assertIn("Render node", text)
        self.assertEqual(text.count("missing"), 2)

    def test_doctor_prints_persistent_gpu_device_access_fix(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            kfd = root / "kfd"
            render_node = root / "renderD128"
            kfd.touch()
            render_node.touch()
            with patch("rocmplete.cli.os.access", return_value=False):
                with redirect_stdout(io.StringIO()) as output:
                    _print_doctor_devices(kfd, (render_node,))
        text = output.getvalue()
        self.assertIn("insufficient access", text)
        self.assertIn("permits every local user", text)
        self.assertIn(
            "/etc/udev/rules.d/70-rocmplete-gpu.rules",
            text,
        )
        self.assertIn('KERNEL=="kfd", MODE="0666"', text)
        self.assertIn(
            'SUBSYSTEM=="drm", KERNEL=="renderD*", MODE="0666"',
            text,
        )
        self.assertIn("sudo udevadm control --reload-rules", text)
        self.assertNotIn("usermod", text)

    @patch(
        "rocmplete.cli.podman.selinux_container_device_access",
        return_value=False,
    )
    def test_doctor_reports_disabled_selinux_device_policy(self, device_access):
        with redirect_stdout(io.StringIO()) as output:
            self.assertIs(_print_doctor_selinux_device_policy(), False)
        text = output.getvalue()
        self.assertIn("SELinux", text)
        self.assertIn("container_use_devices is off", text)
        self.assertIn(
            "\nHost action: administrator access is required:\n"
            "  sudo setsebool -P container_use_devices 1\n",
            text,
        )

    def test_doctor_reports_selinux_host_action_once_before_failing(self):
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            stack.enter_context(
                patch("rocmplete.cli.podman.require_rootless")
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli.podman.capture",
                    return_value="podman version test",
                )
            )
            stack.enter_context(
                patch("rocmplete.cli.podman.image_exists", return_value=True)
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli.podman.selinux_container_device_access",
                    return_value=False,
                )
            )
            stack.enter_context(patch("rocmplete.cli._print_doctor_devices"))
            stack.enter_context(
                patch("rocmplete.cli._print_doctor_apparmor_userns_policy")
            )
            stack.enter_context(
                patch("rocmplete.cli._read_ttm_state", return_value=None)
            )
            output = io.StringIO()
            errors = io.StringIO()
            with redirect_stdout(output), redirect_stderr(errors):
                self.assertEqual(
                    main(["doctor", "--data-dir", directory]),
                    1,
                )

        command = "sudo setsebool -P container_use_devices 1"
        self.assertEqual(output.getvalue().count(command), 1)
        self.assertNotIn(command, errors.getvalue())
        self.assertIn(
            "Host action: administrator access is required:",
            output.getvalue(),
        )
        self.assertIn(
            "apply the reported Host action and retry",
            errors.getvalue(),
        )

    def test_doctor_omits_unavailable_apparmor_userns_policy(self):
        with tempfile.TemporaryDirectory() as directory:
            restriction = Path(directory) / "missing"
            with redirect_stdout(io.StringIO()) as output:
                _print_doctor_apparmor_userns_policy(restriction)
        self.assertEqual(output.getvalue(), "")

    def test_doctor_reports_allowed_apparmor_user_namespaces(self):
        with tempfile.TemporaryDirectory() as directory:
            restriction = Path(directory) / "restriction"
            restriction.write_text("0\n")
            with redirect_stdout(io.StringIO()) as output:
                _print_doctor_apparmor_userns_policy(restriction)
        text = output.getvalue()
        self.assertIn("AppArmor", text)
        self.assertIn("user namespace restriction is off", text)
        self.assertNotIn("Host action", text)

    def test_doctor_prints_apparmor_userns_opt_out_with_security_scope(self):
        with tempfile.TemporaryDirectory() as directory:
            restriction = Path(directory) / "restriction"
            restriction.write_text("1\n")
            with redirect_stdout(io.StringIO()) as output:
                _print_doctor_apparmor_userns_policy(restriction)
        text = output.getvalue()
        self.assertIn("restricts unprivileged user namespaces", text)
        self.assertIn("bubblewrap", text)
        self.assertIn(
            "kernel.apparmor_restrict_unprivileged_userns = 0",
            text,
        )
        self.assertIn(
            "/etc/sysctl.d/70-rocmplete-userns.conf",
            text,
        )
        self.assertIn("sudo sysctl --system", text)
        self.assertIn("system-wide", text)

    def test_gpu_diagnostic_output_requires_every_displayed_field(self):
        with self.assertRaisesRegex(
            LauncherError,
            r"missing PyTorch, ROCm/HIP, GPU operation",
        ):
            _parse_gpu_diagnostic_output(
                "Device: test\nArchitecture: gfx1150"
            )

    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch(
        "rocmplete.cli.podman.capture",
        return_value="podman version test",
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_doctor_missing_probe_image_is_an_actionable_skip(
        self, require_rootless, capture, image_exists
    ):
        _, arguments = parse_arguments(["doctor"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_doctor(arguments), 0)
        text = output.getvalue()
        self.assertIn("GPU probe", text)
        self.assertIn("Image", text)
        self.assertIn("not built", text)
        self.assertIn("Operation", text)
        self.assertIn("skipped", text)
        self.assertIn("./rocmplete build base", text)

    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch(
        "rocmplete.cli.podman.capture",
        return_value="podman version test",
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_doctor_rejects_an_explicit_missing_probe_image(
        self, require_rootless, capture, image_exists
    ):
        _, arguments = parse_arguments(
            ["doctor", "--image", "localhost/example:missing"]
        )
        with redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(
                LauncherError,
                r"GPU diagnostic image is not built: localhost/example:missing",
            ):
                command_doctor(arguments)

    def test_rdna35_ttm_targets_leave_host_headroom(self):
        gib = 1024**3
        self.assertEqual(_rdna35_ttm_target_gib(48 * gib), 32)
        self.assertEqual(_rdna35_ttm_target_gib(64 * gib), 48)
        self.assertEqual(_rdna35_ttm_target_gib(128 * gib), 112)
        self.assertEqual(_rdna35_ttm_target_gib(119 * gib), 100)
        self.assertIsNone(_rdna35_ttm_target_gib(32 * gib))

    def test_ttm_state_includes_the_page_pool_limit(self):
        with tempfile.TemporaryDirectory() as directory:
            parameters = Path(directory) / "ttm" / "parameters"
            parameters.mkdir(parents=True)
            (parameters / "pages_limit").write_text("29360128\n")
            (parameters / "page_pool_size").write_text("29360128\n")

            self.assertEqual(
                _read_ttm_state(Path(directory)),
                ("ttm", 29360128, 29360128),
            )

    def test_large_ttm_target_requires_the_matching_page_pool(self):
        with ExitStack() as stack:
            stack.enter_context(
                patch(
                    "rocmplete.cli._uses_rpm_ostree_boot",
                    return_value=True,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_system_memory_bytes",
                    return_value=128 * 1024**3,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_gtt_total_bytes",
                    return_value=112 * 1024**3,
                )
            )
            with redirect_stdout(io.StringIO()) as output:
                _print_rdna35_memory_guidance(
                    "/dev/dri/renderD128",
                    ("ttm", 112 * 262144, 64 * 262144),
                    "Strix Halo",
                )

        text = output.getvalue()
        self.assertIn("TTM pool", text)
        self.assertIn("64.00 GiB", text)
        self.assertIn(
            "effective GTT or TTM pool is below the 112 GiB starting point",
            text,
        )
        self.assertIn("ttm.page_pool_size=29360128", text)

    @patch(
        "rocmplete.cli.shutil.which",
        return_value="/usr/bin/rpm-ostree",
    )
    def test_rpm_ostree_detection_requires_boot_marker(self, which):
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "ostree-booted"
            self.assertFalse(_uses_rpm_ostree_boot(marker))
            marker.touch()
            self.assertTrue(_uses_rpm_ostree_boot(marker))

    @patch(
        "rocmplete.cli.shutil.which",
        return_value="/usr/sbin/update-grub",
    )
    def test_grub_drop_in_detection_requires_directory_and_tool(self, which):
        with tempfile.TemporaryDirectory() as directory:
            config_dir = Path(directory) / "grub.d"
            self.assertFalse(_uses_grub_drop_in(config_dir))
            config_dir.mkdir()
            which.return_value = None
            self.assertFalse(_uses_grub_drop_in(config_dir))
            which.return_value = "/usr/sbin/update-grub"
            self.assertTrue(_uses_grub_drop_in(config_dir))

    @patch("rocmplete.cli.shutil.which", return_value="/usr/sbin/grubby")
    def test_grubby_detection_requires_tool(self, which):
        self.assertTrue(_uses_grubby())
        which.return_value = None
        self.assertFalse(_uses_grubby())

    def test_rdna35_guidance_uses_rpm_ostree_kernel_arguments(self):
        with ExitStack() as stack:
            stack.enter_context(
                patch(
                    "rocmplete.cli._uses_rpm_ostree_boot",
                    return_value=True,
                )
            )
            refresh = stack.enter_context(
                patch("rocmplete.cli._initramfs_refresh_command")
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_system_memory_bytes",
                    return_value=128 * 1024**3,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_gtt_total_bytes",
                    return_value=64 * 1024**3,
                )
            )
            with redirect_stdout(io.StringIO()) as output:
                _print_rdna35_memory_guidance(
                    "/dev/dri/renderD128",
                    ("ttm", 64 * 262144, 64 * 262144),
                    "Strix Point",
                )
        text = output.getvalue()
        self.assertIn(
            "Replace the detected active TTM values with the 112 GiB "
            "starting point:",
            text,
        )
        self.assertIn(
            "sudo rpm-ostree kargs \\\n"
            "      --delete-if-present 'ttm.pages_limit=16777216' \\\n"
            "      --delete-if-present "
            "'ttm.page_pool_size=16777216' \\\n"
            "      --append-if-missing 'amdgpu.gttsize=114688' \\\n"
            "      --append-if-missing 'ttm.pages_limit=29360128' \\\n"
            "      --append-if-missing "
            "'ttm.page_pool_size=29360128'",
            text,
        )
        self.assertNotIn("/etc/modprobe.d/rocmplete-ttm.conf", text)
        self.assertIn("sudo reboot", text)
        self.assertIn("\n\nNote: These are dynamic", text)
        self.assertNotIn("\n  These are dynamic", text)
        refresh.assert_not_called()

    @patch(
        "rocmplete.cli._uses_rpm_ostree_boot",
        return_value=False,
    )
    @patch(
        "rocmplete.cli._uses_grub_drop_in",
        return_value=True,
    )
    @patch(
        "rocmplete.cli._initramfs_refresh_command",
    )
    @patch(
        "rocmplete.cli._read_gtt_total_bytes",
        return_value=64 * 1024**3,
    )
    @patch(
        "rocmplete.cli._read_system_memory_bytes",
        return_value=128 * 1024**3,
    )
    @patch(
        "rocmplete.cli._read_ttm_state",
        return_value=("ttm", 64 * 262144, 64 * 262144),
    )
    @patch(
        "rocmplete.cli.platform.release",
        return_value="6.18.4-test",
    )
    @patch(
        "rocmplete.cli.select_render_nodes",
        return_value=("/dev/dri/renderD128",),
    )
    @patch("rocmplete.cli.check_device_access")
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch(
        "rocmplete.cli.podman.capture",
        side_effect=[
            "podman version test",
            "PyTorch: test\n"
            "ROCm/HIP: test\n"
            "Device: test\n"
            "Architecture: gfx1151\n"
            "GPU operation: passed\n"
            "GPU devices: passed",
        ],
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_doctor_prints_copyable_strix_ttm_recommendation(
        self,
        require_rootless,
        capture,
        image_exists,
        check_device_access,
        select_render_nodes,
        release,
        read_ttm_state,
        read_system_memory,
        read_gtt_total,
        initramfs_refresh,
        uses_grub_drop_in,
        uses_rpm_ostree,
    ):
        _, arguments = parse_arguments(["doctor"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_doctor(arguments), 0)
        text = output.getvalue()
        self.assertIn("Strix Halo shared memory", text)
        self.assertIn("  System RAM     128.00 GiB", text)
        self.assertIn("  GTT ready      64.00 GiB", text)
        self.assertIn("  TTM pool       64.00 GiB", text)
        self.assertIn(
            'GRUB_CMDLINE_LINUX_DEFAULT="${GRUB_CMDLINE_LINUX_DEFAULT} '
            'amdgpu.gttsize=114688 ttm.pages_limit=29360128 '
            'ttm.page_pool_size=29360128"',
            text,
        )
        self.assertIn(
            "/etc/default/grub.d/70-rocmplete-ttm.cfg",
            text,
        )
        self.assertIn("sudo update-grub", text)
        self.assertNotIn("/etc/modprobe.d/rocmplete-ttm.conf", text)
        self.assertNotIn("sudo update-initramfs -u", text)
        self.assertIn("sudo reboot", text)
        self.assertIn("\n\nNote: These are dynamic", text)
        self.assertNotIn("\n  These are dynamic", text)
        self.assertNotIn("KFD baseline", text)
        self.assertNotIn("upstream fixes present", text)
        initramfs_refresh.assert_not_called()

    def test_rdna35_guidance_falls_back_to_active_module_configuration(self):
        with ExitStack() as stack:
            stack.enter_context(
                patch(
                    "rocmplete.cli._uses_rpm_ostree_boot",
                    return_value=False,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._uses_grub_drop_in",
                    return_value=False,
                )
            )
            stack.enter_context(
                patch("rocmplete.cli._uses_grubby", return_value=False)
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._initramfs_refresh_command",
                    return_value="sudo dracut --force",
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_system_memory_bytes",
                    return_value=128 * 1024**3,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_gtt_total_bytes",
                    return_value=64 * 1024**3,
                )
            )
            with redirect_stdout(io.StringIO()) as output:
                _print_rdna35_memory_guidance(
                    "/dev/dri/renderD128",
                    ("amd_ttm", 64 * 262144, 64 * 262144),
                    "Strix Halo",
                )
        text = output.getvalue()
        self.assertIn(
            "options amdgpu gttsize=114688",
            text,
        )
        self.assertIn(
            "options amd_ttm pages_limit=29360128 "
            "page_pool_size=29360128",
            text,
        )
        self.assertIn(
            "/etc/modprobe.d/rocmplete-ttm.conf",
            text,
        )
        self.assertIn("sudo dracut --force", text)

    def test_rdna35_guidance_uses_grubby_on_conventional_fedora(self):
        with ExitStack() as stack:
            stack.enter_context(
                patch(
                    "rocmplete.cli._uses_rpm_ostree_boot",
                    return_value=False,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._uses_grub_drop_in",
                    return_value=False,
                )
            )
            stack.enter_context(
                patch("rocmplete.cli._uses_grubby", return_value=True)
            )
            refresh = stack.enter_context(
                patch("rocmplete.cli._initramfs_refresh_command")
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_system_memory_bytes",
                    return_value=128 * 1024**3,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_gtt_total_bytes",
                    return_value=64 * 1024**3,
                )
            )
            with redirect_stdout(io.StringIO()) as output:
                _print_rdna35_memory_guidance(
                    "/dev/dri/renderD128",
                    ("ttm", 64 * 262144, 64 * 262144),
                    "Strix Halo",
                )
        text = output.getvalue()
        self.assertIn(
            "  sudo grubby --update-kernel=ALL \\\n"
            "    --remove-args='amdgpu.gttsize ttm.pages_limit "
            "ttm.page_pool_size' \\\n"
            "    --args='amdgpu.gttsize=114688 "
            "ttm.pages_limit=29360128 "
            "ttm.page_pool_size=29360128'",
            text,
        )
        self.assertNotIn("/etc/modprobe.d/rocmplete-ttm.conf", text)
        self.assertIn("sudo reboot", text)
        refresh.assert_not_called()

    def test_doctor_applies_shared_memory_guidance_without_halo_warning(
        self,
    ):
        captures = [
            "podman version test",
            "PyTorch: test\n"
            "ROCm/HIP: test\n"
            "Device: test\n"
            "Architecture: gfx1150\n"
            "GPU operation: passed\n"
            "GPU devices: passed",
        ]
        with ExitStack() as stack:
            stack.enter_context(
                patch("rocmplete.cli.podman.require_rootless")
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli.podman.capture",
                    side_effect=captures,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli.podman.image_exists",
                    return_value=True,
                )
            )
            stack.enter_context(
                patch("rocmplete.cli.check_device_access")
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli.select_render_nodes",
                    return_value=("/dev/dri/renderD128",),
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_ttm_state",
                    return_value=(
                        "ttm",
                        112 * 262144,
                        112 * 262144,
                    ),
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_system_memory_bytes",
                    return_value=128 * 1024**3,
                )
            )
            stack.enter_context(
                patch(
                    "rocmplete.cli._read_gtt_total_bytes",
                    return_value=112 * 1024**3,
                )
            )
            _, arguments = parse_arguments(["doctor"])
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_doctor(arguments), 0)
        text = output.getvalue()
        self.assertIn("Host\n  Podman", text)
        self.assertIn("\nGPU access\n", text)
        self.assertIn("\nGPU probe\n", text)
        self.assertIn("Strix Point shared memory", text)
        self.assertIn("  Operation      passed", text)
        self.assertIn("  Isolation      passed", text)
        self.assertIn(
            "  Status         meets the 112 GiB starting point",
            text,
        )
        self.assertNotIn("requires upstream 6.18.4 KFD fixes", text)

    @patch("rocmplete.cli.podman.require_rootless")
    def test_web_dry_run_does_not_create_data_directory(
        self, require_rootless
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(command_run(arguments), 0)
            self.assertFalse(data_dir.exists())

    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_missing_web_image_points_to_build_and_content_steps(
        self, require_rootless, image_exists
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    directory,
                ]
            )
            with redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(
                    LauncherError,
                    r"Build image:    \./rocmplete build comfyui",
                ) as caught:
                    command_run(arguments)
        self.assertIn(
            "Install content: ./rocmplete content install comfyui",
            str(caught.exception),
        )

    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_missing_explicit_image_does_not_offer_unrelated_setup(
        self, require_rootless, image_exists
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    directory,
                    "--image",
                    "localhost/custom:test",
                ]
            )
            with redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(
                    LauncherError, "image not found: localhost/custom:test"
                ) as caught:
                    command_run(arguments)
        self.assertNotIn("Install content:", str(caught.exception))

    @patch("rocmplete.cli.podman.require_rootless")
    def test_explicit_wildcard_listen_prints_exposure_warning(
        self, require_rootless
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--listen",
                    "0.0.0.0",
                    "--data-dir",
                    directory,
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
        self.assertIn(
            "WARNING: comfyui is published on "
            "0.0.0.0:8188 without authentication.",
            output.getvalue(),
        )

    @patch(
        "rocmplete.cli.podman.run_managed_foreground",
        return_value=0,
    )
    @patch("rocmplete.cli.podman.run")
    @patch("rocmplete.cli.podman.container_exists", return_value=False)
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_attached_web_run_uses_interrupt_safe_container_lifecycle(
        self,
        require_rootless,
        image_exists,
        container_exists,
        run,
        run_managed_foreground,
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--listen",
                    "127.0.0.1",
                    "--data-dir",
                    directory,
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(command_run(arguments), 0)
        command, name, error = run_managed_foreground.call_args.args
        self.assertEqual(name, "rocmplete-comfyui")
        self.assertEqual(error, "comfyui failed")
        self.assertIn("--rm", command)
        run.assert_not_called()

    @patch("rocmplete.cli.podman.run", return_value=0)
    @patch("rocmplete.cli.podman.run_managed_foreground")
    @patch("rocmplete.cli.podman.container_exists", return_value=False)
    @patch("rocmplete.cli.podman.image_exists", return_value=True)
    @patch("rocmplete.cli.podman.require_rootless")
    def test_detached_web_run_retains_normal_podman_execution(
        self,
        require_rootless,
        image_exists,
        container_exists,
        run_managed_foreground,
        run,
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--listen",
                    "127.0.0.1",
                    "--data-dir",
                    directory,
                    "--detach",
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(command_run(arguments), 0)
        self.assertIn("--detach", run.call_args.args[0])
        run_managed_foreground.assert_not_called()

    def test_llama_server_dry_run_does_not_create_data_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "models" / "model.gguf"
            model.parent.mkdir()
            model.write_bytes(b"test model")
            data_dir = root / "not-created"
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--model",
                    str(model),
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
            self.assertFalse(data_dir.exists())
            command = output.getvalue()
            self.assertNotIn("--network", command)
            self.assertIn("--publish 127.0.0.1:8080:8080/tcp", command)
            self.assertIn("--read-only", command)
            self.assertNotIn("/dev/kfd", command)
            self.assertIn("/content/models/model.gguf", command)
            self.assertIn("ROCMLETE_LISTEN=0.0.0.0", command)
            self.assertIn("ROCMLETE_HOST_LISTEN=127.0.0.1", command)
            self.assertIn("Backend: rocm", command)

    def test_dwarfstar_server_dry_run_accepts_strix_point_and_is_confined(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "not-created"
            model = root / "model.gguf"
            model.write_bytes(b"fixture")
            _, arguments = parse_arguments(
                [
                    "run",
                    "dwarfstar",
                    "server",
                    "--profile",
                    "strix-point",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with patch(
                "rocmplete.cli.select_render_nodes",
                return_value=("/dev/dri/renderD128",),
            ), patch(
                "rocmplete.cli.check_gpu_device_access"
            ), patch(
                "rocmplete.cli._managed_dwarfstar_model", return_value=model
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)

            self.assertFalse(data_dir.exists())
            command = output.getvalue()
            self.assertIn("--read-only", command)
            self.assertIn("--cap-drop all", command)
            self.assertIn("/dev/kfd", command)
            self.assertIn("/dev/dri/renderD128", command)
            self.assertIn("127.0.0.1:8000:8000/tcp", command)
            self.assertIn("ROCMLETE_LISTEN=0.0.0.0", command)
            self.assertIn("ROCMLETE_HOST_LISTEN=127.0.0.1", command)
            self.assertIn("ROCMLETE_DWARFSTAR_CONTEXT=131072", command)
            self.assertIn("ROCMLETE_DWARFSTAR_OUTPUT_TOKENS=16000", command)
            self.assertIn("ROCMLETE_PROFILE=strix-point", command)
            self.assertIn("ROCMLETE_DWARFSTAR_DSPARK=0", command)

    def test_dwarfstar_server_accepts_one_explicit_local_gguf(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "not-created"
            model = root / "models" / "deepseek-v4.gguf"
            model.parent.mkdir()
            model.write_bytes(b"fixture")
            _, arguments = parse_arguments(
                [
                    "run",
                    "dwarfstar",
                    "server",
                    "--model",
                    str(model),
                    "--profile",
                    "strix-halo",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with patch(
                "rocmplete.cli.select_render_nodes",
                return_value=("/dev/dri/renderD128",),
            ), patch(
                "rocmplete.cli.check_gpu_device_access"
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)

            self.assertFalse(data_dir.exists())
            command = output.getvalue()
            self.assertIn(str(model), command)
            self.assertIn(
                "{}:/content/models:ro".format(model.parent), command
            )
            self.assertIn(
                "ROCMLETE_DWARFSTAR_MODEL=/content/models/deepseek-v4.gguf",
                command,
            )

    def test_dwarfstar_rejects_explicit_non_gguf_model(self):
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory) / "model.bin"
            model.write_bytes(b"fixture")
            _, arguments = parse_arguments(
                [
                    "run",
                    "dwarfstar",
                    "server",
                    "--model",
                    str(model),
                    "--dry-run",
                ]
            )
            with patch(
                "rocmplete.cli.select_render_nodes",
                return_value=("/dev/dri/renderD128",),
            ), patch("rocmplete.cli.check_gpu_device_access"):
                with self.assertRaisesRegex(
                    LauncherError, "--model must name a .gguf file"
                ):
                    command_run(arguments)

    def test_dwarfstar_dspark_dry_run_uses_exact_managed_pair(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "not-created"
            model_root = root / "models"
            model_root.mkdir()
            model = model_root / "target.gguf"
            support = model_root / "support.gguf"
            model.write_bytes(b"target")
            support.write_bytes(b"support")
            _, arguments = parse_arguments(
                [
                    "run",
                    "dwarfstar",
                    "server",
                    "--dspark",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with patch(
                "rocmplete.cli.select_render_nodes",
                return_value=("/dev/dri/renderD128",),
            ), patch(
                "rocmplete.cli.check_gpu_device_access"
            ), patch(
                "rocmplete.cli._managed_dwarfstar_dspark_models",
                return_value=(model, support),
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)

            text = output.getvalue()
            self.assertFalse(data_dir.exists())
            self.assertIn("ROCMLETE_DWARFSTAR_DSPARK=1", text)
            self.assertIn(
                "ROCMLETE_DWARFSTAR_DSPARK_MODEL=/content/models/support.gguf",
                text,
            )
            self.assertIn("DSpark support:", text)
            self.assertIn(
                "{}:/content/models:ro".format(model_root), text
            )

    def test_dwarfstar_dspark_rejects_arbitrary_model_override(self):
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory) / "model.gguf"
            model.write_bytes(b"fixture")
            _, arguments = parse_arguments(
                [
                    "run",
                    "dwarfstar",
                    "server",
                    "--dspark",
                    "--model",
                    str(model),
                    "--dry-run",
                ]
            )
            with patch(
                "rocmplete.cli.select_render_nodes",
                return_value=("/dev/dri/renderD128",),
            ), patch("rocmplete.cli.check_gpu_device_access"):
                with self.assertRaisesRegex(
                    LauncherError, "cannot be combined with --model"
                ):
                    command_run(arguments)

    def test_dwarfstar_non_loopback_publication_is_explicit_and_warned(self):
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory) / "deepseek-v4.gguf"
            model.write_bytes(b"fixture")
            _, arguments = parse_arguments(
                [
                    "run",
                    "dwarfstar",
                    "server",
                    "--model",
                    str(model),
                    "--listen",
                    "0.0.0.0",
                    "--dry-run",
                ]
            )
            with patch(
                "rocmplete.cli.select_render_nodes",
                return_value=("/dev/dri/renderD128",),
            ), patch(
                "rocmplete.cli.check_gpu_device_access"
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)

            text = output.getvalue()
            self.assertIn(
                "DwarfStar is published on 0.0.0.0:8000 without "
                "authentication",
                text,
            )
            self.assertIn("--network pasta:-4", text)
            self.assertIn("--publish 0.0.0.0:8000:8000/tcp", text)
            self.assertIn("ROCMLETE_HOST_LISTEN=0.0.0.0", text)

    def test_dwarfstar_rejects_multiple_render_nodes(self):
        _, arguments = parse_arguments(
            ["run", "dwarfstar", "cli", "--prompt", "hello", "--dry-run"]
        )
        with patch(
            "rocmplete.cli.select_render_nodes",
            return_value=("/dev/dri/renderD128", "/dev/dri/renderD129"),
        ):
            with self.assertRaisesRegex(
                LauncherError, "requires exactly one selected render node"
            ):
                command_run(arguments)

    def test_llama_cpu_dry_run_ignores_render_node_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "model.gguf"
            model.write_bytes(b"test model")
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--model",
                    str(model),
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(root / "data"),
                    "--dry-run",
                ]
            )
            with patch.dict(
                "os.environ",
                {"ROCMLETE_RENDER_NODES": "/dev/dri/renderD128"},
            ):
                with redirect_stdout(io.StringIO()) as output:
                    self.assertEqual(command_run(arguments), 0)
        command = output.getvalue()
        self.assertIn("ROCMLETE_GPU_COUNT=0", command)
        self.assertNotIn("/dev/dri/renderD128", command)

    def test_llama_managed_preset_dry_run_uses_catalog_context(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("qwen3-0.6b-q8-0")
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            model = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--listen",
                    "127.0.0.1",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
            command = output.getvalue()
            self.assertIn("--ctx-size 4096", command)
            self.assertIn(
                "/content/llama-cpp/models:/content/models:ro", command
            )
            self.assertIn(
                "/content/models/{}".format(artifact.destination), command
            )
            self.assertIn("ROCMLETE_LLAMA_JINJA=0", command)
            self.assertIn(
                "ROCMLETE_LLAMA_CHAT_TEMPLATE=qwen3-0.6b", command
            )

    def test_llama_server_start_prints_matching_status_command(self):
        preset = load_catalog().llama_preset(
            "qwen3.8-27b-mtp-ud-q8-k-xl"
        )
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--data-dir",
                    directory,
                    "--detach",
                ]
            )
            with patch(
                "rocmplete.cli._llama_preset_status",
                return_value=(
                    "qwen3.8-27b/Qwen3.8-27B-UD-Q8_K_XL.gguf",
                    Path(directory) / "model.gguf",
                ),
            ), patch(
                "rocmplete.cli.podman.require_rootless"
            ), patch(
                "rocmplete.cli.podman.image_exists", return_value=True
            ), patch(
                "rocmplete.cli.podman.container_exists", return_value=False
            ), patch(
                "rocmplete.cli.podman.run", return_value=0
            ), redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)

        self.assertIn(
            "Configuration: ./rocmplete status llama-cpp --model "
            "qwen3.8-27b-mtp-ud-q8-k-xl",
            output.getvalue(),
        )

    def test_llama_qwen35_mtp_dry_run_uses_current_template_policy(self):
        catalog = load_catalog()
        preset = catalog.llama_preset(
            "qwen3.6-35b-a3b-mtp-ud-q8-k-xl"
        )
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            model = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
        command = output.getvalue()
        self.assertIn("--ctx-size 262144", command)
        self.assertIn("ROCMLETE_LLAMA_SPECULATIVE_TYPE=draft-mtp", command)
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=3", command)
        self.assertIn("ROCMLETE_LLAMA_JINJA=0", command)
        self.assertIn("ROCMLETE_LLAMA_CHAT_TEMPLATE=qwen3.6", command)
        self.assertIn(
            "ROCMLETE_LLAMA_SAMPLING_DEFAULTS="
            + _qwen_sampling_defaults(1.5),
            command,
        )

    def test_llama_router_dry_run_does_not_write_generated_preset(self):
        catalog = load_catalog()
        artifact = catalog.artifact("qwen3-0.6b-q8-gguf")
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            model = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--router",
                    "--models-max",
                    "1",
                    "--profile",
                    "cpu",
                    "--listen",
                    "127.0.0.1",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
            self.assertFalse(
                (
                    data_dir / "apps" / "llama-cpp" / "models.ini"
                ).exists()
            )
            command = output.getvalue()
            self.assertIn("ROCMLETE_LLAMA_ROUTER=1", command)
            self.assertIn("ROCMLETE_LLAMA_MODELS_MAX=1", command)

    def test_llama_router_renders_preset_owned_mtp_settings(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("gemma4-31b-it-q8-0-mtp")
        bundle = catalog.bundle(preset.bundle)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            for identifier in bundle.artifacts:
                artifact = catalog.artifact(identifier)
                path = (
                    data_dir
                    / "content"
                    / "llama-cpp"
                    / "models"
                    / artifact.destination
                )
                path.parent.mkdir(parents=True, exist_ok=True)
                with path.open("wb") as handle:
                    handle.truncate(artifact.size)
                _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(installed, ("gemma4-31b-it-q8-0-mtp",))
        self.assertIn("c = 262144", contents)
        self.assertIn("jinja = true", contents)
        self.assertIn("spec-type = draft-mtp", contents)
        self.assertIn("spec-draft-n-max = 4", contents)
        self.assertIn(
            "model-draft = /content/models/"
            "gemma4-31b-it-q8-mtp/mtp-gemma-4-31B-it-Q8_0.gguf",
            contents,
        )

    def test_llama_router_renders_preset_owned_dflash_settings(self):
        catalog = load_catalog()
        preset = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"
        )
        bundle = catalog.bundle(preset.bundle)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            for identifier in bundle.artifacts:
                artifact = catalog.artifact(identifier)
                path = (
                    data_dir
                    / "content"
                    / "llama-cpp"
                    / "models"
                    / artifact.destination
                )
                path.parent.mkdir(parents=True, exist_ok=True)
                with path.open("wb") as handle:
                    handle.truncate(artifact.size)
                _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
            vulkan_contents, _ = _render_llama_router_preset(
                catalog, data_dir, "vulkan"
            )
        self.assertEqual(
            installed,
            (
                "muse-glimmer-30b-kquant-dynamic-q4-k-xl",
                "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash",
                "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k",
            ),
        )
        base_section = contents.split(
            "[muse-glimmer-30b-kquant-dynamic-q4-k-xl]", 1
        )[1].split(
            "[muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash]", 1
        )[0]
        self.assertNotIn("spec-type", base_section)
        dflash_section = contents.split(
            "[muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash]", 1
        )[1].split(
            "[muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k]", 1
        )[0]
        extended_section = contents.split(
            "[muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k]", 1
        )[1]
        self.assertIn("c = 131072", contents)
        self.assertIn("c = 262144", extended_section)
        self.assertIn("fit = off", extended_section)
        self.assertIn(
            "override-kv = "
            "muse-glimmer.context_length=int:262144,"
            "dflash.context_length=int:262144",
            extended_section,
        )
        self.assertEqual(contents.count("jinja = true"), 3)
        self.assertEqual(
            contents.count(
                "chat-template-file = "
                "/usr/local/share/rocmplete/llama-chat-templates/"
                "muse-glimmer-atem.jinja"
            ),
            3,
        )
        self.assertEqual(contents.count("reasoning-preserve = true"), 3)
        self.assertIn("spec-type = draft-dflash", contents)
        self.assertIn("spec-draft-n-max = 15", dflash_section)
        self.assertIn("spec-draft-n-max = 12", extended_section)
        self.assertIn("spec-draft-n-max = 4", vulkan_contents)
        self.assertNotIn("spec-draft-n-max = 15", vulkan_contents)
        self.assertNotIn("spec-draft-n-max = 12", vulkan_contents)
        self.assertIn(
            "model-draft = /content/models/"
            "muse-glimmer-30b/dflash-kquant.gguf",
            contents,
        )

    def test_llama_router_renders_qwen_managed_template_policy(self):
        catalog = load_catalog()
        identifier = "qwen3.6-35b-a3b-mtp-ud-q8-k-xl"
        preset = catalog.llama_preset(identifier)
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(installed, (identifier,))
        self.assertIn("[{}]".format(identifier), contents)
        self.assertIn("c = 262144", contents)
        self.assertIn(
            "chat-template-file = "
            "/usr/local/share/rocmplete/llama-chat-templates/"
            "qwen3.6.jinja",
            contents,
        )
        self.assertIn("spec-type = draft-mtp", contents)
        self.assertIn("spec-draft-n-max = 3", contents)
        self.assertIn(
            "sampling-defaults-by-reasoning = "
            + _qwen_sampling_defaults(1.5),
            contents,
        )

    def test_llama_router_renders_qwen27_strix_cache_policy(self):
        catalog = load_catalog()
        identifier = "qwen3.6-27b-mtp-q8-0"
        preset = catalog.llama_preset(identifier)
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(installed, (identifier,))
        self.assertIn("spec-draft-n-max = 3", contents)
        self.assertIn(
            "sampling-defaults-by-reasoning = "
            + _qwen_sampling_defaults(),
            contents,
        )
        self.assertIn(
            "rocmplete-flash-attn-strix-halo = on", contents
        )
        self.assertIn(
            "rocmplete-kv-cache-strix-halo = q8_0", contents
        )

    def test_llama_router_renders_qwen38_base_and_embedded_mtp_policy(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("qwen3.8-27b-ud-q8-k-xl")
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(
            installed,
            (
                "qwen3.8-27b-ud-q8-k-xl",
                "qwen3.8-27b-mtp-ud-q8-k-xl",
            ),
        )
        self.assertEqual(contents.count("jinja = true"), 2)
        self.assertEqual(contents.count("reasoning-preserve = true"), 2)
        self.assertEqual(
            contents.count(
                "sampling-defaults-by-reasoning = "
                + _qwen_sampling_defaults()
            ),
            2,
        )
        mtp_section = contents.split(
            "[qwen3.8-27b-mtp-ud-q8-k-xl]", 1
        )[1].split("[gemma4-31b-it-q8-0-mtp]", 1)[0]
        self.assertIn("spec-type = draft-mtp", mtp_section)
        self.assertIn("spec-draft-n-max = 3", mtp_section)
        self.assertNotIn("model-draft", mtp_section)

    def test_llama_router_renders_qwen38_q4_base_and_embedded_mtp_policy(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("qwen3.8-27b-ud-q4-k-xl")
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(
            installed,
            (
                "qwen3.8-27b-ud-q4-k-xl",
                "qwen3.8-27b-mtp-ud-q4-k-xl",
            ),
        )
        self.assertEqual(contents.count("jinja = true"), 2)
        self.assertEqual(contents.count("reasoning-preserve = true"), 2)
        self.assertEqual(
            contents.count(
                "sampling-defaults-by-reasoning = "
                + _qwen_sampling_defaults()
            ),
            2,
        )
        mtp_section = contents.split(
            "[qwen3.8-27b-mtp-ud-q4-k-xl]", 1
        )[1]
        self.assertIn("spec-type = draft-mtp", mtp_section)
        self.assertIn("spec-draft-n-max = 3", mtp_section)
        self.assertNotIn("model-draft", mtp_section)

    def test_llama_router_renders_updated_kat_template_policy(self):
        catalog = load_catalog()
        identifier = "kat-coder-v2.5-dev-q8-0"
        preset = catalog.llama_preset(identifier)
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(installed, (identifier,))
        self.assertIn(
            "chat-template-file = "
            "/usr/local/share/rocmplete/llama-chat-templates/"
            "kat-coder-v2.5.jinja",
            contents,
        )
        self.assertNotIn("reasoning-preserve", contents)

    def test_llama_router_renders_updated_qwen_smoke_template(self):
        catalog = load_catalog()
        identifier = "qwen3-0.6b-q8-0"
        preset = catalog.llama_preset(identifier)
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(installed, (identifier,))
        self.assertIn(
            "chat-template-file = "
            "/usr/local/share/rocmplete/llama-chat-templates/"
            "qwen3-0.6b.jinja",
            contents,
        )
        self.assertNotIn("reasoning-preserve", contents)

    def test_llama_router_renders_manually_prompted_translategemma(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("translategemma-27b-it-q8-0")
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            path = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            path.parent.mkdir(parents=True)
            with path.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, path, artifact)
            contents, installed = _render_llama_router_preset(
                catalog, data_dir
            )
        self.assertEqual(installed, ("translategemma-27b-it-q8-0",))
        self.assertIn("[translategemma-27b-it-q8-0]", contents)
        self.assertIn(
            "chat-template-file = "
            "/usr/local/share/rocmplete/llama-chat-templates/"
            "translategemma-manual.jinja",
            contents,
        )

    def test_llama_translategemma_preset_dry_run_maps_chat_template(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("translategemma-27b-it-q8-0")
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            model = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
        command = output.getvalue()
        self.assertIn("--ctx-size 4096", command)
        self.assertIn(
            "ROCMLETE_LLAMA_CHAT_TEMPLATE=translategemma-manual",
            command,
        )

    def test_llama_forced_context_preset_dry_run_maps_override(self):
        catalog = load_catalog()
        preset = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
        )
        bundle = catalog.bundle(preset.bundle)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            for identifier in bundle.artifacts:
                artifact = catalog.artifact(identifier)
                path = (
                    data_dir
                    / "content"
                    / "llama-cpp"
                    / "models"
                    / artifact.destination
                )
                path.parent.mkdir(parents=True, exist_ok=True)
                with path.open("wb") as handle:
                    handle.truncate(artifact.size)
                _record_managed_file(data_dir, path, artifact)
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
            _, vulkan_arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--backend",
                    "vulkan",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as vulkan_output:
                self.assertEqual(command_run(vulkan_arguments), 0)
            _, zero_context = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--context",
                    "0",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with self.assertRaisesRegex(
                LauncherError, "cannot disable the managed context"
            ):
                command_run(zero_context)
            _, smaller_context = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--context",
                    "196608",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as smaller_output:
                self.assertEqual(command_run(smaller_context), 0)
        command = output.getvalue()
        self.assertIn(
            "ROCMLETE_LLAMA_CONTEXT_OVERRIDE="
            "muse-glimmer.context_length=int:262144,"
            "dflash.context_length=int:262144",
            command,
        )
        self.assertIn("--ctx-size 262144", command)
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=12", command)
        self.assertIn(
            "ROCMLETE_LLAMA_DRAFT_TOKENS=4", vulkan_output.getvalue()
        )
        self.assertIn("ROCMLETE_LLAMA_JINJA=0", command)
        self.assertIn(
            "ROCMLETE_LLAMA_CHAT_TEMPLATE=muse-glimmer-atem", command
        )
        self.assertIn(
            "ROCMLETE_LLAMA_REASONING_PRESERVE=1", command
        )
        self.assertIn(
            "ROCMLETE_LLAMA_CONTEXT_OVERRIDE="
            "muse-glimmer.context_length=int:196608,"
            "dflash.context_length=int:196608",
            smaller_output.getvalue(),
        )
        self.assertIn("--ctx-size 196608", smaller_output.getvalue())

    def test_llama_mtp_preset_dry_run_maps_target_and_draft(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("gemma4-31b-it-q8-0-mtp")
        bundle = catalog.bundle(preset.bundle)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            for identifier in bundle.artifacts:
                artifact = catalog.artifact(identifier)
                path = (
                    data_dir
                    / "content"
                    / "llama-cpp"
                    / "models"
                    / artifact.destination
                )
                path.parent.mkdir(parents=True, exist_ok=True)
                with path.open("wb") as handle:
                    handle.truncate(artifact.size)
                _record_managed_file(data_dir, path, artifact)
            _, arguments = parse_arguments(
                [
                    "run",
                    "llama-cpp",
                    "server",
                    "--preset",
                    preset.identifier,
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_run(arguments), 0)
        command = output.getvalue()
        self.assertIn(
            "ROCMLETE_LLAMA_DRAFT_MODEL=/content/models/"
            "gemma4-31b-it-q8-mtp/mtp-gemma-4-31B-it-Q8_0.gguf",
            command,
        )
        self.assertIn("ROCMLETE_LLAMA_SPECULATIVE_TYPE=draft-mtp", command)
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=4", command)
        self.assertIn("ROCMLETE_LLAMA_JINJA=1", command)
        self.assertIn("--ctx-size 262144", command)

    def test_llama_benchmark_dry_run_does_not_create_data_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "model.gguf"
            model.write_bytes(b"small test model")
            data_dir = root / "not-created"
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "llama-cpp",
                    "--model",
                    str(model),
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                    "--prompt-tokens",
                    "32",
                    "--generation-tokens",
                    "16",
                    "--context-depth",
                    "32768",
                    "--batch-size",
                    "1024",
                    "--ubatch-size",
                    "256",
                    "--cache-type-k",
                    "q8_0",
                    "--cache-type-v",
                    "q8_0",
                    "--flash-attn",
                    "on",
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_benchmark(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())
            command = output.getvalue()
            self.assertIn("--network none", command)
            self.assertIn("ROCMLETE_LLAMA_MODE=bench", command)
            self.assertIn("--n-prompt 32", command)
            self.assertIn("--n-gen 16", command)
            self.assertIn("--n-depth 32768", command)
            self.assertIn("--batch-size 1024", command)
            self.assertIn("--ubatch-size 256", command)
            self.assertIn("--cache-type-k q8_0", command)
            self.assertIn("--cache-type-v q8_0", command)
            self.assertIn("--flash-attn on", command)
            self.assertNotIn("/dev/kfd", command)

    def test_llama_benchmark_rejects_quantized_v_without_flash_attention(self):
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory) / "model.gguf"
            model.write_bytes(b"small test model")
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "llama-cpp",
                    "--model",
                    str(model),
                    "--profile",
                    "cpu",
                    "--cache-type-v",
                    "q8_0",
                    "--flash-attn",
                    "auto",
                    "--dry-run",
                ]
            )
            with self.assertRaisesRegex(
                LauncherError, "requires --flash-attn on"
            ):
                command_benchmark(arguments, load_catalog())

    def test_llama_backend_comparison_dry_run_resolves_both_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "model.gguf"
            model.write_bytes(b"small test model")
            data_dir = root / "not-created"
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "llama-cpp",
                    "--model",
                    str(model),
                    "--profile",
                    "strix-halo",
                    "--data-dir",
                    str(data_dir),
                    "--compare-backends",
                    "--dry-run",
                ]
            )
            with ExitStack() as stack:
                stack.enter_context(
                    patch(
                        "rocmplete.cli.select_render_nodes",
                        return_value=("/dev/dri/renderD128",),
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.check_gpu_device_access")
                )
                output = stack.enter_context(
                    redirect_stdout(io.StringIO())
                )
                self.assertEqual(
                    command_benchmark(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())
        command = output.getvalue()
        self.assertIn("Backends: ROCm, Vulkan", command)
        self.assertEqual(command.count("Resolved command:"), 2)
        self.assertIn("ROCMLETE_LLAMA_BACKEND=rocm", command)
        self.assertIn("ROCMLETE_LLAMA_BACKEND=vulkan", command)
        self.assertEqual(command.count("No container was started."), 1)

    def test_llama_backend_comparison_continues_after_one_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "model.gguf"
            model.write_bytes(b"small test model")
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "llama-cpp",
                    "--model",
                    str(model),
                    "--profile",
                    "strix-halo",
                    "--data-dir",
                    str(root / "data"),
                    "--compare-backends",
                ]
            )
            vulkan_result = root / "vulkan.json"
            comparison_result = root / "comparison.json"
            with ExitStack() as stack:
                stack.enter_context(
                    patch("rocmplete.cli.podman.require_rootless")
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.select_render_nodes",
                        return_value=("/dev/dri/renderD128",),
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.check_gpu_device_access")
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.image_exists",
                        return_value=True,
                    )
                )
                run_benchmark = stack.enter_context(
                    patch(
                        "rocmplete.cli.run_llama_benchmark",
                        side_effect=(
                            LauncherError("ROCm initialization failed"),
                            vulkan_result,
                        ),
                    )
                )
                write_comparison = stack.enter_context(
                    patch(
                        "rocmplete.cli.write_backend_comparison",
                        return_value=(
                            comparison_result,
                            {"comparison": None},
                        ),
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli._print_llama_backend_comparison"
                    )
                )
                stack.enter_context(redirect_stdout(io.StringIO()))
                stack.enter_context(redirect_stderr(io.StringIO()))
                self.assertEqual(
                    command_benchmark(arguments, load_catalog()), 1
                )
        self.assertEqual(run_benchmark.call_count, 2)
        self.assertEqual(
            [call.kwargs["backend"] for call in run_benchmark.call_args_list],
            ["rocm", "vulkan"],
        )
        self.assertEqual(
            write_comparison.call_args.kwargs["results"],
            {"vulkan": vulkan_result},
        )
        self.assertIn(
            "ROCm initialization failed",
            write_comparison.call_args.kwargs["errors"]["rocm"],
        )

    def test_llama_backend_comparison_rejects_cpu_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory) / "model.gguf"
            model.write_bytes(b"small test model")
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "llama-cpp",
                    "--model",
                    str(model),
                    "--profile",
                    "cpu",
                    "--compare-backends",
                    "--dry-run",
                ]
            )
            with self.assertRaisesRegex(
                LauncherError, "requires a GPU profile"
            ):
                command_benchmark(arguments, load_catalog())

    def test_llama_benchmark_rejects_speculative_presets(self):
        for preset, speculative_type in (
            ("qwen3.6-27b-mtp-q8-0", "draft-mtp"),
            (
                "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash",
                "draft-dflash",
            ),
        ):
            with self.subTest(preset=preset):
                _, arguments = parse_arguments(
                    [
                        "benchmark",
                        "llama-cpp",
                        "--preset",
                        preset,
                        "--profile",
                        "cpu",
                        "--dry-run",
                    ]
                )
                with self.assertRaisesRegex(
                    LauncherError,
                    ".*{}.*llama-bench does not exercise".format(
                        speculative_type
                    ),
                ):
                    command_benchmark(arguments, load_catalog())

    def test_llama_speculative_benchmark_dry_run_is_checkpointed_server_plan(self):
        catalog = load_catalog()
        preset = catalog.llama_preset(
            "qwen3.8-27b-mtp-ud-q8-k-xl"
        )
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            model = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "llama-cpp-speculative",
                    "--preset",
                    preset.identifier,
                    "--draft-depth",
                    "2",
                    "--draft-depth",
                    "3",
                    "--context-depth",
                    "4096",
                    "--context",
                    "8192",
                    "--generation-tokens",
                    "64",
                    "--repetitions",
                    "1",
                    "--thinking",
                    "medium",
                    "--draft-probability-min",
                    "0.5",
                    "--draft-backend-sampling",
                    "off",
                    "--ngram-simple",
                    "--graph-optimization",
                    "--disable-graphs",
                    "--poll",
                    "0",
                    "--no-host",
                    "--flash-attn",
                    "on",
                    "--cache-type-k",
                    "q8_0",
                    "--cache-type-v",
                    "q8_0",
                    "--batch-size",
                    "2048",
                    "--ubatch-size",
                    "1024",
                    "--profile",
                    "strix-halo",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with ExitStack() as stack:
                stack.enter_context(
                    patch(
                        "rocmplete.cli.select_render_nodes",
                        return_value=("/dev/dri/renderD128",),
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.check_gpu_device_access")
                )
                output = stack.enter_context(redirect_stdout(io.StringIO()))
                self.assertEqual(
                    command_benchmark(arguments, catalog), 0
                )
            self.assertFalse(
                (data_dir / "apps" / "llama-cpp").exists()
            )
        command = output.getvalue()
        self.assertIn("2 requests", command)
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=2", command)
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=3", command)
        self.assertIn(
            "--name rocmplete-llama-speculative-benchmark", command
        )
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.role=benchmark", command
        )
        self.assertIn("--parallel 1", command)
        self.assertIn("--spec-draft-p-min 0.5", command)
        self.assertIn("--no-spec-draft-backend-sampling", command)
        self.assertIn("--spec-type ngram-simple", command)
        self.assertIn("--env GGML_CUDA_GRAPH_OPT=1", command)
        self.assertIn("--env GGML_CUDA_DISABLE_GRAPHS=1", command)
        self.assertIn("--poll 0", command)
        self.assertIn("--no-host", command)
        self.assertIn("ROCMLETE_LLAMA_FLASH_ATTN_STRIX_HALO=on", command)
        self.assertIn("ROCMLETE_LLAMA_KV_CACHE_STRIX_HALO=q8_0", command)
        self.assertIn("--batch-size 2048", command)
        self.assertIn("--ubatch-size 1024", command)
        self.assertNotIn("podman run --rm", command)
        self.assertIn("no checkpoint was written", command)

    def test_llama_speculative_benchmark_requires_speculative_preset(self):
        _, arguments = parse_arguments(
            [
                "benchmark",
                "llama-cpp-speculative",
                "--preset",
                "qwen3.8-27b-ud-q8-k-xl",
                "--profile",
                "strix-halo",
                "--dry-run",
            ]
        )
        with self.assertRaisesRegex(
            LauncherError, "does not enable speculative decoding"
        ):
            command_benchmark(arguments, load_catalog())

    def test_real_run_resolution_prepares_data_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "prepared"
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            options = resolve_run_options(arguments)
            self.assertEqual(options.data_dir, data_dir.resolve())
            self.assertTrue(data_dir.is_dir())

    def test_content_subcommands_parse(self):
        _, content = parse_arguments(
            [
                "content",
                "install",
                "qwen-image-2512-fp8-base",
                "--dry-run",
            ]
        )
        self.assertEqual(content.command, "content")
        self.assertEqual(content.content_command, "install")
        self.assertTrue(content.dry_run)

        _, status = parse_arguments(
            ["content", "status", "family", "qwen", "--verify"]
        )
        self.assertEqual(status.content_command, "status")
        self.assertEqual(status.target, "family")
        self.assertEqual(status.selection, "qwen")
        self.assertTrue(status.verify)

        _, models = parse_arguments(
            [
                "content",
                "list",
                "--models",
                "--application",
                "llama-cpp",
                "--details",
                "--scan",
                "/srv/models",
            ]
        )
        self.assertEqual(models.content_command, "list")
        self.assertTrue(models.models)
        self.assertEqual(models.application, "llama-cpp")
        self.assertTrue(models.details)
        self.assertEqual(models.scan, [Path("/srv/models")])

        _, workflows = parse_arguments(
            [
                "content",
                "workflows",
                "install",
                "qwen-image-2512-fp8-base",
                "--force",
            ]
        )
        self.assertEqual(workflows.workflows_command, "install")
        self.assertTrue(workflows.force)

        _, install = parse_arguments(
            [
                "content",
                "install",
                "wan-2.2-t2v-14b-fp8-base",
                "--acknowledge-license-risk",
            ]
        )
        self.assertTrue(install.acknowledge_license_risk)

        _, benchmark = parse_arguments(
            [
                "benchmark",
                "run",
                "ltx-2-t2v-19b-fp8-full",
                "--accept-license",
                "--runs",
                "3",
            ]
        )
        self.assertEqual(benchmark.runs, 3)
        self.assertEqual(benchmark.cache_mode, "persistent")
        self.assertTrue(benchmark.accept_license)
        self.assertEqual(
            benchmark.bundle, "ltx-2-t2v-19b-fp8-full"
        )
        self.assertEqual(benchmark.benchmark_command, "run")

        _, suite = parse_arguments(
            [
                "benchmark",
                "suite",
                "--family",
                "qwen",
                "--keep-going",
                "--report-format",
                "markdown",
            ]
        )
        self.assertEqual(suite.benchmark_command, "suite")
        self.assertEqual(suite.family, "qwen")
        self.assertTrue(suite.keep_going)

        _, agent = parse_arguments(
            [
                "benchmark",
                "agent",
                "--preset",
                "qwen3.6-27b-q8-0",
                "--task",
                "re-align",
                "--task",
                "fz-eintr",
                "--repetitions",
                "3",
                "--thinking",
                "high",
            ]
        )
        self.assertEqual(agent.benchmark_command, "agent")
        self.assertEqual(agent.preset, "qwen3.6-27b-q8-0")
        self.assertEqual(agent.task, ["re-align", "fz-eintr"])
        self.assertEqual(agent.repetitions, 3)
        self.assertIsNone(agent.context)
        self.assertEqual(agent.thinking, "high")
        self.assertFalse(hasattr(agent, "normalized_comparison"))

        _, isolated = parse_arguments(
            ["benchmark", "suite", "--cache-mode", "isolated"]
        )
        self.assertEqual(isolated.cache_mode, "isolated")

        _, report = parse_arguments(
            [
                "benchmark",
                "report",
                "suite.json",
                "--report-format",
                "html",
            ]
        )
        self.assertEqual(report.subject, "suite.json")

        _, llama = parse_arguments(
            [
                "benchmark",
                "llama-cpp",
                "--preset",
                "qwen3-0.6b-q8-0",
                "--backend",
                "vulkan",
                "--repetitions",
                "3",
            ]
        )
        self.assertEqual(llama.benchmark_command, "llama-cpp")
        self.assertEqual(llama.preset, "qwen3-0.6b-q8-0")
        self.assertEqual(llama.backend, "vulkan")
        self.assertEqual(llama.repetitions, 3)
        self.assertEqual(report.report_format, "html")

        _, interactive = parse_arguments(
            ["content", "install", "--interactive"]
        )
        self.assertIsNone(interactive.target)
        self.assertTrue(interactive.interactive)

    def test_content_without_command_prints_copyable_examples(self):
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(main(["content"]), 2)
        text = output.getvalue()
        self.assertIn(
            "error: choose list, status, install, import, or "
            "workflows",
            text,
        )
        self.assertIn("./rocmplete content list", text)
        self.assertIn("./rocmplete content install comfyui image", text)
        self.assertIn("./rocmplete content status comfyui image", text)
        self.assertIn("./rocmplete content list --models", text)

    def test_content_import_parses_explicit_noninteractive_choices(self):
        _, arguments = parse_arguments(
            [
                "content",
                "import",
                "https://civitai.red/models/123",
                "--version",
                "456",
                "--file",
                "789",
                "--as",
                "comfyui:checkpoint",
                "--save-pack",
                "/tmp/model.json",
                "--non-interactive",
                "--acknowledge-license-risk",
            ]
        )
        self.assertEqual(arguments.content_command, "import")
        self.assertEqual(arguments.version, 456)
        self.assertEqual(arguments.file, "789")
        self.assertEqual(arguments.import_kind, "comfyui:checkpoint")
        self.assertEqual(arguments.save_pack, Path("/tmp/model.json"))
        self.assertTrue(arguments.non_interactive)

        _, guided = parse_arguments(["content", "import"])
        self.assertIsNone(guided.url)

    @patch(
        "builtins.input",
        return_value="https://huggingface.co/owner/repository",
    )
    @patch("rocmplete.cli.sys.stdin")
    def test_bare_content_import_prompts_for_url(self, stdin, user_input):
        stdin.isatty.return_value = True
        _, arguments = parse_arguments(["content", "import"])
        self.assertEqual(
            _remote_import_url(arguments),
            "https://huggingface.co/owner/repository",
        )
        self.assertEqual(
            arguments.url,
            "https://huggingface.co/owner/repository",
        )
        self.assertEqual(
            user_input.call_args.args[0],
            "Civitai or Hugging Face URL: ",
        )

    def test_bare_noninteractive_content_import_requires_url(self):
        _, arguments = parse_arguments(
            ["content", "import", "--non-interactive"]
        )
        with self.assertRaisesRegex(LauncherError, "requires URL"):
            _remote_import_url(arguments)

    @patch("builtins.input", side_effect=("n", "1"))
    @patch("rocmplete.cli.sys.stdin")
    def test_guided_import_can_override_detected_destination(
        self, stdin, user_input
    ):
        stdin.isatty.return_value = True
        _, arguments = parse_arguments(
            [
                "content",
                "import",
                "https://civitai.red/models/123?modelVersionId=456",
            ]
        )
        discovery = RemoteDiscovery(
            provider="civitai",
            source_url=arguments.url,
            title="Example LoRA",
            repository="civitai.red/models/123",
            revision="456",
            files=(),
            declared_license="unknown",
            model_type="LORA",
        )
        file = RemoteFile(
            identifier="789",
            name="example.safetensors",
            size=2048,
            sha256="d" * 64,
        )
        with redirect_stdout(io.StringIO()) as output:
            kind = _remote_import_kind(arguments, discovery, file)
        self.assertEqual(kind.identifier, "comfyui:checkpoint")
        text = output.getvalue()
        self.assertIn("Install as:", text)
        checkpoint = next(
            line for line in text.splitlines()
            if "ComfyUI checkpoint" in line
        )
        diffusion = next(
            line for line in text.splitlines()
            if "ComfyUI diffusion model" in line
        )
        self.assertEqual(
            checkpoint.index("ComfyUI checkpoint"),
            diffusion.index("ComfyUI diffusion model"),
        )

    @patch("builtins.input", return_value="2")
    @patch("rocmplete.cli.sys.stdin")
    def test_civitai_checkpoint_offers_only_matching_loaders(
        self, stdin, user_input
    ):
        stdin.isatty.return_value = True
        _, arguments = parse_arguments(
            [
                "content",
                "import",
                "https://civitai.com/models/123?modelVersionId=456",
            ]
        )
        discovery = RemoteDiscovery(
            provider="civitai",
            source_url=arguments.url,
            title="Example Checkpoint",
            repository="civitai.com/models/123",
            revision="456",
            files=(),
            declared_license="unknown",
            model_type="Checkpoint",
        )
        file = RemoteFile(
            identifier="789",
            name="example.safetensors",
            size=2048,
            sha256="d" * 64,
        )
        with redirect_stdout(io.StringIO()) as output:
            kind = _remote_import_kind(arguments, discovery, file)
        self.assertEqual(kind.identifier, "comfyui:diffusion-model")
        text = output.getvalue()
        self.assertIn("Civitai describes this as a checkpoint", text)
        self.assertIn("CheckpointLoader", text)
        self.assertIn("UNETLoader", text)
        self.assertNotIn("comfyui:lora", text)
        self.assertNotIn("comfyui:vae", text)

    def test_noninteractive_civitai_checkpoint_names_two_choices(self):
        _, arguments = parse_arguments(
            [
                "content",
                "import",
                "https://civitai.com/models/123?modelVersionId=456",
                "--non-interactive",
            ]
        )
        discovery = RemoteDiscovery(
            provider="civitai",
            source_url=arguments.url,
            title="Example Checkpoint",
            repository="civitai.com/models/123",
            revision="456",
            files=(),
            declared_license="unknown",
            model_type="Checkpoint",
        )
        file = RemoteFile(
            identifier="789",
            name="example.safetensors",
            size=2048,
            sha256="d" * 64,
        )
        with self.assertRaisesRegex(
            LauncherError,
            "choose comfyui:checkpoint, comfyui:diffusion-model",
        ) as raised:
            _remote_import_kind(arguments, discovery, file)
        self.assertNotIn("comfyui:lora", str(raised.exception))

    def test_unsupported_civitai_type_refuses_guessing(self):
        _, arguments = parse_arguments(
            [
                "content",
                "import",
                "https://civitai.com/models/123?modelVersionId=456",
            ]
        )
        discovery = RemoteDiscovery(
            provider="civitai",
            source_url=arguments.url,
            title="Embedding",
            repository="civitai.com/models/123",
            revision="456",
            files=(),
            declared_license="unknown",
            model_type="TextualInversion",
        )
        file = RemoteFile(
            identifier="789",
            name="embedding.safetensors",
            size=2048,
            sha256="d" * 64,
        )
        with self.assertRaisesRegex(
            LauncherError,
            "does not map safely to a supported destination",
        ):
            _remote_import_kind(arguments, discovery, file)

    @patch("rocmplete.cli.save_pack")
    @patch("rocmplete.cli._command_content_install", return_value=0)
    @patch("rocmplete.cli.discover_remote")
    def test_content_import_dry_run_validates_without_saving(
        self, discover, install, save
    ):
        discover.return_value = RemoteDiscovery(
            provider="huggingface",
            source_url=(
                "https://huggingface.co/owner/repo/blob/"
                + "b" * 40
                + "/models/example.gguf"
            ),
            title="owner/repo",
            repository="owner/repo",
            revision="b" * 40,
            files=(
                RemoteFile(
                    identifier="models/example.gguf",
                    name="example.gguf",
                    size=4096,
                    sha256="c" * 64,
                    primary=True,
                ),
            ),
            declared_license="apache-2.0",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pack = root / "saved" / "example.json"
            data = root / "data"
            _, arguments = parse_arguments(
                [
                    "content",
                    "import",
                    (
                        "https://huggingface.co/owner/repo/blob/"
                        "main/models/example.gguf"
                    ),
                    "--save-pack",
                    str(pack),
                    "--data-dir",
                    str(data),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_import(arguments, load_catalog()), 0
                )
            self.assertFalse(pack.exists())
            self.assertFalse(data.exists())
        save.assert_called_once_with(pack, ANY, dry_run=True)
        install_arguments = install.call_args.args[0]
        self.assertTrue(install_arguments.dry_run)
        self.assertNotEqual(install_arguments.from_file, [pack])
        self.assertEqual(install_arguments.display_pack_paths, [pack])
        text = output.getvalue()
        self.assertIn("Remote import:", text)
        self.assertIn("example.gguf", text)
        self.assertIn("not saved by dry run", text)

    @patch("rocmplete.cli._command_content_install")
    @patch("rocmplete.cli.discover_remote")
    def test_content_import_saves_reusable_pack_and_installs_it(
        self, discover, install
    ):
        def install_after_confirmation(
            arguments, catalog, before_mutation=None
        ):
            self.assertIsNotNone(before_mutation)
            before_mutation()
            return 0

        install.side_effect = install_after_confirmation
        discover.return_value = RemoteDiscovery(
            provider="civitai",
            source_url=(
                "https://civitai.red/models/123?modelVersionId=456"
            ),
            title="Example LoRA",
            repository="civitai.red/models/123",
            revision="456",
            files=(
                RemoteFile(
                    identifier="789",
                    name="example.safetensors",
                    size=2048,
                    sha256="d" * 64,
                    primary=True,
                    download_url=(
                        "https://civitai.red/api/download/models/456"
                    ),
                ),
            ),
            declared_license="Civitai model-page permissions",
            model_type="LORA",
            provider_host="civitai.red",
            model_id=123,
            model_version_id=456,
            requires_auth=True,
        )
        with tempfile.TemporaryDirectory() as directory:
            pack = Path(directory) / "imports" / "example.json"
            _, arguments = parse_arguments(
                [
                    "content",
                    "import",
                    (
                        "https://civitai.red/models/123"
                        "?modelVersionId=456"
                    ),
                    "--save-pack",
                    str(pack),
                    "--non-interactive",
                    "--acknowledge-license-risk",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_import(arguments, load_catalog()), 0
                )
            saved = json.loads(pack.read_text())
        self.assertIn("artifacts", saved)
        install_arguments = install.call_args.args[0]
        self.assertNotEqual(install_arguments.from_file, [pack])
        self.assertEqual(install_arguments.display_pack_paths, [pack])
        self.assertTrue(install_arguments.non_interactive)
        self.assertTrue(install_arguments.acknowledge_license_risk)
        self.assertIn("Saved local pack:", output.getvalue())
        self.assertIn("./rocmplete run comfyui", output.getvalue())

    @patch("builtins.input", side_effect=("", "n"))
    @patch("rocmplete.cli.sys.stdin")
    @patch("rocmplete.cli.discover_remote")
    def test_content_import_decline_does_not_save_pack(
        self, discover, stdin, user_input
    ):
        stdin.isatty.return_value = True
        discover.return_value = RemoteDiscovery(
            provider="civitai",
            source_url=(
                "https://civitai.red/models/123?modelVersionId=456"
            ),
            title="Example LoRA",
            repository="civitai.red/models/123",
            revision="456",
            files=(
                RemoteFile(
                    identifier="789",
                    name="example.safetensors",
                    size=2048,
                    sha256="d" * 64,
                    primary=True,
                    download_url=(
                        "https://civitai.red/api/download/models/456"
                    ),
                ),
            ),
            declared_license="Civitai model-page permissions",
            model_type="LORA",
            provider_host="civitai.red",
            model_id=123,
            model_version_id=456,
            requires_auth=True,
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pack = root / "imports" / "example.json"
            _, arguments = parse_arguments(
                [
                    "content",
                    "import",
                    (
                        "https://civitai.red/models/123"
                        "?modelVersionId=456"
                    ),
                    "--save-pack",
                    str(pack),
                    "--data-dir",
                    str(root / "data"),
                ]
            )
            with redirect_stdout(io.StringIO()):
                with redirect_stderr(io.StringIO()):
                    with self.assertRaisesRegex(
                        LauncherError, "acknowledgment declined"
                    ):
                        _command_content_import(
                            arguments, load_catalog()
                        )
            self.assertFalse(pack.exists())

    @patch("rocmplete.cli.discover_remote")
    def test_content_import_noninteractive_ambiguity_requires_file(
        self, discover
    ):
        discover.return_value = RemoteDiscovery(
            provider="huggingface",
            source_url="https://huggingface.co/owner/repo",
            title="owner/repo",
            repository="owner/repo",
            revision="b" * 40,
            files=(
                RemoteFile("one", "one.safetensors", 1, "a" * 64),
                RemoteFile("two", "two.safetensors", 1, "b" * 64),
            ),
            declared_license="unknown",
        )
        _, arguments = parse_arguments(
            [
                "content",
                "import",
                "https://huggingface.co/owner/repo",
                "--non-interactive",
            ]
        )
        with self.assertRaisesRegex(LauncherError, "--file FILE"):
            _command_content_import(arguments, load_catalog())

    def test_legacy_content_commands_are_removed(self):
        for command in ("setup", "bundles"):
            with self.subTest(command=command):
                with redirect_stderr(io.StringIO()):
                    with self.assertRaises(SystemExit) as result:
                        parse_arguments([command])
                self.assertEqual(result.exception.code, 2)

    def test_doctor_help_has_no_gpu_opt_in(self):
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaises(SystemExit) as result:
                parse_arguments(["doctor", "--help"])
        self.assertEqual(result.exception.code, 0)
        self.assertNotIn("--gpu", output.getvalue())

    def test_content_list_defaults_to_practical_application_recipes(self):
        _, arguments = parse_arguments(["content", "list"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_content(arguments, load_catalog()), 0)
        text = output.getvalue()
        self.assertIn("comfyui image  1  bundle", text)
        self.assertIn("comfyui edit   1  bundle", text)
        self.assertIn("comfyui t2v    1  bundle", text)
        self.assertIn("comfyui i2v    1  bundle", text)
        self.assertNotIn("comfyui all", text)
        self.assertIn("llama-cpp qwen3.6", text)
        self.assertIn("llama-cpp qwen3.8", text)
        self.assertNotIn("llama-cpp ornith", text)
        self.assertIn("llama-cpp kat-coder", text)
        self.assertNotIn("llama-cpp assistant", text)
        self.assertNotIn("llama-cpp agent ", text)
        self.assertNotIn("llama-cpp laguna-s-2.1", text)
        self.assertNotIn("llama-cpp laguna-xs-2.1", text)
        self.assertIn("llama-cpp muse-glimmer", text)
        self.assertIn("llama-cpp translation-hy", text)
        self.assertIn("llama-cpp translation-gemma", text)
        self.assertIn("llama-cpp shisa-v2.1", text)
        self.assertNotIn("Exact bundles:", text)
        self.assertNotIn("family qwen", text)
        self.assertIn("content list --models", text)
        self.assertIn("content list --bundles", text)
        self.assertIn("content list --families", text)
        self.assertIn("content install APPLICATION all", text)
        self.assertNotIn("recommended", text)
        llama_lines = tuple(
            line
            for line in text.splitlines()
            if line.strip().startswith("llama-cpp ")
        )
        count_offsets = {
            line.index("  2  bundles")
            if "  2  bundles" in line
            else line.index("  1  bundle")
            for line in llama_lines
        }
        self.assertEqual(len(count_offsets), 1)
        for removed in ("starter", "gemma4", "mtp", "large"):
            self.assertNotIn("llama-cpp {}".format(removed), text)

    def test_content_list_exposes_exact_bundles_only_when_requested(self):
        _, arguments = parse_arguments(["content", "list", "--bundles"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_content(arguments, load_catalog()), 0)
        text = output.getvalue()
        self.assertIn("Exact bundles:", text)
        self.assertIn("qwen-image-2512-fp8-lightning", text)
        self.assertIn("llama-qwen3.6-27b-q8-0", text)
        self.assertIn("llama-qwen3.6-27b-mtp-q8-0", text)
        self.assertNotIn("Applications:", text)

        _, arguments = parse_arguments(
            ["content", "list", "--bundles", "--application", "llama-cpp"]
        )
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_content(arguments, load_catalog()), 0)
        text = output.getvalue()
        self.assertIn("llama.cpp bundles:", text)
        self.assertIn("llama-qwen3.6-27b-q8-0", text)
        self.assertIn("llama-qwen3.6-27b-mtp-q8-0", text)
        self.assertNotIn("qwen-image-2512-fp8-lightning", text)

    def test_content_list_exposes_aggregates_only_when_requested(self):
        _, arguments = parse_arguments(["content", "list", "--families"])
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(command_content(arguments, load_catalog()), 0)
        text = output.getvalue()
        self.assertIn("family qwen   8  bundles", text)
        self.assertIn("all  51  bundles", text)
        self.assertNotIn("Exact bundles:", text)

    def test_content_list_application_filter_requires_filterable_view(self):
        _, arguments = parse_arguments(
            ["content", "list", "--application", "comfyui"]
        )
        with self.assertRaisesRegex(
            LauncherError, "requires --bundles or --models"
        ):
            command_content(arguments, load_catalog())

    def test_content_list_model_options_require_model_view(self):
        _, arguments = parse_arguments(
            ["content", "list", "--details"]
        )
        with self.assertRaisesRegex(LauncherError, "require --models"):
            command_content(arguments, load_catalog())

    def test_content_models_subcommand_is_removed(self):
        with redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as result:
                parse_arguments(["content", "models"])
        self.assertEqual(result.exception.code, 2)

    def test_content_status_accepts_a_selection(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "content",
                    "status",
                    "family",
                    "qwen",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, load_catalog()), 1
                )
            self.assertFalse(data_dir.exists())
        text = output.getvalue()
        self.assertIn("missing    qwen-image-2512-fp8-base", text)

    def test_content_list_models_finds_loose_ggufs_without_creating_data(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            external = Path(directory) / "external"
            external.mkdir()
            model = external / "manual.gguf"
            model.write_bytes(b"model")
            _, arguments = parse_arguments(
                [
                    "content",
                    "list",
                    "--models",
                    "--data-dir",
                    str(data_dir),
                    "--scan",
                    str(external),
                ]
            )

            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, load_catalog()), 0
                )

            self.assertFalse(data_dir.exists())
            text = output.getvalue()
            self.assertIn("llama.cpp models", text)
            self.assertIn("local", text)
            self.assertIn(str(model), text)

    def test_content_list_models_shows_all_managed_applications(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "content",
                    "list",
                    "--models",
                    "--data-dir",
                    str(data_dir),
                ]
            )

            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, load_catalog()), 0
                )

            self.assertFalse(data_dir.exists())
            text = output.getvalue()
            self.assertIn("missing", text)
            self.assertIn("qwen3.6-27b-mtp-q8-0", text)
            self.assertIn("DwarfStar models", text)
            self.assertIn(
                "dwarfstar-deepseek-v4-flash-0731-q2-imatrix", text
            )
            translate_line = next(
                line
                for line in text.splitlines()
                if "translategemma-27b-it-q8-0" in line
            )
            self.assertTrue(
                translate_line.endswith("translategemma-27b-it-q8-0")
            )
            self.assertNotIn(
                "qwen3.6-27b-mtp/Qwen3.6-27B-Q8_0.gguf", text
            )

    def test_content_list_models_can_select_only_dwarfstar(self):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "list",
                    "--models",
                    "--application",
                    "dwarfstar",
                    "--data-dir",
                    str(Path(directory) / "not-created"),
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, load_catalog()), 0
                )
        text = output.getvalue()
        self.assertIn("DwarfStar models", text)
        self.assertNotIn("llama.cpp models", text)
        self.assertNotIn("--preset", text)

    def test_content_list_models_reports_verified_dwarfstar_model_ready(self):
        catalog = load_catalog()
        bundle = catalog.bundle(
            "dwarfstar-deepseek-v4-flash-0731-q2-imatrix"
        )
        artifact = catalog.artifact(bundle.artifacts[0])
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "data"
            model = (
                data_dir
                / "content"
                / "dwarfstar"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "content",
                    "list",
                    "--models",
                    "--application",
                    "dwarfstar",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(command_content(arguments, catalog), 0)
        self.assertIn(
            "ready    80.76 GiB  dwarfstar-deepseek-v4-flash-0731-q2-imatrix",
            output.getvalue(),
        )

    def test_content_list_models_rejects_scan_for_dwarfstar_only(self):
        _, arguments = parse_arguments(
            [
                "content",
                "list",
                "--models",
                "--application",
                "dwarfstar",
                "--scan",
                "/tmp/models",
            ]
        )
        with self.assertRaisesRegex(LauncherError, "only to llama.cpp"):
            command_content(arguments, load_catalog())

    def test_content_list_model_details_show_managed_runtime_policy(self):
        catalog = load_catalog()
        preset = catalog.llama_preset("qwen3-0.6b-q8-0")
        artifact = catalog.artifact(preset.artifact)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "data"
            model = (
                data_dir
                / "content"
                / "llama-cpp"
                / "models"
                / artifact.destination
            )
            model.parent.mkdir(parents=True)
            with model.open("wb") as handle:
                handle.truncate(artifact.size)
            _record_managed_file(data_dir, model, artifact)
            _, arguments = parse_arguments(
                [
                    "content",
                    "list",
                    "--models",
                    "--data-dir",
                    str(data_dir),
                    "--details",
                ]
            )

            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, catalog),
                    0,
                )

        text = output.getvalue()
        self.assertIn("Managed preset details", text)
        self.assertIn("qwen3-0.6b-q8-0", text)
        self.assertIn("llama-qwen3-0.6b-q8-0", text)
        self.assertIn("Catalog size", text)
        self.assertIn("609.82 MiB", text)
        self.assertIn("Files", text)
        self.assertIn("Default context", text)
        self.assertIn("4096 tokens", text)
        self.assertIn("model metadata; Jinja enabled", text)
        self.assertIn("Speculation", text)
        self.assertIn("Reasoning", text)
        self.assertIn("not exposed", text)
        self.assertIn("Flash Attention", text)
        self.assertIn("K/V cache", text)
        self.assertIn("llama.cpp default", text)

    def test_content_model_policy_details_cover_special_presets(self):
        catalog = load_catalog()
        embedded_mtp = catalog.llama_preset(
            "qwen3.6-27b-mtp-q8-0"
        )
        separate_draft = catalog.llama_preset(
            "gemma4-31b-it-q8-0-mtp"
        )
        dflash = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"
        )
        dflash_256k = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
        )
        qwen27 = catalog.llama_preset("qwen3.6-27b-mtp-q8-0")
        translate = catalog.llama_preset("translategemma-27b-it-q8-0")

        self.assertEqual(
            _llama_speculation_policy(embedded_mtp),
            "MTP, 3 draft tokens from model heads",
        )
        self.assertIn(
            "gemma4-31b-it-mtp-q8-gguf",
            _llama_speculation_policy(separate_draft),
        )
        self.assertEqual(
            _llama_speculation_policy(dflash),
            "DFlash, 15 draft tokens (vulkan=4); draft "
            "muse-glimmer-30b-dflash-kquant-gguf",
        )
        self.assertEqual(
            _llama_speculation_policy(dflash_256k),
            "DFlash, 12 draft tokens (vulkan=4); draft "
            "muse-glimmer-30b-dflash-kquant-gguf",
        )
        self.assertEqual(
            _llama_context_policy(dflash), "model metadata"
        )
        self.assertEqual(
            _llama_context_policy(dflash_256k),
            "forced for muse-glimmer, dflash; automatic fitting disabled",
        )
        self.assertEqual(
            _llama_kv_cache_policy(qwen27),
            "strix-halo=q8_0; otherwise llama.cpp default",
        )
        self.assertEqual(
            _llama_reasoning_policy(qwen27),
            "toggle; off, on; default on",
        )
        self.assertEqual(
            _llama_sampling_policy(qwen27),
            "catalog qwen3.6-27b (thinking/non-thinking)",
        )
        self.assertEqual(
            _llama_sampling_policy(dflash_256k),
            "request or llama.cpp default",
        )
        self.assertEqual(
            _llama_reasoning_policy(dflash_256k),
            "strength; low, medium, high, xhigh; default high",
        )
        self.assertEqual(
            _llama_template_policy(translate),
            "managed translategemma-manual",
        )

    def test_content_install_dry_run_does_not_create_data_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "qwen-image-2512-fp8-base",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(
                    command_content(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())

    def test_summary_only_content_plan_hides_per_file_audit(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "comfyui",
                    "image",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            arguments.summary_only = True
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_install(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())
        text = output.getvalue()
        self.assertIn("Content state:", text)
        self.assertIn("Download: 28.80 GiB", text)
        self.assertIn("Workflow: qwen-image-2512-fp8-lightning", text)
        self.assertNotIn("source:", text)

    def test_content_install_accepts_repeatable_content_pack_paths(self):
        _, arguments = parse_arguments(
            [
                "content",
                "install",
                "--from-file",
                "/local-content/base.json",
                "--from-file",
                "/local-content/loras.json",
            ]
        )
        self.assertEqual(
            arguments.from_file,
            [
                Path("/local-content/base.json"),
                Path("/local-content/loras.json"),
            ],
        )

    def test_content_pack_dry_run_selects_only_pack_bundles(self):
        pack = {
            "schema_version": 2,
            "bundles": {
                "private-bootstrap": {
                    "description": "Private bootstrap content",
                    "application": "comfyui",
                    "artifacts": ["qwen-image-vae"],
                    "groups": ["all", "comfyui"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "private.json"
            path.write_text(json.dumps(pack))
            data_dir = root / "not-created"
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "--from-file",
                    str(path),
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())
        rendered = output.getvalue()
        self.assertIn("Content packs:", rendered)
        self.assertIn(str(path), rendered)
        self.assertIn("local content packs — 1 bundle", rendered)
        self.assertIn("private-bootstrap", rendered)
        self.assertNotIn("qwen-image-2512-fp8-base", rendered)

    def test_content_pack_cannot_be_combined_with_target_or_menu(self):
        for extra, expected in (
            (["qwen"], "explicit TARGET"),
            (["--interactive"], "cannot be combined with --interactive"),
        ):
            with self.subTest(extra=extra):
                _, arguments = parse_arguments(
                    ["content", "install"]
                    + extra
                    + ["--from-file", "/local-content/models.json"]
                )
                with self.assertRaisesRegex(LauncherError, expected):
                    _command_content_install(arguments, load_catalog())

    def test_content_install_parses_local_mirror_modes(self):
        _, arguments = parse_arguments(
            [
                "content",
                "install",
                "qwen",
                "--local-mirror",
                "/old/rocmplete",
                "--local-mirror-move",
            ]
        )
        self.assertEqual(arguments.local_mirror, Path("/old/rocmplete"))
        self.assertTrue(arguments.local_mirror_move)

    def test_content_install_rejects_move_without_local_mirror(self):
        _, arguments = parse_arguments(
            [
                "content",
                "install",
                "qwen",
                "--local-mirror-move",
            ]
        )
        with self.assertRaisesRegex(LauncherError, "requires --local-mirror"):
            _command_content_install(arguments, load_catalog())

    def test_content_install_dry_run_validates_mirror_without_creating_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            mirror = root / "old-data"
            mirror.mkdir()
            data_dir = root / "new-data"
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "qwen-image-2512-fp8-base",
                    "--local-mirror",
                    str(mirror),
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_install(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())
            self.assertIn("worst case", output.getvalue())

    @patch("rocmplete.cli.install_artifacts", return_value=0)
    def test_content_pack_install_uses_only_declared_bundle_content(
        self, install_artifacts
    ):
        pack = {
            "schema_version": 2,
            "bundles": {
                "private-bootstrap": {
                    "description": "Private bootstrap content",
                    "application": "comfyui",
                    "artifacts": ["qwen-image-vae"],
                    "groups": ["all", "comfyui"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "private.json"
            path.write_text(json.dumps(pack))
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "--from-file",
                    str(path),
                    "--data-dir",
                    str(root / "data"),
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_install(arguments, load_catalog()),
                    0,
                )
        artifacts = install_artifacts.call_args.args[0]
        self.assertEqual(
            tuple(item.identifier for item in artifacts),
            ("qwen-image-vae",),
        )
        self.assertIn(
            "Content ready: 1 bundle and 0 workflows.",
            output.getvalue(),
        )

    @patch("rocmplete.cli.install_artifacts")
    def test_content_pack_preserves_builtin_model_agreements(
        self, install_artifacts
    ):
        pack = {
            "schema_version": 2,
            "bundles": {
                "private-ltx": {
                    "description": "Private LTX selection",
                    "application": "comfyui",
                    "artifacts": ["ltx-2-19b-dev-fp8"],
                    "groups": ["all", "comfyui", "ltx"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "private.json"
            path.write_text(json.dumps(pack))
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "--from-file",
                    str(path),
                    "--data-dir",
                    str(root / "data"),
                ]
            )
            with patch(
                "rocmplete.cli.sys.stdin.isatty",
                return_value=False,
            ):
                with redirect_stdout(io.StringIO()):
                    with self.assertRaisesRegex(
                        LauncherError, "--accept-license"
                    ):
                        _command_content_install(arguments, load_catalog())
        install_artifacts.assert_not_called()

    def test_workflow_status_does_not_create_data_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "content",
                    "workflows",
                    "status",
                    "--data-dir",
                    str(data_dir),
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(
                    command_content(arguments, load_catalog()), 1
                )
            self.assertFalse(data_dir.exists())

    @patch("rocmplete.cli.install_workflow")
    def test_workflow_install_defaults_to_comfyui_image(
        self, install_workflow
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "workflows",
                    "install",
                    "qwen-image-2512-fp8-base",
                    "--data-dir",
                    directory,
                ]
            )
            with patch.dict(os.environ, {}, clear=True):
                self.assertEqual(
                    command_content(arguments, load_catalog()), 0
                )
        self.assertEqual(
            install_workflow.call_args.args[2],
            APPLICATIONS["comfyui"].image,
        )

    @patch("rocmplete.cli.run_benchmark")
    @patch("rocmplete.cli.check_device_access")
    @patch(
        "rocmplete.cli.select_render_nodes",
        return_value=("/dev/dri/renderD128",),
    )
    def test_benchmark_dry_run_does_not_create_data_directory(
        self, select_render_nodes, check_device_access, run_benchmark
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "benchmark",
                    "run",
                    "qwen-image-2512-fp8-base",
                    "--profile",
                    "rdna4",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()):
                self.assertEqual(
                    command_benchmark(arguments, load_catalog()), 0
                )
            self.assertFalse(data_dir.exists())
            self.assertEqual(
                run_benchmark.call_args.args[2].image,
                APPLICATIONS["comfyui"].image,
            )

    def test_content_status_details_lists_artifacts(self):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "status",
                    "qwen-image-2512-fp8-base",
                    "--details",
                    "--data-dir",
                    directory,
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_content(arguments, load_catalog()), 1
                )
        text = output.getvalue()
        self.assertIn("qwen-image-2512-fp8-base:", text)
        self.assertIn("workflow/", text)

    @patch("builtins.input", side_effect=("1", "3"))
    @patch("rocmplete.cli.sys.stdin")
    def test_guided_content_install_selects_recipe(
        self, stdin, input_mock
    ):
        stdin.isatty.return_value = True
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(
                _interactive_content_target(load_catalog()),
                ("comfyui", "t2v"),
            )
        top_level = output.getvalue().split("ComfyUI content:", 1)[0]
        self.assertFalse(
            any(
                line.split(")", 1)[-1].strip().startswith("all ")
                for line in top_level.splitlines()
                if ")" in line
            )
        )
        self.assertNotIn("family", top_level)

    @patch("builtins.input", side_effect=("4", "4", "1"))
    @patch("rocmplete.cli.sys.stdin")
    def test_guided_exact_bundle_browser_uses_categories(
        self, stdin, input_mock
    ):
        stdin.isatty.return_value = True
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(
                _interactive_content_target(load_catalog()),
                ("llama-qwen3-0.6b-q8-0", None),
            )
        text = output.getvalue()
        self.assertIn("exact-bundles", text)
        self.assertIn("Browse exact bundles:", text)
        self.assertIn("ComfyUI — image models (9 bundles)", text)
        self.assertIn("llama.cpp (13 bundles)", text)
        self.assertIn("Qwen3 0.6B", text)

    @patch("builtins.input", side_effect=("8", "1"))
    @patch("rocmplete.cli.sys.stdin")
    def test_llama_recipe_menu_can_browse_exact_llama_bundles(
        self, stdin, input_mock
    ):
        stdin.isatty.return_value = True
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(
                _interactive_content_target(load_catalog(), "llama-cpp"),
                ("llama-qwen3-0.6b-q8-0", None),
            )
        text = output.getvalue()
        self.assertNotIn("Browse exact bundles:", text)
        self.assertIn("llama.cpp:", text)
        self.assertIn("qwen3-0.6b-q8-0", text)

    def test_exact_bundle_categories_cover_catalog_once(self):
        catalog = load_catalog()
        counts = {
            identifier: len(_exact_bundles(catalog, identifier))
            for identifier, _ in _exact_categories(catalog)
        }
        self.assertEqual(
            counts,
            {
                "comfyui-images": 9,
                "comfyui-videos": 20,
                "comfyui-addons": 7,
                "llama-cpp": 13,
                "dwarfstar": 2,
            },
        )
        categorized = [
            bundle.identifier
            for bundle in catalog.bundles.values()
            if _exact_bundle_category(bundle)
        ]
        self.assertEqual(len(categorized), len(set(categorized)))
        self.assertEqual(set(categorized), set(catalog.bundles))

    def test_exact_bundle_categories_reject_unknown_application(self):
        with self.assertRaisesRegex(
            LauncherError, "has no exact-bundle browser"
        ):
            _exact_categories(load_catalog(), "unknown")

    @patch("rocmplete.cli.sys.stdin")
    def test_guided_content_install_rejects_noninteractive_input(self, stdin):
        stdin.isatty.return_value = False
        with self.assertRaisesRegex(LauncherError, "requires TARGET"):
            _interactive_content_target(load_catalog())

    @patch("builtins.input", return_value="2")
    @patch("rocmplete.cli.sys.stdin")
    def test_partial_comfyui_install_guides_to_edit(
        self, stdin, input_mock
    ):
        stdin.isatty.return_value = True
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(
                _interactive_content_target(load_catalog(), "comfyui"),
                ("comfyui", "edit"),
            )
        self.assertIn("ComfyUI content:", output.getvalue())

    def test_noninteractive_content_install_requires_target(self):
        _, arguments = parse_arguments(
            ["content", "install", "--non-interactive"]
        )
        with self.assertRaisesRegex(
            LauncherError, "requires a complete application selection"
        ):
            _command_content_install(arguments, load_catalog())

    def test_noninteractive_content_install_requires_application_category(self):
        _, arguments = parse_arguments(
            ["content", "install", "comfyui", "--non-interactive"]
        )
        with self.assertRaisesRegex(
            LauncherError, "requires a complete application selection"
        ):
            _command_content_install(arguments, load_catalog())

    def test_content_interaction_modes_are_mutually_exclusive(self):
        with redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as result:
                parse_arguments(
                    [
                        "content",
                        "install",
                        "--interactive",
                        "--non-interactive",
                    ]
                )
        self.assertEqual(result.exception.code, 2)

    def test_content_recipes_and_aggregates_cover_the_catalog(self):
        catalog = load_catalog()
        self.assertEqual(
            len(_resolve_content_bundles(catalog, "family", "qwen")),
            8,
        )
        self.assertEqual(
            len(_resolve_content_bundles(catalog, "family", "wan")),
            10,
        )
        expected = {
            ("comfyui", "image"): ("qwen-image-2512-fp8-lightning",),
            ("comfyui", "edit"): ("qwen-image-edit-2511-fp8-lightning",),
            ("comfyui", "t2v"): ("wan-2.2-t2v-14b-fp8-lightning",),
            ("comfyui", "i2v"): ("wan-2.2-i2v-14b-fp8-lightning",),
            ("llama-cpp", "qwen3.6"): (
                "llama-qwen3.6-27b-mtp-q8-0",
                "llama-qwen3.6-35b-a3b-mtp-ud-q8-k-xl",
            ),
            ("llama-cpp", "qwen3.8"): (
                "llama-qwen3.8-27b-ud-q8-k-xl",
            ),
            ("llama-cpp", "kat-coder"): (
                "llama-kat-coder-v2.5-dev-q8-0",
            ),
            (
                "llama-cpp",
                "muse-glimmer",
            ): (
                "llama-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash",
            ),
            (
                "llama-cpp",
                "translation-hy",
            ): ("llama-hy-mt1.5-7b-q8-0",),
            (
                "llama-cpp",
                "translation-gemma",
            ): ("llama-translategemma-27b-it-q8-0",),
            (
                "llama-cpp",
                "shisa-v2.1",
            ): ("llama-shisa-v2.1-llama3.3-70b-q8-0",),
        }
        for (application, recipe), bundles in expected.items():
            with self.subTest(application=application, recipe=recipe):
                self.assertEqual(
                    tuple(
                        item.identifier
                        for item in _resolve_content_bundles(
                            catalog, application, recipe
                        )
                    ),
                    bundles,
                )
        for removed in (
            "starter",
            "gemma4",
            "mtp",
            "large",
        ):
            with self.subTest(removed=removed):
                with self.assertRaisesRegex(
                    LauncherError, "unknown llama-cpp recipe"
                ):
                    _resolve_content_bundles(
                        catalog, "llama-cpp", removed
                    )
        for application in (
            "comfyui",
            "llama-cpp",
            "dwarfstar",
        ):
            with self.subTest(application=application):
                self.assertEqual(
                    _resolve_content_bundles(catalog, application, "all"),
                    tuple(
                        bundle
                        for bundle in catalog.bundles.values()
                        if bundle.application == application
                    ),
                )
        self.assertEqual(
            _resolve_content_bundles(
                catalog, "llama-qwen3-0.6b-q8-0"
            )[0].identifier,
            "llama-qwen3-0.6b-q8-0",
        )
        for application in ("comfyui", "llama-cpp", "dwarfstar"):
            with self.assertRaisesRegex(LauncherError, "unknown .* recipe"):
                _resolve_content_bundles(
                    catalog, application, "recommended"
                )
        self.assertEqual(
            tuple(_resolve_content_bundles(catalog, "all")),
            tuple(catalog.bundles.values()),
        )

    @patch("rocmplete.cli.install_workflow")
    @patch("rocmplete.cli.install_artifacts", return_value=0)
    def test_content_install_all_with_both_acceptance_flags_installs_everything(
        self, install_artifacts, install_workflow
    ):
        catalog = load_catalog()
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "all",
                    "--data-dir",
                    directory,
                    "--image",
                    "localhost/custom-content-tools",
                    "--accept-license",
                    "--acknowledge-license-risk",
                ]
            )
            with patch.dict(os.environ, {}, clear=True):
                with redirect_stdout(io.StringIO()) as output:
                    self.assertEqual(
                        _command_content_install(arguments, catalog), 0
                    )

        artifacts = install_artifacts.call_args.args[0]
        self.assertEqual(len(artifacts), 66)
        self.assertEqual(len({item.identifier for item in artifacts}), 66)
        self.assertEqual(
            install_artifacts.call_args.args[2],
            "localhost/custom-content-tools",
        )
        self.assertTrue(
            install_artifacts.call_args.kwargs["acknowledge_license_risk"]
        )
        self.assertEqual(install_workflow.call_count, 28)
        self.assertTrue(
            all(
                item.args[2] == APPLICATIONS["comfyui"].image
                for item in install_workflow.call_args_list
            )
        )
        self.assertIn(
            "Content ready: 51 bundles and 28 workflows.",
            output.getvalue(),
        )

    @patch("rocmplete.cli.install_artifacts", return_value=0)
    def test_content_install_llama_all_uses_application_aggregate(
        self, install_artifacts
    ):
        catalog = load_catalog()
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "llama-cpp",
                    "all",
                    "--data-dir",
                    directory,
                    "--accept-license",
                    "--acknowledge-license-risk",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_install(arguments, catalog), 0
                )

        artifacts = install_artifacts.call_args.args[0]
        self.assertEqual(len(artifacts), 15)
        self.assertEqual(len({item.identifier for item in artifacts}), 15)
        self.assertIn(
            "Content ready: 13 bundles and 0 workflows.",
            output.getvalue(),
        )

    @patch("rocmplete.cli.install_artifacts", return_value=0)
    def test_llama_content_install_leads_with_router_and_agent_clients(
        self, install_artifacts
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "llama-cpp",
                    "qwen3.6",
                    "--data-dir",
                    directory,
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_install(arguments, load_catalog()), 0
                )

        rendered = output.getvalue()
        self.assertIn(
            "./rocmplete run llama-cpp server --router --models-max 1",
            rendered,
        )
        self.assertIn("./rocmplete agent opencode", rendered)
        self.assertIn("./rocmplete agent pi", rendered)
        self.assertIn("./rocmplete agent omp", rendered)
        self.assertIn("./rocmplete agent maki", rendered)
        self.assertNotIn("./rocmplete client", rendered)
        self.assertNotIn("run llama-cpp server --preset", rendered)
        self.assertIn(
            "./rocmplete run llama-cpp cli "
            "--preset qwen3.6-27b-mtp-q8-0",
            rendered,
        )
        self.assertNotIn("--preset qwen3-0.6b-q8-0", rendered)
        self.assertIn(
            "--preset qwen3.6-35b-a3b-mtp-ud-q8-k-xl", rendered
        )
        self.assertLess(
            rendered.index("server --router"), rendered.index("opencode")
        )
        self.assertLess(
            rendered.index("opencode"),
            rendered.index("./rocmplete agent pi"),
        )
        self.assertLess(
            rendered.index("./rocmplete agent pi"),
            rendered.index("./rocmplete agent omp"),
        )
        self.assertLess(
            rendered.index("./rocmplete agent omp"),
            rendered.index("./rocmplete agent maki"),
        )
        self.assertLess(
            rendered.index("./rocmplete agent maki"),
            rendered.index("run llama-cpp cli"),
        )

    @patch("rocmplete.cli.install_artifacts", return_value=0)
    def test_dspark_recipe_install_suggests_dspark_launch(
        self, install_artifacts
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "dwarfstar",
                    "flash-0731-q2-imatrix-dspark",
                    "--data-dir",
                    directory,
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    _command_content_install(arguments, load_catalog()), 0
                )

        rendered = output.getvalue()
        self.assertIn(
            "./rocmplete run dwarfstar server --dspark", rendered
        )
        self.assertNotIn(
            "\n    ./rocmplete run dwarfstar server\n", rendered
        )

    @patch("rocmplete.cli.install_artifacts")
    def test_hy_translation_recipe_requires_license_acceptance(
        self, install_artifacts
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "llama-cpp",
                    "translation-hy",
                    "--data-dir",
                    directory,
                    "--non-interactive",
                ]
            )
            with self.assertRaisesRegex(
                LauncherError, "--accept-license"
            ):
                _command_content_install(arguments, load_catalog())
        install_artifacts.assert_not_called()

    @patch("rocmplete.cli.install_artifacts")
    def test_content_install_all_requires_license_acceptance(
        self, install_artifacts
    ):
        catalog = load_catalog()
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "all",
                    "--data-dir",
                    directory,
                    "--acknowledge-license-risk",
                ]
            )
            with redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(
                    LauncherError, "--accept-license"
                ):
                    _command_content_install(arguments, catalog)
        install_artifacts.assert_not_called()

    @patch("builtins.input", return_value="yes")
    def test_content_install_can_accept_model_terms_interactively(
        self, user_input
    ):
        catalog = load_catalog()
        bundles = (catalog.bundle("ltx-2-t2v-19b-fp8-full"),)
        with patch(
            "rocmplete.cli.sys.stdin.isatty", return_value=True
        ):
            with redirect_stderr(io.StringIO()):
                _require_selection_license_acceptance(
                    catalog,
                    bundles,
                    accepted=False,
                )
        user_input.assert_called_once()

    @patch("builtins.input", return_value="no")
    def test_content_install_can_decline_model_terms(
        self, user_input
    ):
        catalog = load_catalog()
        bundles = (catalog.bundle("ltx-2-t2v-19b-fp8-full"),)
        with patch(
            "rocmplete.cli.sys.stdin.isatty", return_value=True
        ):
            with redirect_stderr(io.StringIO()):
                with self.assertRaisesRegex(LauncherError, "declined"):
                    _require_selection_license_acceptance(
                        catalog,
                        bundles,
                        accepted=False,
                    )

    @patch("builtins.input")
    def test_noninteractive_model_terms_require_flag_on_a_terminal(
        self, user_input
    ):
        catalog = load_catalog()
        bundles = (catalog.bundle("ltx-2-t2v-19b-fp8-full"),)
        with patch(
            "rocmplete.cli.sys.stdin.isatty", return_value=True
        ):
            with self.assertRaisesRegex(LauncherError, "--accept-license"):
                _require_selection_license_acceptance(
                    catalog,
                    bundles,
                    accepted=False,
                    non_interactive=True,
                )
        user_input.assert_not_called()

    def test_explicit_install_enables_interactive_license_prompt(self):
        catalog = load_catalog()
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "comfyui",
                    "t2v",
                    "--data-dir",
                    directory,
                    "--acknowledge-license-risk",
                ]
            )
            with ExitStack() as stack:
                require_acceptance = stack.enter_context(
                    patch(
                        "rocmplete.cli."
                        "_require_selection_license_acceptance"
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli."
                        "_acknowledge_unverified_downloads",
                        return_value=True,
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.install_artifacts",
                        return_value=0,
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.install_workflow")
                )
                stack.enter_context(redirect_stdout(io.StringIO()))
                self.assertEqual(
                    _command_content_install(arguments, catalog), 0
                )
        self.assertFalse(
            require_acceptance.call_args.kwargs["non_interactive"]
        )

    @patch("rocmplete.cli.install_artifacts")
    def test_content_install_all_requires_license_risk_acknowledgment(
        self, install_artifacts
    ):
        catalog = load_catalog()
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "content",
                    "install",
                    "all",
                    "--data-dir",
                    directory,
                    "--accept-license",
                ]
            )
            with redirect_stdout(io.StringIO()):
                with redirect_stderr(io.StringIO()):
                    with patch(
                        "rocmplete.cli.sys.stdin.isatty",
                        return_value=False,
                    ):
                        with self.assertRaisesRegex(
                            LauncherError, "--acknowledge-license-risk"
                        ):
                            _command_content_install(arguments, catalog)
        install_artifacts.assert_not_called()

    def test_unverified_download_requires_noninteractive_flag(self):
        license_info = SimpleNamespace(
            warning="No license declared.", status="unverified"
        )
        artifact = SimpleNamespace(license=license_info)
        status = SimpleNamespace(artifact=artifact, state="missing")
        with patch(
            "rocmplete.cli.sys.stdin.isatty", return_value=False
        ):
            with redirect_stderr(io.StringIO()):
                with self.assertRaisesRegex(LauncherError, "noninteractive"):
                    _acknowledge_unverified_downloads([status], False)
        self.assertTrue(_acknowledge_unverified_downloads([status], True))

    @patch("builtins.input", return_value="yes")
    def test_unverified_download_can_be_acknowledged_interactively(self, user_input):
        license_info = SimpleNamespace(
            warning="No license declared.", status="unverified"
        )
        artifact = SimpleNamespace(license=license_info)
        status = SimpleNamespace(artifact=artifact, state="missing")
        with patch(
            "rocmplete.cli.sys.stdin.isatty", return_value=True
        ):
            with redirect_stderr(io.StringIO()):
                self.assertTrue(
                    _acknowledge_unverified_downloads([status], False)
                )

    def test_help_alias_is_preserved(self):
        with redirect_stdout(io.StringIO()):
            with self.assertRaises(SystemExit) as result:
                parse_arguments(["help"])
        self.assertEqual(result.exception.code, 0)

    def test_root_help_documents_explicit_lifecycle(self):
        parser, arguments = parse_arguments([])

        self.assertIsNone(arguments.command)
        help_text = parser.format_help()
        self.assertIn("./rocmplete build APPLICATION", help_text)
        self.assertIn(
            "./rocmplete content install APPLICATION RECIPE", help_text
        )
        self.assertIn("./rocmplete run APPLICATION", help_text)
        self.assertNotIn("quickstart", help_text.lower())

    def test_profile_alias_commands_are_removed(self):
        for command in (
            "run-rdna4",
            "run-strix-halo",
            "run-strix-point",
            "run-cpu",
        ):
            with self.subTest(command=command):
                with redirect_stderr(io.StringIO()):
                    with self.assertRaises(SystemExit) as result:
                        parse_arguments([command])
                self.assertEqual(result.exception.code, 2)

    def test_comfy_arguments_are_forwarded_only_after_separator(self):
        _, arguments = parse_arguments(
            [
                "run",
                "comfyui",
                "--profile",
                "rdna4",
                "--listen",
                "127.0.0.1",
                "--",
                "--lowvram",
            ]
        )
        self.assertEqual(arguments.profile, "rdna4")
        self.assertEqual(arguments.comfy_args, ["--lowvram"])

    def test_comfy_manager_flag_is_forwarded_after_separator(self):
        _, arguments = parse_arguments(
            [
                "run",
                "comfyui",
                "--listen",
                "192.168.1.50",
                "--",
                "--enable-manager",
            ]
        )
        self.assertEqual(arguments.listen, "192.168.1.50")
        self.assertEqual(arguments.comfy_args, ["--enable-manager"])

    def test_comfy_help_documents_manager_passthrough(self):
        with redirect_stdout(io.StringIO()) as output:
            with self.assertRaises(SystemExit) as result:
                parse_arguments(["run", "comfyui", "--help"])
        self.assertEqual(result.exception.code, 0)
        self.assertIn(
            "./rocmplete run comfyui -- --enable-manager",
            output.getvalue(),
        )

    def test_explicit_profile_overrides_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    directory,
                ]
            )
            options = resolve_run_options(
                arguments,
                {
                    "HOME": directory,
                    "ROCMLETE_PROFILE": "rdna4",
                },
            )
        self.assertEqual(options.profile, "cpu")

    def test_run_uses_environment_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                ["run", "comfyui", "--data-dir", directory]
            )
            options = resolve_run_options(
                arguments,
                {"HOME": directory, "ROCMLETE_PROFILE": "cpu"},
            )
        self.assertEqual(options.profile, "cpu")

    def test_web_run_defaults_to_loopback_and_allows_environment_override(self):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    directory,
                ]
            )
            options = resolve_run_options(arguments, {"HOME": directory})
            exposed = resolve_run_options(
                arguments,
                {
                    "HOME": directory,
                    "ROCMLETE_LISTEN": "0.0.0.0",
                },
            )
        self.assertEqual(options.listen, "127.0.0.1")
        self.assertEqual(exposed.listen, "0.0.0.0")

    def test_application_selecting_commands_print_copyable_examples(self):
        for command in ("build", "shell", "logs", "stop"):
            with self.subTest(command=command):
                with redirect_stderr(io.StringIO()) as output:
                    self.assertEqual(main([command]), 2)
                self.assertIn("Try one of these:", output.getvalue())

    def test_run_without_application_prints_copyable_examples(self):
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(main(["run"]), 2)
        text = output.getvalue()
        self.assertIn("error: choose an application", text)
        self.assertIn("./rocmplete run comfyui", text)
        self.assertIn("./rocmplete run llama-cpp server", text)

    def test_agent_without_client_prints_copyable_examples(self):
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(main(["agent"]), 2)
        text = output.getvalue()
        self.assertIn("error: choose opencode, pi, omp, or maki", text)
        self.assertIn("./rocmplete agent opencode", text)
        self.assertIn("./rocmplete agent pi", text)
        self.assertIn("./rocmplete agent omp", text)
        self.assertIn("./rocmplete agent maki", text)

    def test_agent_clients_are_not_top_level_commands(self):
        for command in ("opencode", "pi", "omp", "maki"):
            with self.subTest(command=command):
                with redirect_stderr(io.StringIO()):
                    with self.assertRaises(SystemExit) as result:
                        parse_arguments([command])
                self.assertEqual(result.exception.code, 2)

    def test_acceptance_without_operation_prints_copyable_examples(self):
        with redirect_stderr(io.StringIO()) as output:
            self.assertEqual(main(["acceptance"]), 2)
        text = output.getvalue()
        self.assertIn("error: choose run", text)
        self.assertIn("./rocmplete acceptance run", text)
        self.assertIn("--resume RESULT.json", text)

    def test_acceptance_output_requires_json_and_preserves_neighbor_report(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--output",
                    str(root / "result.md"),
                ]
            )
            with self.assertRaisesRegex(LauncherError, r"\.json suffix"):
                _acceptance_result_path(arguments, root)

            report = root / "result.md"
            report.write_text("preserve")
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--output",
                    str(root / "result.json"),
                ]
            )
            with self.assertRaisesRegex(LauncherError, "acceptance report"):
                _acceptance_result_path(arguments, root)
            self.assertEqual(report.read_text(), "preserve")

    def test_acceptance_rejects_bad_output_before_podman_or_data_creation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "not-created"
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--data-dir",
                    str(data_dir),
                    "--output",
                    str(root / "result.txt"),
                ]
            )
            with patch("rocmplete.cli.podman.require_rootless") as rootless:
                with self.assertRaisesRegex(LauncherError, r"\.json suffix"):
                    command_acceptance(arguments, load_catalog())
            rootless.assert_not_called()
            self.assertFalse(data_dir.exists())

    def test_acceptance_rejects_bad_resume_before_podman_or_data_creation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_dir = root / "not-created"
            resume = root / "resume.json"
            resume.write_text('{"schema_version": 3}\n')
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--data-dir",
                    str(data_dir),
                    "--resume",
                    str(resume),
                ]
            )
            with patch("rocmplete.cli.podman.require_rootless") as rootless:
                with self.assertRaisesRegex(LauncherError, "invalid suite ID"):
                    command_acceptance(arguments, load_catalog())
            rootless.assert_not_called()
            self.assertFalse(data_dir.exists())

    def test_acceptance_checkpoints_and_completes_selected_smoke(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result_path = root / "result.json"
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--application",
                    "llama-cpp",
                    "--data-dir",
                    str(root),
                    "--output",
                    str(result_path),
                    "--non-interactive",
                ]
            )
            with ExitStack() as stack:
                stack.enter_context(
                    patch("rocmplete.cli.podman.require_rootless")
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.select_render_nodes",
                        return_value=("/dev/dri/renderD128",),
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.check_gpu_device_access")
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.image_exists",
                        return_value=True,
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.inspect_bundle",
                        return_value=[
                            SimpleNamespace(
                                state="installed", integrity="verified"
                            )
                        ],
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.probe_hardware",
                        return_value={
                            "PyTorch": "test",
                            "ROCm/HIP": "test",
                            "Device": "test",
                            "Architecture": "gfx1150",
                            "GPU operation": "passed",
                            "GPU devices": "passed",
                            "Profile": "strix-point",
                        },
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.container_exists",
                        return_value=False,
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.image_id",
                        return_value="sha256:" + "1" * 64,
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.run_host_case", return_value={})
                )
                app_case = stack.enter_context(
                    patch(
                        "rocmplete.cli.run_application_case",
                        return_value={"artifacts": ["/tmp/llama.json"]},
                    )
                )
                with redirect_stdout(io.StringIO()):
                    self.assertEqual(
                        command_acceptance(arguments, load_catalog()),
                        0,
                    )

            result = json.loads(result_path.read_text())
            self.assertEqual(result["status"], "pass")
            self.assertEqual(
                [entry["status"] for entry in result["cases"]],
                ["pass", "pass"],
            )
            self.assertTrue(result_path.with_suffix(".md").is_file())
            app_case.assert_called_once()

    def test_acceptance_defers_all_visual_reviews_until_workloads_finish(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result_path = root / "result.json"
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--application",
                    "comfyui",
                    "--data-dir",
                    str(root),
                    "--output",
                    str(result_path),
                ]
            )
            events = []
            review_answers = iter(("p", "d"))

            def run_application(identifier, *args, **kwargs):
                events.append("run:{}".format(identifier))
                return {
                    "artifacts": ["/tmp/{}.png".format(identifier)]
                }

            def review_output(message):
                events.append("review")
                self.assertEqual(
                    events[:2],
                    [
                        "run:comfyui-image",
                        "run:comfyui-video",
                    ],
                )
                return next(review_answers)

            with ExitStack() as stack:
                stack.enter_context(
                    patch("rocmplete.cli.podman.require_rootless")
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.select_render_nodes",
                        return_value=("/dev/dri/renderD128",),
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.check_gpu_device_access")
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.image_exists",
                        return_value=True,
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.inspect_bundle",
                        return_value=[
                            SimpleNamespace(
                                state="installed", integrity="verified"
                            )
                        ],
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.probe_hardware",
                        return_value={
                            "PyTorch": "test",
                            "ROCm/HIP": "test",
                            "Device": "test",
                            "Architecture": "gfx1150",
                            "GPU operation": "passed",
                            "GPU devices": "passed",
                            "Profile": "strix-point",
                        },
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.container_exists",
                        return_value=False,
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.podman.image_id",
                        return_value="sha256:" + "1" * 64,
                    )
                )
                stack.enter_context(
                    patch("rocmplete.cli.run_host_case", return_value={})
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.run_application_case",
                        side_effect=run_application,
                    )
                )
                stack.enter_context(
                    patch(
                        "rocmplete.cli.sys.stdin",
                        SimpleNamespace(isatty=lambda: True),
                    )
                )
                stack.enter_context(
                    patch("builtins.input", side_effect=review_output)
                )
                output = io.StringIO()
                with redirect_stdout(output):
                    self.assertEqual(
                        command_acceptance(arguments, load_catalog()),
                        2,
                    )

            self.assertEqual(
                events,
                [
                    "run:comfyui-image",
                    "run:comfyui-video",
                    "review",
                    "review",
                ],
            )
            result = json.loads(result_path.read_text())
            self.assertEqual(result["status"], "blocked")
            text = output.getvalue()
            self.assertEqual(text.count("Smoke pass criteria:"), 2)
            self.assertIn(
                "Composition, sharpness, and aesthetic quality are not graded.",
                text,
            )
            self.assertIn("Visual reviews:", text)
            self.assertRegex(
                text,
                r"pass\s+comfyui-image",
            )
            self.assertRegex(
                text,
                r"deferred\s+comfyui-video",
            )
            self.assertIn(
                "./rocmplete acceptance run --resume {}".format(
                    result_path
                ),
                text,
            )

    @patch("rocmplete.cli.podman.image_exists", return_value=False)
    @patch("rocmplete.cli.check_gpu_device_access")
    @patch(
        "rocmplete.cli.select_render_nodes",
        return_value=("/dev/dri/renderD128",),
    )
    @patch("rocmplete.cli.podman.require_rootless")
    def test_acceptance_dry_run_is_non_mutating_and_plans_all_apps(
        self,
        require_rootless,
        select_render_nodes,
        check_gpu_device_access,
        image_exists,
    ):
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory) / "not-created"
            _, arguments = parse_arguments(
                [
                    "acceptance",
                    "run",
                    "--data-dir",
                    str(data_dir),
                    "--dry-run",
                ]
            )
            with redirect_stdout(io.StringIO()) as output:
                self.assertEqual(
                    command_acceptance(arguments, load_catalog()),
                    0,
                )
            self.assertFalse(data_dir.exists())
        text = output.getvalue()
        self.assertIn("Smoke acceptance plan", text)
        self.assertIn("ComfyUI Qwen Image", text)
        self.assertIn("ComfyUI Wan 2.2", text)
        self.assertIn("llama.cpp Qwen3", text)
        self.assertIn("\n            Worst-case download:", text)
        self.assertIn("No image, content, container, or result was changed", text)

    def test_managed_forwarded_argument_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "cpu",
                    "--data-dir",
                    directory,
                    "--",
                    "--listen",
                    "127.0.0.1",
                ]
            )
            with self.assertRaises(LauncherError):
                resolve_run_options(arguments, {"HOME": directory})

    @patch("rocmplete.cli.check_device_access")
    @patch(
        "rocmplete.cli.select_render_nodes",
        return_value=("/dev/dri/renderD128",),
    )
    def test_gpu_profile_resolves_render_nodes(self, select, access):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "rdna4",
                    "--data-dir",
                    directory,
                ]
            )
            options = resolve_run_options(arguments, {"HOME": directory})
        self.assertEqual(options.render_nodes, ("/dev/dri/renderD128",))
        select.assert_called_once()
        self.assertEqual(access.call_count, 2)

    def test_render_node_parser_preserves_explicit_order(self):
        _, arguments = parse_arguments(
            [
                "run",
                "comfyui",
                "--render-node",
                "/dev/dri/renderD129",
                "--render-node",
                "/dev/dri/renderD128",
            ]
        )
        self.assertEqual(
            arguments.render_node,
            ["/dev/dri/renderD129", "/dev/dri/renderD128"],
        )

    def test_plural_render_node_environment_is_ordered(self):
        self.assertEqual(
            requested_render_nodes(
                None,
                {
                    "ROCMLETE_RENDER_NODES": (
                        "/dev/dri/renderD129,/dev/dri/renderD128"
                    )
                },
            ),
            ("/dev/dri/renderD129", "/dev/dri/renderD128"),
        )

    def test_duplicate_render_nodes_are_rejected(self):
        with self.assertRaisesRegex(LauncherError, "duplicates"):
            select_render_nodes(
                ("/dev/dri/renderD128", "/dev/dri/renderD128")
            )

    @patch("rocmplete.cli.check_device_access")
    @patch(
        "rocmplete.cli.select_render_nodes",
        return_value=(
            "/dev/dri/renderD128",
            "/dev/dri/renderD129",
        ),
    )
    def test_comfyui_accepts_an_explicit_multi_gpu_set(
        self, select, access
    ):
        with tempfile.TemporaryDirectory() as directory:
            _, arguments = parse_arguments(
                [
                    "run",
                    "comfyui",
                    "--profile",
                    "rdna4",
                    "--data-dir",
                    directory,
                    "--render-node",
                    "/dev/dri/renderD128",
                    "--render-node",
                    "/dev/dri/renderD129",
                ]
            )
            options = resolve_run_options(arguments, {"HOME": directory})
        self.assertEqual(
            options.render_nodes,
            ("/dev/dri/renderD128", "/dev/dri/renderD129"),
        )
        self.assertEqual(access.call_count, 3)

if __name__ == "__main__":
    unittest.main()
