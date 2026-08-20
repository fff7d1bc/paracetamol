#!/usr/bin/env python3
"""Resume one pinned HTTPS artifact inside a constrained download container."""

from __future__ import annotations

import argparse
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Optional


_CHUNK_SIZE = 8 * 1024 * 1024


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True)
    parser.add_argument("--output", required=True, type=Path)
    size = parser.add_mutually_exclusive_group(required=True)
    size.add_argument("--expected-size", type=int)
    size.add_argument("--maximum-size", type=int)
    parser.add_argument("--token-env")
    return parser


def _request(url: str, offset: int, token: str) -> urllib.request.Request:
    headers = {"User-Agent": "Paracetamol/1"}
    if offset:
        headers["Range"] = "bytes={}-".format(offset)
    request = urllib.request.Request(url, headers=headers)
    if token:
        if "\n" in token or "\r" in token:
            raise ValueError("download token contains a newline")
        # Civitai authenticates this request, then redirects to a signed
        # object-storage URL. Do not forward the credential to that URL:
        # besides leaking the token across origins, an Authorization header
        # conflicts with the storage provider's signature and yields HTTP 400.
        request.add_unredirected_header(
            "Authorization", "Bearer {}".format(token)
        )
    return request


def download(
    url: str,
    output: Path,
    expected_size: Optional[int] = None,
    token: str = "",
    maximum_size: Optional[int] = None,
) -> None:
    if not url.startswith("https://"):
        raise ValueError("download URL must use HTTPS")
    if (expected_size is None) == (maximum_size is None):
        raise ValueError(
            "exactly one expected size or maximum size is required"
        )
    if expected_size is not None and expected_size <= 0:
        raise ValueError("expected size must be positive")
    if maximum_size is not None and maximum_size <= 0:
        raise ValueError("maximum size must be positive")
    output.parent.mkdir(parents=True, exist_ok=True)
    if maximum_size is not None:
        # Archive providers may replace the bytes behind a stable version ID.
        # Starting over avoids joining a partial old ZIP to the new object.
        _download_bounded(url, output, maximum_size, token)
        return

    assert expected_size is not None
    try:
        current_size = output.stat().st_size
    except FileNotFoundError:
        current_size = 0
    if current_size == expected_size:
        return
    if current_size > expected_size:
        current_size = 0

    try:
        response = urllib.request.urlopen(
            _request(url, current_size, token)
        )
    except urllib.error.HTTPError as error:
        if error.code == 416 and output.stat().st_size == expected_size:
            return
        raise

    with response:
        status = getattr(response, "status", response.getcode())
        append = current_size > 0 and status == 206
        if append:
            content_range = response.headers.get("Content-Range", "")
            if not content_range.startswith(
                "bytes {}-".format(current_size)
            ):
                raise RuntimeError(
                    "server returned an unexpected content range"
                )
        mode = "ab" if append else "wb"
        with output.open(mode) as handle:
            while True:
                block = response.read(_CHUNK_SIZE)
                if not block:
                    break
                handle.write(block)

    actual_size = output.stat().st_size
    if actual_size != expected_size:
        raise RuntimeError(
            "downloaded size is {} bytes; expected {}".format(
                actual_size, expected_size
            )
        )


def _download_bounded(
    url: str,
    output: Path,
    maximum_size: int,
    token: str,
) -> None:
    response = urllib.request.urlopen(_request(url, 0, token))
    with response:
        content_length = response.headers.get("Content-Length")
        if content_length:
            try:
                declared_size = int(content_length)
            except ValueError:
                declared_size = 0
            if declared_size > maximum_size:
                raise RuntimeError(
                    "download declares {} bytes; maximum is {}".format(
                        declared_size, maximum_size
                    )
                )
        downloaded = 0
        with output.open("wb") as handle:
            while True:
                remaining = maximum_size - downloaded
                block = response.read(min(_CHUNK_SIZE, remaining + 1))
                if not block:
                    break
                if len(block) > remaining:
                    raise RuntimeError(
                        "download exceeds maximum size of {} bytes".format(
                            maximum_size
                        )
                    )
                handle.write(block)
                downloaded += len(block)
    if downloaded == 0:
        raise RuntimeError("downloaded file is empty")


def main() -> int:
    arguments = _parser().parse_args()
    token = (
        os.environ.get(arguments.token_env, "")
        if arguments.token_env
        else ""
    )
    try:
        download(
            arguments.url,
            arguments.output,
            arguments.expected_size,
            token,
            arguments.maximum_size,
        )
    except (OSError, RuntimeError, ValueError, urllib.error.URLError) as error:
        print("download error: {}".format(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
