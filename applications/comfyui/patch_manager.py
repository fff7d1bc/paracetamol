"""Adapt pinned ComfyUI Manager to Paracetamol's container boundaries."""

import importlib.metadata
import os
from pathlib import Path


def replace_once(path, old, new, label):
    text = path.read_text()
    if text.count(old) != 1:
        raise SystemExit(
            "unexpected ComfyUI Manager source while patching " + label
        )
    path.write_text(text.replace(old, new))


def patch_manager(root):
    common_security = root / "common" / "manager_security.py"
    replace_once(
        common_security,
        """def is_loopback(address):
    import ipaddress
    try:
""",
        """def is_loopback(address):
    import ipaddress
    address = os.environ.get("PARACETAMOL_HOST_LISTEN", address)
    try:
""",
        "effective listen address",
    )

    legacy_server = root / "legacy" / "manager_server.py"
    replace_once(
        legacy_server,
        """def is_loopback(address):
    import ipaddress
    try:
""",
        """def is_loopback(address):
    import ipaddress
    address = os.environ.get("PARACETAMOL_HOST_LISTEN", address)
    try:
""",
        "legacy effective listen address",
    )

    manager_util = root / "common" / "manager_util.py"
    replace_once(
        manager_util,
        """    global use_uv
    base_cmd = get_pip_cmd(force_uv=use_uv)
""",
        """    global use_uv
    force_uv = (
        use_uv
        and os.environ.get("PARACETAMOL_CUSTOM_NODE_ENV") != "1"
    )
    base_cmd = get_pip_cmd(force_uv=force_uv)
""",
        "persistent custom-node package installer",
    )


def installed_manager_root():
    override = os.environ.get("PARACETAMOL_MANAGER_PACKAGE_DIR")
    if override:
        return Path(override)
    distribution = importlib.metadata.distribution("comfyui-manager")
    return Path(distribution.locate_file("comfyui_manager"))


if __name__ == "__main__":
    patch_manager(installed_manager_root())
