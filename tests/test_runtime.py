import tempfile
import unittest
from pathlib import Path

from rocmplete.errors import LauncherError
from rocmplete.hardware_profiles import (
    ARCHITECTURE_PROFILES,
    SUPPORTED_ARCHITECTURES,
)
from rocmplete.build import (
    build_cache_dir,
    build_command,
    prepare_pip_build_cache,
)
from rocmplete.config import (
    APPLICATIONS,
    ROCM_BASE_IMAGE,
    ROCM_RUNTIME_IMAGE,
)
from rocmplete.runtime.diagnostic import (
    cpu_isolation_diagnostic_command,
    gpu_diagnostic_command,
    parse_gpu_diagnostic_output,
)
from rocmplete.runtime.dwarfstar import (
    DwarfStarOptions,
    dwarfstar_command,
)
from rocmplete.runtime.llama import (
    LlamaBenchmarkOptions,
    LlamaOptions,
    llama_command,
    llama_benchmark_command,
)
from rocmplete.runtime.shell import shell_command
from rocmplete.runtime.web import WebOptions, web_command


class RuntimeCommandTests(unittest.TestCase):
    def test_containerfile_default_base_tag_matches_launcher(self):
        root = Path(__file__).resolve().parents[1]
        containerfile = (root / "Containerfile").read_text()
        self.assertIn(
            "ARG ROCM_BASE_IMAGE={}\n".format(ROCM_BASE_IMAGE),
            containerfile,
        )
        self.assertIn(
            "ARG ROCM_RUNTIME_IMAGE={}\n".format(ROCM_RUNTIME_IMAGE),
            containerfile,
        )
        self.assertIn(
            "FROM ${ROCM_RUNTIME_IMAGE} AS rocm-base\n",
            containerfile,
        )
        self.assertIn(
            "FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk\n",
            containerfile,
        )
        self.assertIn(
            "FROM native-rocm-sdk AS llama-rocm-sdk\n", containerfile
        )
        self.assertIn(
            "FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp\n",
            containerfile,
        )
        comfy_stage = containerfile.split(
            "FROM ${ROCM_BASE_IMAGE} AS comfyui\n", 1
        )[1].split("FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk\n", 1)[0]
        self.assertIn("ARG ROCM_VERSION\n", comfy_stage)
        self.assertIn("ARG TORCH_VERSION\n", comfy_stage)
        self.assertIn(
            "--requirement /opt/ComfyUI/manager_requirements.txt",
            comfy_stage,
        )
        self.assertIn(
            "COPY applications/comfyui/patch_manager.py",
            comfy_stage,
        )
        self.assertIn(
            "python /opt/rocmplete/patch_comfyui_manager.py",
            comfy_stage,
        )
        self.assertIn(
            "ARG RGTHREE_COMMIT="
            "6b76ee6f2c5a007710b5a16f97c94330d6ecc871",
            comfy_stage,
        )
        self.assertIn(
            "test -f requirements.txt",
            comfy_stage,
        )
        self.assertIn(
            "test ! -s requirements.txt",
            comfy_stage,
        )
        self.assertIn(
            'io.github.fff7d1bc.rocmplete.rgthree-comfy.license="MIT"',
            comfy_stage,
        )
        constraints = (
            Path(__file__).resolve().parents[1]
            / "applications"
            / "comfyui"
            / "constraints.txt"
        ).read_text()
        self.assertIn("comfyui-manager==4.2.2\n", constraints)
        self.assertIn(
            "chmod 0644 /opt/rocmplete/container_download.py",
            containerfile,
        )
        self.assertIn(
            "COPY containers/content_tools/requirements.txt "
            "/opt/content-requirements.txt",
            containerfile,
        )
        self.assertEqual(
            containerfile.count(
                "COPY containers/content_tools/download.py"
            ),
            2,
        )
        self.assertEqual(
            containerfile.count("COPY containers/common/profile.py"),
            1,
        )
        wheel_targets = ",".join(
            "device-{}".format(item) for item in SUPPORTED_ARCHITECTURES
        )
        self.assertIn("torch[{}]".format(wheel_targets), containerfile)
        cmake_targets = ";".join(SUPPORTED_ARCHITECTURES)
        self.assertIn(
            '"-DGPU_TARGETS={}"'.format(cmake_targets), containerfile
        )
        image_targets = ",".join(SUPPORTED_ARCHITECTURES)
        self.assertEqual(
            containerfile.count(
                'io.github.fff7d1bc.rocmplete.gpu.targets="{}"'.format(
                    image_targets
                )
            ),
            4,
        )
        self.assertIn(
            "COPY applications/llama-cpp/hip-apu-host-buffer.patch",
            containerfile,
        )
        self.assertIn(
            "COPY applications/llama-cpp/reasoning-controls.patch",
            containerfile,
        )
        self.assertIn(
            "COPY applications/llama-cpp/quantized-kv-flash-attention.patch",
            containerfile,
        )
        self.assertIn(
            "COPY applications/llama-cpp/vulkan-f16-kv-contiguize.patch",
            containerfile,
        )
        self.assertIn("-DGGML_HIP=ON", containerfile)
        self.assertIn("-DGGML_VULKAN=ON", containerfile)
        self.assertIn(
            '"mesa-vulkan-drivers=${MESA_VULKAN_ROCM714_VERSION}"',
            containerfile,
        )
        self.assertIn('"glslc=${GLSLC_ROCM714_VERSION}"', containerfile)
        llama_entrypoint = (
            root / "applications" / "llama-cpp" / "entrypoint.sh"
        ).read_text()
        for architecture, profile in ARCHITECTURE_PROFILES.items():
            self.assertIn(
                "        {}) detected_profile={} ;;".format(
                    architecture, profile
                ),
                llama_entrypoint,
            )
        self.assertIn("export GGML_VK_FA_KV_CONTIG=1", llama_entrypoint)
        self.assertIn(
            "git apply --check "
            "/opt/rocmplete/llama-hip-apu-host-buffer.patch",
            containerfile,
        )
        self.assertIn(
            "git apply --check "
            "/opt/rocmplete/llama-reasoning-controls.patch",
            containerfile,
        )
        self.assertIn(
            "git apply --check "
            "/opt/rocmplete/llama-quantized-kv-flash-attention.patch",
            containerfile,
        )
        self.assertIn(
            "git apply --check "
            "/opt/rocmplete/llama-vulkan-f16-kv-contiguize.patch",
            containerfile,
        )
        reasoning_patch = (
            root
            / "applications"
            / "llama-cpp"
            / "reasoning-controls.patch"
        ).read_text()
        self.assertIn("tools/server/server-common.cpp", reasoning_patch)
        self.assertNotIn("rocmplete_maki", reasoning_patch)
        self.assertNotIn("thinking_budget_tokens", reasoning_patch)
        self.assertNotIn("reasoning_effort", reasoning_patch)
        self.assertIn("common/arg.cpp", reasoning_patch)
        self.assertIn("common/common.h", reasoning_patch)
        self.assertIn("tools/server/server-common.h", reasoning_patch)
        self.assertIn("tools/server/server-context.cpp", reasoning_patch)
        self.assertIn(
            "--sampling-defaults-by-reasoning", reasoning_patch
        )
        self.assertIn(
            "parse_sampling_defaults_by_reasoning", reasoning_patch
        )
        self.assertIn(
            "rocmplete_apply_sampling_defaults", reasoning_patch
        )
        self.assertIn('body.find(item.key())', reasoning_patch)
        self.assertIn(
            'enable_thinking ? "thinking" : "non_thinking"',
            reasoning_patch,
        )
        self.assertNotIn("rocmplete_sampling_profile", reasoning_patch)
        self.assertNotIn('"qwen3.6-27b"', reasoning_patch)
        self.assertNotIn('"qwen3.8-27b"', reasoning_patch)
        self.assertIn("it->is_null()", reasoning_patch)
        self.assertIn(
            "/opt/rocmplete/container_profile.py", comfy_stage
        )
        self.assertIn(
            "/opt/rocmplete/rocmplete/hardware_profiles.py", comfy_stage
        )
        self.assertIn("chmod 1777 /tmp/comfy", comfy_stage)
        comfy_entrypoint = (
            root / "applications" / "comfyui" / "entrypoint.sh"
        ).read_text()
        self.assertIn(
            'custom_python_root="/data/custom-node-python"',
            comfy_entrypoint,
        )
        self.assertIn(
            '"$image_python" -m venv --without-pip',
            comfy_entrypoint,
        )
        self.assertIn(
            "export ROCMLETE_CUSTOM_NODE_ENV=1",
            comfy_entrypoint,
        )
        self.assertIn(
            "for name in ComfyUI-GGUF rgthree-comfy",
            comfy_entrypoint,
        )
        self.assertIn(
            'persistent_node_overrides+=("$name")',
            comfy_entrypoint,
        )
        self.assertIn(
            "profile_args+=(--cpu --disable-all-custom-nodes)",
            comfy_entrypoint,
        )
        extra_paths = (
            root / "applications" / "comfyui" / "extra-model-paths.yaml"
        ).read_text()
        self.assertIn(
            "custom_nodes: /tmp/rocmplete-bundled-custom-nodes",
            extra_paths,
        )
        self.assertIn(
            'org.opencontainers.image.licenses="BSD-3-Clause AND '
            'GPL-3.0-only AND Apache-2.0 AND MIT"',
            comfy_stage,
        )
    def test_build_command(self):
        command = build_command(Path("/project"), "localhost/test:latest", True)
        self.assertEqual(
            command,
            [
                "podman",
                "build",
                "--tag",
                "localhost/test:latest",
                "--file",
                "/project/Containerfile",
                "--target",
                "comfyui",
                "--no-cache",
                "/project",
            ],
        )

    def test_build_command_mounts_the_host_pip_cache(self):
        command = build_command(
            Path("/project"),
            "localhost/test:latest",
            True,
            pip_cache_dir=Path("/cache/rocmplete/build/pip"),
            volume_suffix=":rw,Z",
        )
        self.assertIn("PIP_NO_CACHE_DIR=", command)
        self.assertIn(
            "PIP_CACHE_DIR=/var/cache/rocmplete/pip", command
        )
        self.assertIn(
            "/cache/rocmplete/build/pip:"
            "/var/cache/rocmplete/pip:rw,Z",
            command,
        )
        self.assertIn("--no-cache", command)

    def test_build_cache_uses_xdg_or_home_cache_location(self):
        self.assertEqual(
            build_cache_dir(
                {
                    "HOME": "/home/test",
                    "XDG_CACHE_HOME": "/cache/test",
                }
            ),
            Path("/cache/test/rocmplete/build"),
        )
        self.assertEqual(
            build_cache_dir({"HOME": "/home/test"}),
            Path("/home/test/.cache/rocmplete/build"),
        )
        with self.assertRaisesRegex(LauncherError, "absolute"):
            build_cache_dir(
                {"HOME": "/home/test", "XDG_CACHE_HOME": "relative"}
            )

    def test_prepare_pip_build_cache_creates_only_the_owned_path(self):
        with tempfile.TemporaryDirectory() as directory:
            cache = prepare_pip_build_cache(
                {"HOME": directory, "XDG_CACHE_HOME": directory}
            )
            self.assertEqual(
                cache, Path(directory) / "rocmplete/build/pip"
            )
            self.assertTrue(cache.is_dir())

    def test_prepare_pip_build_cache_refuses_owned_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            cache_home = Path(directory) / "cache"
            destination = Path(directory) / "elsewhere"
            cache_home.mkdir()
            destination.mkdir()
            (cache_home / "rocmplete").symlink_to(
                destination, target_is_directory=True
            )
            with self.assertRaisesRegex(LauncherError, "symlinked"):
                prepare_pip_build_cache(
                    {"HOME": directory, "XDG_CACHE_HOME": str(cache_home)}
                )

    def test_application_build_uses_exact_local_base_without_pulling(self):
        command = build_command(
            Path("/project"),
            "localhost/test:latest",
            True,
            target="comfyui",
            base_image="localhost/rocmplete:base",
        )
        self.assertIn(
            "ROCM_BASE_IMAGE=localhost/rocmplete:base", command
        )
        self.assertIn("--pull=never", command)
        self.assertIn("--no-cache", command)
        self.assertEqual(command[command.index("--target") + 1], "comfyui")

    def test_native_build_uses_exact_local_runtime_without_pulling(self):
        command = build_command(
            Path("/project"),
            "localhost/test:latest",
            target="llama-cpp",
            runtime_image="localhost/rocmplete:runtime",
        )
        self.assertIn(
            "ROCM_RUNTIME_IMAGE=localhost/rocmplete:runtime", command
        )
        self.assertIn("--pull=never", command)

    def test_cpu_run_has_no_devices(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="cpu",
                listen="127.0.0.1",
                port=8188,
                data_dir=Path("/data/comfy"),
                comfy_args=("--quick-test-for-ci",),
            ),
            ":rw",
        )
        self.assertNotIn("--device", command)
        self.assertNotIn("--network", command)
        self.assertEqual(command.count("--publish"), 1)
        self.assertIn("127.0.0.1:8188:8188/tcp", command)
        self.assertEqual(command[command.index("--userns") + 1], "keep-id")
        self.assertRegex(command[command.index("--umask") + 1], r"^0[0-7]{3}$")
        self.assertIn("ROCMLETE_PROFILE=cpu", command)
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.managed=true", command
        )
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.application=comfyui", command
        )
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.role=application", command
        )
        self.assertIn("ROCMLETE_LISTEN=0.0.0.0", command)
        self.assertIn("ROCMLETE_HOST_LISTEN=127.0.0.1", command)
        self.assertIn("/data/comfy/apps/comfyui:/data:rw", command)
        self.assertIn(
            "/data/comfy/content/comfyui/models:/content/models:ro",
            command,
        )
        self.assertNotIn("/content/native", command)
        self.assertIn("core=0:0", command)
        self.assertEqual(command[-2:], ["localhost/test", "--quick-test-for-ci"])

    def test_comfy_manager_passthrough_preserves_confinement(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="cpu",
                listen="192.168.1.50",
                port=8188,
                data_dir=Path("/data/comfy"),
                comfy_args=("--enable-manager",),
            ),
            ":rw",
        )
        self.assertIn("--read-only", command)
        self.assertEqual(command[command.index("--cap-drop") + 1], "all")
        self.assertIn("192.168.1.50:8188:8188/tcp", command)
        self.assertEqual(command[-2:], ["localhost/test", "--enable-manager"])

    def test_gpu_run_applies_optional_security_and_lifecycle_flags(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="strix-halo",
                listen="0.0.0.0",
                port=9000,
                data_dir=Path("/data/comfy"),
                render_nodes=("/dev/dri/renderD129",),
                detach=True,
                unconfined=True,
            ),
            ":rw,Z",
        )
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD129", command)
        self.assertIn("seccomp=unconfined", command)
        self.assertIn("--detach", command)
        self.assertIn("0.0.0.0:9000:9000/tcp", command)
        self.assertEqual(command[command.index("--network") + 1], "pasta:-4")
        self.assertIn("ROCMLETE_HOST_LISTEN=0.0.0.0", command)
        self.assertIn("/data/comfy/apps/comfyui:/data:rw,Z", command)
        self.assertNotIn("/content/native", command)

    def test_comfyui_multi_gpu_exposes_only_the_selected_set(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="rdna4",
                listen="127.0.0.1",
                port=8188,
                data_dir=Path("/data/comfy"),
                render_nodes=(
                    "/dev/dri/renderD128",
                    "/dev/dri/renderD130",
                ),
            ),
            ":rw",
        )
        self.assertEqual(command.count("--device"), 3)
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD128", command)
        self.assertIn("/dev/dri/renderD130", command)
        self.assertNotIn("/dev/dri/renderD129", command)

    def test_ipv6_host_publication_is_bracketed(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="cpu",
                listen="::1",
                port=8188,
                data_dir=Path("/data/comfy"),
            ),
            ":rw",
        )
        self.assertIn("[::1]:8188:8188/tcp", command)
        self.assertIn("ROCMLETE_LISTEN=::", command)
        self.assertNotIn("--network", command)

    def test_custom_container_name_and_extension_opt_out(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="rdna4",
                listen="127.0.0.1",
                port=8190,
                data_dir=Path("/data/comfy"),
                render_nodes=("/dev/dri/renderD128",),
                container_name="rocmplete-benchmark",
                container_role="benchmark",
                disable_bundled_extensions=True,
                environment=(
                    "HOME=/data/benchmarks/.cache/test/home",
                    "TRITON_CACHE_DIR=/data/benchmarks/.cache/test/triton",
                ),
            ),
            ":rw",
        )
        self.assertIn("rocmplete-benchmark", command)
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.role=benchmark", command
        )
        self.assertIn(
            "ROCMLETE_DISABLE_BUNDLED_EXTENSIONS=1", command
        )
        self.assertIn("HOME=/data/benchmarks/.cache/test/home", command)
        self.assertIn(
            "TRITON_CACHE_DIR=/data/benchmarks/.cache/test/triton", command
        )

    def test_unpublished_web_workload_is_network_isolated(self):
        command = web_command(
            WebOptions(
                image="localhost/comfyui",
                application="comfyui",
                container_name="rocmplete-benchmark",
                profile="strix-point",
                listen="127.0.0.1",
                port=8188,
                data_dir=Path("/data/rocmplete"),
                render_nodes=("/dev/dri/renderD128",),
                publish=False,
                network_none=True,
                container_role="benchmark",
                environment=("ROCMLETE_TEST_MODE=acceptance",),
            ),
            ":rw,Z",
        )
        self.assertNotIn("--publish", command)
        self.assertEqual(command[command.index("--network") + 1], "none")
        self.assertIn("ROCMLETE_TEST_MODE=acceptance", command)
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.role=benchmark", command
        )
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD128", command)

    def test_shell_retains_container_hardening(self):
        command = shell_command(
            "localhost/test", Path("/data/comfy"), ":rw"
        )
        self.assertIn("--read-only", command)
        self.assertNotIn("--network", command)
        self.assertNotIn("--publish", command)
        self.assertEqual(command[command.index("--userns") + 1], "keep-id")
        self.assertIn("no-new-privileges", command)
        self.assertIn("core=0:0", command)
        self.assertEqual(command[-1], "localhost/test")
        self.assertIn("/data/comfy/apps/comfyui:/data:rw", command)

    def test_experimental_policy_sets_kernel_environment(self):
        command = web_command(
            WebOptions(
                image="localhost/test",
                profile="rdna4",
                listen="127.0.0.1",
                port=8188,
                data_dir=Path("/data/comfy"),
                render_nodes=("/dev/dri/renderD128",),
                memory_policy="conservative",
                kernel_policy="experimental",
            ),
            ":rw",
        )
        self.assertIn("ROCMLETE_MEMORY_POLICY=conservative", command)
        self.assertIn("ROCMLETE_KERNEL_POLICY=experimental", command)
        self.assertIn("TORCH_ROCM_AOTRITON_ENABLE_EXPERIMENTAL=1", command)
        self.assertIn("TORCH_BLAS_PREFER_HIPBLASLT=1", command)

    def test_gpu_diagnostic_retains_runtime_hardening(self):
        command = gpu_diagnostic_command(
            "localhost/test", ("/dev/dri/renderD128",)
        )
        self.assertEqual(command[command.index("--userns") + 1], "keep-id")
        self.assertEqual(command[command.index("--network") + 1], "none")
        self.assertIn("--read-only", command)
        self.assertIn("--cap-drop", command)
        self.assertIn("no-new-privileges", command)
        self.assertIn("core=0:0", command)
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD128", command)
        probe = command[command.index("-c") + 1]
        self.assertIn("torch.arange(1024, device='cuda:%d' % i)", probe)
        self.assertIn('print("GPU operation: passed")', probe)
        self.assertIn(
            "assert sorted(nodes) == sorted(expected)", probe
        )
        self.assertIn("unsupported GPU architecture(s)", probe)
        self.assertLess(
            probe.index("unsupported GPU architecture(s)"),
            probe.index("torch.arange(1024"),
        )

    def test_gpu_diagnostic_probes_every_selected_gpu(self):
        command = gpu_diagnostic_command(
            "localhost/test",
            ("/dev/dri/renderD128", "/dev/dri/renderD129"),
        )
        self.assertEqual(command.count("--device"), 3)
        self.assertIn("/dev/dri/renderD128", command)
        self.assertIn("/dev/dri/renderD129", command)
        probe = command[command.index("-c") + 1]
        self.assertIn("assert count == len(expected)", probe)
        self.assertIn("for i in range(count)", probe)
        self.assertIn("assert len(set(architectures)) == 1", probe)

    def test_cpu_diagnostic_asserts_gpu_devices_are_absent(self):
        command = cpu_isolation_diagnostic_command("localhost/test")
        self.assertNotIn("--device", command)
        self.assertEqual(command[command.index("--network") + 1], "none")
        probe = command[command.index("-c") + 1]
        self.assertIn("assert not pathlib.Path('/dev/kfd').exists()", probe)
        self.assertIn("assert not nodes", probe)

    def test_gpu_diagnostic_parser_requires_device_isolation_result(self):
        with self.assertRaisesRegex(LauncherError, "GPU devices"):
            parse_gpu_diagnostic_output(
                "PyTorch: test\n"
                "ROCm/HIP: test\n"
                "Device: test\n"
                "Architecture: gfx1150\n"
                "GPU operation: passed"
            )
        with self.assertRaisesRegex(LauncherError, "did not pass"):
            parse_gpu_diagnostic_output(
                "PyTorch: test\n"
                "ROCm/HIP: test\n"
                "Device: test\n"
                "Architecture: gfx1150\n"
                "GPU operation: passed\n"
                "GPU devices: failed"
            )

    def test_llama_server_mounts_only_model_directory_and_private_state(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="strix-halo",
                mode="server",
                model=Path("/models/qwen/model.gguf"),
                data_dir=Path("/data/rocmplete"),
                render_nodes=("/dev/dri/renderD129",),
                listen="127.0.0.1",
                port=8080,
                context=8192,
                detach=True,
            ),
            ":rw,Z",
        )
        self.assertIn("--read-only", command)
        self.assertIn("no-new-privileges", command)
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD129", command)
        self.assertIn("/models/qwen:/content/models:ro,z", command)
        self.assertIn(
            "/data/rocmplete/apps/llama-cpp:/data:rw,Z", command
        )
        self.assertIn("ROCMLETE_PROFILE=strix-halo", command)
        self.assertIn("ROCMLETE_LLAMA_BACKEND=rocm", command)
        self.assertIn("ROCMLETE_LLAMA_MODE=server", command)
        self.assertNotIn("--network", command)
        self.assertEqual(command.count("--publish"), 1)
        self.assertIn("127.0.0.1:8080:8080/tcp", command)
        self.assertIn("ROCMLETE_LISTEN=0.0.0.0", command)
        self.assertIn("ROCMLETE_HOST_LISTEN=127.0.0.1", command)
        self.assertIn("--detach", command)
        self.assertEqual(command[-2:], ["--ctx-size", "8192"])

    def test_llama_server_supports_retained_named_benchmark_lifecycle(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="strix-halo",
                mode="server",
                managed_model="qwen/model.gguf",
                data_dir=Path("/data/rocmplete"),
                render_nodes=("/dev/dri/renderD128",),
                detach=True,
                container_name="rocmplete-llama-speculative-benchmark",
                container_role="benchmark",
                auto_remove=False,
            ),
            ":rw",
        )
        self.assertNotIn("--rm", command)
        self.assertEqual(
            command[command.index("--name") + 1],
            "rocmplete-llama-speculative-benchmark",
        )
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.role=benchmark", command
        )
        self.assertIn("--detach", command)

    def test_llama_multi_gpu_passes_exact_devices_and_managed_count(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="rdna4",
                mode="server",
                managed_model="large/model.gguf",
                data_dir=Path("/data/rocmplete"),
                render_nodes=(
                    "/dev/dri/renderD128",
                    "/dev/dri/renderD129",
                ),
            ),
            ":rw",
        )
        self.assertEqual(command.count("--device"), 3)
        self.assertIn("ROCMLETE_GPU_COUNT=2", command)
        self.assertIn("ROCMLETE_SOURCE_REVISION=", command)
        self.assertIn(
            "ROCMLETE_RENDER_NODES=/dev/dri/renderD128,/dev/dri/renderD129",
            command,
        )
        self.assertIn("/dev/dri/renderD128", command)
        self.assertIn("/dev/dri/renderD129", command)

    def test_llama_vulkan_backend_is_selected_explicitly(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="strix-halo",
                mode="server",
                backend="vulkan",
                managed_model="large/model.gguf",
                data_dir=Path("/data/rocmplete"),
                render_nodes=("/dev/dri/renderD128",),
            ),
            ":rw",
        )
        self.assertIn("ROCMLETE_LLAMA_BACKEND=vulkan", command)
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD128", command)

    def test_llama_ipv4_wildcard_uses_ipv4_only_pasta(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="server",
                managed_model="small/model.gguf",
                data_dir=Path("/data/rocmplete"),
                listen="0.0.0.0",
                port=8080,
            ),
            ":rw",
        )
        self.assertEqual(command[command.index("--network") + 1], "pasta:-4")
        self.assertIn("ROCMLETE_LISTEN=0.0.0.0", command)
        self.assertIn("0.0.0.0:8080:8080/tcp", command)

    def test_llama_ipv6_publication_uses_ipv6_container_bind(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="server",
                managed_model="small/model.gguf",
                data_dir=Path("/data/rocmplete"),
                listen="::1",
                port=8080,
            ),
            ":rw",
        )
        self.assertNotIn("--network", command)
        self.assertIn("ROCMLETE_LISTEN=::", command)
        self.assertIn("[::1]:8080:8080/tcp", command)

    def test_llama_entrypoint_manages_layer_split_for_multi_gpu(self):
        entrypoint = (
            Path(__file__).resolve().parents[1]
            / "applications"
            / "llama-cpp"
            / "entrypoint.sh"
        ).read_text()
        self.assertIn(
            "profile_args+=(--split-mode layer)", entrypoint
        )
        self.assertIn(
            "bench_profile_args+=(--split-mode layer)", entrypoint
        )
        self.assertIn('print "split-mode = layer"', entrypoint)
        self.assertIn(
            "expected $gpu_count visible $backend GPUs", entrypoint
        )

    def test_llama_managed_chat_templates_are_readable_in_image(self):
        root = Path(__file__).resolve().parents[1]
        containerfile = (root / "Containerfile").read_text()
        llama_stage = containerfile.split(
            "FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp\n", 1
        )[1]
        self.assertIn(
            "chmod 0444 \\\n"
            "        /usr/local/share/rocmplete/"
            "llama-chat-templates/*.jinja",
            llama_stage,
        )

        entrypoint = (
            root / "applications" / "llama-cpp" / "entrypoint.sh"
        ).read_text()
        self.assertIn(
            '[[ -f "$chat_template_path" && -r "$chat_template_path" ]]',
            entrypoint,
        )
        self.assertIn(
            "ROCMLETE_LLAMA_SAMPLING_DEFAULTS",
            entrypoint,
        )
        self.assertIn("--sampling-defaults-by-reasoning", entrypoint)
        self.assertIn('"$sampling_defaults"', entrypoint)
        self.assertIn("/tmp/rocmplete-llama-runtime", entrypoint)
        self.assertIn("printf 'profile=%s\\n'", entrypoint)
        self.assertIn("printf 'architecture=%s\\n'", entrypoint)
        self.assertIn("printf 'api_key=%s\\n'", entrypoint)
        self.assertIn(
            '""|kat-coder-v2.5|muse-glimmer-atem|qwen3-0.6b|qwen3.6|qwen3.8|'
            'translategemma-manual',
            entrypoint,
        )

    def test_llama_production_image_uses_the_modular_rocm_runtime(self):
        root = Path(__file__).resolve().parents[1]
        containerfile = (root / "Containerfile").read_text()
        shared_runtime_stage = containerfile.split(
            "FROM ${UBUNTU_IMAGE} AS rocm-runtime\n", 1
        )[1].split("FROM ${ROCM_RUNTIME_IMAGE} AS rocm-base\n", 1)[0]
        sdk_stage = containerfile.split(
            "FROM ${ROCM_RUNTIME_IMAGE} AS native-rocm-sdk\n", 1
        )[1].split("FROM native-rocm-sdk AS llama-rocm-sdk\n", 1)[0]
        llama_sdk_stage = containerfile.split(
            "FROM native-rocm-sdk AS llama-rocm-sdk\n", 1
        )[1].split("FROM llama-rocm-sdk AS llama-builder\n", 1)[0]
        builder_stage = containerfile.split(
            "FROM llama-rocm-sdk AS llama-builder\n", 1
        )[1].split("FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp\n", 1)[0]
        runtime_stage = containerfile.split(
            "FROM ${ROCM_RUNTIME_IMAGE} AS llama-cpp\n", 1
        )[1]
        wheel_targets = ",".join(
            "device-{}".format(item) for item in SUPPORTED_ARCHITECTURES
        )

        self.assertIn(
            "rocm[libraries,{}]==${{ROCM_VERSION}}".format(wheel_targets),
            shared_runtime_stage,
        )
        self.assertIn(
            "rocm[devel]==${ROCM_VERSION}",
            sdk_stage,
        )
        self.assertIn("rocm-sdk init", sdk_stage)
        self.assertIn("libvulkan-dev", llama_sdk_stage)
        self.assertIn(
            "-DCMAKE_PREFIX_PATH=$(rocm-sdk path --cmake)", builder_stage
        )
        self.assertIn("_rocm_sdk_core/lib", builder_stage)
        self.assertIn("_rocm_sdk_libraries/lib", builder_stage)
        self.assertNotIn("python -m pip install", runtime_stage)
        self.assertNotIn("devel", runtime_stage)
        self.assertNotIn("torch", runtime_stage.lower())
        self.assertIn("python -m pip check", shared_runtime_stage)
        self.assertIn("XDG_CACHE_HOME=/tmp", runtime_stage)

        self.assertNotIn("LLAMA_ROCM_VERSION", containerfile)
        self.assertNotIn("LLAMA_UBUNTU_IMAGE", containerfile)

    def test_ubuntu_base_records_snapshot_tag_and_immutable_digest(self):
        root = Path(__file__).resolve().parents[1]
        lines = (root / "Containerfile").read_text().splitlines()
        self.assertIn("resolute-20260724.1 (26.04)", lines[0])
        argument = next(
            line for line in lines if line.startswith("ARG UBUNTU_IMAGE=")
        )
        prefix = "ARG UBUNTU_IMAGE=docker.io/library/ubuntu@sha256:"
        self.assertTrue(argument.startswith(prefix))
        digest = argument.removeprefix(prefix)
        self.assertEqual(len(digest), 64)
        self.assertTrue(
            all(
                character in "0123456789abcdef"
                for character in digest
            )
        )

    def test_dwarfstar_image_is_locally_built_from_pinned_source(self):
        root = Path(__file__).resolve().parents[1]
        containerfile = (root / "Containerfile").read_text()
        builder_stage = containerfile.split(
            "FROM native-rocm-sdk AS dwarfstar-builder\n", 1
        )[1].split("FROM ${ROCM_RUNTIME_IMAGE} AS dwarfstar\n", 1)[0]
        runtime_stage = containerfile.split(
            "FROM ${ROCM_RUNTIME_IMAGE} AS dwarfstar\n", 1
        )[1]

        self.assertIn(
            "ARG DWARFSTAR_COMMIT="
            "84cc882352757baf628a1776badf7cc54d584e28",
            containerfile,
        )
        self.assertIn("https://github.com/antirez/ds4.git", builder_stage)
        self.assertIn(
            'test "$(git rev-parse HEAD)" = "${DWARFSTAR_COMMIT}"',
            builder_stage,
        )
        self.assertIn("make -j\"$(nproc)\" strix-halo", builder_stage)
        self.assertIn("--offload-jobs=jobserver", builder_stage)
        self.assertIn("git apply --check", builder_stage)
        self.assertIn(
            "/opt/rocmplete/dwarfstar-multiarch-wmma-fallback.patch",
            builder_stage,
        )
        for architecture in ("gfx1150", "gfx1151", "gfx1200", "gfx1201"):
            self.assertIn(
                "--offload-arch={}".format(architecture), builder_stage
            )
        multiarch_patch = (
            root
            / "applications"
            / "dwarfstar"
            / "multiarch-wmma-fallback.patch"
        ).read_text()
        self.assertIn("g_gpu_device_major == 11", multiarch_patch)
        self.assertIn("defined(__gfx1150__)", multiarch_patch)
        self.assertIn("defined(__gfx1151__)", multiarch_patch)
        self.assertIn("_rocm_sdk_core/lib", builder_stage)
        self.assertIn("_rocm_sdk_libraries/lib", builder_stage)
        self.assertIn(
            "COPY --from=dwarfstar-builder /opt/dwarfstar-install/",
            runtime_stage,
        )
        self.assertNotIn("git ", runtime_stage)
        self.assertNotIn("python -m pip install", runtime_stage)
        self.assertIn(
            'io.github.fff7d1bc.rocmplete.gpu.targets="'
            'gfx1150,gfx1151,gfx1200,gfx1201"',
            runtime_stage,
        )

    def test_managed_gpu_images_follow_the_project_rocm_version(self):
        root = Path(__file__).resolve().parents[1]
        containerfile = (root / "Containerfile").read_text()
        version_line = next(
            line
            for line in containerfile.splitlines()
            if line.startswith("ARG ROCM_VERSION=")
        )
        rocm_version = version_line.partition("=")[2]
        tag_version = (
            rocm_version[:-2]
            if rocm_version.endswith(".0")
            else rocm_version
        )

        for application, spec in APPLICATIONS.items():
            self.assertIn(
                "-rocm{}-".format(tag_version),
                spec.image,
                "{} image diverges from ROCM_VERSION".format(application),
            )

    def test_llama_cpu_cli_has_no_devices_and_no_network(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="cli",
                model=Path("/models/model.gguf"),
                data_dir=Path("/data/rocmplete"),
                prompt="hello",
            ),
            ":rw",
        )
        self.assertNotIn("--device", command)
        self.assertEqual(command[command.index("--network") + 1], "none")
        self.assertNotIn("--publish", command)
        self.assertEqual(
            command[-3:], ["--prompt", "hello", "--single-turn"]
        )

    def test_llama_managed_speculative_preset_is_constrained_by_environment(
        self,
    ):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="server",
                data_dir=Path("/data/rocmplete"),
                managed_model="gemma/model.gguf",
                managed_draft="gemma/draft.gguf",
                speculative_type="draft-mtp",
                draft_tokens=4,
            ),
            ":rw,Z",
        )
        self.assertIn(
            "ROCMLETE_LLAMA_MODEL=/content/models/gemma/model.gguf",
            command,
        )
        self.assertIn(
            "ROCMLETE_LLAMA_DRAFT_MODEL=/content/models/gemma/draft.gguf",
            command,
        )
        self.assertIn("ROCMLETE_LLAMA_SPECULATIVE_TYPE=draft-mtp", command)
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=4", command)
        self.assertNotIn("--spec-type", command)

    def test_llama_managed_dflash_uses_the_same_closed_environment(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="server",
                data_dir=Path("/data/rocmplete"),
                managed_model="muse/model.gguf",
                managed_draft="muse/dflash.gguf",
                speculative_type="draft-dflash",
                draft_tokens=15,
                context_override_architectures=("muse-glimmer", "dflash"),
                reasoning_preserve=True,
                context=262144,
            ),
            ":rw,Z",
        )
        self.assertIn(
            "ROCMLETE_LLAMA_SPECULATIVE_TYPE=draft-dflash", command
        )
        self.assertIn("ROCMLETE_LLAMA_DRAFT_TOKENS=15", command)
        self.assertIn(
            "ROCMLETE_LLAMA_REASONING_PRESERVE=1", command
        )
        self.assertIn(
            "ROCMLETE_LLAMA_CONTEXT_OVERRIDE="
            "muse-glimmer.context_length=int:262144,"
            "dflash.context_length=int:262144",
            command,
        )
        self.assertIn("--ctx-size", command)
        self.assertIn("262144", command)
        self.assertNotIn("--spec-type", command)

    def test_llama_entrypoint_maps_reasoning_preserve_to_llama_cpp(self):
        entrypoint = (
            Path(__file__).resolve().parents[1]
            / "applications"
            / "llama-cpp"
            / "entrypoint.sh"
        ).read_text()
        self.assertIn(
            'case "$reasoning_preserve" in', entrypoint
        )
        self.assertIn(
            "model_policy_args+=(--reasoning-preserve)", entrypoint
        )

    def test_llama_managed_model_policy_is_constrained_by_environment(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="auto",
                mode="server",
                data_dir=Path("/data/rocmplete"),
                managed_model="laguna/model.gguf",
                jinja=True,
                profile_flash_attention={
                    "rdna4": "auto",
                    "strix-halo": "off",
                    "strix-point": "off",
                },
                profile_kv_cache={"strix-halo": "q8_0"},
            ),
            ":rw,Z",
        )
        self.assertIn("ROCMLETE_LLAMA_JINJA=1", command)
        self.assertIn("ROCMLETE_LLAMA_FLASH_ATTN_RDNA4=auto", command)
        self.assertIn(
            "ROCMLETE_LLAMA_FLASH_ATTN_STRIX_HALO=off", command
        )
        self.assertIn(
            "ROCMLETE_LLAMA_FLASH_ATTN_STRIX_POINT=off", command
        )
        self.assertIn(
            "ROCMLETE_LLAMA_KV_CACHE_STRIX_HALO=q8_0", command
        )
        self.assertIn("ROCMLETE_LLAMA_KV_CACHE_RDNA4=", command)
        self.assertIn("ROCMLETE_LLAMA_KV_CACHE_STRIX_POINT=", command)
        self.assertNotIn("--jinja", command)
        self.assertNotIn("--flash-attn", command)
        self.assertNotIn("--cache-type-k", command)
        self.assertNotIn("--cache-type-v", command)

    def test_llama_entrypoint_maps_profile_kv_cache_to_direct_and_router(self):
        entrypoint = (
            Path(__file__).resolve().parents[1]
            / "applications"
            / "llama-cpp"
            / "entrypoint.sh"
        ).read_text()
        self.assertIn(
            'case "$kv_cache_strix_halo" in', entrypoint
        )
        self.assertIn(
            '--cache-type-k "$kv_cache"', entrypoint
        )
        self.assertIn(
            '--cache-type-v "$kv_cache"', entrypoint
        )
        self.assertIn(
            '/^rocmplete-kv-cache-strix-halo = /', entrypoint
        )
        self.assertIn('print "cache-type-k = " $0', entrypoint)
        self.assertIn('print "cache-type-v = " $0', entrypoint)

    def test_llama_managed_chat_template_is_constrained_by_environment(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="server",
                data_dir=Path("/data/rocmplete"),
                managed_model="translategemma/model.gguf",
                chat_template="translategemma-manual",
            ),
            ":rw,Z",
        )
        self.assertIn(
            "ROCMLETE_LLAMA_CHAT_TEMPLATE=translategemma-manual",
            command,
        )
        self.assertNotIn("--chat-template-file", command)

    def test_llama_router_mounts_only_managed_models_and_exact_preset(self):
        command = llama_command(
            LlamaOptions(
                image="localhost/llama",
                profile="cpu",
                mode="server",
                data_dir=Path("/data/rocmplete"),
                router_preset=Path(
                    "/data/rocmplete/apps/llama-cpp/models.ini"
                ),
                models_max=3,
                listen="127.0.0.1",
            ),
            ":rw,Z",
        )
        self.assertIn(
            "/data/rocmplete/content/llama-cpp/models:"
            "/content/models:ro,z",
            command,
        )
        self.assertIn(
            "/data/rocmplete/apps/llama-cpp/models.ini:"
            "/run/rocmplete/models.ini:ro,z",
            command,
        )
        self.assertIn("ROCMLETE_LLAMA_ROUTER=1", command)
        self.assertIn("ROCMLETE_LLAMA_MODELS_MAX=3", command)
        self.assertIn("ROCMLETE_LLAMA_MODEL=", command)
        self.assertNotIn("--device", command)

    def test_llama_benchmark_is_one_shot_offline_and_confined(self):
        command = llama_benchmark_command(
            LlamaBenchmarkOptions(
                image="localhost/llama",
                profile="rdna4",
                data_dir=Path("/data/rocmplete"),
                managed_model="qwen/model.gguf",
                render_nodes=("/dev/dri/renderD128",),
                repetitions=3,
                prompt_tokens=64,
                generation_tokens=32,
                context_depth=32768,
                batch_size=1024,
                ubatch_size=256,
                cache_type_k="q8_0",
                cache_type_v="q8_0",
                flash_attention="on",
            ),
            ":rw,Z",
        )
        self.assertIn("--rm", command)
        self.assertIn("--read-only", command)
        self.assertIn("no-new-privileges", command)
        self.assertEqual(command[command.index("--network") + 1], "none")
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD128", command)
        self.assertIn(
            "/data/rocmplete/content/llama-cpp/models:"
            "/content/models:ro,z",
            command,
        )
        self.assertNotIn("/data:rw", " ".join(command))
        self.assertIn("ROCMLETE_LLAMA_MODE=bench", command)
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.role=benchmark", command
        )
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.application=llama-cpp", command
        )
        self.assertIn("ROCMLETE_LLAMA_BACKEND=rocm", command)
        self.assertIn("--output", command)
        self.assertIn("json", command)
        self.assertIn("--progress", command)
        self.assertEqual(command[command.index("--n-depth") + 1], "32768")
        self.assertEqual(command[command.index("--batch-size") + 1], "1024")
        self.assertEqual(command[command.index("--ubatch-size") + 1], "256")
        self.assertEqual(
            command[command.index("--cache-type-k") + 1], "q8_0"
        )
        self.assertEqual(
            command[command.index("--cache-type-v") + 1], "q8_0"
        )
        self.assertEqual(command[command.index("--flash-attn") + 1], "on")

    def test_llama_benchmark_passes_an_exact_multi_gpu_set(self):
        command = llama_benchmark_command(
            LlamaBenchmarkOptions(
                image="localhost/llama",
                profile="rdna4",
                data_dir=Path("/data/rocmplete"),
                managed_model="qwen/model.gguf",
                render_nodes=(
                    "/dev/dri/renderD128",
                    "/dev/dri/renderD129",
                ),
            ),
            ":rw",
        )
        self.assertEqual(command.count("--device"), 3)
        self.assertIn("ROCMLETE_GPU_COUNT=2", command)
        self.assertIn("/dev/dri/renderD128", command)
        self.assertIn("/dev/dri/renderD129", command)

    def test_llama_vulkan_benchmark_records_backend_selection(self):
        command = llama_benchmark_command(
            LlamaBenchmarkOptions(
                image="localhost/llama",
                profile="strix-halo",
                backend="vulkan",
                data_dir=Path("/data/rocmplete"),
                managed_model="qwen/model.gguf",
                render_nodes=("/dev/dri/renderD128",),
            ),
            ":rw",
        )
        self.assertIn("ROCMLETE_LLAMA_BACKEND=vulkan", command)

    def test_dwarfstar_server_is_confined_to_one_model_and_gpu(self):
        command = dwarfstar_command(
            DwarfStarOptions(
                image="localhost/dwarfstar",
                mode="server",
                data_dir=Path("/data/rocmplete"),
                model=Path("/models/deepseek/model.gguf"),
                render_nodes=("/dev/dri/renderD128",),
                profile="strix-halo",
                listen="127.0.0.1",
                port=8001,
                context=131072,
                output_tokens=16000,
            ),
            ":rw,Z",
        )
        self.assertIn("--rm", command)
        self.assertIn("--read-only", command)
        self.assertIn("no-new-privileges", command)
        self.assertIn("--cap-drop", command)
        self.assertIn("/dev/kfd", command)
        self.assertIn("/dev/dri/renderD128", command)
        self.assertEqual(command.count("--device"), 2)
        self.assertIn(
            "/models/deepseek:/content/models:ro,z", command
        )
        self.assertIn(
            "/data/rocmplete/apps/dwarfstar:/data:rw,Z", command
        )
        self.assertIn("127.0.0.1:8001:8001/tcp", command)
        self.assertIn("ROCMLETE_LISTEN=0.0.0.0", command)
        self.assertIn("ROCMLETE_HOST_LISTEN=127.0.0.1", command)
        self.assertIn("ROCMLETE_DWARFSTAR_CONTEXT=131072", command)
        self.assertIn("ROCMLETE_DWARFSTAR_OUTPUT_TOKENS=16000", command)
        self.assertIn("ROCMLETE_DWARFSTAR_DSPARK=0", command)
        self.assertNotIn("ROCMLETE_DWARFSTAR_DSPARK_MODEL", command)
        self.assertIn(
            "io.github.fff7d1bc.rocmplete.application=dwarfstar", command
        )

    def test_dwarfstar_cli_is_offline_and_passes_prompt_by_environment(self):
        command = dwarfstar_command(
            DwarfStarOptions(
                image="localhost/dwarfstar",
                mode="cli",
                data_dir=Path("/data/rocmplete"),
                model=Path("/models/model.gguf"),
                render_nodes=("/dev/dri/renderD128",),
                prompt="reply exactly once",
                no_thinking=True,
            ),
            ":rw",
        )
        self.assertEqual(command[command.index("--network") + 1], "none")
        self.assertNotIn("--publish", command)
        self.assertIn(
            "ROCMLETE_DWARFSTAR_PROMPT=reply exactly once", command
        )
        self.assertIn("ROCMLETE_DWARFSTAR_NO_THINKING=1", command)
        self.assertEqual(command[-1], "localhost/dwarfstar")

    def test_dwarfstar_dspark_mounts_one_managed_directory(self):
        command = dwarfstar_command(
            DwarfStarOptions(
                image="localhost/dwarfstar",
                mode="cli",
                data_dir=Path("/data/rocmplete"),
                model=Path("/models/deepseek/target.gguf"),
                support_model=Path("/models/deepseek/support.gguf"),
                dspark=True,
                render_nodes=("/dev/dri/renderD128",),
                prompt="reply once",
            ),
            ":rw,Z",
        )
        self.assertEqual(
            command.count(
                "/models/deepseek:/content/models:ro,z"
            ),
            1,
        )
        self.assertIn("ROCMLETE_DWARFSTAR_DSPARK=1", command)
        self.assertIn(
            "ROCMLETE_DWARFSTAR_DSPARK_MODEL=/content/models/support.gguf",
            command,
        )

    def test_dwarfstar_dspark_rejects_split_model_directories(self):
        with self.assertRaisesRegex(LauncherError, "must share one directory"):
            dwarfstar_command(
                DwarfStarOptions(
                    image="localhost/dwarfstar",
                    mode="server",
                    data_dir=Path("/data/rocmplete"),
                    model=Path("/models/target.gguf"),
                    support_model=Path("/support/support.gguf"),
                    dspark=True,
                ),
                ":rw",
            )

    def test_dwarfstar_entrypoint_maps_every_supported_architecture(self):
        root = Path(__file__).resolve().parents[1]
        entrypoint = (
            root / "applications" / "dwarfstar" / "entrypoint.sh"
        ).read_text()
        self.assertIn("gfx1200|gfx1201) detected_profile=rdna4", entrypoint)
        self.assertIn("gfx1151) detected_profile=strix-halo", entrypoint)
        self.assertIn("gfx1150) detected_profile=strix-point", entrypoint)
        self.assertIn("does not match detected architecture", entrypoint)
        self.assertIn("(container namespace)", entrypoint)
        self.assertIn('common+=(--mtp "$dspark_model" --dspark)', entrypoint)
        self.assertIn('cli_sampling=(--temp 0)', entrypoint)
        self.assertIn('dspark="${ROCMLETE_DWARFSTAR_DSPARK:-0}"', entrypoint)


if __name__ == "__main__":
    unittest.main()
