# vLLM integration direction

## Decision

vLLM is a planned additive inference application. It will not replace
llama.cpp. The first intended target is NVIDIA hardware, with the soft-unlocked
CMP 170HX cards expected for initial acceptance once the host, cooling, and
device stack are ready.

This page records direction, not implemented behavior. Paracetamol does not
currently build, install, advertise, or run vLLM.

The intended application roles are:

- llama.cpp remains the primary GGUF path and the proven ROCm path.
- DwarfStar remains a separate model-specific experimental application.
- vLLM may add native model formats, CUDA execution, continuous batching, and
  tensor parallelism where measured results justify it.
- The gateway remains the single client-facing endpoint across the supported
  text-inference applications.

Initial vLLM work is NVIDIA-first. AMD support is research-only unless a
supported release produces a clear benefit over llama.cpp on accepted AMD
hardware.

## Why keep vLLM separate

llama.cpp and vLLM solve overlapping problems with different strengths.
Paracetamol already has a mature llama.cpp path for pinned GGUF content,
reasoning controls, speculative decoding, long contexts, and AMD hardware.
Replacing it would discard working behavior without evidence of a better
result.

vLLM is interesting because its native PyTorch and safetensors path may fit
NVIDIA releases and quantization formats that are not best served through
GGUF. Its scheduler may also improve aggregate throughput when several
requests run at once. Neither property implies a single-stream speedup, so
both must be measured on the actual target hardware.

The first implementation should expose a narrow reviewed server surface. It
must not make every vLLM command-line option part of Paracetamol's public
interface.

## Application and image boundary

vLLM should be a new application in `internal/application/`, with its own final
image, entrypoint, capabilities, data directory, and gateway allocation. The
image must be built locally from exact base, CUDA, PyTorch, vLLM, and Python
dependency pins. Paracetamol must not depend on a prebuilt vLLM service image
as its application artifact.

Adding a CUDA application requires a deliberate refinement of the current
shared-runtime rule:

- every ROCm application must continue to use the one project ROCm tuple;
- vLLM must use one explicit, internally consistent CUDA, PyTorch, and vLLM
  tuple;
- CUDA and ROCm application images must not inherit one another's payload;
- native host control remains Go and must not acquire a Python runtime.

The initial application should support server operation only. Offline
inference, training, arbitrary plugins, and a direct vLLM CLI are out of scope
until a concrete Paracetamol use case needs them.

The existing confinement policy still applies. The container must be
rootless, read-only, capability-free, and restricted by `no-new-privileges`.
Managed model content stays read-only below `/content`. Only application state
may use `/data`, with explicit `tmpfs` mounts for temporary writes. Runtime
model downloads, update checks, and telemetry must be disabled.

## NVIDIA platform boundary

NVIDIA support needs an explicit platform identity and device policy. It must
not be hidden under an AMD profile or added as an unreviewed special case in
the vLLM launcher. The exact profile name and device mechanism should be
chosen when the real host is available.

The runtime must expose only the selected NVIDIA devices. It must not use a
broad all-GPUs shortcut as the durable interface. Whether Podman CDI, explicit
device nodes, or another rootless mechanism is suitable must be established
on the acceptance host and represented in the shared runtime boundary.

The expected CMP 170HX cards are research hardware. Reported identity,
available memory, peer access, topology, and stability must be verified from
inside the constrained application container. The soft unlock and reported
64 GB memory capacity are not acceptance evidence on their own.

Single-card execution comes first. Two-card tensor parallelism may follow only
after explicit device selection works and one card is stable. The two-card
path must record the tensor-parallel layout, PCIe topology, communication
backend, memory use on each device, and whether the workload gains enough to
justify the added complexity.

## Managed model content

The first vLLM model should use a native format that vLLM supports well on the
accepted NVIDIA tuple. GGUF compatibility is not an integration goal because
llama.cpp already owns that path.

Native Hugging Face repositories still pass through Paracetamol's content
trust boundary. Every required config, tokenizer, and weight shard needs an
exact revision, size, SHA-256 hash, destination, and license declaration.
Runtime code must not fill missing files from the network.

vLLM bundles should be owned by the vLLM application rather than reusing
llama.cpp presets. A native checkpoint and a GGUF repack are distinct managed
artifacts even when they descend from the same model. Their gateway model IDs
must not suggest interchangeable quality, context, reasoning behavior, or
sampling defaults unless acceptance proves those properties.

Quantization must be selected from measurements on the actual cards. A format
name or upstream support table is not proof that its kernels are efficient on
the CMP 170HX.

## Gateway integration

The gateway should treat vLLM as another application allocation, using the
same lifecycle rules already applied to llama.cpp and DwarfStar:

- startup freezes a receipt-verified inventory;
- automatic discovery includes vLLM only when its image exists and at least
  one compatible verified model is schedulable;
- explicit application selection remains strict;
- the first matching request starts the backend lazily;
- the backend publishes only an ephemeral loopback port;
- exact ownership labels govern cleanup;
- runtime output is followed with a `vllm |` prefix;
- vLLM initially shares the gateway's one mutually exclusive GPU resource
  pool with llama.cpp and DwarfStar.

The first version should target the gateway's existing Models and Chat
Completions surface. Unknown request fields should continue to pass through.
Any translation for reasoning, tool use, sampling, usage accounting, health,
or cancellation must live in typed application capabilities. Do not add
model-name checks or client-specific request hacks to the generic proxy.

The gateway must never advertise a model merely because vLLM can see its
files. The catalog, verification receipt, application compatibility, selected
profile, selected devices, and built image must all agree.

## Aion research snapshot

An exploratory vLLM 0.28.0 run on the `gfx1151` Aion host established that the
control-plane shape is feasible. It did not establish a reason to integrate
vLLM for AMD.

At an 8K context, the exact Qwen3.8 27B Unsloth Dynamic Q8 GGUF produced about
7.0 to 7.4 generated tokens per second through vLLM. Paracetamol's current
llama.cpp ROCm path produced 7.16 generated tokens per second on the same
single-stream class of request. The result was parity, not an improvement.

That vLLM GGUF run also required the still-open
[text-only Qwen GGUF plugin change][qwen-gguf-pr].
The native Qwen3.8 FP8 path loaded its weights quickly, then reported missing
tuned W8A8 configuration for Aion's GPU. The matching
[upstream Radeon issue][radeon-fp8-issue]
records the resulting first-run compilation and performance problem. A
separate [long-context ROCm split-KV change][rocm-split-kv-pr]
also remains open.

The exploratory allocator reported enough KV capacity for a 256K context, but
no 256K inference acceptance was run because the tested software path depended
on open changes. These results support leaving AMD on llama.cpp and beginning
durable vLLM work on NVIDIA.

This snapshot is dated 2026-08-29. Recheck every upstream status and rerun the
comparison before using it as a current support claim.

## Entry gates

Implementation should begin only when all of the following are available:

1. The NVIDIA host, cooling, power, and rootless device access are stable.
2. The container can verify the exact GPU identity and usable memory without
   broad device exposure.
3. One coherent driver, CUDA, PyTorch, vLLM, and Python dependency tuple has
   been selected from released sources.
4. One native model with immutable content metadata is supported by that
   exact tuple.
5. A small GPU execution probe succeeds before a large model is downloaded or
   started.
6. The chosen model gives vLLM a concrete advantage or fills a capability gap
   rather than duplicating a working llama.cpp bundle.

## Acceptance ladder

Acceptance should proceed in this order:

1. Build the application image locally from clean pins and run its dependency
   checks.
2. Confirm offline startup, read-only root, dropped capabilities, exact
   mounts, no host network publication, and exact device exposure.
3. Run a tiny deterministic CUDA computation inside the final image.
4. Start a small reviewed model on one card and verify health, Chat
   Completions, streaming, cancellation, tool calls, reasoning controls, and
   clean shutdown.
5. Run the target model at short context before attempting 256K.
6. Compare output validity, time to first token, prompt processing, generated
   tokens per second, peak memory, startup cost, and kernel or driver faults.
7. Measure both one request and explicit concurrent worker counts. Report
   aggregate generated tokens divided by total wall time, per-request latency,
   and output truncation.
8. Repeat through the gateway and verify frozen discovery, lazy startup,
   allocation switching, status accounting, ownership labels, and recovery
   from backend failure.
9. Add a two-card tensor-parallel run only after the single-card baseline is
   accepted.

A successful build, import, CPU startup, model load, or synthetic memory
allocation is not GPU inference acceptance.

## Revisit conditions for AMD

AMD vLLM integration becomes worth another pass when released software no
longer needs the open GGUF plugin change, includes tuned kernels for the target
Radeon architecture, and has a released long-context decode path. Even then,
it should be added only if repeatable target-hardware results beat llama.cpp
or provide a capability users cannot get from the existing application.

Until those conditions change, vLLM planning is NVIDIA-first and llama.cpp
remains Paracetamol's AMD inference baseline.

[qwen-gguf-pr]: https://github.com/vllm-project/vllm-gguf-plugin/pull/120
[radeon-fp8-issue]: https://github.com/vllm-project/vllm/issues/52663
[rocm-split-kv-pr]: https://github.com/vllm-project/vllm/pull/45916
