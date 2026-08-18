import re
import unittest
from pathlib import Path
from urllib.parse import unquote


PROJECT_ROOT = Path(__file__).parents[1]
MARKDOWN_LINK = re.compile(r"!?\[[^\]]*\]\((<[^>]+>|[^)\s]+)")
MARKDOWN_HEADING = re.compile(r"^#{1,6}\s+(.+?)\s*#*\s*$")
URI_SCHEME = re.compile(r"^[A-Za-z][A-Za-z0-9+.-]*:")


def _documentation_files():
    files = list(PROJECT_ROOT.glob("*.md"))
    for directory in ("catalog", "docs"):
        files.extend((PROJECT_ROOT / directory).rglob("*.md"))
    return sorted(files)


def _lines_outside_fences(path):
    fence = None
    for line_number, line in enumerate(
        path.read_text(encoding="utf-8").splitlines(), 1
    ):
        stripped = line.lstrip()
        if stripped.startswith(("```", "~~~")):
            marker = stripped[:3]
            if fence is None:
                fence = marker
            elif marker == fence:
                fence = None
            continue
        if fence is None:
            yield line_number, line


def _github_heading_anchors(path):
    anchors = set()
    seen = {}

    for _, line in _lines_outside_fences(path):
        match = MARKDOWN_HEADING.match(line)
        if match is None:
            continue
        heading = match.group(1)
        heading = re.sub(r"\[([^\]]+)\]\([^)]+\)", r"\1", heading)
        heading = re.sub(r"<[^>]+>", "", heading).replace("`", "")
        base = re.sub(r"[^\w\- ]", "", heading.lower())
        base = re.sub(r"\s", "-", base)
        duplicate = seen.get(base, 0)
        seen[base] = duplicate + 1
        anchors.add(base if duplicate == 0 else "{}-{}".format(base, duplicate))

    return anchors


class DocumentationTests(unittest.TestCase):
    def test_local_markdown_links_and_anchors_resolve(self):
        documents = _documentation_files()
        self.assertGreater(len(documents), 10)
        anchor_cache = {}
        errors = []

        for document in documents:
            for line_number, line in _lines_outside_fences(document):
                for match in MARKDOWN_LINK.finditer(line):
                    target = match.group(1)
                    if target.startswith("<"):
                        target = target[1:-1]
                    if URI_SCHEME.match(target) or target.startswith("//"):
                        continue

                    resource, separator, fragment = target.partition("#")
                    resource = unquote(resource)
                    fragment = unquote(fragment)
                    destination = (
                        document
                        if not resource
                        else (document.parent / resource).resolve()
                    )
                    location = "{}:{}".format(
                        document.relative_to(PROJECT_ROOT), line_number
                    )
                    if not destination.is_relative_to(PROJECT_ROOT):
                        errors.append(
                            "{}: local link leaves the repository: {}".format(
                                location, target
                            )
                        )
                        continue
                    if not destination.exists():
                        errors.append(
                            "{}: local link does not exist: {}".format(
                                location, target
                            )
                        )
                        continue
                    if (
                        separator
                        and fragment
                        and destination.suffix.lower() == ".md"
                    ):
                        anchors = anchor_cache.setdefault(
                            destination, _github_heading_anchors(destination)
                        )
                        if fragment not in anchors:
                            errors.append(
                                "{}: Markdown anchor does not exist: {}".format(
                                    location, target
                                )
                            )

        self.assertFalse(errors, "\n" + "\n".join(errors))


if __name__ == "__main__":
    unittest.main()
