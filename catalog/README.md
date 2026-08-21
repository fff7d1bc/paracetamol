# Curated content catalog

The catalog describes agreements, downloadable artifacts, shared archive
collections, installable bundles and selector groups, deterministic UI
workflows, and managed benchmark graphs. It contains no model weights or sample
media. Use `./paracetamol content list bundles` for the current exact inventory.

## Structure

- `agreements` records non-permissive model terms that must be reviewed and
  explicitly accepted before content installation or benchmarking.
- `artifacts` owns one immutable remote file, its destination, exact byte
  size, SHA-256, repository, full revision, and license provenance.
- `archive_collections` compactly describes multiple independently verified
  artifacts extracted from one bounded archive transport. The installer
  downloads the shared archive once, then verifies every member separately.
- `bundles` references shared artifact IDs, an optional workflow, and explicit
  selector groups. Reusing an artifact prevents duplicate downloads and
  destination ambiguity.
- `workflows` references an official template resource already pinned in the
  image, its source hash, deterministic renderer, rendered hash, and MIT
  provenance.
- `benchmarks` pins an API-format graph mechanically exported from each
  rendered workflow by the matching ComfyUI frontend. Closely related
  variants may additionally pin an allowlisted deterministic renderer and its
  rendered hash.
- `llama_sampling_policies` owns validated thinking and non-thinking sampler
  defaults that the managed server selects after resolving a request's native
  reasoning mode. Presets reference these policies instead of duplicating
  numeric tuples or encoding them in Jinja.
- `llama_presets` connects one llama.cpp bundle and target GGUF artifact to a
  stable router model ID and a conservative `default_context`. A preset may
  additionally own constrained `draft-mtp` or `draft-dflash` speculative
  decoding with an optional closed ROCm/Vulkan draft-depth override, embedded
  Jinja or project-bundled chat-template policy, profile-specific Flash
  Attention and symmetric K/V-cache policy, and one verified draft GGUF from
  the same bundle. A reviewed experimental preset
  may narrowly override the target and draft `context_length` metadata while
  disabling automatic fitting. `agent_tools` records the smaller reviewed set
  maintained for function-tool agent clients.
  `reasoning_control` records a reviewed native toggle, effort, or strength
  contract; companion level, default, and off-mode fields define the exact
  client surface without inventing cross-model reasoning levels.
  `sampling_policy` optionally references one catalog policy when the model's
  authoritative sampling differs between thinking and non-thinking requests.

The loader rejects unsafe paths, malformed revisions and hashes, missing
references, repeated bundle content, unknown groups, and destination
collisions.

## Included content

The catalog currently covers ComfyUI image, edit, and video stacks; managed
llama.cpp GGUF presets; and the high-memory DwarfStar model with its optional
DSpark support artifact. Do not maintain a second inventory in this file.
Inspect the catalog through the same public commands users run:

```bash
./paracetamol content list bundles
./paracetamol content list models --details
./paracetamol content install all --dry-run
```

The application and content guides explain how to choose and run these
artifacts. `THIRD_PARTY_NOTICES.md` records third-party provenance, while the
catalog itself remains authoritative for exact revisions, sizes, hashes,
licenses, agreements, relationships, and runtime policy.

## Import policy

New artifacts must be traced to their direct source and immutable revision.
Size and SHA-256 come from the pinned Hugging Face LFS/Xet metadata and must be
verified before catalog entry. A missing or unclear license must be represented
honestly as `NOASSERTION`; it may never be silently treated as its upstream
project's license.

Benchmark graphs are independently derived from Paracetamol's pinned
MIT-licensed official templates. Application and extension source is fetched
directly from the pinned, attributed public repositories documented in
`THIRD_PARTY_NOTICES.md`.
