# ROCmplete documentation

This is the complete documentation index. Start with the root
[`README.md`](../README.md) for installation and the shortest working path,
then choose a user guide or maintainer reference below.

## User guides

- [Applications](guides/applications.md) covers ComfyUI, llama.cpp,
  DwarfStar, managed models, APIs, coding-agent clients, and multi-GPU runs.
- [Content](guides/content.md) covers recipes, exact bundles, licensing,
  verification, resumable downloads, mirrors, imports, and workflows.
- [Operations](guides/operations.md) covers hardware acceptance, builds,
  caches, image transfer, persistent data, logs, and scoped cleanup.
- [Tuning and benchmarks](guides/tuning.md) covers host GPU access, runtime
  policies, memory configuration, and repeatable performance comparisons.
- [Qwen3.8 Dynamic Q4 versus Q8](guides/qwen3.8-dynamic-quant-comparison.md)
  reports the hidden-graded medium and `xhigh` coding-agent comparison,
  context results, exact Unsloth artifacts, and limitations.

For a shorter application walkthrough made of copyable commands, use the
built-in guide:

```bash
./rocmplete guide
./rocmplete guide comfyui
./rocmplete guide llama-cpp
./rocmplete guide dwarfstar
```

Command-specific `--help` output remains the authoritative interface
reference.

## Maintainer documentation

Coming back to ROCmplete after some time away? Start here. These references
explain how the project fits together, how to change it safely, and how to
know when an upgrade is genuinely finished. The public contribution path and
baseline checks are summarized in [`CONTRIBUTING.md`](../CONTRIBUTING.md).

### Start here after time away

1. Read the [architecture and invariants](architecture.md).
2. Inspect the current pins rather than trusting remembered version numbers:

   ```bash
   rg -n '^(ARG .*VERSION|ARG .*COMMIT|ARG .*UBUNTU_IMAGE)' Containerfile
   cat containers/content_tools/requirements.txt
   cat applications/comfyui/constraints.txt
   cat applications/llama-cpp/hip-apu-host-buffer.patch
   sed -n '1,220p' applications/dwarfstar/entrypoint.sh
   jq '.schema_version' catalog/catalog.json
   ```

3. Inspect the current CLI and content inventory:

   ```bash
   ./rocmplete --help
   ./rocmplete guide
   ./rocmplete run --help
   ./rocmplete content list
   ./rocmplete content list --bundles
   ./rocmplete content install all --dry-run
   ```

   Then check that the short path in the root README and the focused
   application, content, operations, and tuning guides still match it.

4. Establish a clean baseline before changing anything:

   ```bash
   git status --short
   python3 -m compileall -q applications containers src/rocmplete tests tools
   bash -n applications/comfyui/entrypoint.sh \
     applications/llama-cpp/entrypoint.sh \
     applications/dwarfstar/entrypoint.sh
   PYTHONPATH=src python3 -m unittest discover -s tests
   git diff --check
   ```

5. Use the maintainer guide or dated research record matching the task:

   - [Routine upgrade scan and execution checklist](routine-upgrade-runbook.md)
   - [Upgrading dependencies and upstream applications](upgrading.md)
   - [Adding or updating models, bundles, workflows, and benchmarks](content-catalog.md)
   - [Adding applications, commands, modes, or hardware profiles](extending.md)
   - [Testing, release checks, and routine housekeeping](testing-and-release.md)
   - [Coding-agent evaluation maintenance](coding-agent-evaluation.md)
   - [Target-hardware acceptance matrix](hardware-acceptance.md)
   - [DeepSeek V4 Flash llama.cpp feasibility
     snapshot](deepseek-v4-flash-llama-cpp-feasibility.md)
   - [Muse Glimmer llama.cpp agent feasibility
     snapshot](muse-glimmer-llama-cpp-agent-feasibility.md)
   - [Ling 3.0 Flash llama.cpp feasibility
     snapshot](ling-3.0-flash-llama-cpp-feasibility.md)
   - [Qwen3.6 llama.cpp Strix Halo tuning feasibility
     snapshot](qwen3.6-strix-halo-llama-cpp-tuning-feasibility.md)
   - [Qwen3.8 Strix Halo platform power-policy feasibility
     snapshot](qwen3.8-strix-halo-platform-power-policy-feasibility.md)

### Sources of truth

| Concern | Authoritative files |
| --- | --- |
| Runtime/base images, ROCm/PyTorch, application commits | `Containerfile` |
| Application defaults and profile validation | `src/rocmplete/config.py` |
| GPU profile and architecture identities | `src/rocmplete/hardware_profiles.py` |
| Offline managed image archives | `src/rocmplete/image_archive.py` |
| Public command tree and usage examples | `src/rocmplete/cli_parser.py` |
| Command validation and orchestration | `src/rocmplete/cli.py` |
| Terminal styling and measured columns | `src/rocmplete/ui.py` |
| Repository-root discovery | `src/rocmplete/project.py` |
| Small runnable content recipes | `src/rocmplete/recipes.py` |
| Built-in application walkthroughs | `src/rocmplete/application_guides.py` |
| Local image build commands | `src/rocmplete/build.py` |
| Podman isolation and device arguments | `src/rocmplete/runtime/` |
| In-container GPU/profile validation | `containers/common/profile.py` |
| Resumable pinned HTTPS downloads | `containers/content_tools/download.py` |
| Remote URL resolution and generated local packs | `src/rocmplete/remote_import.py` |
| Read-only source, archive, and workflow research | `tools/*_probe.py` |
| Application build and runtime policy | `applications/<application>/` |
| Content metadata and relationships | `catalog/catalog.json` |
| Workflow transformation and provenance | `src/rocmplete/workflows.py` |
| ComfyUI benchmark preparation and results | `src/rocmplete/benchmark.py` |
| PATH launchers | `bin/rocmplete`, `bin/opencode`, `bin/pi`, `bin/omp`, `bin/maki` |
| Agent model policy and sandbox | `src/rocmplete/agent_models.py`, `src/rocmplete/agent_sandbox.py` |
| Runtime client configuration | `src/rocmplete/opencode.py`, `src/rocmplete/pi_agent.py`, `src/rocmplete/omp_agent.py`, `src/rocmplete/maki_agent.py` |
| Read-only local GGUF inventory | `src/rocmplete/model_inventory.py` |
| Native llama.cpp benchmark results | `src/rocmplete/llama_benchmark.py` |
| Server-side llama.cpp speculative-depth screens | `src/rocmplete/llama_speculative_benchmark.py` |
| Frozen coding-agent tasks and results | `evaluations/coding/`, `src/rocmplete/agent_evaluation.py` |
| Checkpointed target-hardware smoke acceptance | `src/rocmplete/acceptance.py` |
| Third-party provenance summary | `THIRD_PARTY_NOTICES.md` |
| Enforced behavior | `tests/` |

The files above are authoritative. When their counts, versions, commands, or
behavior change, update the corresponding prose in the same change rather than
leaving a known-stale snapshot behind.

Commands in these guides use obvious uppercase placeholders such as
`CURRENT_IMAGE`, `WORKFLOW_ID`, and `REVISION`. Replace them with values from
the current source tree; they are not shell variables.

### Non-negotiable project properties

Preserve these unless deliberately redesigning the project:

- Images are built locally from immutable base and source pins.
- Applications have separate final images. A minimal ROCm runtime is shared
  by the ROCm/PyTorch base and native applications. PyTorch applications share
  the higher ROCm/PyTorch base; native applications do not inherit PyTorch.
- Every GPU application uses the same project ROCm release. A native build
  changes the development toolchain, not the runtime version policy.
- Runtime is rootless, read-only, and capability-free. Each application gets
  only its writable `/data`; managed content is mounted read-only below
  `/content`; temporary writes go to explicit `tmpfs` mounts.
- Only the exact selected render-node set and `/dev/kfd` are exposed to GPU
  runs. CPU mode exposes neither.
- Multiple render nodes are never guessed. An application must explicitly
  declare support before one workload may receive more than one.
- Forced hardware profiles are checked against the architecture PyTorch sees.
- Model downloads use full Hugging Face revisions or exact Civitai
  model-version IDs, plus exact byte sizes and SHA-256 hashes.
- Managed workflows and native application paths do not silently fetch
  unpinned models.
- License uncertainty is represented as `NOASSERTION`, not upgraded by
  assumption from an upstream base model.
- Curated workflows derive from pinned licensed sources, contain provenance,
  and have deterministic rendered hashes.
- Build-time source patches fail closed when their expected upstream text
  changes.
- No GPU inference is part of ordinary development checks on a weak host.

Changing one of these is an architectural decision, not routine maintenance.

### The meaning of “mode”

Use precise language in changes and commit messages:

- A **profile** selects hardware behavior: `auto`, `cpu`, `rdna4`,
  `strix-halo`, or `strix-point`.
- An **application** is an independently built image or service, such as
  ComfyUI or llama.cpp.
- An application **command mode** is an operation such as llama.cpp `server`
  or `cli`.
- A **bundle variant** selects model precision or behavior, such as FP8,
  BF16, base, or Lightning.
- A workflow **renderer** deterministically adapts an official workflow
  template for one bundle variant.

This distinction prevents an application feature from accidentally becoming a
hardware profile or a proliferation of top-level commands.
