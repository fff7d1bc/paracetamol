# Content

[Documentation index](../README.md)

Paracetamol treats application images and model content as two different things.
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
./paracetamol content list
```

Recipes are organized by their consuming application:

```text
comfyui
  image  edit  t2v  i2v
llama-cpp
  qwen3.8  qwen3.8-flash-next
```

Install interactively, or select one recipe explicitly:

```bash
./paracetamol content install
./paracetamol content install comfyui image
./paracetamol content install llama-cpp qwen3.8
./paracetamol content install llama-cpp qwen3.8-flash-next \
  --accept-license --acknowledge-license-risk
```

Before a large install, choose the [data location](operations.md#persistent-data)
and inspect the same selection with `--dry-run`. The plan includes all required
companions, sizes and license gates, not only the main weight file.
For current model choices, recipe contents, optional variants and hardware
limits, use [choosing a managed Qwen preset](applications.md#choosing-a-managed-qwen-preset).
The [upgrade notes](upgrade-notes.md) cover retired models and changed companions.

After installing new gateway models or required companions, restart the gateway
to refresh its frozen inventory, then [refresh native client configuration](remote-maki.md#refreshes-and-alternative-addresses).

Catalog pruning does not delete GGUFs installed by an older checkout. Retired
files remain visible as local model rows in `content list models`; remove
their exact files deliberately only when they are no longer needed.

Managed text-model paths include the GGUF publisher in both directory and
filename. A catalog rename changes the expected destination without deleting
the old file. Use a separate local mirror to reuse verified bytes when moving
between destination layouts.

If a recipe is not specific enough, choose the exact-bundle browser in the
guided installer. It narrows the catalog by application and task before
showing individual bundles. Menus use shorter local names where the context is
obvious, and the final plan always prints the complete bundle
identifier.

Use an explicit application aggregate when you want every managed bundle for
one application rather than one practical recipe. This stays out of the guided
menu because it can be hundreds of gigabytes:

```bash
./paracetamol content install llama-cpp all --dry-run
./paracetamol content install llama-cpp all
```

The command uses the normal resumable installer. Existing verified models are
reported as ready, and only missing files are downloaded. A file from an older
Paracetamol checkout may be shown as `verify` the first time. The installer
hashes it in place instead of downloading it again. Model terms and
unverified-license acknowledgments are collected once for the complete plan.

Use the advanced inventory directly when a recipe is insufficient:

```bash
./paracetamol content list bundles
./paracetamol content list bundles --application comfyui
./paracetamol content list families
./paracetamol content install qwen-image-2512-bf16-base --dry-run
```

The `family qwen` and `family wan` targets select those model families
within ComfyUI. The literal `all` target selects the entire catalog and can
require around a tebibyte plus resumable cache space. Inspect it first:

```bash
./paracetamol content install all --dry-run
./paracetamol content install all \
  --accept-license --acknowledge-license-risk
```

The guided menu does not offer families, application-wide `all`, or the
global `all`. Those choices are too large to select accidentally.

## Terms and verification

Paracetamol shows additional model terms and separately identifies content whose
license metadata could not be verified. On a terminal, omitted approval flags
become confirmation questions. Noninteractive use requires the applicable
flags:

```bash
./paracetamol content install BUNDLE \
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
Civitai import or installing a user-owned pack. Paracetamol passes tokens to the
short-lived download container by environment variable name and never writes
their values into command arguments, generated content packs, images, or
persistent data.

### What happens during a download

If a download is interrupted, run the same command again. Completed files are
reused, and the active file resumes or restarts safely.

While network transfer is active, the installer reports the staged byte count;
the percentage is exact for fixed-size files and a hard byte limit is shown for
source archives whose final compressed size is not pinned. SHA-256 checks show
separate exact-byte verification progress for local-mirror candidates, existing
managed files, and newly staged payloads. A terminal updates one line, while
redirected output emits periodic newline-delimited milestones.

Direct HTTPS downloads enforce the pinned byte count while streaming, also
when resuming a partial file; oversized responses are rejected before bytes
beyond that limit can reach staging.
Build the current helper image with `./paracetamol build content-tools` after
updating. An older content-tools image does not satisfy the new image revision.
Interrupted archive transfers restart on retry. Complete staged ZIPs are
reused, while completed archives that fail member validation stay available
for inspection rather than being downloaded again automatically.

Content installation and data, download or runtime-cache cleanup share one
lock per data directory. A competing command fails immediately with a clear
message instead of deleting or sharing an active installation's staging,
verification, or final moves. The lock is released on
normal completion, failure, Ctrl-C, and process exit; resumable staging remains
in place. The lock belongs to the actual data directory, so directory aliases
and different home, runtime, or temporary-directory settings do not bypass it.
Locks for different data directories are independent. The separate downloader
guard still blocks installation while any managed downloader container remains,
even for another data directory. Wait for active downloads to finish. Remove a
leftover container only after confirming it is stale.

Installation begins only after exact size and SHA-256 verification.
Paracetamol records a durable receipt after a successful hash. A missing or stale
receipt makes the file `unverified`, not ready. The receipt avoids repeatedly
hashing large unchanged files; it is not a signature or a substitute for the
catalog's pinned hash.

This creates a one-time migration after upgrading an existing data directory.
Run the same `content install` command you originally used. It hashes
same-sized existing files, records successful results, refuses mismatches, and
does not redownload or replace the files. A dry run reports the bytes under
`Verify` but neither hashes files nor writes receipts.

The selected data directory may be an alias: managed-content commands resolve
it to one physical root without creating missing directories. Below that root,
managed files and their parent directories must stay in their assigned storage
partition without symlinks, even when a link points to another managed
directory. Planning, verification and runtime readiness refuse linked parents;
an older receipt does not make such a path usable. Keep the real files in the
normal managed layout, or reuse a separate library through a local mirror.
Destructive cleanup still refuses a data directory selected through any
symlink. Select its physical path explicitly.

Ctrl-C kills the downloader, waits until its exact container is gone, and keeps
partial data for the next attempt.

If a host crash or older launcher leaves a `paracetamol-download-*` container,
the next installation refuses to access staging and prints its exact name,
including when it would only reuse staged files or a local mirror. Real
installs require successful Podman inspection even without network transfer.
Confirm that it is stale, remove only that named container with the printed
`podman rm --force NAME` command, and retry; staging remains available for
resume.

Inspect installed state:

```bash
./paracetamol content status comfyui image
./paracetamol content status family qwen --details
./paracetamol content status comfyui image --verify
```

Normal `content status` uses the durable receipts. `--verify` is a read-only
audit that hashes every installed byte again and does not update receipts.
Running `content install` is the way to migrate or refresh stale receipts.

`content status` answers whether a catalog recipe is complete. For the more
practical question of what can be run, list the managed llama.cpp choices
alongside local llama.cpp GGUFs found on disk:

```bash
./paracetamol content list models
./paracetamol content list models --application llama-cpp
./paracetamol content list models --details
```

Every managed model is shown even when it is not installed. Missing rows use
the expected catalog size so the inventory is useful before a download. A
llama.cpp managed row is named by its preset. Imported and manually copied GGUFs
below the llama.cpp model root are shown as local files, and split GGUFs are grouped into
one model instead of one row per shard. An incomplete split set or a catalog
size mismatch is called out rather than presented as runnable.
Aliases sharing one target and bundle share a row. Variants with different
bundles have separate readiness and size, so an absent optional draft does
not make the plain model look incomplete. Drafts and vision projectors count
toward their variant's size and readiness, not its target shard count.

Use `--details` when choosing between managed llama.cpp presets. It shows the
exact bundle and preset IDs, catalog file count and total size, conservative
starting context, chat-template policy, MTP setup, profile-specific Flash
Attention policy, and copyable install and run commands. These are
properties Paracetamol needs to launch a model correctly, not task descriptions
or claims about which model is best.

Paracetamol does not search the rest of the machine by default. Add known model
locations explicitly; `--scan` is repeatable and accepts one GGUF or a
directory:

```bash
./paracetamol content list models \
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
```

## Local mirror migration

Moving from an older Paracetamol data directory or another machine? A local
mirror can save the download without trusting the old directory layout.

```bash
./paracetamol content install all \
  --local-mirror /path/to/old-paracetamol \
  --accept-license --acknowledge-license-risk
```

Paracetamol searches relevant filenames, rejects wrong sizes, and hashes every
candidate before reuse. Copying is the default. On filesystems without
reflinks, exact validated matches can be moved:

```bash
./paracetamol content install all \
  --local-mirror /path/to/old-paracetamol \
  --local-mirror-move \
  --accept-license --acknowledge-license-risk
```

Mirror copies publish the staged payload only after the copy completes. An
interrupted copy can be retried without removing partial files manually;
the source is preserved in copy mode. Linked or unexpected staging files are
refused instead of overwritten.

Move mode removes only catalog-hash matches, but may leave the old tree's
managed symlinks broken. Keep the active data directory outside the mirror,
verify the new installation, then remove the old directory. Planning and dry
runs require an existing mirror directory and reject overlap with the active
data directory after resolving symlinks. They do not index or hash mirror
candidates, so the download total is worst-case. Installation rechecks the
mirror root before indexing and verifying candidates.

For an in-place catalog destination rename, move only the exact managed GGUFs
to a separate mirror. Leave application state, staging, and local experiments
in the active data directory. Reinstall the affected llama.cpp
bundles from that mirror, check that they are ready, and remove only empty
retired model directories afterward.

## Local content packs

Use a local content pack when a private or machine-specific selection needs
several pinned files or needs to be installed again later. Packs live in
ignored JSON files:

```bash
./paracetamol content install \
  --from-file local-content/models.json \
  --dry-run

HF_TOKEN=... ./paracetamol content install \
  --from-file local-content/models.json
```

`--from-file` is repeatable and cannot accompany a positional target. Every
bundle from the supplied packs is selected. Packs temporarily extend the
catalog, but each bundle may reference only artifacts defined in its own pack.
Existing identifiers cannot be overridden. Within each target, artifact
destinations must be unique across the built-in catalog and all supplied
packs, even for identical bytes. One artifact's destination cannot be a parent
directory of another artifact's destination. Packs use the same revision,
hash, path, license, staging, and verification checks as built-in content.
Tokens do not belong in JSON.

See [Local content packs](../content-catalog.md#local-content-packs) for
the schema and maintainer detail.

## Import one remote file

Use remote import for one model or workflow that does not belong in the public
catalog. It resolves provider metadata into a small local content pack and
then hands installation back to the normal verified downloader.

```bash
./paracetamol content import

./paracetamol content import \
  'https://huggingface.co/OWNER/REPOSITORY/blob/main/MODEL.gguf'

./paracetamol content import \
  'https://civitai.com/models/MODEL_ID?modelVersionId=VERSION_ID'
```

Both `civitai.com` and `civitai.red` are accepted. Hugging Face imports accept
a model repository page or one `blob`/`resolve` file URL. Paracetamol resolves
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
./paracetamol content import URL \
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

Before downloading, Paracetamol prints the source, resolved file, exact size and
hash, destination, and license state. Remote imports deliberately use
`NOASSERTION`: provider metadata is recorded as context, but Paracetamol does not
claim that it establishes rights to the hosted bytes. Interactive use asks for
the normal license-risk acknowledgment. Scripts need
`--non-interactive --acknowledge-license-risk`. Export `HF_TOKEN` or
`CIVITAI_TOKEN` for private, gated, or authenticated sources.

For an interactive download, that acknowledgment is the final confirmation
after the full size, destination, disk-space, and license summary. Paracetamol
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
./paracetamol content install \
  --from-file local-content/imports/IMPORT.json
```

The model bytes still come from the pinned provider and undergo normal
resumable download, exact verification, collision checks, and atomic
installation. A dry run resolves and validates everything but saves no pack,
downloads no bytes, and creates no persistent data:

```bash
./paracetamol content import URL --dry-run
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
./paracetamol content install qwen-image-2512-bf16-lightning
./paracetamol content install wan-2.2-i2v-14b-fp16-base \
  --acknowledge-license-risk
./paracetamol content install ltx-2-t2v-19b-bf16-full \
  --accept-license --acknowledge-license-risk
./paracetamol content install hunyuan-video-1.5-t2v-720p-fp16 \
  --accept-license --acknowledge-license-risk
```

Official LTX camera LoRAs are separate alternatives:

```bash
./paracetamol content install ltx-2-camera-dolly-left --accept-license
```

Select one camera LoRA in the bypassed camera loader of a compatible
full-model workflow; do not stack the entire camera set.

Third-party Civitai content belongs in a user-owned local content pack.
Civitai permits files to be replaced behind an unchanged model-version ID, so
Paracetamol does not include those mutable files in its built-in complete
install. `content import` creates a reviewed local pack for a direct model
file and immediately subjects it to the normal size and SHA-256 checks.

`krea-2-turbo-fp8-base` installs the official pinned Krea model stack from an
immutable Hugging Face revision after presenting its terms:

```bash
./paracetamol content install krea-2-turbo-fp8-base --accept-license
```

Curated workflows use pinned licensed sources and deterministic renderers:

```text
apps/comfyui/user/default/workflows/curated/
```

`content install` preflights selected workflows before approval or model
installation. It checks destination paths and conflicts, and verifies source
resources for workflows that need installing. Differing workflows require
explicit replacement with `--force-workflow`. A dry run never writes workflows
or model content.

Exact third-party workflows are preserved under:

```text
apps/comfyui/user/default/workflows/imported/
```

These names describe Paracetamol's processing level, not who initiated the
installation. Workflows elsewhere in the ComfyUI user tree remain user-owned.
Advanced workflow-only inspection and repair is available through:

```bash
./paracetamol content workflows status
```
