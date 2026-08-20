import importlib.util
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PATCH_PATH = ROOT / "applications" / "comfyui" / "patch_manager.py"
SPEC = importlib.util.spec_from_file_location(
    "rocmplete_comfy_manager_patch", PATCH_PATH
)
PATCH_MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PATCH_MODULE)


class ComfyManagerPatchTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "common").mkdir()
        (self.root / "legacy").mkdir()
        loopback = """def is_loopback(address):
    import ipaddress
    try:
        return ipaddress.ip_address(address).is_loopback
    except ValueError:
        return False
"""
        (self.root / "common" / "manager_security.py").write_text(loopback)
        (self.root / "legacy" / "manager_server.py").write_text(loopback)
        (self.root / "common" / "manager_util.py").write_text(
            """def make_pip_cmd(cmd):
    global use_uv
    base_cmd = get_pip_cmd(force_uv=use_uv)
    return base_cmd + cmd
"""
        )

    def test_patch_uses_host_publication_and_persistent_python(self):
        PATCH_MODULE.patch_manager(self.root)
        common = (self.root / "common" / "manager_security.py").read_text()
        legacy = (self.root / "legacy" / "manager_server.py").read_text()
        manager_util = (self.root / "common" / "manager_util.py").read_text()
        for text in (common, legacy):
            self.assertIn('os.environ.get("ROCMLETE_HOST_LISTEN", address)', text)
        self.assertIn(
            'os.environ.get("ROCMLETE_CUSTOM_NODE_ENV") != "1"', manager_util
        )
        self.assertIn("get_pip_cmd(force_uv=force_uv)", manager_util)

    def test_patch_fails_closed_after_upstream_text_changes(self):
        (self.root / "common" / "manager_security.py").write_text(
            "def is_loopback(address):\n    return False\n"
        )
        with self.assertRaises(SystemExit):
            PATCH_MODULE.patch_manager(self.root)


if __name__ == "__main__":
    unittest.main()
