import unittest
from types import SimpleNamespace

from containers.common.profile import (
    ARCHITECTURE_PROFILES,
    PROFILES,
    resolve_profile,
)


class FakeCuda:
    def __init__(self, architecture="gfx1201", available=True):
        self.architectures = (
            [architecture] if isinstance(architecture, str) else architecture
        )
        self.available = available

    def is_available(self):
        return self.available

    def device_count(self):
        return len(self.architectures)

    def get_device_properties(self, index):
        self._check_index(index)
        return SimpleNamespace(gcnArchName=self.architectures[index])

    def get_device_name(self, index):
        self._check_index(index)
        return "Test AMD GPU {}".format(index)

    def _check_index(self, index):
        if not 0 <= index < len(self.architectures):
            raise AssertionError("unexpected device index")


def fake_torch(architecture="gfx1201", hip="7.14.0", available=True):
    return SimpleNamespace(
        __version__="2.11.0+rocm7.14.0",
        version=SimpleNamespace(hip=hip),
        cuda=FakeCuda(architecture, available),
    )


class ContainerProfileTests(unittest.TestCase):
    def test_manifest_covers_supported_profiles(self):
        self.assertEqual(
            ARCHITECTURE_PROFILES,
            {
                "gfx1150": "strix-point",
                "gfx1151": "strix-halo",
                "gfx1200": "rdna4",
                "gfx1201": "rdna4",
            },
        )
        self.assertEqual(
            set(ARCHITECTURE_PROFILES.values()), set(PROFILES) - {"auto", "cpu"}
        )

    def test_auto_resolves_supported_architecture(self):
        info = resolve_profile("auto", fake_torch("gfx1151:sramecc+:xnack-"))
        self.assertEqual(info.profile, "strix-halo")
        self.assertEqual(info.architecture, "gfx1151")

    def test_homogeneous_multi_gpu_set_resolves_one_profile(self):
        info = resolve_profile("auto", fake_torch(["gfx1201", "gfx1201"]))
        self.assertEqual(info.profile, "rdna4")
        self.assertEqual(info.device_name, "Test AMD GPU 0; Test AMD GPU 1")

    def test_mixed_architecture_gpu_set_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "requires one GPU architecture"):
            resolve_profile("auto", fake_torch(["gfx1200", "gfx1201"]))

    def test_forced_profile_requires_matching_architecture(self):
        with self.assertRaisesRegex(ValueError, "does not match"):
            resolve_profile("rdna4", fake_torch("gfx1151"))

    def test_cpu_does_not_require_a_gpu(self):
        info = resolve_profile("cpu", fake_torch(hip=None, available=False))
        self.assertEqual(info.architecture, "cpu")

    def test_gpu_profile_requires_rocm_and_available_gpu(self):
        with self.assertRaisesRegex(ValueError, "not a ROCm-enabled"):
            resolve_profile("auto", fake_torch(hip=None))
        with self.assertRaisesRegex(ValueError, "cannot see"):
            resolve_profile("auto", fake_torch(available=False))

    def test_unsupported_architecture_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "unsupported GPU"):
            resolve_profile("auto", fake_torch("gfx1030"))


if __name__ == "__main__":
    unittest.main()
