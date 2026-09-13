"""Closed container policy, without model bytes or a GPU."""
import os
from pathlib import Path
import subprocess
import unittest


POLICY = Path(__file__).resolve().parents[1] / "applications/llama-cpp/model-load-policy.sh"


class LlamaModelLoadPolicyTests(unittest.TestCase):
    def invoke(self, function, *args):
        return subprocess.run(
            ["bash", "-euc", 'source "$1"; shift; "$@"', "policy-test", str(POLICY), function, *args],
            text=True, capture_output=True, check=True,
            env={**os.environ, "GGML_CUDA_ENABLE_UNIFIED_MEMORY": "1"},
        ).stdout.splitlines()

    def test_direct_and_router_share_the_same_tuple(self):
        for backend in ("rocm", "vulkan"):
            args = self.invoke("llama_stream_token_embedding_args", backend, "server")
            expected = {}
            index = 0
            while index < len(args):
                key = args[index].removeprefix("--")
                index += 1
                value = "true"
                if index < len(args) and not args[index].startswith("--"):
                    value = args[index]
                    index += 1
                expected[key] = value
            ini = dict(line.split(" = ", 1) for line in self.invoke("llama_stream_token_embedding_ini", backend))
            self.assertEqual(ini, expected)
            if backend == "rocm":
                self.assertEqual(ini["lazy-mode"], "on-direct")
                for key in ("no-unified-memory", "no-host", "no-kv-unified"):
                    self.assertEqual(ini[key], "true")
                self.assertEqual(ini["parallel"], "1")
                self.assertEqual(ini["cache-ram"], "0")
                self.assertEqual(ini["batch-size"], "2048")
                self.assertEqual(ini["ubatch-size"], "2048")
            else:
                self.assertEqual(ini, {"load-mode": "mmap", "lazy-mode": "on", "override-tensor": "per_layer_token_embd.weight=CPU"})

    def test_cli_and_failure_boundaries(self):
        args = self.invoke("llama_stream_token_embedding_args", "rocm", "cli")
        self.assertIn("--no-unified-memory", args)
        self.assertNotIn("--parallel", args)
        self.assertNotIn("--no-kv-unified", args)
        for backend, mode in (("rocm", "bench"), ("unknown", "server")):
            with self.assertRaises(subprocess.CalledProcessError):
                self.invoke("llama_stream_token_embedding_args", backend, mode)


if __name__ == "__main__":
    unittest.main()
