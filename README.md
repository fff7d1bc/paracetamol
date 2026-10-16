# Paracetamol

Paracetamol builds and runs local AI applications as rootless containers. It
pins the software, verifies the content, and keeps application data outside the
images. Hardware support currently centers on AMD ROCm, but the project is not
designed around the assumption that ROCm will always be its only accelerator
stack.

Bringing up a new machine should take a few commands that you can inspect and
understand. If the host is not ready, the tool should tell you what is wrong
and what to do next.

The supported AMD targets cover RDNA 4 discrete GPUs and the two current Ryzen
AI families. The 32 GB Radeon AI PRO R9700, RX 9070 XT, RX 9070, and RX 9070
GRE use `gfx1201`. The RX 9060 XT and RX 9060 use `gfx1200`. Ryzen AI Max,
also known as Strix Halo, uses `gfx1151`. Ryzen AI 300, also known as Strix
Point, uses `gfx1150`. The managed application builds target all four architectures.
The project is still pre-release, and work on the hardware acceptance matrix
is ongoing.

Paracetamol currently manages these applications and services.

- ComfyUI for image generation, image editing, and video on port 8188
- llama.cpp as an OpenAI-compatible API server and interactive GGUF CLI on
  port 8080, with selectable ROCm and Vulkan backends
- DwarfStar as an experimental, high-memory DeepSeek V4 Flash server and CLI
  on port 8000
- Paracetamol's lazy gateway for the verified llama.cpp and DwarfStar
  inventory through one OpenAI-compatible API on port 7455

## Hardware exercised so far

These machines are used during development. The last column records what was
actually exercised. It is useful evidence, not a claim that every row in the
[target-hardware acceptance matrix](docs/hardware-acceptance.md) has passed.

| Host | GPU target | Workloads exercised |
| --- | --- | --- |
| Fedora Kinoite 44, Ryzen AI 9 HX 370, 128 GB DDR5-5600 SODIMM | Strix Point, `gfx1150` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets |
| Fedora Linux 44 (non-OSTree), Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash at 4K and 128K context, including the optional DSpark pair. Managed Qwen3.6 and Qwen3.8 llama.cpp MTP and tool paths, including Qwen3.8 Dynamic Q4_K_XL at 128K. Managed agent clients, ROCm and Vulkan paths, Muse Glimmer dynamic and 256K probes, plus historical Laguna XS and Ling controls. |
| Ubuntu 26.04, Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets |
| SteamOS 3.8, Radeon RX 9070 XT 16 GB | RDNA 4, `gfx1201` | ComfyUI and the Qwen3 0.6B llama.cpp smoke |

## Why Paracetamol

The name is a small joke about making local AI less painful. The serious part
is keeping powerful machinery clear enough to inspect and use responsibly.

Getting a container to start is the easy part. It does not prove that PyTorch
found the right GPU or that inference will work on the real hardware.
Paracetamol checks the rest of that path too.

- Ubuntu base images, ROCm and PyTorch versions, application commits,
  dependencies, source patches, models, and workflows are pinned.
- One checkout builds for `gfx1201`, `gfx1200`, `gfx1151`, and `gfx1150`.
  Runtime checks confirm the architecture and apply the matching policy.
- `doctor` checks Podman, GPU devices, permissions, SELinux, shared-memory
  setup, the detected architecture, and a real PyTorch tensor operation.
- Downloads are pinned by revision or model-version ID, size, and SHA-256.
  License state is recorded, downloads resume, and content survives image
  rebuilds.
- Containers are rootless, read-only, capability-free, and expose only the
  selected GPU devices. Web applications publish on loopback by default.
- The gateway exports native Maki configuration for macOS or Linux clients.
  Other clients can use its OpenAI-compatible API directly.
- `acceptance` goes beyond startup. It runs small real workloads, checkpoints
  progress, and collects visual review after unattended work.

## Requirements

The host control plane supports Linux and needs rootless Podman, GNU Make, Go
1.26 or newer, a C compiler, and glibc development headers. The normal build
uses glibc so network clients follow the host's NSS configuration, including
mDNS providers. Platform-qualified directories below `build/` separate local
build state. They do not imply that the complete controller runs on other
operating systems. The checkout launcher rebuilds its repository-local binary
when Go or Makefile inputs change. Generated binaries and Go caches stay below
the ignored `build/` directory.

Development hosts currently run SteamOS 3.8, Fedora 44 in conventional and
Kinoite deployments, and Ubuntu 26.04. Minimal installations may not include
Podman. Remote clients need only their own application and network access to
an inference host. They do not need a Paracetamol checkout.

GPU use needs read/write access to `/dev/kfd` and the selected
`/dev/dri/renderD*` nodes. Run Doctor before changing permissions or kernel
settings. It reports the problem and the host-specific action when one is
needed. The
[host GPU access guide](docs/guides/tuning.md#host-gpu-access) explains the
SELinux, group, and udev choices.

### Pre-release compatibility

The Go control plane uses the Paracetamol identity for its command,
configuration prefix, default data directory, image and container names,
ownership labels, container protocol, and result schemas. It does not create
aliases or automatically migrate state from a differently named checkout.
Existing files are never deleted as part of this transition. To reuse managed
content, select the intended `data_dir` and run the normal installer so that
Paracetamol can validate it.

The Go transition changed a few commands and checkpoint formats. `acceptance`
is now a direct leaf command. Benchmarks use `comfyui run`, `comfyui suite`,
`llama-cpp throughput` and `llama-cpp speculative`. Older benchmark and acceptance JSON is still
useful as historical evidence, but it cannot resume a current run.

Managed Pi support, the Pi and Maki launchers, their bubblewrap sandbox, and
`benchmark agent` have been retired. Installed runtimes, sessions, credentials
and evaluation results are left untouched. Use native Maki with the
[gateway configuration export](docs/guides/remote-maki.md). It runs with your
normal user access, not the former launcher isolation.

## Quick start

Clone the source and inspect the host. Paracetamol does not publish prebuilt
application images.

```bash
git clone https://github.com/fff7d1bc/paracetamol.git
cd paracetamol
./paracetamol --version
./paracetamol doctor
```

Before the first build, Doctor reports that its containerized PyTorch probe was
skipped. Build all applications, inspect the available content, and choose a
recipe.

```bash
./paracetamol build all
./paracetamol doctor
./paracetamol content install
```

`build all` builds ComfyUI, llama.cpp, and the experimental DwarfStar image.
Build one application instead when that is all you need. Models range from
hundreds of MiB to more than 100 GiB, so inspect a dry run before a large
installation.

```bash
./paracetamol content install llama-cpp muse-glimmer --dry-run
```

The built-in guides provide short, copyable walkthroughs.

```bash
./paracetamol guide comfyui
./paracetamol guide llama-cpp
./paracetamol guide dwarfstar
```

### ComfyUI

The practical image recipe installs Qwen Image FP8 Lightning and its curated
workflow.

```bash
./paracetamol build comfyui
./paracetamol content install comfyui image
./paracetamol run comfyui
```

Open `http://127.0.0.1:8188`. Image editing, T2V, I2V, imported content,
Manager, and multi-GPU graphs are covered in the
[application guide](docs/guides/applications.md#comfyui).

### llama.cpp

Qwen3.8 Flash-Next Dynamic Q4_K_XL at `xhigh` is the exported Maki default
when the gateway advertises it. It needs a high-memory host. Dense Qwen3.8
27B remains available for smaller systems and as a comparison model.

To start with the dense model:

```bash
./paracetamol build llama-cpp
./paracetamol content install llama-cpp qwen3.8
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-27b-mtp-ud-q8-k-xl
```

The recipe installs one 29.30 GiB GGUF containing the dense target and its MTP
prediction heads. The dense preset starts at 256K context, verifies up to
three draft tokens, and uses native medium reasoning effort. Its non-MTP
control shares the same verified artifact.

Public text model IDs now begin with the publisher of the pinned GGUF. The
models above are `unsloth-…`, and the Muse presets are `meta-models-…`.
These are breaking changes for clients that send a literal `model` ID,
including OpenAI-compatible HTTP clients. Update those IDs after upgrading.
Refresh Maki's exported configuration when the gateway inventory changes.
Installed model files and their verification receipts are not renamed or re-downloaded. Run
`./paracetamol content list models` to see the full current IDs.

A separate 16.35 GiB Unsloth Dynamic v3 Q4_K_XL bundle keeps the same model
family available for more constrained GPUs without changing the recipe or
managed-client default.

```bash
./paracetamol content install llama-unsloth-qwen3.8-27b-ud-q4-k-xl
./paracetamol run llama-cpp server --preset unsloth-qwen3.8-27b-mtp-ud-q4-k-xl
```

The Dynamic v3 Q4_K_XL presets start at a reviewed 128K context. Treat the
smaller quantization as a capacity and throughput tradeoff, not an
equivalent-quality replacement for Dynamic Q8_K_XL. On the
accepted Strix Halo host its matched depth-three MTP screen generated 3.3%
faster than the earlier preview Q4 artifact, with 1.9% slower prompt
processing. It solved three of five fresh hidden-graded Pi tasks. The earlier
artifact also missed both tasks used as direct failure controls and was much
slower on the harder control. ROCm remains the default backend. Dedicated 32
GiB RDNA 4 capacity still needs acceptance on that hardware.

The [Qwen3.8 Dynamic Q4 versus Q8 coding-agent
comparison](docs/guides/qwen3.8-dynamic-quant-comparison.md) adds hidden-graded
medium and `xhigh` quality results, per-task outcomes, targeted retries, and
the limits of applying those Unsloth-specific findings to other GGUF releases.

Qwen3.8 Flash-Next 125B-A6B is a separate family for 128 GB
Strix Halo hosts with fast local storage. Its guided four-shard Unsloth
Dynamic Q4_K_XL conversion occupies 103.7 GiB. Paracetamol reads the model's
large per-layer token embedding on demand from local storage, with the other
weights on the GPU. This is a capacity path, not a claim that SSD becomes
GPU memory. The pinned upstream llama.cpp does not yet expose the model's
MTP heads.

```bash
./paracetamol content install llama-cpp qwen3.8-flash-next \
  --accept-license --acknowledge-license-risk
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl
```

On Strix Halo, direct startup now selects ROCm and a ROCm gateway includes
the installed, verified model. Its model-specific policy uses direct CPU
embedding reads and disables inherited unified-memory allocation only for
that model process. Ordinary ROCm allocation still produces corrupt output
for this model. The accepted Vulkan path remains available with
`--backend vulkan`. Other models keep their existing settings.

Keep `--models-max 1` when using Flash-Next through a gateway or router on a
128 GB host. The model count is not a memory budget. The [acceptance
record](docs/qwen3.8-flash-next-direct-reader-feasibility.md) covers the
262144-token ceiling, populated-context tests, memory use and limitations.

The pinned conversion's license metadata could not be verified against a
license file at its exact revision, so installation requires the explicit
unverified-license acknowledgment. Hardware acceptance is limited to `gfx1151`.
The Maki example selects this model at `xhigh`. HTTP requests that omit
reasoning still use `medium`.

Huihui's abliterated v2 is an optional, separately named Flash-Next variant.
It uses the Unsloth-derived UD-Q4_K_XL files and the same runtime policy,
template and sampling defaults. It does not replace regular Flash-Next.
See the [model guide](docs/guides/applications.md#choosing-a-managed-qwen-preset)
for installation and limitations.

Qwen3.6 has been retired from the catalog and clients. Existing downloads
are left untouched. Muse Glimmer remains a separate comparison family.

```bash
./paracetamol content install llama-cpp muse-glimmer
```

Each dense Qwen3.8 quantization shares one
GGUF between its base and MTP presets. The list prints each pair of
preset aliases on one comma-separated row. Muse's three presets also share
one artifact pair.

Use the managed router to serve several installed presets.

```bash
./paracetamol run llama-cpp server --router --models-max 1
```

The [llama.cpp guide](docs/guides/applications.md#llamacpp) explains presets,
contexts, MTP, DFlash, translations, the terminal CLI, tool calling, and
choosing between the managed Qwen variants. High-memory hosts can also install
the KAT-Coder Q8 coding-agent candidate without changing the default.

```bash
./paracetamol content install llama-cpp kat-coder
```

For Japanese and English translation on a high-memory host, the separate
Shisa V2.1 recipe installs the 70B Q8_0 model and requires acknowledgment of
the Llama 3.3 terms.

```bash
./paracetamol content install llama-cpp shisa-v2.1 --accept-license
./paracetamol run llama-cpp server \
  --preset mradermacher-shisa-v2.1-llama3.3-70b-q8-0
```

### DwarfStar

DwarfStar serves the pinned DeepSeek V4 Flash 0731 Q2 imatrix model. It uses
IQ2_XXS for routed gate/up weights, Q2_K for routed down weights, and Q8 for
attention projections, shared experts, and output. The model is about 80.76
GiB before context and working allocations, so this path is for a host with
enough GPU-mapped memory.

```bash
./paracetamol build dwarfstar
./paracetamol content install dwarfstar flash-0731-q2-imatrix
./paracetamol run dwarfstar server
```

The separate 5.58 GiB DSpark support GGUF is an opt-in speculative-decoding
path. It does not replace the default model or improve its quality.

```bash
./paracetamol content install dwarfstar flash-0731-q2-imatrix-dspark
./paracetamol run dwarfstar server --dspark
```

The [DwarfStar guide](docs/guides/applications.md#dwarfstar) covers its 128K
managed context, memory setup, optional DSpark path, API, agent-client
providers, and bounded acceptance.

## Everyday use

Inspect local state, start an installed application, and let `auto` select the
hardware profile.

```bash
./paracetamol status
./paracetamol run comfyui
```

A running llama.cpp server can also print a pasteable configuration report.

```bash
./paracetamol status llama-cpp
./paracetamol status llama-cpp \
  --model unsloth-qwen3.8-27b-mtp-ud-q8-k-xl
```

The model selector identifies a managed direct preset or chooses one configured
router preset. The report includes the immutable image and source identities,
resolved hardware, model policy, sampling ownership, and exact running llama.cpp
command without printing API-key values or host secret paths.

For Qwen, the gateway fills missing sampling settings according to the
request's thinking mode. Explicit client values still win. This applies to
Maki and other HTTP clients using that endpoint. Direct engine endpoints
use native defaults. See [sampling policy](docs/gateway.md#model-aware-sampling-defaults).

Override the profile only when testing or diagnosing.

```bash
./paracetamol run comfyui --profile rdna4
./paracetamol run comfyui --profile strix-halo
./paracetamol run comfyui --profile strix-point
./paracetamol run comfyui --profile cpu
```

Web applications publish on `127.0.0.1` by default. Select one exact LAN or
Tailscale address only when unauthenticated network access is intentional.

```bash
./paracetamol run comfyui --listen 192.168.1.50
```

For local or remote coding-agent work, start the gateway and connect a native
client. With no application option the gateway discovers every built text
application that has a compatible, verified model. An explicit `-a` or
`--application` list strictly replaces discovery. Startup does not load a
backend.

```bash
./paracetamol content install llama-cpp qwen3.8
./paracetamol run gateway
# In another terminal, use Maki after configuring it as described below.
```

Persistent gateway defaults are optional. Generate a complete runnable host
configuration, or select a committed machine profile for one invocation.

```bash
./paracetamol config init
./paracetamol run gateway -c configs/gateway.toml
```

The configuration selector is global and may also precede the command. Flags
override matching environment variables where those variables are defined.
Both take precedence over configuration values.

`--port` changes the shared gateway port. Used without `--listen`, it also
selects loopback for that invocation. Supply both options to change a remote
publication.

Automatic discovery includes DwarfStar when its image and compatible verified
model are present. Use `-a llama-cpp` for a llama.cpp-only gateway, `-a
dwarfstar` for DwarfStar only, or repeat `-a` to demand both. The first request
lazily starts the matching private backend. Its container output then appears
in the gateway terminal.

Only one backend application stays resident. A request for the other
application drains active work, stops the current backend, and starts the new
one. Queued requests remain FIFO. llama.cpp keeps one model loaded by default
because its router limit counts models without considering their memory use.
Use `--models-max` only for a larger set known to fit.

`./paracetamol status gateway` shows the frozen inventory and live allocation.
Add `--requests 10` while diagnosing a client to see lifetime aggregates and
recent request timing. Loaded llama.cpp rows include their effective context,
training context, parameter count, size, and quantization when the router
reports them. Completion timing metadata supplies weighted prompt-processing
and token-generation rates plus speculative acceptance. Token usage and
client-supplied reasoning or sampler fields appear when available. Prompts and
response content are not retained. Clients may supply their own session UUID
for grouping. The Maki example does not invent one.

Status also shows observed streaming first-output latency, prompt-cache reuse
and a host RAM/GPU sensor snapshot. Missing measurements stay unavailable.

Maki can run on macOS or Linux while the gateway runs on the GPU host. The
gateway retains its loopback endpoint when you add a LAN publication.

```bash
./paracetamol run gateway --listen 192.168.1.50 --port 7455
```

Install Maki separately, then follow the [native Maki setup](docs/guides/remote-maki.md)
to download `providers.toml` and `init.lua` from the gateway. Review and merge
them into your normal Maki configuration. There is no Paracetamol installation,
launcher, automatic refresh, or sandbox on the client.

The export lists only verified coding-capable models in that gateway's frozen
inventory. It prefers regular Flash-Next at `xhigh`. Raw HTTP requests still
use medium when reasoning is omitted. Maki's model picker and `/thinking`
select the model's native controls.

Restrict the published port with your firewall and use
[optional Bearer authentication](docs/gateway.md#optional-authentication)
when appropriate. HTTP does not encrypt credentials or prompts. Other clients
can connect through the same OpenAI-compatible API.

On a multi-GPU host, repeat `--render-node` for every card intended for one
supported workload. Paracetamol never guesses the set and requires matching
architectures.

```bash
./paracetamol run llama-cpp server \
  --model /path/to/large-model.gguf \
  --render-node /dev/dri/renderD128 \
  --render-node /dev/dri/renderD129
```

llama.cpp uses layer splitting automatically. ComfyUI needs graph nodes that
place components or work on the selected cards. See the
[application guide](docs/guides/applications.md) and
[runtime tuning guide](docs/guides/tuning.md#runtime-policies).

Foreground runs own the container lifecycle. Ctrl-C stops and removes the
container. Use the log and stop commands to manage detached runs.

```bash
./paracetamol logs comfyui --follow
./paracetamol stop comfyui
```

Update without rewriting local work or persistent content.

```bash
git pull --ff-only
./paracetamol doctor
./paracetamol build all
```

## Builds, content, and storage

The three main operations are independent and retryable.

```text
build  ->  content install  ->  run
image      models/workflows     application
```

Normal builds reuse Podman layers and a local Python package cache. Use
`--no-layer-cache` to rerun one application image or `--no-cache` for a fully
cold build. The
[operations guide](docs/guides/operations.md#builds-and-local-caches) explains
prerequisite images, cache boundaries, cleanup, and image transfer.

Content discovery starts with practical recipes and expands only when asked.

```bash
./paracetamol content list
./paracetamol content list bundles
./paracetamol content list families
./paracetamol content list models --details
./paracetamol content import
```

Set `HF_TOKEN` before a large Hugging Face installation when you have one.
`CIVITAI_TOKEN` is needed only for authenticated Civitai imports and
user-owned packs. Paracetamol passes supplied tokens only to its download tools
and does not store them in images or persistent state. The
[content guide](docs/guides/content.md) covers recipes, exact bundles, terms,
verification, resumable downloads, mirrors, imports, and workflows.

Persistent data defaults to
`${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol`. Put large content on another
filesystem with `${XDG_CONFIG_HOME:-$HOME/.config}/paracetamol/config.toml`.

```toml
[storage]
data_dir = "/mnt/ai/paracetamol"
```

Every setting is optional. `./paracetamol config init` creates a complete
runnable file at that path without replacing an existing file. Use `-c` or
`--config` to select another file, or `--no-config` to ignore the default XDG
file. Paracetamol never migrates persistent data automatically.
See [persistent data](docs/guides/operations.md#persistent-data) before moving or
cleaning application state, models, inputs, or outputs.

## Acceptance and benchmarks

After onboarding a host or updating software, run the bounded checkpointed
smoke suite.

```bash
./paracetamol acceptance --dry-run
./paracetamol acceptance
```

Automated workloads finish before the visual review pass, so the run can be
left unattended. The
[operations guide](docs/guides/operations.md#target-hardware-smoke-acceptance)
explains resume behavior, result files, and what `PASS`, `FAIL`, and `BLOCKED`
mean.

Compare llama.cpp's ROCm and Vulkan backends on the exact model you use.

```bash
./paracetamol benchmark llama-cpp throughput \
  --preset unsloth-qwen3.8-27b-ud-q8-k-xl \
  --compare-backends
```

Sparse and dense models, and even different quantizations from one family,
can prefer different backends on the same GPU. The
[tuning guide](docs/guides/tuning.md#benchmarks) covers repeatable comparisons.

Sweep the server-side MTP or DFlash draft depth separately. This benchmark uses
the managed chat template, reasoning, sampling, cache, and speculative policy
that native `llama-bench` cannot exercise.

```bash
./paracetamol benchmark llama-cpp speculative \
  --preset unsloth-qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium --dry-run
```

The real run checkpoints every fresh-server request and can resume after an
interruption. Its default long-context screen is intentionally an unattended
hardware experiment, not a routine smoke test or an automatic preset change.

The former Pi coding-task runner is retired. Its frozen tasks and historical
results remain documented in the [archived evaluation record](docs/coding-agent-evaluation.md).
Native llama.cpp and ComfyUI benchmarks are unchanged.

## User guides

The complete [documentation index](docs/README.md) includes user guides,
maintainer references, research snapshots, and source-of-truth pointers.

- [Applications](docs/guides/applications.md) covers ComfyUI, llama.cpp,
  DwarfStar, managed models, APIs, Maki, and multi-GPU
  workloads.
- [Content](docs/guides/content.md) covers recipes, exact bundles, licenses,
  verification, resumable downloads, mirrors, imports, and workflows.
- [Operations](docs/guides/operations.md) covers acceptance, builds, caches, image
  archives, persistent state, logs, stop, and scoped cleanup.
- [Tuning and benchmarks](docs/guides/tuning.md) covers host GPU access, runtime
  policies, RDNA 3.5 shared memory, RDNA 4, and repeatable measurements.

Command-specific help is the authoritative interface reference.

```bash
./paracetamol --help
./paracetamol build --help
./paracetamol content --help
./paracetamol run --help
./paracetamol acceptance --help
./paracetamol benchmark llama-cpp --help
```

Host development uses the installed Go toolchain by default and keeps its
module cache, build cache, temporary files, telemetry state, and binaries
below `build/`.

```bash
make check
make test
make -B build
make static
```

`make build` is the normal incremental path. The forced build is useful before
finishing Go or Makefile changes. `make clean` removes only the repository's
ignored `build/` tree. Normal builds require CGO and use the host's glibc and
NSS resolver. `make static` explicitly builds the optional pure-Go binary. That
binary can use DNS and `/etc/hosts`, but it cannot use NSS-only sources such as
mDNS. The optional native-Linux `make race` check uses CGO as well.

Normal commands use `GOTOOLCHAIN=local`, so the installed Go toolchain must
satisfy `go.mod`. Use `make GOTOOLCHAIN=auto -B build` only when automatic
toolchain selection is intentional. Its Go state stays below
`build/`.

To copy a checkout to a test host without transferring repository-local
binaries or Go caches, use the following command.

```bash
rsync -a -v --progress --delete \
  --exclude=/.git/ --exclude=/build/ \
  --exclude='__pycache__/' --exclude='*.py[cod]' \
  ~/src/paracetamol/ gateway.example:src/paracetamol
```

The anchored exclusions preserve the destination's own Git metadata and
`build/` cache, while the Python patterns keep ignored container-test bytecode
out of a deployment. `--delete` still removes obsolete source files. Do not
add `--delete-excluded`.

The pre-release product identity has one definition shared by the Go packages.
`make -B PRODUCT_ID=new-name DISPLAY_NAME='New Name' build`
changes the built command, host-configuration environment prefix (`NEW_NAME_*`),
persistent namespace, local image namespace, container names, and
ownership-label namespace together.
Set `ENV_PREFIX` explicitly only when a renamed command needs another spelling.
Changing that identity selects new state and image ownership. It does not
migrate an existing Paracetamol data directory.

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

Paracetamol source is available under the [BSD 3-Clause License](LICENSE).
Downloaded models, workflows, and bundled third-party components retain their
own terms. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and the catalog
license metadata before using them.
