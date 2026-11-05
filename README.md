# Paracetamol

Paracetamol builds and runs local AI applications as rootless containers. It
pins the software, verifies models and workflows, and keeps persistent data
outside the images. The name is a small joke about making local AI less painful.

It currently manages ComfyUI for image generation, editing and video, and
llama.cpp for text inference and image-capable Flash-Next models. A lazy gateway
serves the verified llama.cpp inventory through one OpenAI-compatible API.
Application images are built locally, not downloaded from Paracetamol.

The managed AMD builds target RDNA 4 (`gfx1200` and `gfx1201`), Strix Halo
(`gfx1151`) and Strix Point (`gfx1150`). The project is pre-release. A successful
build or CPU startup does not prove GPU inference works on a particular host.
See the [hardware acceptance record](docs/hardware-acceptance.md) for tested
machines, workloads and remaining gaps.

## Why Paracetamol

- Software, application sources, models and workflows use immutable pins.
- Downloads are size- and SHA-256-verified, resumable and license-aware.
- Containers are rootless, read-only and capability-free, with only the exact
  selected GPU devices. Web services publish on loopback by default.
- `doctor` explains host readiness problems. `acceptance` runs bounded real
  workloads with checkpointing and a separate visual review.
- The gateway loads models on demand and exports native Maki configuration.
  Other clients can use the same OpenAI-compatible API.

## Requirements

The Linux host needs rootless Podman, GNU Make, Go 1.26 or newer, a C compiler
and glibc development headers. The checkout launcher builds its local binary
automatically. Binaries, Go caches and temporary build state stay below the
ignored `build/` directory. Use the checkout, not a copied bare binary.

Development hosts have used conventional Fedora, Fedora Kinoite 44,
Ubuntu 26.04 and SteamOS 3.8. The Strix Halo host now runs conventional Fedora
45. These are host observations, not distribution-wide certification or fresh
inference acceptance after a firmware or OS upgrade.

GPU use needs read/write access to `/dev/kfd` and the selected
`/dev/dri/renderD*` nodes. Run `doctor` before changing permissions or kernel
settings. See [host GPU access and memory setup](docs/guides/tuning.md#host-gpu-access)
for remediation. Remote clients need only their own application and network
access to the GPU host, not a Paracetamol checkout.

## Quick start

Clone the source, inspect the host and build the applications locally.

```bash
git clone https://github.com/fff7d1bc/paracetamol.git
cd paracetamol
./paracetamol --version
./paracetamol doctor
./paracetamol build all
./paracetamol doctor
```

Before the first build, Doctor reports that its containerized PyTorch probe
was skipped. `build all` builds ComfyUI and llama.cpp. You can instead build
only the application you need with `build comfyui` or `build llama-cpp`.

Choose models before downloading. Recipes range from hundreds of MiB to more
than 100 GiB, so inspect the plan and license requirements first.

```bash
./paracetamol content list
./paracetamol content install llama-cpp qwen3.8 --dry-run
```

Set `HF_TOKEN` when needed for Hugging Face access. Authentication does not
replace license acceptance. The [content guide](docs/guides/content.md)
covers recipes, exact bundles, terms, verification, mirrors and imports.

### Text inference through the gateway

Start with the dense Qwen3.8 27B recipe if the model and its runtime memory
fit your host. It installs a 29.30 GiB Dynamic Q8_K_XL GGUF.

```bash
./paracetamol content install llama-cpp qwen3.8
./paracetamol run gateway
```

Open `http://127.0.0.1:7455/` for connection instructions, the searchable model
list and Maki configuration downloads. The gateway exposes a frozen inventory
of compatible, receipt-verified models and starts no backend until the first
inference request. It allows one loaded model at a time by default.

Install Maki separately on macOS or Linux and follow the
[native Maki guide](docs/guides/remote-maki.md). Other OpenAI-compatible clients
can use `http://127.0.0.1:7455/v1` and a model ID from the gateway page.

For high-memory Strix Halo hosts, the separate Qwen3.8 Flash-Next recipe offers
plain and DFlash2 variants with image input. It needs more than 100 GiB of
model storage, fast local storage and explicit license-risk review. Regular
Flash-Next DFlash2 at `xhigh` is the Maki export's preferred model when present.
Dense Q4 and other optional variants remain available. See
[choosing a managed model](docs/guides/applications.md#choosing-a-managed-qwen-preset)
for commands, memory limits and measured tradeoffs.

For an isolated direct server or interactive terminal instead of the gateway:

```bash
./paracetamol run llama-cpp server --preset unsloth-qwen3.8-27b-ud-q8-k-xl
./paracetamol run llama-cpp cli --preset unsloth-qwen3.8-27b-ud-q8-k-xl
```

Run those alternatives separately. The direct server uses port 8080. The
[llama.cpp guide](docs/guides/applications.md#llamacpp) covers contexts,
reasoning, tool calls, ROCm/Vulkan, local GGUFs and multi-GPU runs.

### ComfyUI

The practical image recipe installs Qwen Image FP8 Lightning and its curated
workflow.

```bash
./paracetamol content install comfyui image
./paracetamol run comfyui
```

Open `http://127.0.0.1:8188`. See the
[ComfyUI guide](docs/guides/applications.md#comfyui) for image editing, video,
Manager, custom nodes and multi-GPU workflows.

## Everyday use

Let the default `auto` profile select hardware. Inspect local state and the
gateway's live allocation with:

```bash
./paracetamol status
./paracetamol status gateway
./paracetamol status gateway --requests 10
```

Foreground runs own their lifecycle. Ctrl-C stops the service and removes its
owned containers. The [operations guide](docs/guides/operations.md#status-logs-and-stop)
also covers detached runs, logs, stop commands and shareable llama.cpp reports.

For remote access, publish only on one trusted address, restrict the port with
your firewall and consider [Bearer authentication](docs/gateway.md#optional-authentication).
HTTP does not encrypt credentials or prompts. Direct applications are
unauthenticated unless their reviewed mode explicitly provides authentication.

```bash
./paracetamol run gateway --listen 192.0.2.10 --port 7455
```

`192.0.2.10` is a documentation address. Substitute the host's intended address.
See [remote client setup](docs/guides/remote-maki.md) before exposing a service.

Persistent data defaults to
`${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol`. Configure another location
before a large install with `./paracetamol config init` and `[storage].data_dir`.
The [persistent-data guide](docs/guides/operations.md#persistent-data) explains
configuration precedence and safe moves. Existing data is never migrated
automatically.

Before updating, review the [upgrade notes](docs/guides/upgrade-notes.md) for
retired applications, model ID changes and client refreshes.

```bash
git pull --ff-only
./paracetamol doctor
./paracetamol build all
```

After onboarding or updating a suitable GPU host, inspect and run the bounded
smoke suite. This generates real workloads, not just startup checks.

```bash
./paracetamol acceptance --dry-run
./paracetamol acceptance
```

The [acceptance walkthrough](docs/guides/operations.md#target-hardware-smoke-acceptance)
explains prerequisites, resume and visual review. Use the
[tuning and benchmark guide](docs/guides/tuning.md#benchmarks) for repeatable
performance comparisons rather than treating smoke success as a speed claim.

## Documentation and development

The [documentation index](docs/README.md) routes user guides, maintainer
contracts and research records. Start with:

- [Applications](docs/guides/applications.md) for ComfyUI, llama.cpp, managed
  models, APIs and clients.
- [Content](docs/guides/content.md) for downloads, licenses, recipes and workflows.
- [Operations](docs/guides/operations.md) for builds, caches, image transfer,
  persistent state and scoped cleanup.
- [Tuning and benchmarks](docs/guides/tuning.md) for GPU access, memory and
  runtime policy, hardware limits and measurement.
- [Upgrade notes](docs/guides/upgrade-notes.md) before updating an older setup.

Built-in guides provide copyable walkthroughs. Command-specific help is the
authoritative interface reference.

```bash
./paracetamol guide
./paracetamol --help
./paracetamol run --help
```

For development, run `make check`, `make test` and `make static`. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the full baseline and bug-report guidance,
and [development and deployment](docs/testing-and-release.md#development-and-deployment)
for build variants, checkout transfer and custom product identities.
Report security issues through the private path in [SECURITY.md](SECURITY.md).

## License

Paracetamol source uses the [BSD 3-Clause License](LICENSE). Downloaded models,
workflows and third-party components retain their own terms. See
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and
[catalog policy](catalog/README.md) before using them.
