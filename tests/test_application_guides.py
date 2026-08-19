import io
import os
import shlex
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

from rocmplete.application_guides import (
    APPLICATION_GUIDES,
    guide_commands,
    print_application_guide,
)
from rocmplete.cli_parser import parse_arguments
from rocmplete.config import APPLICATIONS, APPLICATION_NAMES
from rocmplete.recipes import application_recipes


PROJECT_ROOT = Path(__file__).parents[1]


class _TerminalBuffer(io.StringIO):
    def isatty(self):
        return True


class ApplicationGuideTests(unittest.TestCase):
    def test_every_application_has_one_guide(self):
        self.assertEqual(tuple(APPLICATION_GUIDES), APPLICATION_NAMES)
        for identifier, guide in APPLICATION_GUIDES.items():
            with self.subTest(application=identifier):
                self.assertEqual(guide.application, identifier)
                self.assertTrue(guide.title)
                self.assertTrue(guide.summary)
                self.assertTrue(guide.reference.startswith("docs/guides/"))
                reference_path = guide.reference.partition("#")[0]
                self.assertTrue((PROJECT_ROOT / reference_path).is_file())

    def test_guides_include_every_application_recipe(self):
        for application, guide in APPLICATION_GUIDES.items():
            commands = set(guide_commands(guide))
            for recipe in application_recipes(application):
                with self.subTest(
                    application=application,
                    recipe=recipe.identifier,
                ):
                    self.assertIn(
                        "./rocmplete content install {} {}".format(
                            application, recipe.identifier
                        ),
                        commands,
                    )

    def test_every_rocmplete_guide_command_still_parses(self):
        for application, guide in APPLICATION_GUIDES.items():
            for command in guide_commands(guide):
                with self.subTest(
                    application=application,
                    command=command,
                ), redirect_stdout(io.StringIO()), redirect_stderr(
                    io.StringIO()
                ):
                    try:
                        _, arguments = parse_arguments(
                            shlex.split(command)[1:]
                        )
                    except SystemExit as result:
                        self.assertEqual(result.code, 0)
                    else:
                        self.assertIsNotNone(arguments.command)

    def test_web_guide_urls_use_registered_ports(self):
        for application in ("comfyui",):
            guide = APPLICATION_GUIDES[application]
            text = " ".join(
                section_text
                for section in guide.sections
                for section_text in section.paragraphs
            )
            self.assertIn(
                "127.0.0.1:{}".format(APPLICATIONS[application].port),
                text,
            )

    def test_dwarfstar_guide_explains_bind_and_model_boundaries(self):
        guide = APPLICATION_GUIDES["dwarfstar"]
        text = " ".join(
            section_text
            for section in guide.sections
            for section_text in section.paragraphs
        )
        commands = guide_commands(guide)
        self.assertIn("private container namespace", text)
        self.assertIn("host publication remains 127.0.0.1", text)
        self.assertIn("Without --model", text)
        self.assertIn("3.9 generated tokens per second", text)
        self.assertIn("feasibility observation", text)
        self.assertIn(
            "./rocmplete run dwarfstar server --model "
            "/path/to/deepseek-v4.gguf",
            commands,
        )

    def test_terminal_guide_uses_semantic_color_roles(self):
        output = _TerminalBuffer()
        with patch.dict(
            os.environ, {"TERM": "xterm-256color"}, clear=True
        ), redirect_stdout(output):
            self.assertEqual(print_application_guide("llama-cpp"), 0)

        text = output.getvalue()
        normalized = " ".join(text.split())
        self.assertIn("\033[1;36mllama.cpp\033[0m", text)
        self.assertIn("\033[1;32mStart here\033[0m", text)
        self.assertIn(
            "\033[1;34mServe one model\033[0m",
            text,
        )
        self.assertIn("Use an OpenAI-compatible client for automation", text)
        self.assertNotIn("./rocmplete client", text)
        self.assertIn(
            "qwen3.6 recipe installs dense 27B MTP Q8_0 and sparse "
            "35B-A3B MTP Dynamic Q8_K_XL together",
            normalized,
        )
        self.assertIn(
            "separate qwen3.8 recipe installs dense 27B Dynamic",
            text,
        )
        self.assertIn(
            "Dense Qwen3.8 27B MTP at native medium effort is the common "
            "managed-client default",
            normalized,
        )
        self.assertIn("non-MTP control", normalized)
        self.assertIn("MTP proposes and verifies extra tokens", text)
        self.assertIn("not a reasoning mode", text)
        self.assertIn(
            "Qwen3.6, Qwen3.8, KAT-Coder, Gemma 4, and Muse "
            "families have maintained 256K presets",
            normalized,
        )
        self.assertIn("--context 131072", text)
        self.assertIn("omit it for a mixed-model router", normalized)
        self.assertIn("Tool-using clients", text)
        self.assertIn("reviewed Jinja", text)
        self.assertIn("fixed managed template", normalized)
        self.assertIn("./rocmplete agent pi", text)
        self.assertIn("bin/pi", text)
        self.assertIn("./rocmplete agent maki", text)
        self.assertIn("bin/maki", text)
        self.assertIn("Pi package commands such as install", normalized)
        self.assertIn("trusted executable inputs", normalized)
        self.assertIn("content install llama-cpp qwen3.6", text)
        self.assertIn("content install llama-cpp shisa-v2.1", text)
        self.assertIn("Shisa V2.1", text)
        self.assertIn("starts at 16K", text)
        self.assertIn("content install llama-cpp all --dry-run", text)
        self.assertIn("Muse reasons unconditionally", normalized)
        self.assertIn(
            "Qwen3.8 exposes instant, low, medium, and xhigh", normalized
        )
        self.assertIn("native low, medium, high, and xhigh strength", normalized)
        self.assertIn(
            "Pi's model shortcut and bare /model group presets by family",
            normalized,
        )
        self.assertIn("Shift+Tab, /settings", normalized)
        self.assertIn("Maki uses /thinking", normalized)
        self.assertNotIn("agent opencode", text)
        self.assertNotIn("agent omp", text)
        self.assertIn("\033[1;33mNetwork access\033[0m", text)
        self.assertIn(
            "\033[1;36m./rocmplete build llama-cpp\033[0m",
            text,
        )
        self.assertIn("\033[2mGGUF terminal chat", text)

    def test_redirected_guide_output_stays_plain(self):
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(print_application_guide("llama-cpp"), 0)

        self.assertNotIn("\033[", output.getvalue())


if __name__ == "__main__":
    unittest.main()
