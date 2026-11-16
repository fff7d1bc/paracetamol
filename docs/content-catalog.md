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
3. Which direct files or bounded archive members are required?
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

Four read-only Go probes print structured research metadata without modifying
the catalog or persistent data. Run them from the checkout with Go state
anchored below the ignored `build/` tree:

```bash
probe_go() (
  export GOENV=off GOTOOLCHAIN=local GOTELEMETRY=off
  export GOCACHE="$PWD/build/probes/gocache"
  export GOMODCACHE="$PWD/build/probes/gomodcache"
  export GOPATH="$PWD/build/probes/gopath"
  export GOTMPDIR="$PWD/build/probes/tmp"
  export GOTELEMETRYDIR="$PWD/build/probes/telemetry"
  mkdir -p "$GOTMPDIR"
  go run "$@"
)
probe_go ./tools/huggingface-probe repository OWNER/REPOSITORY
probe_go ./tools/huggingface-probe revision OWNER/REPOSITORY FULL_REVISION
probe_go ./tools/huggingface-probe file OWNER/REPOSITORY FULL_REVISION PATH
probe_go ./tools/civitai-probe version MODEL_VERSION_ID
probe_go ./tools/archive-probe --member EXACT/MEMBER archive.zip
probe_go ./tools/workflow-probe workflow.json
```

The wrapper controls compiler/module state, not provider side effects.
Metadata calls need network access and are not part of offline Tier 1 checks.

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

Update `THIRD_PARTY_NOTICES.md` for new third-party families and provenance.
Update catalog policy documentation when the schema, license or risk handling
changes, without adding another per-model inventory to `catalog/README.md`.

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

Existing artifact inspection uses the same managed-parent validation as new
file promotion. CLI managed-content readers and writers resolve a selected
data-root alias once, including aliases above a missing suffix, without
creating directories. Cleanup retains its raw-path symlink refusal. Below the
resolved root, every existing parent through the artifact's containing directory
must be a directory, not a symlink, including links within the same target
partition. Reject invalid parents before consulting receipts or
hashing, and revalidate parents after hashing before recording a receipt.
Missing roots or parent tails remain valid read-only plans. Inspection does
not create them. This shared inspection rule covers install planning,
existing-file verification and runtime bundle readiness.

`internal/storage/` owns the shared nonblocking Linux `flock` on an open
data-root directory descriptor. The installer retains it across downloader
checks, staging, verification, receipt publication, and completed-staging
cleanup. Destructive data, download and runtime-cache cleanup acquires the
same lock before subprocess checks, planning or approval and retains it through
removal. Container checks alone cannot protect offline mirror moves or
existing-file verification. A busy cleanup fails without prompting or deleting
anything. Exclusion follows the directory inode, including installer aliases,
rather than a pathname or launcher environment; cleanup still refuses selected
symlink paths. Different real roots remain independent. The descriptor is
close-on-exec and released on every return or process exit; no persistent lock
file is created. Read-only planning, dry runs, and rejected install plans do
not create the root or acquire the lock. Cleanup of an absent root also creates
no state. Caller-owned `BeforeMutation` metadata publication still precedes
installer root preparation and lock acquisition, so a failed publication leaves
an absent data root absent.

If an artifact filename appears in a curated workflow, update the reviewed
JSON resource and its model provenance together. Installation copies that
resource; it does not regenerate filenames from a host-side alias table.

Managed GGUF files use `"target": "llama-models"` and a destination relative
to `content/llama-cpp/models`. Text artifact IDs, destination directories,
and destination filenames start with the lowercase owner of the pinned GGUF
repository, as public preset and bundle IDs do. Keep the source path exactly
as published; the destination name is independent of it. Add a bundle using
application `llama-cpp` and groups `all` and `llama`, plus the intended public
selection group, then
connect it to a stable router identity:

The following is a minimal built-in-catalog fragment; the referenced bundle
and artifact must also exist:

```json
{
  "llama_presets": {
    "publisher-model-q8-0": {
      "bundle": "llama-publisher-model-q8-0",
      "artifact": "publisher-model-q8-0",
      "default_context": 8192
    }
  }
}
```

Add optional policies only when needed by the reviewed model tuple. Do not
copy a maximal example that combines unrelated speculation, vision, allocation
and reasoning settings.

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
Throughput and speculative benchmarks enforce the same boundary. Validate
coherent multi-turn output and tool calls because startup, prompt processing,
or a short retrieval key alone cannot establish backend compatibility.

`backend_profiles` optionally limits a backend to a non-empty list of concrete
GPU profiles, for example `"backend_profiles": {"rocm": ["strix-halo"]}`.
Other backends keep their existing availability. The host resolves automatic
selection using the exact render nodes' KFD topology. An unknown profile is
not sufficient to expose a restricted model, and the container repeats the
profile check before startup. This allows a measured ROCm exception without
declaring that exception safe on every architecture.

`speculative_type` is optional and accepts only `draft-mtp` or
`draft-dflash`. Managed entrypoints require a positive matching
`draft_tokens`, limited to eight for MTP and fifteen for DFlash; DFlash
requires a separate draft. MTP can instead use prediction heads embedded in
the target. A declared `draft_artifact` must be a different GGUF in the same
bundle and partition, which the catalog relationship checks validate.

Keep review policy and enforcement distinct. The general loader validates
strategy names, nonnegative depth and positive backend overrides, but does
not enforce every entrypoint combination or upper bound. Its streamed-policy
checks are narrower and stronger. A successful catalog load alone does not
prove a preset can start. Test direct and router validation for the exact
tuple; these fields are not arbitrary upstream arguments.

Managed DFlash always selects greedy proposal sampling explicitly in both
direct startup and router sections. Target sampling still comes from the
gateway or the caller. Probabilistic draft sampling is not a managed option.

`mmproj_artifact` optionally names a distinct matching `.gguf` projector in
the same bundle and `llama-models` partition. Once declared, it is required
content, not an optional runtime switch. The target, draft and projector must
have separate artifact identities. Validate the exact companion tuple rather
than borrowing an upstream base model's license or a similarly named repack.
The host maps its verified destination to direct startup and router `mmproj`
consistently. Vision capability is derived from this reference and frozen only
for a complete receipt-verified bundle; do not add a separately configurable
vision boolean. Missing/stale companions require the normal installer pass.
Projectors count toward installed size and readiness, not target shard counts.
Keep both plain and speculative sibling bundles tied to their matching
projector and accept image input, full image-history replay and text after
images on every supported runtime tuple before promotion.

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

`jinja` enables llama.cpp's Jinja chat-template engine for a preset.
`chat_template` names a project-bundled template accepted by the native
entrypoint's closed mapping. Add the template to the llama.cpp image, the
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
path cannot use the normal resident policy. Its profile keys are limited to
`strix-halo` or `strix-point`. The base choices are `resident` and
`mmap-lazy-token-embedding`. The latter
maps to llama.cpp mmap, lazy tensor reads, and CPU placement of the exact
`per_layer_token_embd.weight` tensor. It is intentionally not a generic
tensor-override surface. Add it only after cold and warm startup, long-context
retrieval, output coherence, host-memory headroom, and kernel-journal checks
on the named hardware profile. Omitted Strix profiles retain resident loading.

The additional `stream-token-embedding` policy is limited to Strix Halo with
Flash Attention and F16 KV. It preserves the lazy-mmap Vulkan path and selects
explicit CPU embedding reads with device allocation on ROCm. The shared
container helper owns its complete direct/router argument tuple, including
2048 batch/microbatch and no RAM prompt archive. Four request slots share one
configured context pool by default. A single-slot launch retains separate KV.
The ROCm backend must explicitly restrict itself to Strix Halo. Its only
reviewed speculative tuple is ROCm-only `draft-dflash` at depth three with a
separate verified draft and no backend depth overrides. MTP, other depths and
Vulkan speculation remain rejected for this policy. Native `llama-bench`
cannot apply it and therefore
rejects the managed ROCm preset rather than measuring a different allocator.

`agent_tools` is an explicit reviewed Chat Completions function-tool claim,
not an installation selector or general quality score. Check that the selected
template and context can sustain the maintained client's tool round trip.
Jinja can come from the preset flag or a named managed template; there is no
general loader-enforced 16K context or `jinja: true` prerequisite. Do not
describe a review recommendation as a schema guarantee. After
changing preset IDs, contexts, templates, or this flag, regenerate every
maintained agent-client configuration and accept a complete function-tool
round trip through at least one maintained client on target hardware. Record
which client supplied that evidence and which remain provisional. Before a
newly exposed model receives unattended write access through another client,
repeat the end-to-end loop there as well. Sibling presets may share protocol
evidence when they use the byte-identical target and chat template; keep
unaccepted context or speculative-decoding behavior explicitly experimental.

`reasoning_control` is a narrower reviewed capability for agent clients.
Review it alongside `agent_tools`; the loader does not enforce that dependency.
`toggle` represents a native off/on model and must default to `on`, enable
`reasoning_off` and omit named levels. `effort` and `strength` represent distinct
native parameters. Review their `reasoning_levels` as an ordered subset of
the model's real low, medium, high and xhigh controls, with a matching
`reasoning_default`. The loader checks nonempty unique identifiers and permits
`off`, `on` or a declared default, not whether the model implements those
labels. `reasoning_off`
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

`sampling_policies` owns reusable thinking and non-thinking sampler defaults.
Presets for any text engine can reference one through `sampling_policy`.
The gateway snapshots that exact policy with its verified inventory, resolves
the request's thinking mode, and fills only omitted or null sampler fields.
It rejects conflicting reasoning selectors before scheduling. Policy fields
are a closed numeric allowlist with range checks, not arbitrary request
overrides. Direct engine startup and router INI files do not carry the policy.

Qwen3.8's dense variants share their family policy.
Regular and Huihui Flash-Next share one policy with thinking min-p 0.05.
Generated clients omit static tuples for these
models so every request can select its own thinking mode. Direct benchmark
runners send the selected tuple explicitly from the same catalog.
The native Maki export adds no static sampler tuple for other families either.
Models without a catalog policy retain engine defaults unless the client sends
values explicitly. Adding `agent_tools` still requires a sampling audit.
Do not encode sampling as a bundle variant, client-name heuristic, or Jinja
side effect. See [gateway sampling](gateway.md#model-aware-sampling-defaults).

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
installed presets, ignores wholly missing target families or an absent
optional separate draft beside verified targets, and refuses partial
managed installs.

Current recipes and defaults come from `internal/recipes/recipes.go`, and
exact optional variants come from the catalog. Use
`content list`, `content list bundles` and
[upgrade notes](guides/upgrade-notes.md) instead of maintaining another model
inventory here. Recipes are family-oriented practical selections; precision,
speculation and large optional variants may remain exact bundles.

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

`internal/contentpack/` owns the deliberately narrow schema used by ignored
local packs and one-file remote imports. It is not the built-in catalog
schema. Use the ignored repository-root `local-content/` directory for
private packs, and protect metadata that contains sensitive repository names.

```bash
./paracetamol content install \
  --from-file local-content/models.json --dry-run
```

`--from-file` is repeatable and cannot accompany a positional target. Every
bundle in each supplied pack is selected. Files compose additively, without
overriding built-in or earlier IDs, but each bundle must reference artifacts
declared in its own pack, not built-in or cross-pack artifacts.

The complete merge also rejects two artifacts with the same target and
destination, or one artifact's file destination used as another's directory,
whether built-in, in one pack or in different supplied packs.
Identical hashes do not exempt a collision. Different targets have independent
destination namespaces. Catalog loading, pack merging and direct installer
planning share this destination-ownership check before installation mutates data.

### Supported schema

Schema 2 accepts only `schema_version`, `artifacts` and `bundles`, with
nonempty artifact and bundle objects. Unknown fields fail. It does not accept
agreements, archive collections, curated workflows, benchmarks, serving
presets, executable code or renderer definitions.

This complete shape uses fictional source identities. Replace them with
independently verified metadata before installing; they are not downloadable
example bytes.

```json
{
  "schema_version": 2,
  "artifacts": {
    "local-example": {
      "description": "Locally reviewed model",
      "source": {
        "provider": "huggingface",
        "repository": "example-org/example-model",
        "revision": "1111111111111111111111111111111111111111",
        "path": "example.safetensors"
      },
      "target": "models",
      "destination": "diffusion_models/imported/example.safetensors",
      "size": 1234,
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "license": {
        "spdx": "NOASSERTION",
        "status": "unverified",
        "url": "https://huggingface.co/example-org/example-model",
        "warning": "Hosted-file licensing has not been independently verified."
      }
    }
  },
  "bundles": {
    "local-example": {
      "description": "Local example selection",
      "application": "comfyui",
      "artifacts": ["local-example"],
      "groups": ["all", "comfyui"]
    }
  }
}
```

Artifact `target` is required: `models` for ComfyUI models,
`workflows` for exact imported ComfyUI JSON, or `llama-models` for llama.cpp.
The pack loader requires safe relative destinations, positive sizes, lowercase
SHA-256, and explicit `NOASSERTION`/`unverified` metadata with a warning and
HTTPS provenance URL. It does not accept a verified-license claim. Optional
upstream lineage remains context, not an inherited license grant.

Bundles declare `comfyui` with exactly the `all`/`comfyui` groups, or
`llama-cpp` with exactly `all`/`llama`. Their distinct artifact references
must belong to that same application and pack. Normal destination planning,
license-risk approval, resumable installation and verification still apply.
Packs cannot add API model IDs to the gateway; use an exact local
`--model` path for an isolated compatibility trial.

Hugging Face source fields are `repository`, full `revision` and safe
relative `path`. Civitai uses explicit `provider: "civitai"`, allowlisted
`host`, positive `model_id`/`model_version_id`, basename `filename`
and exact same-host `download_url` under
`/api/download/models/VERSION`. Unlike the built-in source loader, the pack
loader does not supply omitted Civitai host/URL defaults. Preserve the
provider-returned format-selection query when present.

Civitai can replace a hosted file behind an unchanged model-version ID.
Exact size and SHA-256 prevent silent substitution, but a later install may
fail because those bytes are gone. Keep Civitai artifacts in reviewed
user-owned packs, not the built-in catalog. A model-version ID is not a claim
of provider immutability.

Environment tokens `HF_TOKEN` and `CIVITAI_TOKEN` remain outside packs,
arguments and persistent data. Civitai authorization goes only to its initial
endpoint, not object-storage redirects. Direct downloads resume using Range
and restart safely if the provider does not honor it.

### Remote imports and approval

`content import URL` resolves one supported Hugging Face or Civitai file,
infers a destination, builds a schema-valid pack and uses the normal installer.
It is an authoring convenience, not another downloader or arbitrary URL
surface. `internal/remoteimport/` owns provider parsing and metadata.

Keep these boundaries:

- Hosts are limited to `huggingface.co`, `civitai.com` and `civitai.red`.
- Named Hugging Face revisions resolve to full commits; imported files need
  exact LFS size and SHA-256 metadata.
- Civitai needs an exact version and supported file with size/hash and a
  same-provider download URL.
- Ambiguous versions, files and destinations need explicit selection;
  inferred destinations remain overridable in guided terminal use.
- Generated licensing stays `NOASSERTION`; metadata authorization is not
  forwarded across redirects.
- Dry runs validate the generated pack and artifact/destination plan without
  saving metadata, creating data or downloading content.

The installer invokes `InstallOptions.BeforeMutation` only after the complete
artifact/destination plan, license-risk approval, dry-run check and context
validation, before creating data directories or downloading. Import uses it
to publish the pack. This does not promise every later Podman, mirror or
network check has passed. Once published, the pack remains available for
retry after downstream failure. Do not move metadata publication ahead of
approval or the destination plan. Status validation does not replace the
installer's later parent-path, staging and mirror confinement checks.

Keep `remoteimport.Kinds` aligned with artifact targets,
`content.ArtifactPath`, ComfyUI extra model paths and workflow destinations.
An archive, multi-file recipe, custom node or serving preset needs a reviewed
integration change, not another import kind.

### Exact imported workflows

A `workflows` artifact must end in `.json` and installs below
`apps/comfyui/user/default/workflows/imported/`, separate from curated
resources. This installs exact bytes only. It does not execute or validate the
graph, install nodes/dependencies, create a benchmark or claim runnable output.

For research, inspect package declarations, pinned node versions, active and
bypassed nodes, model references, core types and unattributed node types
without executing the graph or resolving its dependencies.

## Bounded archive maintenance

The built-in catalog and installer retain bounded Civitai ZIP-member
transport and `archive_collections` support. The current local-pack schema
does not expose either, and built-in policy excludes mutable Civitai content.
Do not present ZIP-member installation as a currently supported pack/import
operation. This implementation capability needs a deliberate schema/policy
review before use through a new public content route.

Preserve the existing transport safety properties when maintaining it:

- Inspect the central directory without extraction: unsafe paths, duplicates,
  member types, compressed/uncompressed sizes and the selected member hash.
  A selected member must occur exactly once and be regular, not a symlink.
- Bound download bytes with `max_size`; start small archive transfers from
  zero. Verify the member's exact size and SHA-256 before atomic installation.
- Member identity, not the outer ZIP hash, governs extracted bytes. A harmless
  repack can retain identity; changed member bytes fail closed.
- Keep failed layout/size/hash archives for investigation instead of silently
  downloading replacements on retry. Interrupted unmarked archives restart;
  older complete archives need central-directory validation.
- Collections expand shared source/archive/license metadata into independently
  verified members with separate destinations. Download once, and prune shared
  staging only after all selected members install.

Record provider version, filename, size/hash metadata and independently
verified bytes. The archive hash remains useful research evidence, not a
substitute for selected-member identity.

## Local mirror invariants

The [content guide](guides/content.md) documents copy and move usage.
Mirror candidates are regular files indexed without following symlinks,
shortlisted by basename or hash name, and accepted only after exact size and
SHA-256 verification. The mirror and destination roots cannot overlap.
Agreement and license-risk gates still apply.

Move mode is explicitly destructive to the old library: a validated source
moves into resumable staging and is not restored on interruption. Failed or
interrupted staging must remain retryable. Before approval or mutation,
planning and dry runs require an existing mirror directory and reject canonical
overlap with the data root, including through existing symlink ancestors of a
not-yet-created data root. Dry runs report worst-case network size, not promised
reuse. They do not index or hash mirror candidates. Actual installation
revalidates the mirror root before indexing candidates. Root validation does
not guarantee that every descendant is readable.

## Add a content recipe

Public installation is application-first. Small runnable recipes live in
`internal/recipes/` and resolve directly to exact catalog bundle IDs.
The same definitions drive guided `content install`, `content list`, and the
application guides, so those interfaces cannot silently select different
content.

To expose a new recipe:

- add one `recipes.Recipe` to the application's `byApplication` entry in
  `internal/recipes/recipes.go`;
- select the smallest exact bundle set that produces one useful outcome;
- set its explicit `Next` argument list; `Recipe.NextCommand()` formats the
  copyable command printed after installation;
- update content help, the user README, the relevant application guide, and
  focused recipe-resolution tests.

Do not use a recipe as a model-family inventory. Quantization alternatives,
optional add-ons, imported workflow libraries, speculative-decoding
mechanisms, smoke-test roles, and size classes remain exact bundles. A recipe
that grows from one practical result into hundreds of GiB of choices has
stopped serving its purpose.

Catalog groups are internal tags for ownership, the literal `all` aggregate
and explicit model-family selectors. The loader's `selectorGroups` map is in
`internal/catalog/catalog.go`; public family selection and listing are
explicit in `internal/cli/commands_content.go`. Change both only when users
need a named family across several bundles, and test its complete selection.
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

The trust chain is pinned upstream provenance → reviewed checked-in JSON →
hash-checked exact-byte installation. Curated resources are not arbitrary
hand-authored graphs, but the current installer does not extract or render
upstream templates. It reads `catalog/workflows/<workflow-id>.json` and
checks `rendered_sha256` before publication.

### Pin and review the source

Record the official template's package/version, full upstream revision,
resource path, `source_sha256`, license and immutable license URL.
Extract the exact resource from the pinned ComfyUI image in a network-free
temporary container, or otherwise obtain the same pinned upstream bytes with
equivalent identity evidence. Verify the bytes before adapting them.

### Preserve the transformation evidence

The workflow's `renderer` field records provenance, not a currently executed
host renderer. There is no maintained general curated-workflow regeneration
command. Do not imply that catalog loading or installation independently
reproduces that transformation.

For a change, retain a reviewable derivation from the pinned source: exact
model filename changes, removed dependencies, sampler changes, rewired links,
cleared sample media and enabled/disabled stages. Preserve model-source and
license provenance under `extra.paracetamol`. If a reusable authoring
transform is introduced, keep it deterministic and fail closed on unexpected
node types, counts and links; do not invent a permissive graph adapter.

Check graph properties directly in focused tests rather than relying only on
a hash. Keep source and final-byte evidence sufficient to review the change;
an unavailable historical transform is a reproducibility limitation, not an
invitation to silently recreate different bytes.

### Publish the reviewed resource

Store final bytes at `catalog/workflows/<workflow-id>.json`, calculate
`sha256sum`, and update `rendered_sha256`. Do not commit placeholder hashes.
Catalog tests check every resource's exact hash and JSON validity; they do not
reexecute its original derivation.

Install into disposable application data, inspect visually, queue on suitable
hardware, and export the matching benchmark graph. Existing differing
installed workflows remain user modifications unless explicitly replaced
with `--force`. Imported exact workflow artifacts use their separate
destination and make no runnable-graph claim.

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

`benchmark.PreparePrompt` rewrites deterministic seeds, the `LoadImage` path
and output prefix; the runner supplies the generated 768×768 input. It rejects
`LoadVideo`.

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
make test GO_PACKAGES='./internal/catalog ./internal/content ./internal/cli ./internal/benchmark'
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
