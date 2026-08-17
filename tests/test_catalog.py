import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from rocmplete.catalog import (
    DEFAULT_CATALOG_PATH,
    load_catalog,
    load_content_packs,
)
from rocmplete.errors import LauncherError


class CatalogTests(unittest.TestCase):
    def _catalog_copy(self, directory, raw):
        root = Path(directory)
        path = root / "catalog.json"
        path.write_text(json.dumps(raw))
        return path

    def _content_pack(self, artifact_identifier="private-model"):
        contents = b"private model"
        return {
            "schema_version": 2,
            "artifacts": {
                artifact_identifier: {
                    "description": "Private model",
                    "source": {
                        "repository": "owner/private-repository",
                        "revision": "a" * 40,
                        "path": "models/private-model.bin",
                    },
                    "destination": "checkpoints/private-model.bin",
                    "size": len(contents),
                    "sha256": (
                        "cf50cbcd8b2e1d9c20c8e0e2c5f052fe14733b68"
                        "45f5709b3b72c4f6607f1177"
                    ),
                    "license": {
                        "spdx": "LicenseRef-Private",
                        "status": "verified",
                        "url": "https://example.invalid/private-license",
                    },
                }
            },
            "bundles": {
                "private-bootstrap": {
                    "description": "Private bootstrap content",
                    "application": "comfyui",
                    "artifacts": [artifact_identifier],
                    "groups": ["all", "comfyui"],
                }
            },
        }

    def _archive_collection(self):
        contents = b"workflow"
        return {
            "source": {
                "provider": "civitai",
                "model_id": 123,
                "model_version_id": 456,
                "filename": "workflow.zip",
            },
            "archive": {"max_size": 4096},
            "license": {
                "spdx": "MIT",
                "status": "verified",
                "url": "https://example.invalid/license",
            },
            "members": {
                "fixture-archive-workflow": {
                    "description": "Archive workflow fixture",
                    "member": "pack/workflow.json",
                    "destination": "fixtures/archive-workflow.json",
                    "target": "workflows",
                    "size": len(contents),
                    "sha256": (
                        "da7f739f627198465eeab537a6f7a435"
                        "dc4a0c332f9e4a8462293eb3f4ab7ee0"
                    ),
                }
            },
        }

    def test_default_catalog_contains_all_application_bundles(self):
        catalog = load_catalog()
        self.assertEqual(len(catalog.bundles), 51)
        self.assertEqual(len(catalog.artifacts), 66)
        self.assertEqual(len(catalog.benchmarks), 28)
        self.assertEqual(len(catalog.llama_presets), 17)
        self.assertEqual(len(catalog.llama_sampling_policies), 3)
        self.assertFalse(
            [
                artifact.identifier
                for artifact in catalog.artifacts.values()
                if artifact.source.provider == "civitai"
            ],
            "the built-in catalog must not depend on mutable Civitai files",
        )
        krea = catalog.bundle("krea-2-turbo-fp8-base")
        self.assertIn("krea", krea.groups)
        self.assertNotIn("comfyui", krea.groups)
        self.assertEqual(
            [item.identifier for item in catalog.bundle_agreements(krea)],
            ["krea-2-community-license"],
        )
        llama = catalog.llama_preset("qwen3-0.6b-q8-0")
        self.assertEqual(llama.bundle, "llama-qwen3-0.6b-q8-0")
        self.assertEqual(llama.artifact, "qwen3-0.6b-q8-gguf")
        self.assertEqual(llama.default_context, 4096)
        self.assertFalse(llama.jinja)
        self.assertEqual(llama.chat_template, "qwen3-0.6b")
        self.assertEqual(
            catalog.artifact(llama.artifact).target, "llama-models"
        )
        dwarfstar = catalog.bundle(
            "dwarfstar-deepseek-v4-flash-0731-q2-imatrix"
        )
        self.assertEqual(dwarfstar.application, "dwarfstar")
        self.assertEqual(dwarfstar.groups, ("all", "dwarfstar"))
        dwarfstar_artifact = catalog.artifact(dwarfstar.artifacts[0])
        self.assertEqual(dwarfstar_artifact.target, "dwarfstar-models")
        self.assertEqual(dwarfstar_artifact.size, 86720111488)
        self.assertEqual(
            dwarfstar_artifact.sha256,
            "ca22ae2f838e14077c22bc1c1417b71b45b5e5a3687bd96c"
            "2ac6e17fdb6261c0",
        )
        self.assertEqual(dwarfstar_artifact.license.spdx, "MIT")
        self.assertEqual(dwarfstar_artifact.license.status, "verified")
        dspark = catalog.bundle(
            "dwarfstar-deepseek-v4-flash-0731-q2-imatrix-dspark"
        )
        self.assertEqual(
            dspark.artifacts,
            (
                "deepseek-v4-flash-0731-iq2xxs-gguf",
                "deepseek-v4-flash-0731-dspark-support-gguf",
            ),
        )
        support = catalog.artifact(dspark.artifacts[1])
        self.assertEqual(support.size, 5989114272)
        self.assertEqual(
            support.sha256,
            "7e319924541db3f7a163ed7e11d7532a70d48228ab59d36cb81e"
            "1d4511885360",
        )
        self.assertEqual(support.license.spdx, "MIT")
        assistant = catalog.llama_preset("qwen3.6-27b-q8-0")
        assistant_artifact = catalog.artifact(assistant.artifact)
        self.assertEqual(
            assistant.bundle, "llama-qwen3.6-27b-q8-0"
        )
        self.assertEqual(assistant.default_context, 262144)
        self.assertEqual(
            catalog.bundle_size(catalog.bundle(assistant.bundle)),
            28595763424,
        )
        self.assertEqual(
            assistant_artifact.sha256,
            "f93f517f38e696d35a1a7df2c0e3155a64f4c4dc"
            "d662107a146ae263f7fb14ce",
        )
        assistant_mtp = catalog.llama_preset("qwen3.6-27b-mtp-q8-0")
        assistant_mtp_artifact = catalog.artifact(assistant_mtp.artifact)
        self.assertEqual(
            assistant_mtp.bundle, "llama-qwen3.6-27b-mtp-q8-0"
        )
        self.assertEqual(assistant_mtp.default_context, 262144)
        self.assertEqual(assistant_mtp.speculative_type, "draft-mtp")
        self.assertEqual(assistant_mtp.draft_tokens, 3)
        self.assertEqual(
            assistant_mtp.flash_attention, {"strix-halo": "on"}
        )
        self.assertEqual(
            assistant_mtp.kv_cache, {"strix-halo": "q8_0"}
        )
        self.assertEqual(
            catalog.bundle_size(catalog.bundle(assistant_mtp.bundle)),
            29047084160,
        )
        self.assertEqual(
            assistant_mtp_artifact.sha256,
            "9408dcb356cc061a05c139e5647cbde0698ff980c"
            "6a69f7fc214e9989f86cfa8",
        )
        qwen35 = catalog.llama_preset("qwen3.6-35b-a3b-ud-q8-k-xl")
        qwen35_artifact = catalog.artifact(qwen35.artifact)
        self.assertEqual(qwen35.default_context, 262144)
        self.assertEqual(qwen35.speculative_type, "")
        self.assertEqual(qwen35.draft_tokens, 0)
        self.assertTrue(qwen35.agent_tools)
        self.assertEqual(
            catalog.bundle_size(catalog.bundle(qwen35.bundle)),
            38451182560,
        )
        self.assertEqual(
            qwen35_artifact.sha256,
            "b762215c5f507f4865df4ac3d1afa803828afa41"
            "e05ecac3fac431a67bbd88e8",
        )
        qwen35_mtp = catalog.llama_preset(
            "qwen3.6-35b-a3b-mtp-ud-q8-k-xl"
        )
        qwen35_mtp_artifact = catalog.artifact(qwen35_mtp.artifact)
        self.assertEqual(qwen35_mtp.default_context, 262144)
        self.assertEqual(qwen35_mtp.speculative_type, "draft-mtp")
        self.assertEqual(qwen35_mtp.draft_tokens, 3)
        self.assertTrue(qwen35_mtp.agent_tools)
        self.assertEqual(qwen35_mtp.flash_attention, {})
        self.assertEqual(qwen35_mtp.kv_cache, {})
        self.assertEqual(
            catalog.bundle_size(catalog.bundle(qwen35_mtp.bundle)),
            39099447584,
        )
        self.assertEqual(
            qwen35_mtp_artifact.sha256,
            "6c6b816537abad90b250a0972b345466028d861dd"
            "fe316d5f0de31ca6440f781",
        )
        coding_candidates = {
            "kat-coder-v2.5-dev-q8-0": (
                "bartowski/Kwaipilot_KAT-Coder-V2.5-Dev-GGUF",
                36914690464,
                "5fa510f44779b0e3d38a6678985f417a1c65e3000405ca5d6dcf7fd065e47a15",
                "Apache-2.0",
            ),
        }
        for identifier, expected in coding_candidates.items():
            with self.subTest(preset=identifier):
                preset = catalog.llama_preset(identifier)
                artifact = catalog.artifact(preset.artifact)
                self.assertEqual(preset.default_context, 262144)
                self.assertTrue(preset.agent_tools)
                self.assertEqual(preset.reasoning_control, "")
                self.assertEqual(artifact.source.repository, expected[0])
                self.assertEqual(artifact.size, expected[1])
                self.assertEqual(artifact.sha256, expected[2])
                self.assertEqual(artifact.license.spdx, expected[3])
                self.assertEqual(artifact.license.status, "verified")
        kat = catalog.llama_preset("kat-coder-v2.5-dev-q8-0")
        self.assertFalse(kat.jinja)
        self.assertEqual(kat.chat_template, "kat-coder-v2.5")
        self.assertFalse(kat.reasoning_preserve)
        gemma = catalog.llama_preset("gemma4-31b-it-q8-0-mtp")
        self.assertEqual(gemma.default_context, 262144)
        self.assertEqual(gemma.speculative_type, "draft-mtp")
        self.assertEqual(gemma.draft_tokens, 4)
        self.assertTrue(gemma.jinja)
        self.assertTrue(gemma.agent_tools)
        self.assertEqual(gemma.reasoning_control, "")
        self.assertEqual(
            gemma.draft_artifact, "gemma4-31b-it-mtp-q8-gguf"
        )
        gemma_artifact = catalog.artifact(gemma.artifact)
        self.assertEqual(
            gemma_artifact.source.repository,
            "ggml-org/gemma-4-31B-it-GGUF",
        )
        self.assertEqual(
            gemma_artifact.sha256,
            "fcd52cebacb165a98df5abe6fb70dbf076835f4a"
            "06e064ffb33dd739b8835c9c",
        )
        self.assertEqual(
            catalog.bundle_size(catalog.bundle(gemma.bundle)),
            33150364000,
        )
        muse = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"
        )
        muse_base = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl"
        )
        muse_256k = catalog.llama_preset(
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
        )
        muse_artifact = catalog.artifact(muse.artifact)
        muse_draft = catalog.artifact(muse.draft_artifact)
        self.assertEqual(muse.default_context, 131072)
        self.assertEqual(muse_base.default_context, 131072)
        self.assertEqual(muse_base.speculative_type, "")
        self.assertEqual(muse_base.artifact, muse.artifact)
        self.assertEqual(muse_base.bundle, muse.bundle)
        self.assertEqual(muse.speculative_type, "draft-dflash")
        self.assertEqual(muse.draft_tokens, 15)
        self.assertEqual(muse.draft_tokens_by_backend, {"vulkan": 4})
        self.assertEqual(muse.draft_tokens_for_backend("rocm"), 15)
        self.assertEqual(muse.draft_tokens_for_backend("vulkan"), 4)
        self.assertEqual(muse.context_override_architectures, ())
        self.assertEqual(muse_256k.artifact, muse.artifact)
        self.assertEqual(muse_256k.draft_artifact, muse.draft_artifact)
        self.assertEqual(muse_256k.bundle, muse.bundle)
        self.assertEqual(muse_256k.default_context, 262144)
        self.assertEqual(muse_256k.draft_tokens, 12)
        self.assertEqual(muse_256k.draft_tokens_by_backend, {"vulkan": 4})
        self.assertEqual(muse_256k.draft_tokens_for_backend("rocm"), 12)
        self.assertEqual(muse_256k.draft_tokens_for_backend("vulkan"), 4)
        self.assertEqual(
            muse_256k.context_override_architectures,
            ("muse-glimmer", "dflash"),
        )
        for preset in (
            muse_base,
            muse,
            muse_256k,
        ):
            with self.subTest(preset=preset.identifier):
                self.assertFalse(preset.jinja)
                self.assertEqual(
                    preset.chat_template, "muse-glimmer-atem"
                )
                self.assertTrue(preset.agent_tools)
                self.assertEqual(preset.reasoning_control, "strength")
                self.assertEqual(
                    preset.reasoning_levels,
                    ("low", "medium", "high", "xhigh"),
                )
                self.assertEqual(preset.reasoning_default, "high")
                self.assertFalse(preset.reasoning_off)
                self.assertTrue(preset.reasoning_preserve)
        self.assertEqual(
            muse_artifact.source.repository,
            "meta-models/Muse-Glimmer-30B-GGUF",
        )
        self.assertEqual(
            muse_artifact.source.revision,
            "93769bc7ab5ad1e9cd22d857e3138cf5d977ae81",
        )
        self.assertEqual(
            muse_artifact.source.path,
            "muse-glimmer-30B-kquant-dynamic.gguf",
        )
        self.assertEqual(muse_artifact.size, 19653957984)
        self.assertEqual(
            muse_artifact.sha256,
            "513109c8319115f69eb09fb7b118c97c8167d15bc014fd7670d2e30489bf106c",
        )
        self.assertEqual(
            muse_draft.source.repository,
            "meta-models/Muse-Glimmer-30B-GGUF",
        )
        self.assertEqual(muse_draft.size, 1631205312)
        self.assertEqual(
            muse_draft.sha256,
            "27d9a805fa29b943cfb6ad4843367cd4eaaaf06bd452d8cc3e00a2cd18a677bc",
        )
        self.assertEqual(
            catalog.bundle_size(catalog.bundle(muse.bundle)),
            21285163296,
        )
        muse_template = (
            DEFAULT_CATALOG_PATH.parent.parent
            / "applications"
            / "llama-cpp"
            / "chat-templates"
            / "muse-glimmer-atem.jinja"
        )
        self.assertEqual(
            hashlib.sha256(muse_template.read_bytes()).hexdigest(),
            "4849b801303b351a82dab37107a665410"
            "070cd58315fadccd8f5fde02084bd34",
        )
        self.assertIn(
            "'low' if enable_thinking is defined and "
            "enable_thinking is false else 'high'",
            muse_template.read_text(),
        )
        managed_template_hashes = {
            "kat-coder-v2.5.jinja": (
                "e409e9daee03f51b2612d96f0a253027"
                "baec06abd1c2429e184380479662d416"
            ),
            "qwen3-0.6b.jinja": (
                "a55ee1b1660128b7098723e0abcd92caa"
                "0788061051c62d51cbe87d9cf1974d8"
            ),
            "qwen3.6.jinja": (
                "ea69920311f2efccf6343675490b27bd2"
                "2d03787ebb8ccaf6e9101bfeba72898"
            ),
            "qwen3.8.jinja": (
                "7e450592d49f8ee825815fa3d7eb7f51"
                "02200d4e5e18571cc68ed66540ce9e31"
            ),
        }
        for filename, expected_hash in managed_template_hashes.items():
            with self.subTest(template=filename):
                template = muse_template.parent / filename
                self.assertEqual(
                    hashlib.sha256(template.read_bytes()).hexdigest(),
                    expected_hash,
                )
        qwen38_template = (
            muse_template.parent / "qwen3.8.jinja"
        ).read_text()
        self.assertIn("reasoning_effort|default('medium')", qwen38_template)
        self.assertIn("('xhigh', 'medium', 'low')", qwen38_template)
        self.assertIn(
            "resolved_reasoning_effort == 'xhigh'", qwen38_template
        )
        self.assertIn(
            "resolved_reasoning_effort == 'low'", qwen38_template
        )
        self.assertNotIn(
            "resolved_reasoning_effort == 'medium'", qwen38_template
        )
        self.assertNotIn(
            "resolved_reasoning_effort == 'high'", qwen38_template
        )
        qwen_presets = (
            assistant,
            assistant_mtp,
            qwen35,
            qwen35_mtp,
        )
        self.assertTrue(all(not preset.jinja for preset in qwen_presets))
        self.assertTrue(
            all(preset.chat_template == "qwen3.6" for preset in qwen_presets)
        )
        self.assertEqual(assistant.sampling_policy, "qwen3.6-27b")
        self.assertEqual(assistant_mtp.sampling_policy, "qwen3.6-27b")
        self.assertEqual(qwen35.sampling_policy, "qwen3.6-35b-a3b")
        self.assertEqual(qwen35_mtp.sampling_policy, "qwen3.6-35b-a3b")
        dense_sampling = catalog.llama_sampling_policy("qwen3.6-27b")
        sparse_sampling = catalog.llama_sampling_policy(
            "qwen3.6-35b-a3b"
        )
        self.assertEqual(dense_sampling.thinking["presence_penalty"], 0.0)
        self.assertEqual(sparse_sampling.thinking["presence_penalty"], 1.5)
        self.assertEqual(dense_sampling.non_thinking["temperature"], 0.7)
        self.assertTrue(
            all(preset.reasoning_control == "toggle" for preset in qwen_presets)
        )
        self.assertTrue(
            all(preset.reasoning_default == "on" for preset in qwen_presets)
        )
        self.assertTrue(all(preset.reasoning_off for preset in qwen_presets))
        qwen38 = catalog.llama_preset("qwen3.8-27b-ud-q8-k-xl")
        qwen38_mtp = catalog.llama_preset(
            "qwen3.8-27b-mtp-ud-q8-k-xl"
        )
        qwen38_artifact = catalog.artifact(qwen38.artifact)
        self.assertEqual(qwen38.bundle, qwen38_mtp.bundle)
        self.assertEqual(qwen38.artifact, qwen38_mtp.artifact)
        self.assertEqual(qwen38.default_context, 262144)
        self.assertFalse(qwen38.jinja)
        self.assertEqual(qwen38.chat_template, "qwen3.8")
        self.assertEqual(qwen38.sampling_policy, "qwen3.8-27b")
        self.assertTrue(qwen38.agent_tools)
        self.assertEqual(qwen38.reasoning_control, "effort")
        self.assertEqual(
            qwen38.reasoning_levels, ("low", "medium", "xhigh")
        )
        self.assertEqual(qwen38.reasoning_default, "medium")
        self.assertTrue(qwen38.reasoning_off)
        self.assertTrue(qwen38.reasoning_preserve)
        self.assertEqual(qwen38_mtp.speculative_type, "draft-mtp")
        self.assertEqual(qwen38_mtp.draft_tokens, 3)
        self.assertEqual(qwen38_artifact.size, 31457991680)
        self.assertEqual(
            qwen38_artifact.source.repository,
            "unsloth/Qwen3.8-27B-GGUF",
        )
        self.assertEqual(
            qwen38_artifact.source.revision,
            "4604b899a826000505a834e623272db5b7fd62f6",
        )
        self.assertEqual(
            qwen38_artifact.sha256,
            "af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377",
        )
        self.assertEqual(qwen38_artifact.license.spdx, "Apache-2.0")
        qwen38_q4 = catalog.llama_preset("qwen3.8-27b-ud-q4-k-xl")
        qwen38_q4_mtp = catalog.llama_preset(
            "qwen3.8-27b-mtp-ud-q4-k-xl"
        )
        qwen38_q4_artifact = catalog.artifact(qwen38_q4.artifact)
        self.assertEqual(qwen38_q4.bundle, qwen38_q4_mtp.bundle)
        self.assertEqual(qwen38_q4.artifact, qwen38_q4_mtp.artifact)
        self.assertEqual(qwen38_q4.default_context, 65536)
        self.assertEqual(qwen38_q4.chat_template, "qwen3.8")
        self.assertEqual(qwen38_q4.sampling_policy, "qwen3.8-27b")
        self.assertTrue(qwen38_q4.agent_tools)
        self.assertEqual(qwen38_q4.reasoning_control, "effort")
        self.assertEqual(
            qwen38_q4.reasoning_levels, ("low", "medium", "xhigh")
        )
        self.assertEqual(qwen38_q4.reasoning_default, "medium")
        self.assertTrue(qwen38_q4.reasoning_off)
        self.assertTrue(qwen38_q4.reasoning_preserve)
        self.assertEqual(qwen38_q4_mtp.speculative_type, "draft-mtp")
        self.assertEqual(qwen38_q4_mtp.draft_tokens, 3)
        self.assertEqual(qwen38_q4_artifact.size, 17923394624)
        self.assertEqual(
            qwen38_q4_artifact.source.repository,
            "unsloth/Qwen3.8-27B-GGUF",
        )
        self.assertEqual(
            qwen38_q4_artifact.source.revision,
            "4604b899a826000505a834e623272db5b7fd62f6",
        )
        self.assertEqual(
            qwen38_q4_artifact.sha256,
            "bee238bbeb3dc0a34bde4d0dedbaee1f98c009e8bb4226f03070054c12fb1372",
        )
        self.assertEqual(qwen38_q4_artifact.license.spdx, "Apache-2.0")
        hy = catalog.llama_preset("hy-mt1.5-7b-q8-0")
        hy_bundle = catalog.bundle(hy.bundle)
        hy_artifact = catalog.artifact(hy.artifact)
        self.assertEqual(hy.default_context, 32768)
        self.assertEqual(catalog.bundle_size(hy_bundle), 7981929344)
        self.assertEqual(
            hy_artifact.license.spdx,
            "LicenseRef-Tencent-HY-Community",
        )
        self.assertEqual(
            [
                item.identifier
                for item in catalog.bundle_agreements(hy_bundle)
            ],
            ["tencent-hy-community-license"],
        )
        translate_gemma = catalog.llama_preset("translategemma-27b-it-q8-0")
        self.assertEqual(translate_gemma.default_context, 4096)
        self.assertEqual(
            translate_gemma.chat_template, "translategemma-manual"
        )
        gemma_bundle = catalog.bundle(translate_gemma.bundle)
        self.assertEqual(
            catalog.bundle_size(gemma_bundle), 28707987936
        )
        self.assertEqual(
            [
                item.identifier
                for item in catalog.bundle_agreements(gemma_bundle)
            ],
            ["gemma-terms"],
        )
        shisa = catalog.llama_preset("shisa-v2.1-llama3.3-70b-q8-0")
        shisa_bundle = catalog.bundle(shisa.bundle)
        shisa_artifact = catalog.artifact(shisa.artifact)
        self.assertEqual(shisa.default_context, 16384)
        self.assertTrue(shisa.jinja)
        self.assertFalse(shisa.agent_tools)
        self.assertEqual(
            catalog.bundle_size(shisa_bundle), 74975055328
        )
        self.assertEqual(
            shisa_artifact.source.repository,
            "mradermacher/shisa-v2.1-llama3.3-70b-GGUF",
        )
        self.assertEqual(
            shisa_artifact.source.revision,
            "25cb86e709e7ecbe19a71452bbc374f1f5a6462b",
        )
        self.assertEqual(
            shisa_artifact.sha256,
            "1e02e2c7273bee1f84bf212a901ba6a4206859592f63afeb760127ecd0eb1ad5",
        )
        self.assertEqual(
            shisa_artifact.license.spdx,
            "LicenseRef-Llama-3.3-Community",
        )
        self.assertEqual(shisa_artifact.license.status, "verified")
        self.assertEqual(
            [
                item.identifier
                for item in catalog.bundle_agreements(shisa_bundle)
            ],
            ["llama-3.3-community-license"],
        )
        template_path = (
            DEFAULT_CATALOG_PATH.parent.parent
            / "applications"
            / "llama-cpp"
            / "chat-templates"
            / "translategemma-manual.jinja"
        )
        template = template_path.read_text()
        self.assertIn('message["content"] | trim', template)
        self.assertNotIn("English", template)
        self.assertNotIn("Japanese", template)

        base = catalog.bundle("qwen-image-2512-fp8-base")
        lightning = catalog.bundle("qwen-image-2512-fp8-lightning")
        self.assertEqual(base.artifacts, lightning.artifacts[:3])
        self.assertEqual(catalog.bundle_size(base), 30069156070)

        edit = catalog.bundle("qwen-image-edit-2511-fp8-base")
        self.assertIn("qwen-2.5-vl-7b-fp8-text-encoder", edit.artifacts)
        self.assertIn("qwen-image-vae", edit.artifacts)

        ltx = catalog.bundle("ltx-2-t2v-19b-fp8-full")
        self.assertEqual(
            [item.identifier for item in catalog.bundle_agreements(ltx)],
            ["ltx-2-community-license", "gemma-terms"],
        )
        camera = catalog.bundle("ltx-2-camera-dolly-left")
        self.assertEqual(camera.workflow, "")
        self.assertIn("ltx-camera", camera.groups)
        self.assertNotIn("ltx", camera.groups)
        hunyuan = catalog.bundle(
            "hunyuan-video-1.5-i2v-720p-fp16"
        )
        self.assertEqual(
            catalog.bundle_agreements(hunyuan)[0].identifier,
            "hunyuan-video-1.5-community-license",
        )
        wan_v2 = catalog.bundle("wan-2.2-t2v-14b-fp8-lightning-v2")
        self.assertIn("wan-2.2-t2v-lightning-v2-high", wan_v2.artifacts)
        self.assertEqual(
            catalog.artifact("wan-2.2-t2v-lightning-v2-high").sha256,
            "78cc2c9b44aca9ded8f69c6619639edca7459308051161253c3aa04ac6169a58",
        )

    def test_qwen_is_verified_and_wan_repack_is_unverified(self):
        catalog = load_catalog()
        qwen = catalog.artifact("qwen-image-2512-fp8-model")
        wan = catalog.artifact("wan-2.2-t2v-high-fp8")
        self.assertEqual(qwen.license.spdx, "Apache-2.0")
        self.assertEqual(qwen.license.status, "verified")
        self.assertEqual(wan.license.spdx, "NOASSERTION")
        self.assertEqual(wan.license.status, "unverified")
        self.assertEqual(wan.license.upstream_license, "Apache-2.0")
        self.assertTrue(wan.license.warning)

    def test_catalog_rejects_removed_model_tree_collection(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["model_trees"] = {}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            path.write_text(json.dumps(raw))
            with self.assertRaisesRegex(
                LauncherError, "unsupported collections: model_trees"
            ):
                load_catalog(path)

    def test_catalog_rejects_removed_bundle_trees_field(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["bundles"]["qwen-image-2512-fp8-base"]["trees"] = []
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            path.write_text(json.dumps(raw))
            with self.assertRaisesRegex(LauncherError, "removed trees field"):
                load_catalog(path)

    def test_duplicate_destination_is_rejected(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        duplicate = dict(raw["artifacts"]["qwen-image-2512-fp8-model"])
        duplicate["description"] = "Duplicate"
        raw["artifacts"]["duplicate-artifact"] = duplicate
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            path.write_text(json.dumps(raw))
            with self.assertRaisesRegex(LauncherError, "share destination"):
                load_catalog(path)

    def test_llama_mtp_draft_count_is_bounded(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["llama_presets"]["gemma4-31b-it-q8-0-mtp"][
            "draft_tokens"
        ] = 9
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "between 1 and 8"
            ):
                load_catalog(path)

    def test_llama_dflash_requires_a_separate_draft_artifact(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"][
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"
        ]
        del preset["draft_artifact"]
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "draft-dflash requires draft_artifact"
            ):
                load_catalog(path)

    def test_llama_draft_tokens_require_a_speculative_type(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3-0.6b-q8-0"]
        preset["draft_tokens"] = 1
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "draft_tokens requires speculative_type"
            ):
                load_catalog(path)

    def test_llama_backend_draft_tokens_are_strict(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"][
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"
        ]
        for value, message in (
            ({"cuda": 4}, "key must be one of"),
            ({"vulkan": 0}, "integer between 1 and 15"),
            (4, "must be an object"),
        ):
            with self.subTest(value=value):
                preset["draft_tokens_by_backend"] = value
                with tempfile.TemporaryDirectory() as directory:
                    path = self._catalog_copy(directory, raw)
                    with self.assertRaisesRegex(LauncherError, message):
                        load_catalog(path)

        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["llama_presets"]["qwen3-0.6b-q8-0"][
            "draft_tokens_by_backend"
        ] = {"vulkan": 4}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "requires speculative_type"
            ):
                load_catalog(path)

        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["llama_presets"]["gemma4-31b-it-q8-0-mtp"][
            "draft_tokens_by_backend"
        ] = {"vulkan": 9}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "draft-mtp backend draft tokens"
            ):
                load_catalog(path)

    def test_llama_context_override_architectures_are_strict(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"][
            "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
        ]
        preset["context_override_architectures"] = [
            "muse-glimmer",
            "dflash.context_length=int:1",
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "contain GGUF architecture names"
            ):
                load_catalog(path)

    def test_llama_preset_rejects_obsolete_context_field(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3-0.6b-q8-0"]
        preset["context"] = preset.pop("default_context")
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "unsupported fields: context"
            ):
                load_catalog(path)

    def test_llama_preset_policy_is_strictly_typed(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3.8-27b-ud-q8-k-xl"]
        preset["jinja"] = "yes"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(LauncherError, "jinja must be"):
                load_catalog(path)

        preset["jinja"] = False
        preset["agent_tools"] = "yes"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "agent_tools must be"
            ):
                load_catalog(path)

        preset["agent_tools"] = True
        preset["reasoning_control"] = "native"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "reasoning_control must be"
            ):
                load_catalog(path)

        preset["reasoning_control"] = "effort"
        preset["reasoning_preserve"] = "yes"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "reasoning_preserve must be"
            ):
                load_catalog(path)

        preset["reasoning_preserve"] = False
        preset["default_context"] = 4096
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "requires Jinja and at least 16384"
            ):
                load_catalog(path)

        preset["agent_tools"] = True
        preset["default_context"] = 32768
        preset["flash_attention"] = {"auto": "off"}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError,
                "profile must be one of rdna4, strix-halo, strix-point",
            ):
                load_catalog(path)

        preset["flash_attention"] = {"strix-halo": "sometimes"}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "value must be on, off, or auto"
            ):
                load_catalog(path)

        preset["flash_attention"] = {"strix-halo": "off"}
        preset["arbitrary_arguments"] = ["--unsafe"]
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "unsupported fields: arbitrary_arguments"
            ):
                load_catalog(path)

        del preset["arbitrary_arguments"]
        preset["flash_attention"] = {
            "rdna4": "auto",
            "strix-halo": "off",
            "strix-point": "off",
        }
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            loaded = load_catalog(path).llama_preset(
                "qwen3.8-27b-ud-q8-k-xl"
            )
        self.assertEqual(
            loaded.flash_attention,
            {
                "rdna4": "auto",
                "strix-halo": "off",
                "strix-point": "off",
            },
        )

        preset["kv_cache"] = []
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "kv_cache must be an object"
            ):
                load_catalog(path)

        preset["kv_cache"] = {"auto": "q8_0"}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError,
                "kv_cache profile must be one of rdna4, strix-halo, "
                "strix-point",
            ):
                load_catalog(path)

        preset["kv_cache"] = {"strix-halo": "q5_0"}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "value must be f16, q8_0, or q4_0"
            ):
                load_catalog(path)

        preset["kv_cache"] = {"strix-halo": "q8_0"}
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "requires flash_attention on"
            ):
                load_catalog(path)

        preset["flash_attention"]["strix-halo"] = "on"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            loaded = load_catalog(path).llama_preset(
                "qwen3.8-27b-ud-q8-k-xl"
            )
        self.assertEqual(loaded.kv_cache, {"strix-halo": "q8_0"})

        preset["chat_template"] = "some-file"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "chat_template must be"
            ):
                load_catalog(path)

        preset["jinja"] = True
        preset["chat_template"] = "translategemma-manual"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "already enables Jinja"
            ):
                load_catalog(path)

    def test_llama_reasoning_control_requires_agent_tools(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3-0.6b-q8-0"]
        preset["reasoning_control"] = "toggle"
        preset["reasoning_default"] = "on"
        preset["reasoning_off"] = True
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError,
                "reasoning_control requires agent_tools",
            ):
                load_catalog(path)

    def test_llama_reasoning_controls_fail_closed(self):
        cases = (
            ("reasoning_levels", ["high", "low"], "unique, ordered"),
            ("reasoning_default", "instant", "must be off, on, or one"),
            ("reasoning_off", "yes", "reasoning_off must be a boolean"),
            ("reasoning_control", "native", "empty, toggle, effort, or strength"),
        )
        for field, value, message in cases:
            with self.subTest(field=field):
                raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
                preset = raw["llama_presets"][
                    "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"
                ]
                preset[field] = value
                with tempfile.TemporaryDirectory() as directory:
                    path = self._catalog_copy(directory, raw)
                    with self.assertRaisesRegex(LauncherError, message):
                        load_catalog(path)

        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3.6-27b-q8-0"]
        preset["reasoning_off"] = False
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "toggle reasoning_control must expose off"
            ):
                load_catalog(path)

    def test_llama_reasoning_preserve_requires_agent_tools(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3-0.6b-q8-0"]
        preset["reasoning_preserve"] = True
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError,
                "reasoning_preserve requires agent_tools",
            ):
                load_catalog(path)

    def test_llama_sampling_policy_fails_closed(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3.6-27b-q8-0"]
        preset["sampling_policy"] = "missing-policy"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "references unknown sampling policy"
            ):
                load_catalog(path)

        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        preset = raw["llama_presets"]["qwen3.6-27b-q8-0"]
        del preset["reasoning_control"]
        del preset["reasoning_off"]
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError,
                "sampling_policy requires reasoning_control",
            ):
                load_catalog(path)

    def test_llama_sampling_policy_values_fail_closed(self):
        cases = (
            (
                lambda policy: policy.__setitem__("unexpected", {}),
                "unsupported fields: unexpected",
            ),
            (
                lambda policy: policy.__setitem__("thinking", []),
                "thinking must be a non-empty object",
            ),
            (
                lambda policy: policy["thinking"].__setitem__("mirostat", 1),
                "unsupported fields: mirostat",
            ),
            (
                lambda policy: policy["thinking"].__setitem__("top_p", 2.0),
                "invalid top_p",
            ),
            (
                lambda policy: policy["thinking"].__setitem__("top_k", True),
                "invalid top_k",
            ),
            (
                lambda policy: policy["thinking"].__setitem__(
                    "temperature", float("nan")
                ),
                "invalid temperature",
            ),
        )
        for mutate, message in cases:
            with self.subTest(message=message):
                raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
                policy = raw["llama_sampling_policies"]["qwen3.8-27b"]
                mutate(policy)
                with tempfile.TemporaryDirectory() as directory:
                    path = self._catalog_copy(directory, raw)
                    with self.assertRaisesRegex(LauncherError, message):
                        load_catalog(path)

    def test_llama_mtp_draft_must_belong_to_preset_bundle(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["llama_presets"]["gemma4-31b-it-q8-0-mtp"][
            "draft_artifact"
        ] = "qwen3-0.6b-q8-gguf"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "draft artifact is not in bundle"
            ):
                load_catalog(path)

    def test_archive_collection_rejects_unknown_member_fields(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["archive_collections"]["fixture"] = self._archive_collection()
        collection = raw["archive_collections"]["fixture"]
        member = next(iter(collection["members"].values()))
        member["unexpected"] = True
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            path.write_text(json.dumps(raw))
            with self.assertRaisesRegex(
                LauncherError, "unsupported fields: unexpected"
            ):
                load_catalog(path)

    def test_archive_collection_rejects_obsolete_envelope_hash(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["archive_collections"]["fixture"] = self._archive_collection()
        collection = raw["archive_collections"]["fixture"]
        collection["archive"]["sha256"] = "a" * 64
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "unsupported fields: sha256"
            ):
                load_catalog(path)

    def test_content_pack_can_reference_builtin_content(self):
        pack = {
            "schema_version": 2,
            "bundles": {
                "private-bootstrap": {
                    "description": "Private bootstrap content",
                    "application": "comfyui",
                    "artifacts": ["qwen-image-vae"],
                    "groups": ["all", "comfyui"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private.json"
            path.write_text(json.dumps(pack))
            merged, selected = load_content_packs(
                load_catalog(), (path,)
            )
        self.assertEqual(selected, ("private-bootstrap",))
        self.assertIn("qwen-image-vae", merged.artifacts)
        self.assertEqual(
            merged.bundle("private-bootstrap").artifacts,
            ("qwen-image-vae",),
        )

    def test_schema_one_content_pack_is_rejected(self):
        pack = self._content_pack()
        pack["schema_version"] = 1
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "unsupported or invalid content pack schema"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_content_packs_can_reference_each_other(self):
        first = self._content_pack()
        second = {
            "schema_version": 2,
            "bundles": {
                "private-second-machine": {
                    "description": "Second private selection",
                    "application": "comfyui",
                    "artifacts": ["private-model"],
                    "groups": ["all", "comfyui"],
                }
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first_path = root / "base.json"
            second_path = root / "second.json"
            first_path.write_text(json.dumps(first))
            second_path.write_text(json.dumps(second))
            merged, selected = load_content_packs(
                load_catalog(), (first_path, second_path)
            )
        self.assertEqual(
            selected,
            ("private-bootstrap", "private-second-machine"),
        )
        self.assertEqual(
            merged.bundle("private-second-machine").artifacts,
            ("private-model",),
        )

    def test_content_pack_definition_cannot_override_builtin_catalog(self):
        pack = self._content_pack("qwen-image-vae")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "conflicts with an existing definition"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_content_pack_destination_collision_is_rejected(self):
        pack = self._content_pack()
        pack["artifacts"]["private-model"]["destination"] = (
            "vae/qwen_image_vae.safetensors"
        )
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "share destination"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_content_pack_rejects_non_declarative_collections(self):
        pack = self._content_pack()
        pack["workflows"] = {}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "unsupported collections: workflows"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_content_pack_loads_pinned_civitai_workflow_artifact(self):
        pack = self._content_pack()
        artifact = pack["artifacts"]["private-model"]
        artifact["source"] = {
            "provider": "civitai",
            "host": "civitai.red",
            "model_id": 123,
            "model_version_id": 456,
            "filename": "workflow.json",
            "download_url": "https://civitai.red/api/download/models/456",
            "requires_auth": False,
            "archive": {
                "member": "pack/workflow.json",
                "max_size": 1234,
            },
        }
        artifact["destination"] = "krea/workflow.json"
        artifact["target"] = "workflows"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "local.json"
            path.write_text(json.dumps(pack))
            merged, _ = load_content_packs(load_catalog(), (path,))
        loaded = merged.artifact("private-model")
        self.assertEqual(loaded.source.provider, "civitai")
        self.assertEqual(loaded.source.provider_host, "civitai.red")
        self.assertEqual(
            loaded.source.download_url,
            "https://civitai.red/api/download/models/456",
        )
        self.assertEqual(loaded.source.model_version_id, 456)
        self.assertEqual(
            loaded.source.archive_member, "pack/workflow.json"
        )
        self.assertEqual(loaded.source.archive_max_size, 1234)
        self.assertEqual(loaded.target, "workflows")

    def test_content_pack_rejects_obsolete_archive_size(self):
        pack = self._content_pack()
        artifact = pack["artifacts"]["private-model"]
        artifact["source"] = {
            "provider": "civitai",
            "model_id": 123,
            "model_version_id": 456,
            "filename": "workflow.zip",
            "archive": {
                "member": "pack/workflow.json",
                "max_size": 1234,
                "size": 1000,
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "local.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "unsupported fields: size"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_content_pack_rejects_cross_host_civitai_download_url(self):
        pack = self._content_pack()
        artifact = pack["artifacts"]["private-model"]
        artifact["source"] = {
            "provider": "civitai",
            "host": "civitai.red",
            "model_id": 123,
            "model_version_id": 456,
            "filename": "model.safetensors",
            "download_url": "https://example.invalid/api/download/models/456",
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "local.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "download URL on civitai.red"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_workflow_artifact_destination_must_be_json(self):
        pack = self._content_pack()
        pack["artifacts"]["private-model"]["target"] = "workflows"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "local.json"
            path.write_text(json.dumps(pack))
            with self.assertRaisesRegex(
                LauncherError, "destination must end in .json"
            ):
                load_content_packs(load_catalog(), (path,))

    def test_unknown_selector_group_is_rejected(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        bundle = raw["bundles"]["qwen-image-2512-fp8-base"]
        bundle["groups"].append("surprise")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            path.write_text(json.dumps(raw))
            with self.assertRaisesRegex(LauncherError, "unknown selector"):
                load_catalog(path)

    def test_dwarfstar_bundle_requires_one_dedicated_gguf(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        bundle = raw["bundles"][
            "dwarfstar-deepseek-v4-flash-0731-q2-imatrix"
        ]
        artifact = raw["artifacts"][bundle["artifacts"][0]]
        artifact["target"] = "llama-models"
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "must reference only dwarfstar-models"
            ):
                load_catalog(path)

    def test_dwarfstar_bundle_rejects_more_than_one_support_artifact(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        bundle = raw["bundles"][
            "dwarfstar-deepseek-v4-flash-0731-q2-imatrix-dspark"
        ]
        bundle["artifacts"].append("qwen3-0.6b-q8-gguf")
        with tempfile.TemporaryDirectory() as directory:
            path = self._catalog_copy(directory, raw)
            with self.assertRaisesRegex(
                LauncherError, "at most one support artifact"
            ):
                load_catalog(path)

    def test_unknown_application_is_rejected(self):
        raw = json.loads(DEFAULT_CATALOG_PATH.read_text())
        raw["bundles"]["qwen-image-2512-fp8-base"][
            "application"
        ] = "unmanaged"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "catalog.json"
            path.write_text(json.dumps(raw))
            with self.assertRaisesRegex(
                LauncherError, "unknown application"
            ):
                load_catalog(path)


if __name__ == "__main__":
    unittest.main()
