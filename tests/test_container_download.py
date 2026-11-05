import io
import tempfile
import unittest
import urllib.request
from pathlib import Path
from unittest.mock import patch

from containers.content_tools.download import _request, download


class FakeResponse:
    def __init__(self, contents, status=200, headers=None):
        self.contents = io.BytesIO(contents)
        self.status = status
        self.headers = headers or {}

    def __enter__(self):
        return self

    def __exit__(self, *unused):
        return None

    def getcode(self):
        return self.status

    def read(self, size):
        return self.contents.read(size)


class ContainerDownloadTests(unittest.TestCase):
    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_complete_file_needs_no_network_request(self, urlopen):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "model.bin"
            output.write_bytes(b"complete")
            download("https://example.invalid/model", output, len(b"complete"))
        urlopen.assert_not_called()

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_download_resumes_when_server_honors_range(self, urlopen):
        urlopen.return_value = FakeResponse(
            b"plete", status=206, headers={"Content-Range": "bytes 3-7/8"}
        )
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "model.bin"
            output.write_bytes(b"com")
            download(
                "https://example.invalid/model",
                output,
                len(b"complete"),
                "secret-token",
            )
            self.assertEqual(output.read_bytes(), b"complete")
        request = urlopen.call_args.args[0]
        self.assertEqual(request.get_header("Range"), "bytes=3-")
        self.assertEqual(request.get_header("Authorization"), "Bearer secret-token")

    def test_download_token_is_not_forwarded_through_redirect(self):
        request = _request(
            "https://civitai.com/api/download/models/456", 0, "secret-token"
        )
        redirected = urllib.request.HTTPRedirectHandler().redirect_request(
            request,
            None,
            307,
            "Temporary Redirect",
            {},
            "https://object-storage.example.invalid/signed-download",
        )
        self.assertIsNotNone(redirected)
        self.assertIsNone(redirected.get_header("Authorization"))

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_download_restarts_when_server_ignores_range(self, urlopen):
        urlopen.return_value = FakeResponse(b"replacement", status=200)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "model.bin"
            output.write_bytes(b"partial")
            download(
                "https://example.invalid/model", output, len(b"replacement")
            )
            self.assertEqual(output.read_bytes(), b"replacement")

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_download_rejects_unexpected_final_size(self, urlopen):
        urlopen.return_value = FakeResponse(b"short")
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError, "expected 10"):
                download(
                    "https://example.invalid/model",
                    Path(directory) / "model.bin",
                    10,
                )

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_exact_download_bounds_streamed_bytes_in_every_range_mode(self, urlopen):
        for prefix, status in [(b"", 200), (b"com", 206), (b"com", 200)]:
            for declared in [None, "1"]:
                with self.subTest(prefix=prefix, status=status, declared=declared):
                    headers = {"Content-Range": "bytes 3-99/100"}
                    if declared is not None:
                        headers["Content-Length"] = declared
                    response = FakeResponse(b"x" * 100, status=status, headers=headers)
                    urlopen.return_value = response
                    with tempfile.TemporaryDirectory() as directory:
                        output = Path(directory) / "model.bin"
                        output.write_bytes(prefix)
                        with self.assertRaisesRegex(RuntimeError, "exceeds expected"):
                            download("https://example.invalid/model", output, 8)
                        self.assertLessEqual(output.stat().st_size, 8)
                        if status == 206:
                            self.assertTrue(output.read_bytes().startswith(prefix))
                        allowance = 8 - len(prefix) if status == 206 else 8
                        self.assertLessEqual(response.contents.tell(), allowance + 1)

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_exact_download_rejects_oversized_declaration_before_mutation(self, urlopen):
        for status, declared in [(200, "9"), (206, "6")]:
            with self.subTest(status=status):
                response = FakeResponse(
                    b"x" * 100,
                    status=status,
                    headers={"Content-Length": declared, "Content-Range": "bytes 3-8/9"},
                )
                urlopen.return_value = response
                with tempfile.TemporaryDirectory() as directory:
                    output = Path(directory) / "model.bin"
                    output.write_bytes(b"com")
                    with self.assertRaisesRegex(RuntimeError, "declares"):
                        download("https://example.invalid/model", output, 8)
                    self.assertEqual(output.read_bytes(), b"com")
                    self.assertEqual(response.contents.tell(), 0)

    @patch("containers.content_tools.download.urllib.request.urlopen")
    @patch("containers.content_tools.download._CHUNK_SIZE", 4)
    def test_exact_download_never_writes_overflow_after_full_chunks(self, urlopen):
        for prefix in [b"", b"com"]:
            with self.subTest(prefix=prefix):
                status = 206 if prefix else 200
                urlopen.return_value = FakeResponse(
                    b"x" * 100, status=status,
                    headers={"Content-Range": "bytes 3-99/100"},
                )
                with tempfile.TemporaryDirectory() as directory:
                    output = Path(directory) / "model.bin"
                    output.write_bytes(prefix)
                    with self.assertRaisesRegex(RuntimeError, "exceeds expected"):
                        download("https://example.invalid/model", output, 8)
                    self.assertLessEqual(output.stat().st_size, 8)

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_bounded_download_stops_at_the_size_limit(self, urlopen):
        urlopen.return_value = FakeResponse(b"x" * 11)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "workflow.zip"
            with self.assertRaisesRegex(RuntimeError, "exceeds maximum"):
                download(
                    "https://example.invalid/workflow", output, maximum_size=10
                )
            self.assertLessEqual(output.stat().st_size, 10)

    @patch("containers.content_tools.download.urllib.request.urlopen")
    def test_bounded_download_rejects_short_eof_despite_successful_reads(self, urlopen):
        urlopen.return_value = FakeResponse(b"short", headers={"Content-Length": "10"})
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "workflow.zip"
            with self.assertRaisesRegex(RuntimeError, "server declared 10"):
                download("https://example.invalid/workflow", output, maximum_size=20)
            self.assertEqual(output.read_bytes(), b"short")

    def test_download_requires_one_size_policy_and_https(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "model.bin"
            with self.assertRaisesRegex(ValueError, "exactly one"):
                download("https://example.invalid/model", output)
            with self.assertRaisesRegex(ValueError, "must use HTTPS"):
                download("http://example.invalid/model", output, 10)


if __name__ == "__main__":
    unittest.main()
