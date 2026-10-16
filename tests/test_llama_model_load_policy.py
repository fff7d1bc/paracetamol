"""Closed container policy, without model bytes or a GPU."""
import os
from pathlib import Path
import subprocess
import unittest


POLICY = Path(__file__).resolve().parents[1] / "applications/llama-cpp/model-load-policy.sh"


class LlamaModelLoadPolicyTests(unittest.TestCase):
    def invoke(self, function, *args, input=None):
        return subprocess.run(
            ["bash", "-euc", 'source "$1"; shift; "$@"', "policy-test", str(POLICY), function, *args],
            text=True, capture_output=True, check=True, input=input,
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
            if backend == "rocm":
                expected["no-kv-unified"] = "true"
            self.assertEqual(ini, expected)
            if backend == "rocm":
                self.assertEqual(ini["lazy-mode"], "on-direct")
                for key in ("no-unified-memory", "no-host", "no-kv-unified"):
                    self.assertEqual(ini[key], "true")
                self.assertNotIn("parallel", ini)
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

    def test_slots_share_context_without_partitioning_or_multiplying_it(self):
        for slots in (1, 4):
            cache_flag = "no-kv-unified" if slots == 1 else "kv-unified"
            self.assertEqual(self.invoke("llama_server_slot_args", str(slots), "1"),
                             ["--parallel", str(slots), f"--{cache_flag}"])
            self.assertEqual(self.invoke("llama_server_slot_args", str(slots), "0"),
                             ["--parallel", str(slots), "--kv-unified"])
            source = "version = 1\n[flash]\nc = 262144\nno-kv-unified = true\n[dense]\nc = 262144\n"
            result = "\n".join(self.invoke("llama_server_slot_ini", str(slots), input=source))
            self.assertIn(f"[flash]\nparallel = {slots}\n{cache_flag} = true\nc = 262144", result)
            self.assertIn(f"[dense]\nparallel = {slots}\nkv-unified = true\nc = 262144", result)
        for slots, separate in (("0", "1"), ("65", "1"), ("4", "bad")):
            with self.assertRaises(subprocess.CalledProcessError):
                self.invoke("llama_server_slot_args", slots, separate)
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke("llama_server_slot_ini", "4", input="version = 1\n[bad]\nc = 0\n")


if __name__ == "__main__":
    unittest.main()
