"""Resolve and validate a Paracetamol execution profile inside a container."""

from __future__ import annotations

import sys
from typing import Any, NamedTuple

PROFILE_ARCHITECTURES = (
    ("rdna4", ("gfx1200", "gfx1201")),
    ("strix-halo", ("gfx1151",)),
    ("strix-point", ("gfx1150",)),
)
ARCHITECTURE_PROFILES = {
    architecture: profile
    for profile, architectures in PROFILE_ARCHITECTURES
    for architecture in architectures
}
SUPPORTED_ARCHITECTURES = tuple(sorted(ARCHITECTURE_PROFILES))
PROFILES = ("auto",) + tuple(
    profile for profile, _ in PROFILE_ARCHITECTURES
) + ("cpu",)


class ProfileInfo(NamedTuple):
    profile: str
    architecture: str
    device_name: str
    torch_version: str
    rocm_version: str


def resolve_profile(requested: str, torch_module: Any) -> ProfileInfo:
    if requested not in PROFILES:
        raise ValueError(
            "unknown profile {!r} (expected one of {})".format(
                requested, ", ".join(PROFILES)
            )
        )

    torch_version = str(torch_module.__version__)
    rocm_version = str(torch_module.version.hip or "none")
    if requested == "cpu":
        return ProfileInfo(
            "cpu",
            "cpu",
            "CPU smoke-test mode",
            torch_version,
            rocm_version,
        )

    if torch_module.version.hip is None:
        raise ValueError("this is not a ROCm-enabled PyTorch build")
    if not torch_module.cuda.is_available():
        raise ValueError("ROCm PyTorch cannot see an available GPU")

    device_count = torch_module.cuda.device_count()
    if device_count < 1:
        raise ValueError("ROCm PyTorch did not report a visible GPU")
    architectures = []
    names = []
    for index in range(device_count):
        properties = torch_module.cuda.get_device_properties(index)
        architecture = str(getattr(properties, "gcnArchName", ""))
        architecture = architecture.split(":", 1)[0]
        if not architecture:
            raise ValueError(
                "PyTorch did not report the architecture for GPU {}".format(
                    index
                )
            )
        if architecture not in ARCHITECTURE_PROFILES:
            raise ValueError(
                "unsupported GPU architecture {!r}; this image contains "
                "{}".format(architecture, ", ".join(SUPPORTED_ARCHITECTURES))
            )
        architectures.append(architecture)
        names.append(str(torch_module.cuda.get_device_name(index)))
    if len(set(architectures)) != 1:
        raise ValueError(
            "a single workload requires one GPU architecture; found {}".format(
                ", ".join(architectures)
            )
        )
    architecture = architectures[0]
    detected_profile = ARCHITECTURE_PROFILES[architecture]
    if requested != "auto" and requested != detected_profile:
        raise ValueError(
            "profile {!r} does not match detected architecture {!r} "
            "({})".format(requested, architecture, detected_profile)
        )

    return ProfileInfo(
        detected_profile,
        architecture,
        "; ".join(names),
        torch_version,
        rocm_version,
    )


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: container_profile.py PROFILE", file=sys.stderr)
        return 2

    import torch

    try:
        info = resolve_profile(sys.argv[1], torch)
    except ValueError as error:
        print("error: {}".format(error), file=sys.stderr)
        return 1

    for field in info:
        print(field)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
