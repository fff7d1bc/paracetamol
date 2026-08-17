# Muse Glimmer llama.cpp agent feasibility snapshot

This is a dated maintainer research record from 2026-08-10. It captures the
comparison that selected Meta's official dynamic K-quant Muse Glimmer GGUF,
kept DFlash and the managed coding-agent integrations, and declined to add a
second full-size BF16 choice.

This document is evidence, not a current version or support declaration.
Always inspect `Containerfile`, `catalog/catalog.json`, source, and tests for
the current pins and behavior before using it. Hostnames, usernames, private
paths, and other machine-specific identifiers are intentionally omitted.

The agent-quality results and comparative judgments originally recorded here
were retired on 2026-08-17 after model templates, sampling ownership,
reasoning defaults, and harness integrations changed. Native throughput,
memory, protocol, and startup observations remain as historical tuning
evidence. A future Muse pass must produce a fresh quality baseline.

## Question and decision

An initial OpenCode session with the Unsloth Dynamic Q8 target returned a very
short repository summary. That raised four questions which needed to be kept
separate:

1. Was Muse Glimmer intrinsically unsuitable for coding-agent work?
2. Was the Unsloth conversion behaving differently from Meta's official
   release?
3. Was DFlash or the experimental forced-256K context responsible?
4. Was the client scaffold asking for a shallow answer?

The decision was to **keep the family and agent integrations, but replace the
managed target with Meta's official dynamic K-quant GGUF**:

- the official target is substantially smaller than the Q8 and BF16
  alternatives and had the best measured decode throughput;
- the exact BF16 runtime had much larger resident memory and slower decode,
  which did not justify a second catalog choice; and
- llama.cpp's `--reasoning-preserve` policy was applied to all three
  then-managed dynamic-target Muse presets, while Muse remained distinct from
  models that support a client-selectable reasoning-effort budget.

The result at that stage was one installed model and draft with three
orthogonal launch policies: a non-speculative 128K control, the default 128K
DFlash policy, and an experimental forced-256K DFlash policy. A later section
records the current 17 GB target comparison and its subsequent promotion into
the same guided family without changing the default.

## Snapshot under test

| Component | Tested value |
| --- | --- |
| ROCmplete source | `6d59816` before the integration change |
| Project ROCm | `7.14.0` |
| llama.cpp | `62bf73d25c53b8161f8a22894d4f90c4aebbd7d0` |
| Normal llama.cpp image | `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r15` |
| Profile and architecture | Strix Halo, `gfx1151`, Radeon 8060S |
| Host memory | 128 GB LPDDR5X, 112 GiB TTM/GTT policy |
| Host software | Fedora Linux 44 non-OSTree, kernel `7.1.7-200.fc44.x86_64`, rootless Podman |
| Agent clients | Maki 0.4.5 and OpenCode 1.18.15 |

The server probes used the normal ROCmplete confinement: rootless Podman,
read-only root, dropped capabilities, `no-new-privileges`, the selected render
node, and `/dev/kfd`. No tested run produced a GPU reset, device loss, OOM, or
kernel fault.

## Immutable model candidates

### Selected official Meta target and draft

Both files come from
[`meta-models/Muse-Glimmer-30B-GGUF`](https://huggingface.co/meta-models/Muse-Glimmer-30B-GGUF/tree/93769bc7ab5ad1e9cd22d857e3138cf5d977ae81)
at revision `93769bc7ab5ad1e9cd22d857e3138cf5d977ae81`.

| File | Bytes | SHA-256 |
| --- | ---: | --- |
| `muse-glimmer-30B-kquant-dynamic.gguf` | 19,653,957,984 | `513109c8319115f69eb09fb7b118c97c8167d15bc014fd7670d2e30489bf106c` |
| `dflash-kquant.gguf` | 1,631,205,312 | `27d9a805fa29b943cfb6ad4843367cd4eaaaf06bd452d8cc3e00a2cd18a677bc` |

The installed total is 21,285,163,296 bytes, or about 19.82 GiB. Meta
describes the dynamic K-quant as approximately four-bit and reports about
0.2% average degradation across its 15-benchmark comparison. Those upstream
quality results were not independently reproduced here.

### Previously managed Unsloth Q8 target

The comparison retained the exact previously cataloged file from
[`unsloth/Muse-Glimmer-30B-GGUF`](https://huggingface.co/unsloth/Muse-Glimmer-30B-GGUF/tree/c3ac2ebf47426591b4c6d408103c8c15a1e2afd6)
at revision `c3ac2ebf47426591b4c6d408103c8c15a1e2afd6`:

| File | Bytes | SHA-256 |
| --- | ---: | --- |
| `Muse-Glimmer-30B-UD-Q8_K_XL.gguf` | 32,300,651,040 | `e63bf23b7710ecdea2579e4b1de58980c4a2b446e8ecf48b782cfcefd2e31770` |

It was removed from the managed catalog because the official K-quant is the
better practical single choice, not because this Q8 conversion failed agent
workloads.

### BF16 comparison

Meta publishes the official BF16 safetensors checkpoint at revision
`f84ecc3a0ea984a4c04542a84269e3d065350a6e`, but no official BF16 GGUF was
available in the tested repository. The llama.cpp comparison therefore used
Unsloth's BF16 GGUF conversion at revision
`faa5b025c584459c13febfa5c59883516710ae39`:

| File | Bytes | SHA-256 |
| --- | ---: | --- |
| `Muse-Glimmer-30B-BF16-00001-of-00002.gguf` | 29,805,191,936 | `9ed3c904ca80be99e787d95cc430c5b1ce3a44cb7acc012fda63c8e1649df7e5` |
| `Muse-Glimmer-30B-BF16-00002-of-00002.gguf` | 25,920,319,232 | `bbec2aef54b01bff34977a7da2a6e9e73c3edc063fabf0ae4bd62817b7efe1ae` |

The target totals 55,725,511,168 bytes, or about 51.90 GiB, before the DFlash
draft, KV cache, and runtime buffers.

## Fixed native benchmark

All three target files were measured through the same image, backend, host,
and llama-bench shape:

- ROCm backend and one `gfx1151` device;
- prompt processing 512 and text generation 128;
- three repetitions at context depth zero;
- batch and microbatch 4096;
- Q8_0 K and V caches; and
- Flash Attention enabled.

| Target | pp512 tokens/s | tg128 tokens/s |
| --- | ---: | ---: |
| Official dynamic K-quant | 341.168 ± 3.178 | 10.318 ± 0.013 |
| Unsloth Dynamic Q8_K_XL | 376.800 ± 6.109 | 7.351 ± 0.012 |
| Unsloth BF16 | 483.192 ± 6.756 | 4.109 ± 0.004 |

Higher precision improved prompt processing in this fixed shallow benchmark,
but decode moved in the opposite direction. The selected K-quant decoded
about 40% faster than Q8 and about 151% faster than BF16. Do not generalize
these figures across llama.cpp revisions, backends, cache policies, context
depths, or hardware.

The three native benchmark JSON results were retained outside the repository.
Machine-specific result paths are deliberately omitted here.

## Server capacity observations

Fresh 128K servers used four slots, Q8_0 K/V cache, Flash Attention, DFlash,
and reasoning preservation.

| Target | Ready time | Container memory after load |
| --- | ---: | ---: |
| Official dynamic K-quant | 13.997 s | 30.64 GB |
| BF16 | 37.494 s | 66.77 GB |

After the long BF16 request, container memory was 73.83 GB. These are
observations from the container accounting on one integrated-memory host,
not exact model-allocation or peak-memory guarantees.

## Failures and misleading signals

Several failures were environmental or fixture-related rather than model
failures:

- A plain HTTP resume of the first BF16 shard produced a file about 9.3 MB too
  large. Its size and hash failed and the file was discarded. Fresh downloads
  through the pinned Hugging Face/Xet path matched both recorded sizes and
  hashes.
- A disposable research image contained the correct llama.cpp binary commit
  but an older entrypoint without DFlash handling. The normal r15 image's
  current entrypoint succeeded. This is why an entrypoint policy change must
  bump the managed application image tag; source commit identity alone is not
  sufficient.
- An initial Maki fixture used `git clone --shared`. Its object store lived
  outside Maki's bubblewrap view, so Git objects appeared inaccessible and the
  model spent turns diagnosing a broken checkout. A self-contained
  `git clone --no-local` fixed the fixture. This was not model confusion.
- Unit-test failures involving `/run/rocmplete/runtime` occurred inside the
  agent sandbox and reflected its intentionally restricted runtime view. They
  were not an inference crash.

These cases are worth preserving because they can otherwise make a healthy
model or server look unreliable.

## Integration consequences

The accepted integration deliberately does not add vision, an mmproj file, a
BF16 preset, a second Muse recipe, a llama.cpp source update, or client prompt
hacks. It keeps model family, precision, speculation, context, and harness
policy orthogonal.

At this stage, all three dynamic-target Muse presets enabled Jinja, advertised
the reviewed function-tool contract, and set `reasoning_preserve`. The 128K
DFlash preset remained the recipe default. The 256K preset forced both target
and draft `context_length` metadata and remained experimental until useful
prompts beyond 128K pass retrieval, quality, draft-acceptance, latency, and
memory checks.

The removed Q8 file is managed content from an older catalog, but ROCmplete
does not silently delete persistent model bytes during an upgrade. After the
new bundle installs and verifies successfully, an operator may manually
remove the obsolete Q8 file if it is no longer needed.

## Candidate integration verification

The completed integration was synced to the same target host and built as
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r16`, image ID
`15aa29c45b41f011f5edacd9f5fb761db26eae488d447453414e2d1b2a9e07a3`.
The content installer reused the retained official target through its normal
local-mirror path, rechecked all 19,653,957,984 bytes against the catalog
SHA-256, copied them atomically into the managed partition, and recorded a
current verification receipt. The DFlash receipt remained current.

A direct managed 128K DFlash server then:

- reached healthy state on ROCm with four 131072-token slots;
- ran the official target and draft with `--jinja --reasoning-preserve`,
  `draft-dflash`, and 15 maximum draft tokens;
- used 25.17 GB of container memory after load in this warm follow-up run;
- returned exact content `OK` for a bounded request while exposing its parsed
  reasoning separately; and
- generated at 24.73 tokens/s for that short request, accepting 81 of 420
  draft proposals.

The values from one short request are a wiring probe, not a performance
benchmark. The server stopped and its container was removed cleanly.

The managed router also started successfully from the generated private INI
and loaded the 128K DFlash section on demand. Its bounded request returned
exact content `ROUTER_OK`. All three Muse sections contained
`reasoning-preserve = true`; the two DFlash sections retained their draft
settings, and the 256K section retained its two architecture overrides plus
`fit = off`. This closes the stale-entrypoint failure found during research
and verifies both runtime paths without another long agent evaluation.

## 2026-08-12 ATEM template correction

Meta updated the base-model
[`chat_template.jinja`](https://huggingface.co/meta-models/Muse-Glimmer-30B/blob/a4e59da52a7bc87ae7251dd5545c0dd437c44b68/chat_template.jinja)
at revision `a4e59da52a7bc87ae7251dd5545c0dd437c44b68` after the official GGUF bytes
were published. The new 9,992-byte file has SHA-256
`cfc67e5f349f37690dfd31ed1f18bc4442a9dd32fe39a648f993cb4eb3cae678`.
Most of the source diff reformats the original one-line template without
changing its whitespace-stripped output. The substantive change handles an
existing reasoning directive in a caller's system message: it normalizes
`Reasoning effort` to Muse's `Reasoning strength` terminology and does not
append a second default-high directive.

The selected target GGUF still embeds the original 7,167-byte template from
the initial base-model release. No GGUF or draft file changed after
ROCmplete's pinned GGUF revision; later commits in that repository changed
only its model card. Updating the model artifact pin would therefore not
deliver the template correction. ROCmplete instead bundles Meta's exact
immutable template and selects it through the existing closed managed-template
policy for all three then-managed dynamic-target Muse presets. The target and
DFlash artifact revisions, sizes, and hashes remain unchanged.

A temporary override against the existing managed image established the
behavior before integration. With the embedded template, a system prompt
containing `Reasoning effort: medium.` rendered that instruction followed by
`Reasoning strength: high.`. With Meta's corrected template, it rendered only
`Reasoning strength: medium.`. llama.cpp reported the same tool, parallel-call,
system-role, and reasoning-preservation capabilities, and a bounded request
returned a correctly structured `echo_text` call with the required argument.
The server stopped cleanly, and the host recorded no OOM, GPU reset, page
fault, ring timeout, or device loss.

The completed integration was built as
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r17`, image ID
`98369219e680a5e44517ba1955a4fb3ce18fbcbf80cc3d89961e76648ddcb193`.
The bundled template in the image matched the recorded SHA-256, and
`pip check` reported no broken requirements. A direct managed server rendered
only the normalized medium-strength directive and completed both a required
tool call and a tool-result continuation. The router loaded the same 128K DFlash
preset on demand, passed the managed template path to its child, and returned
a required structured tool call. Both paths stopped cleanly without a matching
kernel fault.

This correction removes contradictory prompt formatting but does not change
the model weights, DFlash decoder, tool protocol, sampler policy, or harness
scaffold. It is therefore a correctness retest trigger, not evidence by itself
that Muse's repository-level coding quality or OpenCode behavior has improved.

## 2026-08-13 upstream repack review

Meta subsequently republished the
[official GGUF repository](https://huggingface.co/meta-models/Muse-Glimmer-30B-GGUF/tree/43c7eadd41352a299ea8e0a36b3157978dd63596)
at revision `43c7eadd41352a299ea8e0a36b3157978dd63596`. The revision embeds the
corrected ATEM template and renames the files to canonical Q4_K names:

| File | Bytes | SHA-256 |
| --- | ---: | --- |
| `Muse-Glimmer-30B-KQuant-Dynamic-Q4_K_XL.gguf` | 19,653,960,832 | `ac7023d6a4c704eb9af54ab53e476a66b7f5b6c0ef2fc4a8dde5253c291a6c38` |
| `dflash-Muse-Glimmer-30B-Q4_K_M.gguf` | 1,631,208,128 | `b2e808bf656086fe86bd0d0bd990f01d33e377537a07c02d45371517c8b264ef` |
| `Muse-Glimmer-30B-KQuant-17GB-Q4_K_M.gguf` | 16,756,683,904 | `4cc57c0f51040a226e5a72cc47b7613f7772950e460a665f7083de89f183f60e` |

A full GGUF comparison found that the catalog-pinned and republished dynamic
targets have the same tensor names, shapes, and types. Their tensor-data
regions are byte-for-byte identical after their respective 13113696-byte and
13116544-byte headers. The two DFlash payloads are likewise byte-identical
after offsets 13073344 and 13076160. Their only changed metadata key is
`tokenizer.chat_template`; in both pairs, the whole-file size delta exactly
matches the aligned tensor-data offset delta. The republished dynamic file's
full SHA-256 also matched the recorded candidate hash. Meta's commit describes
the change as a fixed-template update and canonical rename. The embedded
template is byte-for-byte the same 9,992-byte template that ROCmplete already
bundles and verifies.

The renamed dynamic target and draft are therefore a metadata-only repack,
not a model update. Moving the catalog pin would download about 19.82 GiB of
behavior-equivalent content and require an unnecessary managed-content
migration and cleanup. ROCmplete retains the older immutable target and draft
plus its exact managed template.

The 17 GB Q4_K_M target is different. It is a smaller quantization candidate,
not another representation of the selected dynamic target, so it requires
performance and quality acceptance before it could replace or accompany the
managed choice.

## 2026-08-13 AMD reference and tuning follow-up

AMD's
[Strix Halo Muse Glimmer article](https://www.amd.com/en/blogs/2026/run-meta-muse-glimmer-30b-on-amd-ryzen-ai-max-and-radeon-gpus.html)
reports up to 24 generated tokens/s. Its screenshot shows 24.4 generated and
143 prompt tokens/s. Footnote SHO-77 identifies a Ryzen AI Max+ 395 system
with 128 GB RAM, a 64 GB VGM allocation, Windows 11 Pro 25H2, Adrenalin
26.7.1, llama.cpp's Vulkan backend, DFlash, and
`--spec-draft-n-max=4`. It says generation throughput is averaged over at
least three runs.

The report does not identify the target GGUF, llama.cpp revision, context or
prompt depth, slot count, cache types, sample length, or sampling policy. Its
roughly 21 GB screenshot allocation and Meta's own current DFlash example make
the 17 GB target plausible, but this is an inference rather than a disclosed
AMD setting. The headline is therefore a useful lead, not a directly
comparable acceptance result.

The ROCmplete follow-up first held the catalog-pinned dynamic target, managed
ATEM template, image, sampler, 131072-token allocation, one active slot, and
512-token output budget fixed. It compared ROCm and Vulkan with no draft,
DFlash depth 4, and the managed depth 15 at shallow, 32664-token, and
64347-token prompt depths. Shallow cases used three seeds; deeper cases used
one seed and a fresh server. Requests that exhausted the budget while still
reasoning are timing workloads rather than standalone answer-quality tests.

The image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r19`, built from
llama.cpp revision `62bf73d25c53b8161f8a22894d4f90c4aebbd7d0`. Sampling was
temperature 1.0, top-p 0.95, top-k 64, min-p 0, presence penalty 0, and
repeat penalty 1. A slash below separates generated tokens/s from end-to-end
request seconds:

| Backend and draft depth | 85-token prompt | 32664-token prompt | 64347-token prompt |
| --- | ---: | ---: | ---: |
| ROCm, none | 10.63 / 48.51 | 10.35 / 143.69 | 10.16 / 252.05 |
| ROCm, 4 | 12.80 / 40.44 | 17.45 / 127.85 | 18.77 / 236.02 |
| ROCm, 15 | 15.67 / 33.24 | 27.19 / 117.42 | 22.08 / 232.25 |
| Vulkan, none | 11.03 / 46.97 | 10.71 / 205.80 | 10.50 / 379.66 |
| Vulkan, 4 | 18.87 / 27.79 | 25.61 / 189.49 | 20.68 / 378.12 |
| Vulkan, 15 | 5.15 / 100.58 | 8.49 / 228.22 | not run |

Clean repeated ROCm no-draft prefill was 346.82 prompt tokens/s at 32664
tokens and 319.29 at 64347. The corresponding depth-15 rates were 331.52 and
307.95. Vulkan's depth-4 rates were 192.78 and 182.16, versus 206.83 and
194.53 without a draft. ROCm therefore remained the better complete-workload
backend on this Linux host even where Vulkan decode was competitive.

Draft acceptance alone did not select the optimum. At shallow, 32664, and
64347 tokens, ROCm accepted 31.5%, 55.7%, and 63.8% of depth-4 candidates but
only 10.2%, 24.8%, and 20.1% at depth 15. The deeper ROCm setting nevertheless
completed every workload faster because each speculative step offered more
candidates. Vulkan accepted 33.9%, 60.9%, and 46.8% at depth 4. Its depth-15
acceptance fell to 10.1% and 22.1% in the first two cases and made inference
slower than no draft, so the redundant 64347-token case was stopped.

The accepted policy is consequently backend-specific: Muse DFlash uses depth
15 on ROCm and depth 4 on Vulkan. The preset schema owns a closed backend
override so direct server startup, router rendering, client evaluation
identity, and human-facing inspection all resolve the same depth. This is not
a GPU-profile distinction and it does not change either backend globally.

### 17 GB target acceptance

The current revision's 17 GB Q4_K_M target and Q4_K_M draft were downloaded to
an external mirror, verified against the exact catalog candidate sizes and
hashes, and installed through the normal local-mirror path. An initial attempt
correctly refused a mirror inside the active data tree because source and
destination overlapped. Moving the mirror outside managed data allowed the
normal staged copy, hash verification, atomic install, and receipt update to
complete.

The target was measured with each backend's accepted draft depth. The table
reports prompt tokens/s, generated tokens/s, and wall seconds:

| Backend and target | 32664-token prompt | 64347-token prompt |
| --- | ---: | ---: |
| ROCm, dynamic, depth 15 | 331.52 / 27.19 / 117.42 | 307.95 / 22.08 / 232.25 |
| ROCm, 17 GB, depth 15 | 337.48 / 27.31 / 115.60 | 312.06 / 27.35 / 225.03 |
| Vulkan, dynamic, depth 4 | 192.78 / 25.61 / 189.49 | 182.16 / 20.68 / 378.12 |
| Vulkan, 17 GB, depth 4 | 213.77 / 26.87 / 171.91 | 200.57 / 27.23 / 339.73 |

At 64347 prompt tokens, the smaller target raised generated throughput by 24%
on ROCm and 32% on Vulkan. End-to-end time improved by 3% and 10% respectively
because prefill still dominated both long-context requests. Shallow generation
was effectively unchanged on ROCm at 15.68 tokens/s, while Vulkan improved
from 18.87 to 21.17. The one-slot ROCm allocation after load fell from 24.54
GB for dynamic target plus draft to 21.63 GB for the 17 GB pair. Vulkan memory
is not compared because rootless container accounting does not include its
device allocation on this host.

These controlled numbers did not reproduce AMD's disclosed 24.4-token/s
screenshot exactly. The closest comparable shallow Vulkan mean was 21.17
tokens/s, with one repetition at 22.13. Differences in the undisclosed target
file, prompt, sample length, context allocation, slot count, llama.cpp build,
and Windows driver prevent an attribution. Normal coding-agent turns can show
much higher instantaneous rates when their accepted draft sequence is easier,
which is also not comparable to either fixed workload.

Operationally, the transient evaluation service required both an explicit
checkout working directory and the Homebrew binary path. Two setup attempts
exited with status 127 before model inference and are excluded. An early deep
no-draft timing request that overlapped another client was also discarded and
repeated cleanly. Raw machine-specific result JSON remains outside the source
tree under the benchmark and agent-evaluation data partitions.

Final managed acceptance exercised both startup paths. Direct Vulkan startup
resolved `--spec-draft-n-max 4`, loaded the exact installed target and draft,
and reported healthy. A 64-token exact-answer probe exhausted its budget in
reasoning without content; the same probe with the managed low-reasoning
directive and 256-token budget stopped normally with exact content
`VULKAN_OK`. Router rendering put `spec-draft-n-max = 4` in the 17 GB section,
loaded that section on demand, and returned exact content `ROUTER_OK`. The ROCm
server independently exercised depth 15. All containers stopped and were
removed, with no matching GPU
reset, page fault, or ring timeout in the kernel journal. llama.cpp did emit a
ROCm host-buffer size-mismatch warning while destroying the evaluation
context; it followed successful inference and exit status 0, so it is
retained as a cleanup warning rather than classified as an inference failure.

### Two-variant family and forced-256K acceptance

The 17 GB pair was subsequently promoted from an exact advanced choice into
the existing `muse-glimmer` recipe to make direct A/B selection practical.
This did not create another family or change the guided default: the recipe
still launches the dynamic 128K DFlash preset by default. A guided install now
selects both official target/draft pairs, 39,673,055,328 bytes or about 36.95
GiB. Each pair retains its own immutable upstream DFlash artifact rather than
introducing cross-revision coupling solely to remove one duplicate 1.52 GiB
draft.

The 17 GB pair gained the same three policies as dynamic: a non-speculative
128K control, 128K DFlash, and experimental forced-256K DFlash. All six are
generated for OpenCode, Pi, OMP, and Maki with the same reviewed Muse sampler,
managed ATEM template, tool contract, and reasoning-preservation policy.
Focused catalog, router, and client-generation tests cover all six identities.
The public identifiers preserve Meta's quantization names as
`kquant-dynamic-q4-k-xl` and `kquant-17gb-q4-k-m`; DFlash and forced-context
behavior remain explicit suffixes rather than replacing the model identity.

Final `gfx1151` acceptance used the same revision and `r19` image as the
controlled comparison. A direct ROCm forced-256K server loaded four 262144
token slots with depth 15, both architecture metadata overrides, and automatic
fitting disabled. It returned exact content `MUSE_256K_OK`, generated 143
tokens at 37.90 tokens/s, and accepted 118 of 375 draft candidates. The
container reported 24.49 GB after the request.

The managed ROCm router advertised all six Muse IDs among its 19 installed
presets, loaded the 17 GB forced-256K section on demand, and returned exact
content `ROUTER_256K_OK`. That request generated 148 tokens at 39.59 tokens/s
and accepted 123 of 375 draft candidates; the container reported 24.53 GB.
A direct Vulkan run selected depth 4, loaded the same four 262144-token slots,
and returned exact content `VULKAN_256K_OK` at 31.67 tokens/s, accepting 92 of
160 candidates. Rootless container accounting does not include Vulkan device
allocation, so its reported memory was not used.

These tiny exact-answer requests are wiring probes rather than comparable
performance benchmarks. All three services stopped and were removed cleanly,
and the matching kernel-journal window contained no GPU reset, page fault,
ring timeout, or device-loss report. The forced policy remains experimental
because startup and shallow generation do not establish useful retrieval or
quality beyond 128K.

## 2026-08-14 forced-256K XL depth tuning

A follow-up on the same `gfx1151` host used llama.cpp release `b10430`, commit
`4c1a0af40d88c7fbb3b15c85bf2e8016d1d5b64c`, through image
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-4c1a0af-r21`. It retained
the official dynamic target and DFlash bytes, forced 262144 context for both
architectures, F16 target and draft KV, Flash Attention auto, batch 2048,
microbatch 512, and the managed Muse sampler. Each timing case used the same
38,244-token repository packet, seed 4242, and 768-token output allowance.

| DFlash policy | Prompt tokens/s | Generate tokens/s | Total seconds | Accepted / drafted |
| --- | ---: | ---: | ---: | ---: |
| Depth 15, `p_min=0` | 303.00 | 11.87 | 190.83 | 415 / 5,253 |
| Depth 8, `p_min=0` | 304.52 | 12.75 | 185.73 | 426 / 2,698 |
| Depth 12, `p_min=0` | 304.77 | 15.16 | 176.06 | 487 / 3,329 |
| Depth 12, `p_min=0.10` | 302.93 | 15.12 | 176.97 | 487 / 3,329 |
| Depth 12, `p_min=0.25` | 301.81 | 12.59 | 187.66 | 426 / 1,896 |
| Depth 12, `p_min=0.50` | 303.45 | 13.45 | 183.06 | 384 / 617 |

Depth 12 improved the fixed generation interval by 27.8% and reduced complete
server time by 7.7% relative to depth 15. A backend-sampling-off control
reproduced depth 12 at 15.18 tokens/s with identical accepted and drafted
counts, so the ROCm `TOP_K` offload warning did not identify another useful
switch. Forcing Flash Attention was neutral. Q8 target KV reduced generation
to 11.21 tokens/s. A 4096 microbatch raised prompt processing from 302.93 to
319.63 tokens/s but reduced generation to 13.07 tokens/s and slightly increased
total time.

A 4,096-token depth-12 continuation sustained 13.35 tokens/s, and a later
probe remained near 13.4 tokens/s through 7,406 tokens. Both stayed entirely
inside Muse's ATEM reasoning channel; the latter was manually interrupted.
They show that the short result did not collapse during longer decode, but do
not establish answer quality or compare equal-length depth-12 and depth-15
outputs. The accepted change is therefore deliberately narrow: the dynamic XL
forced-256K preset uses depth 12 on ROCm, while its 128K preset retains depth
15, Vulkan retains depth 4, and both 17 GB presets remain unchanged.

## 2026-08-17 scoped off fallback

ROCmplete stopped decoding Maki's output-window-dependent numeric budgets as
native model levels. Removing that server-wide compatibility block also
removed its unsafe generic off-to-low rule, which had changed Qwen off into
low. Muse still cannot disable reasoning, and OMP cannot hide its generic off
choice, so this policy moved to the model-owned template: when
`enable_thinking=false` and no native strength was supplied, Muse renders low;
otherwise its unchanged fallback remains high.

The derivative adds that one condition and an explanatory source comment to
Meta's 9,992-byte template. The resulting 10,219-byte managed file has
SHA-256
`4849b801303b351a82dab37107a665410070cd58315fadccd8f5fde02084bd34`.
Its immutable source revision and hash remain recorded above so later updates
can separate Meta changes from ROCmplete's narrow policy.

## Retest triggers

Repeat the comparison when any of these changes materially:

- Meta publishes an official full-precision GGUF or a materially different
  dynamic quant;
- Muse Glimmer's template, DFlash implementation, reasoning parser, or
  `--reasoning-preserve` behavior changes in llama.cpp;
- the ROCm backend, quantized KV cache, or Flash Attention changes on
  `gfx1151`;
- a maintained client changes its system scaffold, compaction, tool protocol,
  or model metadata schema; or
- forced-256K prompts demonstrate a quality, stability, or memory problem
  that the 128K control does not reproduce.

Use the same immutable bytes, server image, prompt corpus, slot/context
settings, sampler policy, tool permissions, and fresh self-contained checkout
when comparing. Record complete turn/tool traces and output sanity, not just
load success, token rate, or final-answer length.
