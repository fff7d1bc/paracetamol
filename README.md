# ROCmplete

ROCm without the scavenger hunt. ROCmplete builds and runs useful AMD GPU
applications as local, rootless containers with pinned software and verified
content.

Possibly the least frustrating way to run ROCm. That is pretty much the design
goal. Bringing up a new machine should take a few commands that you can inspect
and understand. If the host is not ready, the tool should tell you what is
wrong and what to do next.

ROCmplete targets AMD RDNA 4 discrete GPUs. That includes the 32 GB Radeon AI
PRO R9700, RX 9070 XT, RX 9070, and RX 9070 GRE using `gfx1201`, plus the RX
9060 XT and RX 9060 using `gfx1200`. It also targets AMD Ryzen AI Max / Strix
Halo (`gfx1151`) and AMD Ryzen AI 300 / Strix Point (`gfx1150`). The build and
runtime enforce all four architectures, but the project remains pre-release
while its target-hardware acceptance matrix is being completed.

ROCmplete currently manages:

- ComfyUI for image generation, image editing, and video on port 8188
- llama.cpp as an OpenAI-compatible API server and interactive GGUF CLI on
  port 8080, with selectable ROCm and Vulkan backends
- DwarfStar as an experimental, high-memory DeepSeek V4 Flash server and CLI
  on port 8000

## Hardware exercised so far

These are real machines used during development. The scope column matters. A
working workload is useful evidence, but it does not imply that every row in
the [target-hardware acceptance matrix](docs/hardware-acceptance.md) has
passed.

| Host | GPU target | Workloads exercised |
| --- | --- | --- |
| Fedora Kinoite 44, Ryzen AI 9 HX 370, 128 GB DDR5-5600 SODIMM | Strix Point, `gfx1150` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets |
| Fedora Linux 44 (non-OSTree), Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash at 4K and 128K context, including the optional DSpark pair; managed Qwen3.6 and Qwen3.8 llama.cpp MTP/tool paths, including the optional Qwen3.8 Dynamic Q4_K_XL path at 128K; OMP and ROCm/Vulkan paths; Muse Glimmer dynamic and 256K runtime probes; historical Laguna XS and Ling feasibility controls |
| Ubuntu 26.04, Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets |
| SteamOS 3.8, Radeon RX 9070 XT 16 GB | RDNA 4, `gfx1201` | ComfyUI and the Qwen3 0.6B llama.cpp smoke |

## Why ROCmplete

Getting a container to start is the easy part. It does not prove that PyTorch
found the right GPU or that inference survives contact with the hardware.
ROCmplete tries to cover the whole path.

- Ubuntu base images, ROCm and PyTorch versions, application commits,
  dependencies, source patches, models, and workflows have explicit pins.
- One checkout builds for `gfx1201`, `gfx1200`, `gfx1151`, and `gfx1150`.
  Runtime checks confirm the architecture and apply the matching policy.
- `doctor` checks Podman, GPU devices, permissions, SELinux, shared-memory
  setup, the detected architecture, and a real PyTorch tensor operation.
- Downloads are pinned by revision or model-version ID, size, and SHA-256.
  License state remains explicit, downloads resume, and content survives image
  rebuilds.
- Containers are rootless, read-only, capability-free, and expose only the
  selected GPU devices. Web applications publish on loopback by default.
- Optional OpenCode, Pi, Oh My Pi, and Maki launchers add a bubblewrap filesystem
  boundary around local coding-agent work.
- `acceptance run` checks more than startup. It runs small real workloads,
  checkpoints progress, and collects visual review after unattended work.

## Requirements

The host needs rootless Podman and Python 3.12 or newer. The field-tested hosts
above use SteamOS 3.8, Fedora 44 in conventional and Kinoite deployments, and
Ubuntu 26.04. A minimal installation may not include Podman yet.

GPU use needs read/write access to `/dev/kfd` and the selected
`/dev/dri/renderD*` nodes. Run Doctor before changing permissions or kernel
settings. It reports the problem and the host-specific action when one is
needed. The
[host GPU access guide](docs/guides/tuning.md#host-gpu-access) explains the
SELinux, group, and udev choices.

The optional sandboxed agent launchers also need bubblewrap (`bwrap`). Use the
distribution `bubblewrap` package from `apt`, `dnf`, or `pacman` when possible.
Doctor reports Ubuntu's AppArmor user-namespace policy when it can interfere
with a launcher.

## Quick start

Clone the source and inspect the host. ROCmplete has no host-side Python
dependencies and does not publish prebuilt application images.

```bash
git clone https://github.com/fff7d1bc/rocmplete.git
cd rocmplete
./rocmplete --version
./rocmplete doctor
```

Before the first build, Doctor reports that its containerized PyTorch probe was
skipped. Build all applications, inspect the available content, and choose a
recipe:

```bash
./rocmplete build all
./rocmplete doctor
./rocmplete content install
```

`build all` builds ComfyUI, llama.cpp, and the experimental DwarfStar image.
Build one application instead when that is all you need. Models range from
hundreds of MiB to more than 100 GiB, so inspect a dry run before a large
installation.

```bash
./rocmplete content install llama-cpp muse-glimmer --dry-run
```

Use the built-in guides for short, copyable walkthroughs:

```bash
./rocmplete guide comfyui
./rocmplete guide llama-cpp
./rocmplete guide dwarfstar
```

### ComfyUI

The practical image recipe installs Qwen Image FP8 Lightning and its curated
workflow:

```bash
./rocmplete build comfyui
./rocmplete content install comfyui image
./rocmplete run comfyui
```

Open `http://127.0.0.1:8188`. Image editing, T2V, I2V, imported content,
Manager, and multi-GPU graphs are covered in the
[application guide](docs/guides/applications.md#comfyui).

### llama.cpp

Dense Qwen3.8 27B Dynamic Q8_K_XL with MTP is the common managed default:

```bash
./rocmplete build llama-cpp
./rocmplete content install llama-cpp qwen3.8
./rocmplete run llama-cpp server \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl
```

The recipe installs one 29.30 GiB GGUF containing the dense target and its MTP
prediction heads. The default preset starts at 256K context, verifies up to
three draft tokens, and uses native medium reasoning effort. Its non-MTP
control shares the same verified artifact.

A separate 16.69 GiB Unsloth Dynamic Q4_K_XL bundle keeps the same model
family available for more constrained GPUs without changing the recipe or
managed-client default:

```bash
./rocmplete content install llama-qwen3.8-27b-ud-q4-k-xl
./rocmplete run llama-cpp server --preset qwen3.8-27b-mtp-ud-q4-k-xl
```

The Dynamic Q4_K_XL presets start at a reviewed 128K context. Treat the
smaller quantization as a capacity and throughput tradeoff, not an
equivalent-quality replacement for the Dynamic Q8_K_XL default. On the
accepted Strix Halo host it was within 12.4% of the retired Q4_K_M path across
matched whole requests and generated 25-30% faster than Dynamic Q8_K_XL on
matched ROCm MTP requests. Its 128K server used 30.83 GB while idle and was
observed at 31.73-31.82 GB during controlled agent runs. ROCm and Vulkan traded
wins by context, so there is no model-specific backend override. Dedicated
32 GiB RDNA 4 capacity still needs acceptance on that hardware.

The [Qwen3.8 Dynamic Q4 versus Q8 coding-agent
comparison](docs/guides/qwen3.8-dynamic-quant-comparison.md) adds hidden-graded
medium and `xhigh` quality results, per-task outcomes, targeted retries, and
the limits of applying those Unsloth-specific findings to other GGUF releases.

Qwen3.6 and Muse Glimmer remain separate comparison families. Their recipes
install the dense and sparse Qwen3.6 MTP choices and Muse's Dynamic
target/DFlash pair:

```bash
./rocmplete content install llama-cpp qwen3.6
./rocmplete content install llama-cpp muse-glimmer
```

The Qwen3.6 recipe prints dense 27B MTP as its next step; sparse 35B-A3B MTP
is an optional installed comparison and does not change the Qwen3.8
managed-client default. Qwen3.6 MTP and non-MTP variants use distinct GGUFs
and therefore appear on separate `content list --models` rows. Each Qwen3.8
quantization shares one GGUF between its base and MTP presets, so the list
prints each pair of preset aliases on one comma-separated row. Muse's three
presets similarly share one artifact pair.

For an API serving several installed presets, use the managed router:

```bash
./rocmplete run llama-cpp server --router --models-max 1
```

The [llama.cpp guide](docs/guides/applications.md#llamacpp) explains presets,
contexts, MTP, DFlash, translations, the terminal CLI, tool calling, and
choosing between the managed Qwen variants. High-memory hosts can also install
the KAT-Coder Q8 coding-agent candidate independently without changing the
default:

```bash
./rocmplete content install llama-cpp kat-coder
```

For Japanese and English translation on a high-memory host, the separate
Shisa V2.1 recipe installs the 70B Q8_0 model and requires acknowledgment of
the Llama 3.3 terms:

```bash
./rocmplete content install llama-cpp shisa-v2.1 --accept-license
./rocmplete run llama-cpp server \
  --preset shisa-v2.1-llama3.3-70b-q8-0
```

### DwarfStar

DwarfStar serves the pinned DeepSeek V4 Flash 0731 Q2 imatrix model. It uses
IQ2_XXS for routed gate/up weights, Q2_K for routed down weights, and Q8 for
attention projections, shared experts, and output. The model is about 80.76
GiB before context and working allocations, so this path is for a host with
enough GPU-mapped memory:

```bash
./rocmplete build dwarfstar
./rocmplete content install dwarfstar flash-0731-q2-imatrix
./rocmplete run dwarfstar server
```

The separate 5.58 GiB DSpark support GGUF is an opt-in speculative-decoding
path; it does not replace the default model or improve its quality:

```bash
./rocmplete content install dwarfstar flash-0731-q2-imatrix-dspark
./rocmplete run dwarfstar server --dspark
```

The [DwarfStar guide](docs/guides/applications.md#dwarfstar) covers its 128K
managed context, memory setup, optional DSpark path, API, agent-client
providers, and bounded acceptance run.

## Everyday use

Inspect local state, start an installed application, and let `auto` select the
hardware profile:

```bash
./rocmplete status
./rocmplete run comfyui
```

A running llama.cpp server also exposes a pasteable configuration report:

```bash
./rocmplete status llama-cpp
./rocmplete status llama-cpp \
  --model qwen3.8-27b-mtp-ud-q8-k-xl
```

The model selector identifies a managed direct preset or chooses one configured
router preset. The report includes the immutable image and source identities,
resolved hardware, model policy, sampling defaults, and exact running llama.cpp
command without printing API-key values or host secret paths.

Override the profile only when testing or diagnosing:

```bash
./rocmplete run comfyui --profile rdna4
./rocmplete run comfyui --profile strix-halo
./rocmplete run comfyui --profile strix-point
./rocmplete run comfyui --profile cpu
```

Web applications publish on `127.0.0.1` by default. Select one exact LAN or
Tailscale address only when unauthenticated network access is intentional:

```bash
./rocmplete run comfyui --listen 192.168.1.50
```

For local coding-agent work, start the llama.cpp router and use the sandboxed
PATH launcher. At least one managed agent model must already be installed.

```bash
./rocmplete content install llama-cpp qwen3.8
./rocmplete run llama-cpp server --router --models-max 1
export PATH="$PWD/bin:$PATH"
opencode
# or: pi
# or: omp
# or: maki
```

Qwen3.8 starts at native medium effort. Pi, OpenCode, and OMP expose its off,
low, medium, and xhigh choices without inventing a `high` level. Maki 0.4.8's
llama.cpp transport exposes only an independent numeric reasoning budget; the
managed server does not infer native effort from that number. Use Pi, OpenCode,
or OMP when the model-native level matters.

OpenCode starts new sessions in read-only Investigate mode. All four launchers
keep the current directory and private client state writable while hiding the
real home directory, credentials, Podman state, and GPU devices. The
[tool-using client guide](docs/guides/applications.md#tool-using-clients) documents
models, reasoning variants, agent modes, sandbox limits, and escape hatches.

On a multi-GPU host, repeat `--render-node` for every card intended for one
supported workload. ROCmplete never guesses the set and requires matching
architectures:

```bash
./rocmplete run llama-cpp server \
  --model /path/to/large-model.gguf \
  --render-node /dev/dri/renderD128 \
  --render-node /dev/dri/renderD129
```

llama.cpp uses layer splitting automatically. ComfyUI needs graph nodes that
place components or work on the selected cards. See the
[application guide](docs/guides/applications.md) and
[runtime tuning guide](docs/guides/tuning.md#runtime-policies).

Foreground runs own the container lifecycle. Ctrl-C stops and removes the
container. Detached runs are managed explicitly:

```bash
./rocmplete logs comfyui --follow
./rocmplete stop comfyui
```

Update without rewriting local work or persistent content:

```bash
git pull --ff-only
./rocmplete doctor
./rocmplete build all
```

## Builds, content, and storage

The three main operations remain independent and retryable:

```text
build  ->  content install  ->  run
image      models/workflows     application
```

Normal builds reuse Podman layers and a local Python package cache. Use
`--no-layer-cache` to rerun one application image or `--no-cache` for a fully
cold build. The
[operations guide](docs/guides/operations.md#builds-and-local-caches) explains
prerequisite images, cache boundaries, cleanup, and image transfer.

Content discovery starts with practical recipes and expands only when asked:

```bash
./rocmplete content list
./rocmplete content list --bundles
./rocmplete content list --families
./rocmplete content list --models --details
./rocmplete content import
```

Set `HF_TOKEN` before a large Hugging Face installation when you have one.
`CIVITAI_TOKEN` is needed only for authenticated Civitai imports and
user-owned packs. ROCmplete passes supplied tokens only to its download tools
and does not store them in images or persistent state. The
[content guide](docs/guides/content.md) covers recipes, exact bundles, terms,
verification, resumable downloads, mirrors, imports, and workflows.

Persistent data defaults to
`${XDG_DATA_HOME:-$HOME/.local/share}/rocmplete`. Put large content on another
filesystem with `${XDG_CONFIG_HOME:-$HOME/.config}/rocmplete/config.toml`:

```toml
[storage]
data_dir = "/mnt/ai/rocmplete"
```

The configuration is optional and ROCmplete never creates or migrates it.
See [persistent data](docs/guides/operations.md#persistent-data) before moving or
cleaning application state, models, inputs, or outputs.

## Acceptance and benchmarks

After onboarding a host or updating software, run the bounded checkpointed
smoke suite:

```bash
./rocmplete acceptance run --dry-run
./rocmplete acceptance run
```

Automated workloads finish before the visual review pass, so the run can be
left unattended. The
[operations guide](docs/guides/operations.md#target-hardware-smoke-acceptance)
explains resume behavior, result files, and what `PASS`, `FAIL`, and `BLOCKED`
mean.

Compare llama.cpp's ROCm and Vulkan backends on the exact model you use:

```bash
./rocmplete benchmark llama-cpp \
  --preset qwen3.6-27b-q8-0 \
  --compare-backends
```

Sparse and dense models, and even different quantizations from one family,
can prefer different backends on the same GPU. The
[tuning guide](docs/guides/tuning.md#benchmarks) covers repeatable comparisons.

Sweep the server-side MTP or DFlash draft depth separately. This path uses the
managed chat template, reasoning, sampling, cache, and speculative policy that
native `llama-bench` cannot exercise:

```bash
./rocmplete benchmark llama-cpp-speculative \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium --dry-run
```

The real run checkpoints every fresh-server request and can resume after an
interruption. Its default long-context screen is intentionally an unattended
hardware experiment, not a routine smoke test or an automatic preset change.

Evaluate a managed model as a coding agent against the frozen Go and Python
task suite:

```bash
./rocmplete benchmark agent --list-tasks
./rocmplete benchmark agent \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium --dry-run
```

Reasoning selectors now follow each model's native contract: Qwen3.6 has an
off/on toggle, Qwen3.8 has off/low/medium/xhigh effort, and Muse has
low/medium/high/xhigh strength. The benchmark runner validates the explicit
choice and records both the client selector and native value. There is no
claim that one shared label is an equivalent reasoning condition across these
families; define and record the comparison protocol before running the
separate hardware pass.

This is separate from smoke acceptance and native token-speed benchmarking.
It runs Pi against disposable single-commit fixtures, applies hidden tests
after each implementation attempt, and preserves raw transcripts, patches,
server logs, and a Markdown summary below managed application data. The
[operations guide](docs/guides/operations.md#coding-agent-evaluation) explains the
fixed-harness policy, review tasks, grading, repetitions, and result scope.

## User guides

The complete [documentation index](docs/README.md) includes user guides,
maintainer references, research snapshots, and source-of-truth pointers.

- [Applications](docs/guides/applications.md) covers ComfyUI, llama.cpp,
  DwarfStar, managed models, APIs, OpenCode, Pi, Oh My Pi, Maki, and multi-GPU
  workloads.
- [Content](docs/guides/content.md) covers recipes, exact bundles, licenses,
  verification, resumable downloads, mirrors, imports, and workflows.
- [Operations](docs/guides/operations.md) covers acceptance, builds, caches, image
  archives, persistent state, logs, stop, and scoped cleanup.
- [Tuning and benchmarks](docs/guides/tuning.md) covers host GPU access, runtime
  policies, RDNA 3.5 shared memory, RDNA 4, and repeatable measurements.

Command-specific help remains the authoritative interface reference:

```bash
./rocmplete --help
./rocmplete build --help
./rocmplete content --help
./rocmplete run --help
./rocmplete acceptance --help
./rocmplete benchmark agent --help
```

Human-facing output uses semantic colors on terminals. Redirected output stays
plain, and `NO_COLOR=1` disables styling.

See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and
[catalog/README.md](catalog/README.md) for provenance and catalog policy.
The bounded smoke command complements, but does not replace, the complete
[maintainer acceptance matrix](docs/hardware-acceptance.md).

## Contributing and security

Focused fixes, hardware results, and careful improvements are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the source map, validation commands, and
the details that make a useful bug report. Report security-sensitive issues
through the private path documented in [SECURITY.md](SECURITY.md), not a
public issue.

## License

ROCmplete source is available under the [BSD 3-Clause License](LICENSE).
Downloaded models, workflows, and bundled third-party components retain their
own terms. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and the catalog
license metadata before using them.
