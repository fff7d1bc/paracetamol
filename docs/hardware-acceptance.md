# Target-hardware acceptance

A successful build, CPU startup or unit test is not GPU inference acceptance.
This reference defines the current bounded smoke, manual matrix and result
requirements for `gfx1150`, `gfx1151`, `gfx1200` and `gfx1201`.

The latest bounded llama.cpp acceptance is the 2026-11-14 Strix Halo r78
[pool and reset correction record](research/hardware-acceptance-history.md#strix-halo-flash-next-pool-and-reset-corrections-2026-11-14).
It accepts the tested regular Flash-Next tuple and short controls, not every
model or target GPU. See [llama.cpp maintenance](llama-cpp-maintenance.md) for
the exact baseline, known exceptions and what must be rerun after a change.
The matrix below keeps uncompleted coverage explicit.

Keep raw logs, JSON, media and machine-specific details outside tracked source.
Repository-local evidence may live under ignored `build/`. Public summaries
must omit private identifiers, paths, prompts and logs. Record `PASS`, `FAIL`,
`BLOCKED` or `N/P`, with a capacity or policy reason for `N/P`.

Dated experiments and field-tested host observations are preserved in the
[hardware acceptance history](research/hardware-acceptance-history.md).
Retired engines and clients in that archive are not current setup or testing
requirements.

## Automated bounded smoke

Start acceptance on a new or updated host with:

```bash
./paracetamol acceptance --dry-run
./paracetamol acceptance
```

The checkpointed suite covers exact GPU/CPU device isolation, short ComfyUI
image and video workloads and one llama.cpp workload. Generated
media requires explicit human review before its case becomes `PASS`; the
review phase starts only after all selected automated workloads finish.
Prompts and the Markdown report retain case-specific functional criteria so
aesthetic defects are not confused with broken inference. Unattended runs
leave those cases `BLOCKED`. Preserve the JSON and Markdown result below
`apps/acceptance/results/` and resume an interrupted run with `--resume`.

This command is the fast recurring gate. It intentionally does not replace
the rows below: edit and I2V behavior, additional model families, precision
comparisons, forced-profile failures, memory policy comparisons, and sustained
performance still require the finite manual matrix.

## Host diagnostics

Complete every row on all target hosts before describing their profiles as
accepted.

| Check | RX 9060 family / `gfx1200` | R9700 or RX 9070 family / `gfx1201` | Strix Halo / `gfx1151` | Strix Point / `gfx1150` |
| --- | --- | --- | --- | --- |
| `doctor` reports the exact architecture | pending | pending | pending | pending |
| `auto` resolves the expected profile | pending | pending | pending | pending |
| forcing the expected profile succeeds | pending | pending | pending | pending |
| forcing either other profile fails closed | pending | pending | pending | pending |
| exactly the selected render-node set is exposed | pending | pending | pending | pending |
| CPU mode exposes no GPU devices | pending | pending | pending | pending |
| device access needs no broader container privileges or disabled SELinux labels | pending | pending | pending | pending |
| RAM, TTM module/ceiling, and effective GTT are reported | N/P: dedicated VRAM | N/P: dedicated VRAM | pending | pending |

Record for each host:

```text
Date:
Git commit:
Kernel and distribution:
GPU and system RAM:
Render node:
ROCm/PyTorch base image ID:
Application image IDs:
Persistent-data location:
Result/log location:
```

## Representative application gate

Use the catalog-owned workflow or preset and its default runtime policy unless
the row says otherwise. For every generation row, inspect output sanity as
well as successful process completion. Exercise I2V/edit rows with a fixed
local source image.

| Application path | Exact content or comparison | RX 9060 family | R9700 or RX 9070 family | Strix Halo | Strix Point |
| --- | --- | --- | --- | --- | --- |
| ComfyUI image | `qwen-image-2512-fp8-lightning` | pending | pending | pending | pending |
| ComfyUI edit | `qwen-image-edit-2511-fp8-lightning` | pending | pending | pending | pending |
| ComfyUI Wan T2V | `wan-2.2-t2v-14b-fp8-lightning` | pending | pending | pending | pending |
| ComfyUI Wan I2V | `wan-2.2-i2v-14b-fp8-lightning` | pending | pending | pending | pending |
| LTX-2 T2V camera graph | `ltx-2-t2v-19b-fp8-full`, ordinary and one enabled camera adapter | pending | pending | pending | pending |
| LTX-2 I2V camera graph | `ltx-2-i2v-19b-fp8-full`, ordinary and one enabled camera adapter | pending | pending | pending | pending |
| Hunyuan T2V | `hunyuan-video-1.5-t2v-480p-cfg-distilled` | pending | pending | pending | pending |
| Hunyuan I2V | `hunyuan-video-1.5-i2v-480p-step-distilled` | pending | pending | pending | pending |
| llama.cpp offload smoke | `llama-qwen-qwen3-0.6b-q8-0` | pending | pending | pending | pending |
| llama.cpp regular Flash-Next DFlash2 | `unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-dflash2`, greedy depth three; populated retrieval, history, tools, reasoning, cancellation and vision | N/P by backend/profile policy | N/P by backend/profile policy | accepted 2026-11-14 within the [r78 record's scope](research/hardware-acceptance-history.md#strix-halo-flash-next-pool-and-reset-corrections-2026-11-14) | N/P by backend/profile policy |
| llama.cpp regular Flash-Next plain control | `unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl`, same target without speculation; ROCm protocol/cancellation control and short Vulkan reply/tool smoke | ROCm N/P by model policy, Vulkan requires high-memory host offload | ROCm N/P by model policy, Vulkan requires high-memory host offload | accepted 2026-11-14 for the bounded [r78 controls](research/hardware-acceptance-history.md#strix-halo-flash-next-pool-and-reset-corrections-2026-11-14), not a full Vulkan matrix | ROCm N/P by model policy, Vulkan pending |
| llama.cpp regular dense Qwen3.8 | `unsloth-qwen3.8-27b-ud-q8-k-xl`, short completion and nested tool round trip without speculation | pending, subject to model/context capacity | pending, subject to model/context capacity | short ROCm reply/tool control accepted 2026-11-14; not full dense-model acceptance | pending |
| llama.cpp Huihui dense Qwen3.8 | `huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl`, ordinary dense policy without MTP | pending | pending | accepted 2026-10-22 for ROCm 32K retrieval and short Vulkan/API compatibility at 256K allocation; native Maki off passes, xhigh final-answer miss recorded | pending |
| llama.cpp Huihui Flash-Next | `huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl`, shared regular Flash-Next policy with off, low, medium and xhigh | ROCm N/P by model policy, Vulkan requires high-memory host offload | ROCm N/P by model policy, Vulkan requires high-memory host offload | accepted 2026-10-16 on ROCm at 256K capacity and Vulkan at 32K input | ROCm N/P by model policy, Vulkan pending |
| llama.cpp Qwen3.8 Q4 optional path | `unsloth-qwen3.8-27b-ud-q4-k-xl`, non-speculative completion/tool control; deeper comparison when this path changes | pending | pending | pending for this non-MTP preset; historical MTP results do not transfer | pending |

For a llama.cpp source update, follow the
[source-update acceptance procedure](testing-and-release.md#llamacpp-source-update-acceptance).
Regular Unsloth Flash-Next is the primary large-model case, with greedy
DFlash2 and its matching plain target. Keep the default F16 K/V, four slots,
262144-token shared pool and gateway-owned sampling policy fixed. ROCm
Flash-Next is restricted to Strix Halo; its DFlash2 presets do not support
Vulkan. Dense 27B controls need a short completion and tool round trip, not
another full long-context matrix, unless the changed path or a failed control
requires it. Optional variants retain their own acceptance evidence; passing
the regular model does not accept different weights or a different preset.

Through the ordinary gateway, verify the selected model and native runtime
settings, then send a developer message and a tool schema with a nested
object. Require a structured tool call, return its result as a tool message,
and require a final answer. Preserve complete assistant history, including
reasoning, and repeat with streaming enabled. Exercise supported reasoning
levels and the original nonthinking replay without weakening their oracles.
Compare DFlash2 with the matching plain target to separate protocol handling
from speculative decoding. Only after raw API checks pass should a separately
installed native Maki client be used for an integration round trip. Its
normal user access and permission prompts are not a Paracetamol sandbox.

Muse and dense Qwen MTP presets, the managed Pi client and the coding-agent
runner are retired. Their dated records remain historical evidence,
not current acceptance requirements. Updating a preset ID here does not
transfer an older variant's acceptance to its replacement.

For a future agent comparison, declare explicit model-native selectors rather
than treating one label as equivalent across families. Run every candidate
through the same harness, tasks, repetition count, and hardware/runtime inputs.
Keep edit and shell approvals equivalent across runs. Compare structured
results only after all candidates complete; record quality, protocol,
state-corruption, or truncation failures instead of treating wall time as the
sole rank. Run per-model native-level sweeps as a separate experiment.

Keep the first Qwen3.8 pass on Paracetamol's pinned official-template adaptation.
Do not replace it mid-comparison in response to anecdotal release reports. The
froggeric community template is a separate candidate because it changes
history rendering, role handling, tool serialization, control tags, and
failure-recovery prompt policy. If the official-template baseline exposes a
matching failure, run a Qwen3.8-only A/B with the target GGUF, image, native
reasoning choice, sampling, task, and repetition count fixed; identify the
template revision in the result notes.

For a host with two matching GPUs, add these single-workload checks:

| Multi-GPU path | Check |
| --- | --- |
| Doctor | select both render nodes; verify both named devices, one architecture, and a passed tensor operation on each |
| llama.cpp | run one server with both render nodes; verify managed layer split, activity on both cards, successful generation, and enough per-card headroom for KV/runtime buffers |
| ComfyUI component placement | run one graph with model, CLIP, or VAE deliberately placed on different cards; verify both activity and sane output |
| ComfyUI CFG split | run one graph with `MultiGPU CFG Split`; verify both activity and sane output without claiming pooled VRAM |

## Precision and policy coverage

The representative gate above establishes application and task behavior. Add
these comparisons before making performance or memory recommendations:

| Comparison | RX 9060 family | R9700 or RX 9070 family | Strix Halo | Strix Point |
| --- | --- | --- | --- | --- |
| one Qwen Image FP8/BF16 pair with the same workflow | pending | pending | pending | pending |
| one Wan FP8/FP16 pair with the same workflow | pending | pending | pending | pending |
| one LTX-2 FP8/BF16 full-model pair | pending | pending | pending | pending |
| Wan T2V Seko V1.1 versus V2.0 with identical inputs | pending | pending | pending | pending |
| balanced versus conservative memory policy | pending | pending | pending | pending |
| default versus experimental kernel policy | pending | pending | pending | pending |
| persistent versus isolated benchmark cache | pending | pending | pending | pending |

Use the same image, catalog commit, workflow, prompt, input, dimensions,
frames, seed, and render-node set for each comparison. Performance conclusions
require repeated measurements, not merely successful inference.

## Per-run result

For a manual inference result that is not already captured by managed
benchmark JSON, record:

```text
Status:
Date:
Git commit:
Application image ID:
Catalog bundle and source revisions:
Profile and render-node set:
Runtime policies:
Prompt/input identity:
Dimensions, frames, steps, and seed:
Peak memory:
Prompt/generation timing:
Output sanity:
Warnings or deviations:
Result/log location:
```

## Deferred acceptance handoff

When one contributor cannot reach every required hardware class, leave a
bounded, reproducible handoff instead of a generic request to “test on another
GPU.” Keep hostnames and personal infrastructure out of this document; record
the architecture and observable requirements.

```text
Change under test:
Source commit and application image ID:
Already accepted on:
Deferred architecture/profile:
Behavior or patch path requiring coverage:
Required managed content:
Exact commands:
Expected success criteria:
Result and log destination:
Known warnings or capacity constraints:
```

Commands must name the profile, render-node set, backend, preset, context,
cache policy, prompt and generation sizes, and repetition count whenever those
values affect the conclusion. The person completing the handoff should append
the observed result to the original change or its review record and retain
the generated JSON rather than returning only “works for me.”

The first publication may call uncompleted rows experimental, but it must not
present them as accepted. A later dependency, runtime-policy, ROCm/PyTorch, or
model change invalidates only the coupled rows; repeat those rows on every
hardware class where practical.
