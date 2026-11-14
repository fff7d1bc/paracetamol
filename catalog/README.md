# Curated content catalog

The catalog describes pinned content, licenses, installable bundles, serving
presets and workflow/benchmark provenance. It contains no weights or sample
media. Inspect the current inventory through the public CLI:

```bash
./paracetamol content list
./paracetamol content list bundles
./paracetamol content list models --details
./paracetamol content install all --dry-run
```

Do not maintain another model inventory or retirement ledger here. Use the
[content guide](../docs/guides/content.md) to choose/install content and
[upgrade notes](../docs/guides/upgrade-notes.md) for user migration.
[Content catalog maintenance](../docs/content-catalog.md) owns authoring,
validation and acceptance procedures.

## Structure

- `agreements`: terms that require explicit user acceptance, independently
  of hosted-artifact license-risk acknowledgment.
- `artifacts`: one remote file's immutable source, destination, exact size,
  SHA-256 and license provenance.
- `archive_collections`: independently verified members sharing one bounded
  archive transport. The loader expands them into ordinary artifacts.
- `bundles`: application-owned artifact selections, selector groups and an
  optional curated workflow. Shared artifact IDs avoid duplicate downloads
  and destination ambiguity.
- `workflows`: pinned upstream template provenance plus the rendered hash of
  a reviewed resource in `catalog/workflows/`. Installation validates and
  copies those exact repository bytes, without image extraction or rendering.
- `benchmarks`: pinned ComfyUI API graphs exported from reviewed workflows.
  Closely related variants may use a maintained allowlisted benchmark
  renderer and a separate rendered hash.
- `sampling_policies`: reusable thinking/non-thinking defaults selected by
  the gateway after resolving a request's native reasoning mode.
- `llama_presets`: public model IDs connecting a bundle and target GGUF to
  reviewed context, backend/profile compatibility, template, speculation,
  cache/allocation, companion-file and client-capability policy.

The loader and relationship tests reject unsafe paths, malformed identities,
unknown references, repeated bundle content, unknown groups and conflicting
destinations. A successful load does not prove every entrypoint combination
or hardware/client review condition. See the maintainer field reference for
the actual validation boundaries.

Local content packs have a smaller closed schema: artifacts and bundles,
with explicit unverified-license metadata. They cannot add serving presets, executable
code or curated renderers. Imported exact workflow artifacts make no claim
about runnable graphs, custom nodes or installed dependencies.

## Provenance policy

Trace artifacts to the direct source and full immutable revision. Record exact
size and SHA-256 from pinned metadata and verify downloaded bytes. Keep the
target, speculative draft, projector and template as one reviewed companion
tuple; similar filenames do not establish compatibility.

A missing or unclear license remains `NOASSERTION` with explicit risk
acknowledgment. Do not inherit a conversion's license from its upstream base.
Required model terms remain a separate agreement gate.
`THIRD_PARTY_NOTICES.md` summarizes attribution; the catalog owns exact
artifact identity, relationships and license evidence.

Civitai model-version IDs can retain mutable hosted files. Their exact hashes
still prevent substitution, but removed pinned bytes can make later installs
fail. Keep Civitai artifacts in reviewed user-owned local packs, not the
built-in catalog. The maintenance guide documents bounded archive inspection
and import limitations.

Curated workflow sources are pinned licensed official templates. Preserve
source and transformation provenance in the checked-in resources; a rendered
hash checks final bytes, not independent reproduction of their derivation.
Application and extension sources are fetched directly from their pinned,
attributed public repositories.
