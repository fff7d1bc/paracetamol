import hashlib
import io
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import Mock, patch

from rocmplete import podman
from rocmplete.bundles import (
    artifact_path,
    artifact_payload_path,
    install_artifacts,
)
from rocmplete.catalog import Artifact, ArtifactSource, LicenseInfo
from rocmplete.errors import LauncherError


def _artifact(contents=b"model", target="models"):
    return Artifact(
        identifier="test-artifact",
        description="Test artifact",
        source=ArtifactSource(
            repository="owner/repository",
            revision="a" * 40,
            path="files/model.bin",
        ),
        destination="checkpoints/model.bin",
        size=len(contents),
        sha256=hashlib.sha256(contents).hexdigest(),
        license=LicenseInfo(
            spdx="Apache-2.0",
            status="verified",
            url="https://example.invalid/source",
        ),
        target=target,
    )


class RuntimeLabelEvaluationTests(unittest.TestCase):
    def test_existing_shared_model_is_labeled_before_hashing(self):
        contents = b"good!"
        artifact = _artifact(contents)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            destination = artifact_path(data_dir, artifact)
            destination.parent.mkdir(parents=True)
            destination.write_bytes(contents)

            def relabel(path):
                self.assertEqual(path, destination)
                path.write_bytes(b"evil!")

            with patch(
                "rocmplete.bundles.podman.prepare_shared_content_label",
                side_effect=relabel,
            ), redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(
                    LauncherError, "SHA-256 mismatch for installed content"
                ):
                    install_artifacts((artifact,), data_dir, "unused")

    def test_staged_shared_model_is_labeled_before_hashing(self):
        contents = b"good!"
        artifact = _artifact(contents)
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            staged = artifact_payload_path(data_dir, artifact)
            staged.parent.mkdir(parents=True)
            staged.write_bytes(contents)

            def relabel(path):
                self.assertEqual(path, staged)
                path.write_bytes(b"evil!")

            with patch(
                "rocmplete.bundles.podman.prepare_shared_content_label",
                side_effect=relabel,
            ), redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(LauncherError, "SHA-256 mismatch"):
                    install_artifacts((artifact,), data_dir, "unused")

    def test_non_shared_content_is_not_relabelled(self):
        contents = b"good!"
        artifact = _artifact(contents, target="workflows")
        with tempfile.TemporaryDirectory() as directory:
            data_dir = Path(directory)
            destination = artifact_path(data_dir, artifact)
            destination.parent.mkdir(parents=True)
            destination.write_bytes(contents)

            with patch(
                "rocmplete.bundles.podman.prepare_shared_content_label"
            ) as relabel, redirect_stdout(io.StringIO()):
                self.assertEqual(
                    install_artifacts((artifact,), data_dir, "unused"), 0
                )
            relabel.assert_not_called()

    @patch("rocmplete.podman.selinux_volume_suffix", return_value=":rw,Z")
    @patch("rocmplete.podman.shutil.which", return_value="/usr/bin/chcon")
    @patch("rocmplete.podman.subprocess.run")
    def test_shared_label_command_matches_the_runtime_mount(
        self, run, which, volume_suffix
    ):
        run.return_value = Mock(returncode=0, stderr="")

        podman.prepare_shared_content_label(Path("/data/model.gguf"))

        run.assert_called_once_with(
            [
                "/usr/bin/chcon",
                "--no-dereference",
                "system_u:object_r:container_file_t:s0",
                "--",
                "/data/model.gguf",
            ],
            text=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
            check=False,
        )
