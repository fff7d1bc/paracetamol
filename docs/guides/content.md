# Content

[Documentation index](../README.md)

ROCmplete treats application images and model content as two different things.
Images are replaceable software builds. Content is the much larger collection
of models and workflows that should survive an image rebuild.

```text
build  ->  content install  ->  run
image      models/workflows     application
```

`content install` only deals with the middle step. It never builds an image or
launches an application. Every remote file is pinned to an immutable source
revision or exact model-version ID, exact byte size, and SHA-256.

## Find one useful thing

The default list shows the small recipe surface:

```bash
./rocmplete content list
```

Recipes are organized by their consuming application:

```text
comfyui
  image  edit  t2v  i2v
llama-cpp
  qwen3.6  qwen3.8  kat-coder  muse-glimmer
  shisa-v2.1  translation-gemma  translation-hy
dwarfstar
  flash-0731-q2-imatrix  flash-0731-q2-imatrix-dspark
```

Install interactively, or select one recipe explicitly:

```bash
./rocmplete content install
./rocmplete content install comfyui image
./rocmplete content install llama-cpp qwen3.6
./rocmplete content install llama-cpp qwen3.8
./rocmplete content install llama-cpp muse-glimmer
./rocmplete content install llama-cpp shisa-v2.1 --accept-license
./rocmplete content install llama-cpp translation-gemma --accept-license
./rocmplete content install dwarfstar flash-0731-q2-imatrix
./rocmplete content install dwarfstar flash-0731-q2-imatrix-dspark
```

The `qwen3.6` recipe installs both reviewed MTP choices: dense 27B MTP Q8_0
and sparse 35B-A3B MTP Dynamic Q8_K_XL. Its next-step command starts dense
27B, and Qwen3.8 remains the managed-client default. Matching dense and sparse
non-MTP GGUFs remain available through the exact-bundle browser.

The upstream 27B and 35B-A3B MTP artifact basenames omit `MTP`. Their
ROCmplete directories, bundles, presets, and pinned sources retain the exact
MTP identity and prevent collisions with the non-MTP files.

The separate `qwen3.8` recipe installs one 29.30 GiB Dynamic Q8_K_XL GGUF.
That file contains the dense 27B target and its trained MTP prediction heads,
so the matching base and MTP presets share one verified artifact. The initial
integration is text-only: the optional vision projector remains outside the
recipe until its multimodal runtime contract is separately accepted.

The exact `llama-qwen3.8-27b-ud-q4-k-xl` bundle installs the 16.35 GiB
Unsloth Dynamic v3 Q4_K_XL variant. It deliberately stays outside the guided
`qwen3.8` recipe so installing the recommended model does not also download a
second quantization:

```bash
./rocmplete content install llama-qwen3.8-27b-ud-q4-k-xl
```

Its base and embedded-MTP presets start at a reviewed 128K context for more
constrained hardware. The smaller quantization is optional and never
displaces Dynamic Q8_K_XL as the managed-client default.

The `muse-glimmer` recipe installs Meta's 30B Dynamic Q4_K_XL target and
matching DFlash draft. The next-step command starts the forced-256K DFlash
preset used as the Muse family comparison. The same pair also exposes a
non-speculative 128K control and a 128K DFlash preset. All three preserve
parsed reasoning and advertise Muse's native low, medium, high, and xhigh
strengths; high is the family default and off is not advertised. Managed agent
launchers instead prefer Qwen3.8 27B MTP at medium when it is installed.

Catalog pruning does not delete GGUFs installed by an older checkout. Retired
files remain visible as local model rows in `content list --models`; remove
their exact files deliberately only when they are no longer needed.

If a recipe is not specific enough, choose the exact-bundle browser in the
guided installer. It narrows the catalog by application and task before
showing individual bundles. Menus use shorter local names where the context is
obvious, and the final plan always prints the complete stable bundle
identifier.

Use an explicit application aggregate when you want every managed bundle for
one application rather than one practical recipe. This stays out of the guided
menu because it can be hundreds of gigabytes:

```bash
./rocmplete content install llama-cpp all --dry-run
./rocmplete content install llama-cpp all
./rocmplete content install dwarfstar all
```

The command uses the normal resumable installer. Existing verified models are
reported as ready, and only missing files are downloaded. A file from an older
ROCmplete checkout may be shown as `verify` the first time. The installer
hashes it in place instead of downloading it again. Model terms and
unverified-license acknowledgments are collected once for the complete plan.

Use the advanced inventory directly when a recipe is insufficient:

```bash
./rocmplete content list --bundles
./rocmplete content list --bundles --application comfyui
./rocmplete content list --families
./rocmplete content install qwen-image-2512-bf16-base --dry-run
```

The `family qwen` and `family wan` targets select those model families
within ComfyUI. The literal `all` target selects the entire catalog and can
require around a tebibyte plus resumable cache space. Inspect it first:

```bash
./rocmplete content install all --dry-run
./rocmplete content install all \
  --accept-license --acknowledge-license-risk
```

The guided menu does not offer families, application-wide `all`, or the
global `all`. Those choices are too large to select accidentally.

## Terms and verification

ROCmplete shows additional model terms and separately identifies content whose
license metadata could not be verified. On a terminal, omitted approval flags
become confirmation questions. Noninteractive use requires the applicable
flags:

```bash
./rocmplete content install BUNDLE \
  --non-interactive \
  --accept-license \
  --acknowledge-license-risk
```

Approvals are never persisted. `--dry-run` requires neither. Missing or
ambiguous metadata stays `NOASSERTION`; upstream lineage is provenance, not a
license grant or legal advice. Civitai downloads that require an account use
`CIVITAI_TOKEN`; private Hugging Face repositories use `HF_TOKEN`.

For a large built-in catalog installation, export a Hugging Face token when
you have one:

```bash
export HF_TOKEN='HUGGING_FACE_READ_TOKEN'
```

Public Hugging Face repositories work without a token. Supplying one uses the
account's rate-limit tier instead of the anonymous IP limit and can avoid
throttling during a long installation. It does not guarantee more raw download
bandwidth. Export `CIVITAI_TOKEN` separately when making an authenticated
Civitai import or installing a user-owned pack. ROCmplete passes tokens to the
short-lived download container by environment variable name and never writes
their values into command arguments, generated content packs, images, or
persistent data.

### What happens during a download

If a download is interrupted, run the same command again. Completed files are
reused, and the active file resumes or restarts safely.

One content installation may mutate a given data directory at a time. A second
installer fails immediately with a clear message instead of sharing staging,
verification, or final moves with the active command. The lock is released on
normal completion, failure, and Ctrl-C; resumable staging remains in place.

Installation begins only after exact size and SHA-256 verification.
ROCmplete records a durable receipt after a successful hash. A missing or stale
receipt makes the file `unverified`, not ready. The receipt avoids repeatedly
hashing large unchanged files; it is not a signature or a substitute for the
catalog's pinned hash.

This creates a one-time migration after upgrading an existing data directory.
Run the same `content install` command you originally used. It hashes
same-sized existing files, records successful results, refuses mismatches, and
does not redownload or replace the files. A dry run reports the bytes under
`Verify` but neither hashes files nor writes receipts.

Ctrl-C kills the downloader, waits until its exact container is gone, and keeps
partial data for the next attempt.

If a host crash or older launcher leaves a `rocmplete-download-*` container,
the next download refuses to start and prints its exact name. Confirm that it
is stale, remove only that named container with the printed
`podman rm --force NAME` command, and retry; staging remains available for
resume.

Inspect installed state:

```bash
./rocmplete content status comfyui image
./rocmplete content status family qwen --details
./rocmplete content status comfyui image --verify
```

Normal `content status` uses the durable receipts. `--verify` is a read-only
audit that hashes every installed byte again and does not update receipts.
Running `content install` is the way to migrate or refresh stale receipts.

`content status` answers whether a catalog recipe is complete. For the more
practical question of what can be run, list the managed llama.cpp and
DwarfStar choices alongside local llama.cpp GGUFs found on disk:

```bash
./rocmplete content list --models
./rocmplete content list --models --application llama-cpp
./rocmplete content list --models --application dwarfstar
./rocmplete content list --models --details
```

Every managed model is shown even when it is not installed. Missing rows use
the expected catalog size so the inventory is useful before a download. A
llama.cpp managed row is named by its preset; a DwarfStar row is named by its
installable bundle. The DSpark row includes the target and exact support GGUF
and reports their combined size. Imported and manually copied GGUFs below the
llama.cpp model root are shown as local files, and split GGUFs are grouped into
one model instead of one row per shard. An incomplete split set or a catalog
size mismatch is called out rather than presented as runnable.

Use `--details` when choosing between managed llama.cpp presets. It shows the
exact bundle and preset IDs, catalog file count and total size, conservative
starting context, chat-template policy, MTP setup, and profile-specific Flash
Attention policy. DwarfStar details show its target model, optional support
file, bundle, combined size, and copyable install and run commands. These are
properties ROCmplete needs to launch a model correctly, not task descriptions
or claims about which model is best.

ROCmplete does not search the rest of the machine by default. Add known model
locations explicitly; `--scan` is repeatable and accepts one GGUF or a
directory:

```bash
./rocmplete content list --models \
  --scan ~/models \
  --scan /mnt/shared/ggufs
```

External scans apply only to llama.cpp. This inventory is read-only and does
not create the data directory. Catalog rows receive the same size, file-type,
and verification-receipt checks used by normal startup. Loose local files are
discovered, not cryptographically verified.

## Storage

Managed content lives below the selected data directory:

```text
content/
  comfyui/models/
  llama-cpp/models/
  dwarfstar/models/
```

## Local mirror migration

Moving from an older ROCmplete data directory or another machine? A local
mirror can save the download without trusting the old directory layout.

```bash
./rocmplete content install all \
  --local-mirror /path/to/old-rocmplete \
  --accept-license --acknowledge-license-risk
```

ROCmplete searches relevant filenames, rejects wrong sizes, and hashes every
candidate before reuse. Copying is the default. On filesystems without
reflinks, exact validated matches can be moved:

```bash
./rocmplete content install all \
  --local-mirror /path/to/old-rocmplete \
  --local-mirror-move \
  --accept-license --acknowledge-license-risk
```

Move mode removes only catalog-hash matches, but may leave the old tree's
managed symlinks broken. Keep the active data directory outside the mirror,
verify the new installation, then remove the old directory. Dry-run validates
paths but does not hash hundreds of GiB, so its download total is worst-case.

## Local content packs

Use a local content pack when a private or machine-specific selection needs
several pinned files or needs to be installed again later. Packs live in
ignored JSON files:

```bash
./rocmplete content install \
  --from-file local-content/models.json \
  --dry-run

HF_TOKEN=... ./rocmplete content install \
  --from-file local-content/models.json
```

`--from-file` is repeatable. Without a positional target, every bundle from
the supplied packs is selected. Packs temporarily extend the catalog and may
reference built-in definitions or definitions in another supplied pack.
Existing identifiers and destinations cannot be overridden. They use the same
revision, hash, path, license, staging, and verification checks as built-in
content. Tokens do not belong in JSON.

See [Local content packs](../content-catalog.md#local-content-packs) for
the schema and maintainer detail.

## Import one remote file

Use remote import for one model or workflow that does not belong in the public
catalog. It resolves provider metadata into a small local content pack and
then hands installation back to the normal verified downloader.

```bash
./rocmplete content import

./rocmplete content import \
  'https://huggingface.co/OWNER/REPOSITORY/blob/main/MODEL.gguf'

./rocmplete content import \
  'https://civitai.com/models/MODEL_ID?modelVersionId=VERSION_ID'
```

Both `civitai.com` and `civitai.red` are accepted. Hugging Face imports accept
a model repository page or one `blob`/`resolve` file URL. ROCmplete resolves
the source to exact provider metadata and rejects other hosts and arbitrary
direct-download URLs.

Civitai may replace a file without changing its model-version ID. The local
pack will reject changed bytes rather than silently accepting them, but it
cannot recover a removed older file. Treat these packs as reviewed snapshots
for your own setup, not as a durable upstream archive.

With no positional URL, terminal use asks for one. When a page contains
several versions or files, it presents a menu. Provider metadata and the file
type narrow the compatible destinations, but ambiguous checkpoints still ask
whether they are complete checkpoints or standalone diffusion-model weights.
Unsupported provider categories fail instead of inviting an unsafe guess.

Select everything explicitly for scripts:

```bash
./rocmplete content import URL \
  --version VERSION_ID \
  --file FILE_ID_OR_PATH \
  --as comfyui:diffusion-model \
  --non-interactive \
  --acknowledge-license-risk
```

Supported destination types are:

```text
comfyui:checkpoint         comfyui:diffusion-model
comfyui:lora               comfyui:vae
comfyui:text-encoder       comfyui:controlnet
comfyui:upscaler           comfyui:workflow
llama-cpp:model
```

GGUF and provider-declared LoRA, LoCon, DoRA, VAE, ControlNet, upscaler, or
workflow files are placed automatically when the result is unambiguous.
Imported ComfyUI models go below their normal model category in an `imported/`
subdirectory. Exact workflow JSON goes below the imported workflow directory.
Imported GGUF files go below `content/llama-cpp/models/imported/`; the
completed command prints a copyable `llama-cpp --model` invocation rather
than inventing a managed preset.

Before downloading, ROCmplete prints the source, resolved file, exact size and
hash, destination, and license state. Remote imports deliberately use
`NOASSERTION`: provider metadata is recorded as context, but ROCmplete does not
claim that it establishes rights to the hosted bytes. Interactive use asks for
the normal license-risk acknowledgment. Scripts need
`--non-interactive --acknowledge-license-risk`. Export `HF_TOKEN` or
`CIVITAI_TOKEN` for private, gated, or authenticated sources.

For an interactive download, that acknowledgment is the final confirmation
after the full size, destination, disk-space, and license summary. ROCmplete
saves the reusable pack only after it is accepted, immediately before the
normal verified installation starts. Declining leaves no generated pack and
downloads nothing.

The generated definition is saved by default under the ignored directory:

```text
local-content/imports/
```

Use `--save-pack PATH` to choose another JSON path. Reinstall it later without
contacting the metadata API:

```bash
./rocmplete content install \
  --from-file local-content/imports/IMPORT.json
```

The model bytes still come from the pinned provider and undergo normal
resumable download, exact verification, collision checks, and atomic
installation. A dry run resolves and validates everything but saves no pack,
downloads no bytes, and creates no persistent data:

```bash
./rocmplete content import URL --dry-run
```

This deliberately narrow command does not import Civitai ZIP members, whole
Diffusers/native repository trees, custom nodes, workflow dependencies, or
arbitrary URLs. It also does not create llama.cpp presets or router entries.
Use a reviewed local content pack or a public catalog change when one of those
needs several coordinated files or executable integration.

## ComfyUI content

The curated catalog includes practical variants for:

- Qwen Image 2512 and Qwen Image Edit 2511 in FP8/BF16, with base and
  four-step Lightning workflows;
- Wan 2.2 T2V/I2V in FP8/FP16, with base and Lightning variants;
- LTX-2 T2V/I2V full and distilled variants;
- HunyuanVideo 1.5 T2V/I2V workflows; and
- optional exact LTX-2 camera-control LoRAs.

Examples:

```bash
./rocmplete content install qwen-image-2512-bf16-lightning
./rocmplete content install wan-2.2-i2v-14b-fp16-base \
  --acknowledge-license-risk
./rocmplete content install ltx-2-t2v-19b-bf16-full \
  --accept-license --acknowledge-license-risk
./rocmplete content install hunyuan-video-1.5-t2v-720p-fp16 \
  --accept-license --acknowledge-license-risk
```

Official LTX camera LoRAs are separate alternatives:

```bash
./rocmplete content install ltx-2-camera-dolly-left --accept-license
```

Select one camera LoRA in the bypassed camera loader of a compatible
full-model workflow; do not stack the entire camera set.

Third-party Civitai content belongs in a user-owned local content pack.
Civitai permits files to be replaced behind an unchanged model-version ID, so
ROCmplete does not include those mutable files in its built-in complete
install. `content import` creates a reviewed local pack for a direct model
file and immediately subjects it to the normal size and SHA-256 checks.

`krea-2-turbo-fp8-base` installs the official pinned Krea model stack from an
immutable Hugging Face revision after presenting its terms:

```bash
./rocmplete content install krea-2-turbo-fp8-base --accept-license
```

Curated workflows use pinned licensed sources and deterministic renderers:

```text
apps/comfyui/user/default/workflows/curated/
```

Exact third-party workflows are preserved under:

```text
apps/comfyui/user/default/workflows/imported/
```

These names describe ROCmplete's processing level, not who initiated the
installation. Workflows elsewhere in the ComfyUI user tree remain user-owned.
Advanced workflow-only inspection and repair is available through:

```bash
./rocmplete content workflows status
```
