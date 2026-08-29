# Models, bundles, workflows, and benchmarks

This guide covers content that is installed after an image is built. Read
`catalog/README.md` first for the current catalog schema policy.

## Find candidate content

Start from canonical public sources:

- the model author's or organization's official Hugging Face collection;
- the application or model's official repository and documentation;
- Comfy Org's official workflow template repository for ComfyUI graphs;
- an acceleration project's canonical repository for LoRAs or distilled
  weights.

Treat third-party indexes only as leads for finding canonical sources. Do not
copy their scripts, workflows, documentation, sample media, benchmark results,
or inferred license claims.

For every candidate answer these questions before editing the catalog:

1. What exact user-facing capability does it add?
2. Which application consumes it?
3. Is it a direct ComfyUI file or a native repository tree?
4. What is the canonical repository and full 40-character revision?
5. Which files are strictly required for inference?
6. What license applies to the exact hosted artifact or conversion?
7. Are there separate model terms the user must accept?
8. Does it duplicate an existing content-addressed file?
9. Is there a licensed workflow source, or would a new graph have unclear
   provenance?
10. Can it be tested on all supported GPU classes?

If provenance, compatibility, or value is unclear, leave it out until it can
be answered.

## Obtain immutable Hugging Face metadata

Query Hugging Face's official API or use its official client for discovery.
Store the returned full commit, then repeat all file inspection at that
immutable revision. Record the LFS object size and SHA-256 for every selected
file and independently verify downloaded bytes before editing the catalog.

The API distinguishes the Git blob ID from the Git LFS SHA-256. For an LFS
file, the LFS object ID is the content digest. Never use the Git blob ID as the
catalog SHA-256. If no content digest is available, download the exact file at
the pinned revision and compute:

```bash
sha256sum DOWNLOADED_FILE
stat --format '%s' DOWNLOADED_FILE
```

For gated or private repositories, export `HF_TOKEN` and ensure the account
has accepted any upstream terms. Never write tokens into the catalog, image,
command output, or documentation.

Metadata is necessary but not sufficient. Read the model card, repository
license file, upstream lineage, and conversion notes at the exact revision.

Four read-only Go probes support catalog research without expanding the public
human-oriented CLI. They print structured JSON and never modify the catalog or
persistent data:

```bash
go run ./tools/huggingface-probe repository OWNER/REPOSITORY
go run ./tools/huggingface-probe revision OWNER/REPOSITORY FULL_REVISION
go run ./tools/huggingface-probe file OWNER/REPOSITORY FULL_REVISION PATH
go run ./tools/civitai-probe version MODEL_VERSION_ID
go run ./tools/archive-probe --member EXACT/MEMBER archive.zip
go run ./tools/workflow-probe workflow.json
```

Set `HF_TOKEN` or `CIVITAI_TOKEN` only when the provider requires it. The
provider probes attach a token to the intended API host; their unit tests use
substituted HTTP clients. Probe output is research input, not catalog truth:
review licensing and independently verify downloaded bytes before pinning.

## License and agreement decisions

A verified license entry requires:

```json
{
  "spdx": "Apache-2.0",
  "status": "verified",
  "url": "https://..."
}
```

Use `verified` only when the selected repository/revision clearly declares a
license covering the hosted files.

If the hosted repack or conversion has no clear license, use:

```json
{
  "spdx": "NOASSERTION",
  "status": "unverified",
  "url": "https://...",
  "warning": "A specific factual warning.",
  "upstream_repository": "Canonical/base repository",
  "upstream_license": "The upstream license only",
  "upstream_license_url": "https://..."
}
```

Upstream lineage is context, not a license grant for the conversion. Do not
promote an upstream license to the hosted artifact.

Create an `agreements` entry when users must review non-permissive model terms.
Attach its identifier to every affected artifact. Agreement
acceptance and unverified-license acknowledgment are independent controls:

- `--accept-license` confirms listed model terms;
- `--acknowledge-license-risk` permits a download with unresolved artifact
  licensing.

When either approval is required and stdin is a terminal, omitting its flag
must present a separate fail-closed confirmation question. Noninteractive
callers should pass `--non-interactive` plus every applicable approval flag;
missing approval must fail before any download. Keep this behavior identical
for guided selectors, explicit selectors, local content packs, and benchmark
commands.

Update `THIRD_PARTY_NOTICES.md` and `catalog/README.md` whenever a new family,
license, lineage, or risk class is introduced.

## Add a direct artifact

ComfyUI artifacts install below
`content/comfyui/models/<destination>`. Add one object to
`artifacts` in `catalog/catalog.json` containing:

- a stable lowercase identifier;
- a description;
- repository, full revision, and safe relative source path;
- a unique safe destination relative to `models/`;
- exact positive byte size;
- lowercase 64-character SHA-256;
- license record;
- agreement IDs when applicable.

Choose the ComfyUI destination category intentionally, such as
`diffusion_models/`, `text_encoders/`, `vae/`, `loras/`, or
`latent_upscale_models/`. It must agree with loader nodes and
`applications/comfyui/extra-model-paths.yaml`.

If a new revision changes bytes while keeping the same destination, existing
installations will report a size or hash mismatch and Paracetamol will refuse to
overwrite them. Prefer a versioned upstream filename when practical. Otherwise
document the explicit user migration: move the old file aside, run
`content install TARGET`, and remove the backup only after validation.

Treat an upstream model update as an artifact review even when the repository,
model name, and advertised release name do not change. Resolve the current
upstream state to a new full commit, then compare the pinned and candidate file
inventories, paths, sizes, hashes, licenses, and model cards. For GGUF files,
also compare metadata, tensor names and shapes, tensor data offsets, and tensor
payload bytes. Classify the result before changing the catalog:

- documentation-only changes do not require a new artifact pin;
- a metadata-only repack is useful only when the changed metadata affects the
  managed runtime and is not already supplied by an exact project override;
- changed tensor payload, quantization, tokenizer data, or architecture
  metadata is a new inference candidate and requires the applicable quality,
  protocol, context, backend, memory, and performance acceptance; and
- target, speculative draft, projector, tokenizer, and template files form a
  reviewed companion tuple. Do not update one member merely because its
  filename looks compatible with the others.

Keep the public preset identifier stable when the accepted model policy has
not changed. Artifact identity remains the full revision, source path, size,
and SHA-256, not the preset ID or upstream display name. Prefer a new
destination for changed bytes so the old and candidate files can coexist
during validation. If the upstream canonical filename is reused, retain the
installer's fail-closed collision behavior and document the explicit migration
instead of overwriting managed content. Never follow a floating branch such as
`main` at install or runtime.

Runtime readiness also requires a current entry in
`content/.paracetamol/verification.json`. The installer creates that receipt only
after preparing the shared runtime SELinux label, where applicable, and hashing
the complete file. Labeling before hashing prevents the first container mount
from invalidating the receipt through a ctime-only change. The receipt's
filesystem identity fields still invalidate it after replacement or mutation,
including a same-size change. Existing same-sized content without a receipt is
prepared, hashed in place, and recorded by `content install`; dry runs report
this as verification work without reading or writing the file.

If the artifact filename appears in curated workflow metadata, update
`_MODEL_SOURCES` and, if necessary, `_MODEL_ALIASES` in
`internal/cli/commands_content.go`.

Managed GGUF files use `"target": "llama-models"` and a destination relative
to `content/llama-cpp/models`. Add a bundle using application `llama-cpp` and
groups `all` and `llama`, plus the intended public selection group, then
connect it to a stable router identity:

```json
"llama_presets": {
  "model-id": {
    "bundle": "llama-bundle-id",
    "artifact": "llama-artifact-id",
    "backends": ["vulkan"],
    "default_context": 4096,
    "speculative_type": "draft-mtp",
    "draft_tokens": 4,
    "draft_tokens_by_backend": {
      "vulkan": 3
    },
    "draft_artifact": "optional-draft-artifact-id",
    "context_override_architectures": ["target-architecture"],
    "jinja": true,
    "agent_tools": true,
    "reasoning_control": "effort",
    "reasoning_levels": ["low", "medium", "xhigh"],
    "reasoning_default": "medium",
    "reasoning_off": true,
    "reasoning_preserve": true,
    "flash_attention": {
      "strix-halo": "on",
      "strix-point": "off"
    },
    "kv_cache": {
      "strix-halo": "q8_0"
    },
    "model_load": {
      "strix-halo": "mmap-lazy-token-embedding"
    }
  }
}
```

DwarfStar model files use `"target": "dwarfstar-models"`, application
`dwarfstar`, and groups `all` and `dwarfstar`. They deliberately do not become
llama.cpp presets. Give the reviewed serving identity its own validated policy:

```json
"dwarfstar_presets": {
  "deepseek-v4-flash-0731-q2-imatrix": {
    "bundle": "dwarfstar-target-bundle",
    "dspark_bundle": "dwarfstar-target-and-support-bundle",
    "default_context": 131072,
    "max_output_tokens": 16000,
    "agent_tools": true,
    "reasoning_control": "effort",
    "reasoning_levels": ["high"],
    "reasoning_default": "high",
    "reasoning_off": true
  }
}
```

The public DwarfStar recipe and runtime own one reviewed target-model identity.
The gateway exposes only its non-DSpark bundle. The optional
DSpark bundle contains that target first and its exact support GGUF second;
the runtime accepts the pair only through `--dspark` and never treats the
support file as another user-selectable model.

`default_context` is Paracetamol's reviewed starting context for the preset. It
may be the model's native context for agent-focused models or a smaller
operational default for models aimed at constrained hardware or bounded tasks.
A user can override it for one launch with `--context`; the preset keeps the
repeatable, tested starting point.

The referenced artifact must belong to that bundle and end in `.gguf`.
`backends` is an optional closed list containing `rocm`, `vulkan`, or both.
Omitting it supports both backends. Add a restriction only when target-hardware
acceptance establishes that another compiled backend is unsafe for that exact
preset. Direct startup may select the first declared backend only when the
caller omitted `--backend`; an explicit incompatible choice fails. Managed
routers and gateways omit incompatible presets from their frozen inventory.
Throughput and coding-agent benchmarks enforce the same boundary. Validate
coherent multi-turn output and tool calls because startup, prompt processing,
or a short retrieval key alone cannot establish backend compatibility.

`speculative_type` is optional and accepts only `draft-mtp` or
`draft-dflash`. The matching positive `draft_tokens` value is limited to
eight for MTP and fifteen for DFlash. An optional `draft_artifact` must be a
different `.gguf` artifact in the same bundle. Omit it only when an MTP target
GGUF contains its own prediction heads; DFlash requires the separate draft
artifact. These fields intentionally do not accept arbitrary llama.cpp
arguments.

`draft_tokens_by_backend` is an optional closed `rocm`/`vulkan` map that
overrides the default draft depth only for a measured backend. Use it when
the same target and speculative decoder have materially different optima on
otherwise identical hardware, context, and workload. The host resolves the
effective value consistently for direct and router startup. Do not use GPU
profiles as backend aliases or infer one backend's optimum from another's
acceptance ratio.

`context_override_architectures` is an optional built-in-catalog list of exact
GGUF architecture prefixes. When present, Paracetamol sets each architecture's
`context_length` metadata to the selected launch context and disables
llama.cpp automatic fitting. This is a narrow mechanism for reviewed model
releases whose advertised extended window exceeds their GGUF metadata; it is
not a general metadata-override surface. Keep the ordinary metadata-backed
preset as a control and require long-context hardware acceptance before making
an override the recipe default. `--context 0` is rejected for such a preset
because it would silently discard the managed override.

`jinja` is an optional boolean that enables llama.cpp's Jinja chat-template
engine for a preset. `chat_template` selects one project-bundled template from
the loader's small allowlist. Add the template to the llama.cpp image, the
Containerfile context, the entrypoint validation, and router rendering
together. Pin and record an upstream template's revision, license, and exact
hash; do not assume unchanged GGUF bytes acquire a later tokenizer template.
A managed template enables Jinja itself, so do not also set `jinja`.
`flash_attention` is an optional object whose keys are
the concrete hardware profiles `rdna4`, `strix-halo`, or `strix-point` and
whose values are `on`, `off`, or `auto`. The container resolves `auto`
hardware detection before applying this policy. These narrow fields must be
rendered for both single-model and router startup; do not replace them with a
generic arguments list.

`kv_cache` is an optional profile map for a reviewed symmetric target K/V
cache type. Its values are `f16`, `q8_0`, or `q4_0`. A quantized value requires
the same profile to declare `flash_attention: "on"`; the loader rejects a
configuration that would leave llama.cpp's required kernel choice ambiguous.
This policy does not change the separate speculative draft cache. Add a
profile only after long-context throughput and retrieval acceptance on that
hardware class. Omitted profiles retain llama.cpp's default rather than
inheriting a nearby architecture's result.

`model_load` is an optional profile map for a model whose measured capacity
path cannot use the normal resident policy. It currently accepts only
`strix-halo` or `strix-point`, with `resident` or
`mmap-lazy-token-embedding`. The latter
maps to llama.cpp mmap, lazy tensor reads, and CPU placement of the exact
`per_layer_token_embd.weight` tensor. It is intentionally not a generic
tensor-override surface. Add it only after cold and warm startup, long-context
retrieval, output coherence, host-memory headroom, and kernel-journal checks
on the named hardware profile. Omitted Strix profiles retain resident loading.

`agent_tools` is an optional, explicit compatibility decision. Set it only
when the model, pinned GGUF template, and llama.cpp policy are maintained for
Paracetamol's reviewed Chat Completions function-tool contract. It requires
`jinja: true` and a `default_context` of at least 16384. Do not use it as an
installation selector or a statement about general model quality. After
changing preset IDs, contexts, templates, or this flag, regenerate every
maintained agent-client configuration and accept a complete function-tool
round trip through at least one maintained client on target hardware. Record
which client supplied that evidence and which remain provisional. Before a
newly exposed model receives unattended write access through another client,
repeat the end-to-end loop there as well. Sibling presets may share protocol
evidence when they use the byte-identical target and chat template; keep
unaccepted context or speculative-decoding behavior explicitly experimental.

`reasoning_control` is an optional, narrower compatibility decision and
requires `agent_tools`. `toggle` represents a native off/on model and must
default to `on` without declaring named levels. `effort` and `strength`
represent distinct model-native parameters; they require an ordered subset of
low, medium, high, and xhigh in `reasoning_levels`, and
`reasoning_default` must select one of those real levels. `reasoning_off`
records whether clients may advertise off/instant. Do not insert a generic
level that the template merely aliases to another value: Qwen3.8, for example,
has no native high effort. Keep catalog policy, generated client metadata,
the server compatibility bridge, and target-hardware tests in sync.

`reasoning_preserve` is a separate optional boolean and also requires
`agent_tools`. It maps a reviewed preset to llama.cpp's
`--reasoning-preserve` server policy in both direct and router modes. Use it
when the model's multi-turn template should retain parsed reasoning; it does
not imply that the model accepts a client-selectable reasoning control.
Keep this distinction visible in agent metadata and user documentation.

Agent sampling is normally caller policy. `internal/agent/models.go` owns
the reviewed coding defaults used by generated clients and evaluation
metadata. A model whose authoritative sampling changes with a server-resolved
control instead references one entry from the catalog's closed
`llama_sampling_policies` collection through `sampling_policy`. Direct startup
and router rendering serialize the selected policy into llama.cpp's dedicated
`--sampling-defaults-by-reasoning` option, and generated clients omit fields
that would mask it. The server selects `thinking` or `non_thinking` after
resolving the request and fills only omitted or null fields. Qwen3.6 uses
separate dense and sparse policies while its MTP and non-MTP variants share
their family policy; Qwen3.8 uses one 27B policy. Adding `agent_tools` still
requires an authoritative upstream sampling audit and an explicit policy
entry. Do not encode sampling as a bundle variant or Jinja side effect.

For a split GGUF, put every shard in the same bundle and reference the first
`00001-of-N` shard from the preset. Preset inspection validates the complete
bundle before llama.cpp starts, while llama.cpp discovers the remaining
same-directory shards.
Choose the context that matches the preset's maintained workload. Native
long-context defaults are appropriate for models explicitly aimed at
high-memory agent work; bounded-task and constrained-hardware presets may use
less. Document useful smaller overrides because llama.cpp allocates the cache
at startup. Preset identifiers become public OpenAI API model names, so keep
them stable and descriptive. Router startup includes
installed presets, ignores wholly missing presets, and refuses partial
managed installs.

The public llama.cpp recipes are the paired Qwen3.6 dense 27B and sparse
35B-A3B MTP selection, the separate single-artifact Qwen3.8 dense target with
optional embedded-MTP runtime, KAT-Coder, Muse Glimmer's one Dynamic
target/draft pair, high-memory Japanese and English Shisa V2.1, and the
focused HY and Gemma translation families.
Muse's forced-256K DFlash preset is the recipe launch; its 128K base and DFlash
controls share the installed pair. Qwen3.8 MTP at medium is the managed-client
default and remains its own family recipe even though both Qwen releases serve
similar work. Unrelated models retain separate family recipes instead of being
grouped under a subjective coding role.
The Qwen3.6 recipe still launches dense 27B, and neither Qwen3.6 model changes
the Qwen3.8 managed-client default. Non-MTP Qwen controls, the smoke-test
model, and other deliberately large models remain exact bundles and presets
rather than multiplying beginner choices.
The mandatory internal `all` and `llama` tags still support the literal global
aggregate and application ownership.

Keep model role, quantization, and hardware profile separate. A content model
may be useful on more than one GPU class, so do not create a hardware-named
selector merely because one machine has enough memory for it.

## Add a bundle

A bundle connects content to a user-visible capability:

```json
{
  "description": "...",
  "application": "comfyui",
  "artifacts": ["..."],
  "groups": ["all", "comfyui", "family-name"],
  "workflow": "optional-workflow-id"
}
```

Rules enforced by the loader include:

- at least one artifact;
- no duplicate references;
- only known selector groups;
- mandatory membership in `all`;
- all referenced content and workflows must exist;
- a workflow-bearing bundle must have a benchmark entry.

Bundle variants should say what changes in the identifier and description:
family, model version, task, precision, and base/accelerated behavior. Reuse
shared artifact IDs rather than duplicating entries.

## Local content packs

A content pack extends the built-in catalog for one installation without
committing machine-specific or non-public metadata:

```bash
./paracetamol content install \
  --from-file local-content/base-models.json \
  --from-file local-content/loras.json \
  --dry-run
```

The repository-root `local-content/` directory is ignored by Git and is the
recommended location for packs and their companion manifests.

`content import URL` generates one of these packs for a single supported
Civitai or Hugging Face file. The resolver lives in
`internal/remoteimport/`; it is an authoring convenience, not a second
catalog or downloader. It must continue to emit schema-valid definitions and
then use the normal `content install --from-file` orchestration.

Remote import maintenance boundaries are intentionally strict:

- provider hosts are allowlisted to `civitai.com`, `civitai.red`, and
  `huggingface.co`;
- Hugging Face named revisions resolve to a full commit, and imported files
  require exact Git LFS size and SHA-256 metadata;
- Civitai sources require an exact model-version and a supported file with
  exact size, SHA-256, and a same-provider version download URL;
- ambiguous versions, files, and ComfyUI categories are selected explicitly;
- an inferred destination remains overridable in guided terminal use;
- generated licenses remain `NOASSERTION` and unverified;
- metadata tokens use non-forwarded authorization headers;
- dry run validates the generated pack and destination path without saving
  it or invoking a download.

The actual pack write is passed into the shared installer as its
`before_mutation` callback. This keeps the complete content plan and
license-risk acknowledgment ahead of the write while preserving the pack for
retry once a user accepts and downloading begins. Do not move that callback
earlier than approval or later than the first download mutation.

Keep `IMPORT_KINDS` aligned with `Artifact.target`, `artifact_path`, ComfyUI's
extra model paths, and the workflow destination policy. Adding an archive,
multi-file application recipe, custom node, or llama preset is a
catalog/integration feature rather than another import-kind entry.

`--from-file` is repeatable and cannot be combined with a positional target.
Every bundle declared by the supplied packs is selected.
Multiple packs are composed before validation, so a bundle may reference a
built-in artifact or a definition from another supplied pack.

Content-pack schema version 2 is deliberately smaller than the built-in
catalog schema:

```json
{
  "schema_version": 2,
  "agreements": {},
  "artifacts": {},
  "bundles": {
    "local-bootstrap": {
      "description": "Local bootstrap content",
      "application": "comfyui",
      "artifacts": ["qwen-image-vae"],
      "groups": ["all", "comfyui"]
    }
  }
}
```

Only `agreements`, `artifacts`, and `bundles` are supported; the first two may
be omitted when empty. Every pack must declare at least
one bundle. Artifact and bundle objects use the same fields and validation as
their built-in equivalents.

Artifacts default to the Hugging Face source shape documented above. A
Civitai artifact instead pins an exact model and version:

```json
{
  "description": "Pinned Civitai model",
  "source": {
    "provider": "civitai",
    "host": "civitai.red",
    "model_id": 123456,
    "model_version_id": 234567,
    "filename": "model.safetensors",
    "download_url": "https://civitai.red/api/download/models/234567?type=Model&format=SafeTensor",
    "requires_auth": true
  },
  "destination": "diffusion_models/model.safetensors",
  "size": 123456789,
  "sha256": "replace-with-64-lowercase-hex-digits",
  "license": {
    "spdx": "NOASSERTION",
    "status": "unverified",
    "url": "https://civitai.com/models/123456?modelVersionId=234567",
    "warning": "No SPDX license for the hosted bytes was verified.",
    "upstream_repository": "Civitai model 123456, version 234567",
    "upstream_license": "Civitai model-page permissions",
    "upstream_license_url": "https://civitai.com/models/123456?modelVersionId=234567"
  }
}
```

Keep Civitai artifacts in user-owned local content packs, not Paracetamol's
built-in catalog. A Civitai model-version ID identifies a listing version, but
does not make its hosted file immutable: a creator can replace that file while
the version ID and download URL stay unchanged. Exact size and SHA-256 checks
still prevent silent substitution, but a later install of an older pack will
then fail closed because the pinned bytes are no longer available. A built-in
catalog entry would turn that provider behavior into a broken complete
install for every new user.

`host` is optional and defaults to `civitai.com`; only the two allowlisted
Civitai hosts are accepted. `download_url` is also optional and defaults to
that host's model-version endpoint. When a version has several files, record
the exact provider-returned URL, including its format-selection query. The
loader requires the same host and exact `/api/download/models/VERSION` path;
the catalog hash remains the final identity check.

Set `requires_auth` when anonymous download returns an authorization error.
Paracetamol reads `CIVITAI_TOKEN` from the host environment, passes only the
environment-variable name through the Podman command, and never writes the
token into the pack, process arguments, or persistent data. Authentication is
sent only to Civitai's initial download endpoint; it is not forwarded to the
signed object-storage redirect. The downloader resumes exact direct files
with an HTTP Range request and safely restarts when the server does not honor
Range.

When a Civitai workflow is distributed only inside a ZIP, treat the ZIP as a
bounded transport and pin the member:

```json
"source": {
  "provider": "civitai",
  "model_id": 123456,
  "model_version_id": 234567,
  "filename": "workflow.zip",
  "requires_auth": true,
  "archive": {
    "member": "folder/workflow.json",
    "max_size": 33554432
  }
},
"size": 12345,
"sha256": "extracted-member-sha256"
```

Civitai can replace a ZIP behind an unchanged model-version ID. Paracetamol
therefore does not use the outer ZIP hash as the content identity. It starts
these small archive downloads from byte zero, stops at `max_size`, requires
exactly one regular member with the selected path, and verifies the member's
exact size and SHA-256 before atomic installation. A harmless ZIP repack keeps
working. A changed workflow still fails closed with an error that identifies
the member and keeps the archive for review. This transport support is for
reviewed user-owned packs; it does not make mutable ZIP members suitable for
the built-in catalog.

After downloading a candidate ZIP to a temporary location, inspect its central
directory without extracting it. Check member types, unsafe paths, duplicates,
compressed and uncompressed sizes, and the selected member's SHA-256. Use that
evidence to choose a conservative archive `max_size` and pin the selected
member. A selected member must occur exactly once. Symlinks and non-regular
entries are never followed. The archive hash is useful research evidence, but
it is not a durable identity for an extracted workflow.

When several required files are members of the same archive, declare an
`archive_collections` entry instead of repeating its source and archive
metadata in `artifacts`. The collection owns one `source`, `archive`,
`license`, optional `agreements`, default `target`, and a `members` object.
Each member still declares its own description, archive path, destination,
size, and SHA-256. The loader expands these into ordinary artifacts, so all
normal path, license, collision, and bundle validation remains authoritative.
The staging key is derived from the provider, version, and filename, so one
download can serve every selected member. Staging is pruned only after all
selected members are installed.

For Civitai, query the official API for the exact model version and file.
Record the immutable version ID, filename, reported size, and SHA-256, then
verify the downloaded bytes. If authentication is required, send
`CIVITAI_TOKEN` only as a bearer header and never print it.

A downloaded ComfyUI workflow can use the same artifact schema with:

```json
{
  "target": "workflows",
  "destination": "family/workflow.json"
}
```

Workflow targets must end in `.json` and install below
`apps/comfyui/user/default/workflows/imported/`, separate from the curated,
rendered workflows under `workflows/curated/`. This installs exact bytes only:
Paracetamol does not execute
the JSON, install its custom nodes or extra model dependencies, validate its
graph, create a benchmark, or claim that it is runnable.

Inspect downloaded UI-format or API-format graphs before deciding what can be
supported. Inventory registry and repository package declarations, recorded
versions or commits, active and bypassed node counts, model-like asset
references, core node types, and unattributed node types. Do this without
executing the graph or resolving its dependencies.

Composition is additive and fail-closed:

- a pack cannot override any built-in or earlier pack identifier;
- artifact destinations, hashes, sizes, references,
  licenses, agreements, applications, selector groups, and paths are checked
  across the complete composed catalog;
- workflow-pack definitions, benchmarks, renderer names, commands, and
  executable code are not accepted from content packs; exact workflow JSON
  may only use the constrained workflow artifact target described above;
- files are read in place and are not copied into the data directory,
  application image, or persistent container state;
- normal dry-run, agreement acceptance, unverified-license acknowledgment,
  disk-space, resumable staging, and SHA-256 verification behavior applies.

Private Hugging Face and authenticated Civitai downloads use environment
tokens:

```bash
HF_TOKEN=... CIVITAI_TOKEN=... ./paracetamol content install \
  --from-file local-content/models.json \
  --accept-license \
  --acknowledge-license-risk
```

Never store an access token in a content pack. Protect the file itself when
repository names, filenames, or license details are sensitive. A pack is
configuration, not a source of model bytes; supported sources remain pinned
Hugging Face repository paths at full commit revisions and pinned Civitai
model-version IDs.

## Reuse an existing local model library

`content install` can reuse exact bytes from an old Paracetamol directory or
another local model collection before falling back to the configured remote
source:

```bash
./paracetamol content install all \
  --local-mirror /mnt/old-paracetamol \
  --accept-license --acknowledge-license-risk
```

The mirror search is layout-independent: it recursively indexes regular files,
shortlists catalog hash names and model basenames, checks the expected size,
then computes SHA-256. A same-name file with different content is ignored.
Directory and file symlinks are ignored. Normal agreement and
unverified-license gates still apply.

The default copies matches. `--local-mirror-move` moves each verified source
into resumable staging instead. A same-filesystem move needs no second
full-sized copy, which is useful on ext4:

```bash
./paracetamol content install all \
  --local-mirror /mnt/old-paracetamol \
  --local-mirror-move \
  --accept-license --acknowledge-license-risk
```

Move mode is intentionally destructive to the old library. The active data
directory and mirror may not contain one another. An interrupted install
retains the new `staging/<application>/...`
file for resume; already moved files are not restored to the old tree.

Dry run validates that the mirror exists and is separate from the destination,
but does not hash it. Reported download size is therefore the network
worst-case. This keeps planning fast even for several hundred GiB.

## Add a content recipe

Public installation is application-first. Small runnable recipes live in
`internal/recipes/` and resolve directly to exact catalog bundle IDs.
The same definitions drive guided `content install`, `content list`, and the
application guides, so those interfaces cannot silently select different
content.

To expose a new recipe:

- add one `ContentRecipe` below the consuming application in
  `APPLICATION_RECIPES`;
- select the smallest exact bundle set that produces one useful outcome;
- describe its launch through one typed `RecipeLaunch`; Paracetamol derives the
  copyable next command printed after installation;
- update content help, the user README, the relevant application guide, and
  focused recipe-resolution tests.

Do not use a recipe as a model-family inventory. Quantization alternatives,
optional add-ons, imported workflow libraries, speculative-decoding
mechanisms, smoke-test roles, and size classes remain exact bundles. A recipe
that grows from one practical result into hundreds of GiB of choices has
stopped serving its purpose.

Catalog groups remain internal tags for ownership, the literal `all`
aggregate, and the deliberately explicit model-family selectors. Add a group
to `SELECTOR_GROUPS` and a `CONTENT_FAMILIES` entry only when users need one
named family across several bundles.
`content install APPLICATION all` derives its selection from each bundle's
application ownership instead of adding a recipe or selector group.
Application aggregates, families, and the literal global aggregate remain
absent from the guided installer.

The guided exact-bundle browser is categorized separately from recipes.
`categoryForExactBundle()` in `internal/cli/exact_bundles.go` maps every bundle to
exactly one presentation category using its owning application and existing
family groups. When adding a new application or ComfyUI family, update that
mapping and its exhaustive coverage test. Category-local display names may
remove redundant application prefixes, but catalog identifiers remain stable
and globally unique.

## Add or update a curated workflow

Paracetamol does not store editable UI-format workflows as hand-authored source.
It extracts an official workflow resource from the pinned package in the
ComfyUI image and transforms it deterministically.

### 1. Pin the source

Add a `workflows` entry containing:

- source package and installed package version;
- full upstream source revision;
- original resource path within the upstream package;
- source SHA-256;
- deterministic renderer name;
- destination;
- rendered SHA-256;
- license and pinned license URL.

For a new entry, a temporary 64-zero hash may be used only while calculating
the real value; never commit the placeholder.

Extract the exact upstream resource from the pinned ComfyUI image in a
network-free temporary container and calculate `sha256sum` over those bytes.
The package version, upstream revision, resource path, and source hash remain
provenance even though runtime installation uses a reviewed repository
resource.

### 2. Implement the renderer

Apply a narrowly scoped, reviewable transformation when producing the
repository resource:

- assert expected node types and counts;
- change exact model filenames and sampler values;
- remove unneeded LoRAs by safely rewiring graph links;
- clear sample image/video references;
- enable or disable stages explicitly;
- reject unexpected custom nodes;
- retain automatic provenance injection.

Retain provenance for every model filename left in workflow metadata. Write Go
tests around the exact stored resource and the properties that matter: model
selection, steps, CFG, LoRAs, input clearing, and graph links.

Do not make the transform tolerant of arbitrary upstream graph changes. A
shape mismatch should request maintainer review.

### 3. Calculate the rendered hash

Store the reviewed rendered bytes as
`catalog/workflows/<workflow-id>.json`, calculate `sha256sum`, and put that
value in `rendered_sha256`. The Go catalog tests require every workflow entry
to have one exact resource with the matching hash. Install into a disposable
data directory and visually inspect it before creating a benchmark.

## Add or update a benchmark graph

Every ComfyUI bundle with a workflow needs one API-format prompt under
`catalog/benchmarks/`.

1. Build the exact pinned ComfyUI image.
2. Install the bundle and curated workflow.
3. Start ComfyUI on loopback.
4. Open the curated installed workflow.
5. Export its API-format graph using the pinned frontend.
6. Save it as `catalog/benchmarks/<bundle-id>.json`.
7. Inspect every node and path; remove no nodes by hand unless the change is
   deliberately reflected in the curated workflow and renderer.
8. Confirm there is no sample media or external custom-node dependency.
9. Compute `sha256sum` and add the resource/hash to `benchmarks` in the
   catalog.

When one reviewed upstream graph is the exact structural basis for multiple
model variants, a benchmark entry may name a small allowlisted renderer and a
`rendered_sha256`. The source hash still pins the exported upstream graph; the
rendered hash pins the deterministic task/resolution/model transformation.
Add renderer names in `internal/benchmark/`, implement them fail-closed, and
test all changed node values. Do not use a renderer to
paper over a substantially different workflow.

`prepare_prompt()` supplies deterministic seeds, a generated 768×768 input
for `LoadImage`, and a unique output prefix. It rejects `LoadVideo`.

Run:

```bash
./paracetamol benchmark comfyui run BUNDLE --dry-run
```

Then run both persistent-cache and isolated-cache benchmarks on supported
hardware after installing the bundle. The first request always uses a fresh
ComfyUI process; `--cache-mode isolated` additionally starts with empty
generated compiler caches. Benchmark outputs and result JSON are local test
evidence, not catalog source material.

llama.cpp presets need no catalog benchmark graph. Run the pinned native
binary directly:

```bash
./paracetamol benchmark llama-cpp throughput --preset PRESET --dry-run
./paracetamol benchmark llama-cpp throughput --preset PRESET
```

Keep prompt tokens, generation tokens, repetitions, profile, render-node set,
image, and model preset identical when comparing results. Catalog preset
results retain the artifact revision, size, and SHA-256. Local-model results
cannot make that immutable claim and should not be used as release evidence
unless the exact file identity is recorded separately.

For family-wide validation:

```bash
./paracetamol benchmark comfyui suite --family FAMILY --dry-run
./paracetamol benchmark comfyui suite --family FAMILY --accept-license
```

The real suite requires every selected bundle to be installed and never
downloads content. Preserve its JSON file to resume the exact same selection
and runtime policy with `--resume`; start a new suite after changing a catalog
hash, image build, profile, seed, run count, or memory/kernel policy. The suite
records the image's immutable ID rather than trusting a mutable local tag.
Cache mode is also part of the suite signature.

## Validate a catalog change

Run the cheap validation first:

```bash
python3 -m json.tool catalog/catalog.json >/dev/null
go test ./internal/catalog ./internal/content ./internal/cli ./internal/benchmark
./paracetamol content list
./paracetamol content install NEW_BUNDLE --dry-run
```

For a selector:

```bash
./paracetamol content install SELECTOR --dry-run
./paracetamol content install all --dry-run
```

Then perform an actual download into a test data directory, followed by:

```bash
./paracetamol content status NEW_BUNDLE --verify \
  --data-dir /absolute/test/data
```

`--verify` reads every installed byte and can be expensive. It is a read-only
audit and does not refresh the durable receipt. The normal install command
records successful verification for later runtime gates.

For workflow bundles, also install, open, queue, and benchmark the workflow.
Test on all supported hardware profiles before calling the content supported.
