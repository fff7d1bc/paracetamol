# GLM-5.3 Flash DwarfStar feasibility

This is a dated Strix Halo research record from 2026-09-10, not a declaration
of managed model support. The model is **GLM-5.3 Flash**, not full GLM-5.3.
Inspect the current source, catalog and hardware-acceptance records before
reusing these findings with a different image.

## Result in brief

The 89.88 GiB Q2 conversion fits resident on the 128 GB Strix Halo host,
including a 256K context ceiling. At that ceiling, DwarfStar planned
95.96 GiB and the host retained at least 25.02 GiB of available memory
during the test. SSD streaming and memory-guard overrides were not needed.

Ordinary shallow generation reached about 14 tokens/s. A 6,020-token input
took about 90 seconds. A correct retrieval from 30,048 actual input tokens
took 9 minutes 50 seconds. Those rates support casual experimentation, but
are not evidence of coding quality or an advantage over the managed Qwen
models. Allocating 256K is not the same as testing 256K of populated history.

Embedded MTP did not help. In the matched shallow screen it took about 32%
longer per request and produced different greedy output. Keep it off for
this tested tuple.

Do not add it to the gateway's supported model inventory yet. The pinned
DwarfStar server converts GLM tool arguments to strings regardless of their
declared JSON types. This broke a nested-object tool call. Keep the downloaded
model and verified local content pack for direct experiments and a later
retest, without changing the DeepSeek or Qwen defaults.

## Exact software and content

| Component | Tested value |
| --- | --- |
| Control plane | Paracetamol `e211ad87cc729c79b6cfec9d3867a56fba42dffc` |
| DwarfStar | `6289c516273979173abbc062209a81dd3706b804` |
| Image | `localhost/paracetamol:dwarfstar-ubuntu26.04-rocm10.0-6289c51-r9` |
| Image ID | `1c2841f6899e648260313cce63188b4fa468bfd3e6ce8fea2c5dbc11f9234d8e` |
| Runtime | ROCm 10.0 |
| Hardware | Ryzen AI Max+ 395, Radeon 8060S, `gfx1151`, 128 GB LPDDR5X-8000 |
| Host | Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64` |
| Memory policy | 112 GiB TTM/GTT, IOMMU enabled |
| Power policy | Balanced platform profile, CPU `balance_performance` |

The artifact came from the pinned
[Antirez conversion repository](https://huggingface.co/antirez/glm-5.3-flash-gguf/tree/b2fa29d7a6b410db11221c904973967b80b760f5).

| Field | Value |
| --- | --- |
| Repository | `antirez/glm-5.3-flash-gguf` |
| Revision | `b2fa29d7a6b410db11221c904973967b80b760f5` |
| File | `GLM-5.3-Flash-Q2.gguf` |
| Bytes | 96,505,816,384 |
| SHA-256 | `e81fd6241c6e55a64e1e14e47a3eab61a173fa8d7e4b5c1d1848827119705b32` |
| Conversion license | `NOASSERTION`, explicitly acknowledged for this trial |

The pinned conversion repository had no license declaration or model card.
The original model's MIT license is lineage information, not a verified
license declaration for this hosted conversion. Downloading used the normal
staged installer, full size/hash verification and a durable receipt through
an ignored DwarfStar local content pack.

The Q2 filename does not mean that every tensor is two-bit. Metadata inspection
reported architecture `glm5-next`, 320.76 billion logical parameters and 1,412
tensors. The main tensor payload included 49.89 GiB of IQ2_XXS, 31.75 GiB of
Q2_K, 6.54 GiB of Q8_0 and 1.20 GiB of Q4_K, plus BF16/F32 tensors. Results
belong to these exact bytes, not every GLM quantization.

## Memory and test conditions

Each case started a fresh rootless container with read-only root and model
mounts, dropped capabilities, no new privileges, `/dev/kfd` and the single
selected `/dev/dri/renderD128`. The isolated backend tests used an ephemeral
loopback port. The separate public-CLI smoke used an explicitly selected
loopback port. No other GPU workload ran alongside a case.

The existing image and its normal ROCm kernels were used unchanged. The
experimental driver selected `ds4-server` directly to collect a synthetic
trace and, in the separate MTP case, enable upstream's embedded drafter.
The GLM memory guard remained enabled. Its 104 GiB budget was not increased.
There was no SSD expert streaming, cache-precision override, IOMMU change or
host memory-policy adjustment.

The host exposed 125.07 GiB of usable physical memory. Available host memory
was sampled every 500 ms across loading and requests. This is an observed
minimum, not an exact allocator peak. DwarfStar's planned memory is reported
separately. GPU GTT and host usage are not added together on unified memory.

| Allocated context | Planned resident memory | Minimum host available memory |
| --- | ---: | ---: |
| 4,096 | 92.84 GiB | 28.53 GiB |
| 32,768 | 93.21 GiB | 27.77 GiB |
| 262,144 | 95.96 GiB | 25.02 GiB |

The first model preparation covered 89.87 GiB of tensor spans in 18.55 seconds.
At 32K, the plan comprised 89.87 GiB of resident model, 0.37 GiB of compact
F16 DSA cache and 2.98 GiB of other buffers. That explains why this conversion
did not show a large multiplier over its file size. It is not a rule that can
be carried over to a different model architecture or backend.

At 256K, compact F16 cache was 2.92 GiB and other buffers were 3.16 GiB.
No matching GPU, SVM, protection, reset, timeout or OOM faults appeared in
the captured kernel windows. Available-memory sampling stayed steady during
the long input. Swap use remained about 86.4 MiB during that case.

## Throughput and correctness

The fixed-length screen sent three sequential, uncached, temperature-zero,
seed-42 requests with thinking disabled. Each had a 35-token prompt and
generated 256 tokens of partial Go code. At the 32K ceiling, HTTP times were
19.238, 19.241 and 19.240 seconds. Generated tokens divided by total HTTP time
were 13.31 tokens/s, including prompt processing. Backend decode was about
14.03 tokens/s.

These outputs deliberately exhausted a fixed token budget. They were not
complete programs and were not graded as coding tasks. There was one server
slot and one request at a time. No Pi or Maki quality comparison was run.

The 256K-ceiling run repeated the same short performance and protocol checks.
Short-prompt generation remained about 14 tokens/s, and the same tool-type
and capitalization failures reproduced. Its separate long request populated
30,048 tokens and recovered both the start and middle keys exactly, without
reasoning or truncation. Prefill took 588.859 seconds at 51.03 tokens/s.
The nine-token answer decoded at 10.32 tokens/s and total HTTP wall time was
589.769 seconds. That nine-token output is too short for a strong deep-context
decode estimate. The long-input result is a retrieval smoke, not a broad
long-context quality evaluation.

The separate protocol screen at 32K found the following.

| Check | Observation |
| --- | --- |
| Thinking disabled | No reasoning field content, but exact lowercase `true` became `True` |
| Thinking enabled | Correct `437` for 19 times 23, with separate reasoning |
| 6,020-token retrieval | Correct `LONG_OK`, 89.841 seconds HTTP wall time |
| Nested tool call | Correct function name, wrong JSON argument type |
| Tool-result continuation | Correct `TOOL_OK` after a synthetic result |
| Streaming | Correct `STREAM_OK`, usage present, `[DONE]` received |
| Responses API | Correct `RESPONSES_OK` |
| True prefix continuation | 713 cached tokens, second request 0.657 seconds |
| Cancellation | Stream cancelled and a fresh request succeeded, with the same capitalization failure |

The exact-output failures remain failures. They do not indicate a GPU crash
or failed thinking-off control. Continuing after the malformed tool arguments
was an independent protocol probe, not approval to execute an invalid tool.

### Embedded MTP

GLM carries its draft block in the target GGUF. This test used upstream
`--mtp`, not the separate DeepSeek DSpark support file. Both conditions below
used the same 32,768-token ceiling, exact request JSON, seed, temperature,
image and model, with three sequential 256-token responses. Neither row
used tracing or per-cycle timing logs.

| Strategy | Mean HTTP time | Backend decode | Generated tokens / total HTTP time |
| --- | ---: | ---: | ---: |
| Ordinary | 19.240 s | 14.03 tokens/s | 13.31 tokens/s |
| Embedded MTP | 25.349 s | 10.51 tokens/s | 10.10 tokens/s |

MTP increased request time by 31.8%. An earlier instrumented MTP pass
averaged 25.354 seconds, so removing `--mtp-timing` and `--trace` did not
change the conclusion. Each instrumented performance request accepted
110 of 143 proposals, or 76.9%. High draft acceptance alone was not enough
to offset this implementation's verification and draft cost on the tested
ROCm path.

MTP's partial code was coherent but was not byte-identical to the ordinary
greedy output, consistently across all three repetitions. No greedy
equivalence or nonzero-temperature sampling equivalence was established.
The instrumented case repeated the 6K-input and protocol checks, with the
same parser and capitalization failures. It had no matching kernel faults.

This is a small performance screen, not a claim that MTP is inherently bad
for GLM, other quants, other backends or longer generation workloads.

## Direct experiments

The actual public launcher and container entrypoint were smoke-tested at a
4,096-token ceiling. They loaded the model and completed three 256-token
responses in 19.24 seconds each, with the same text as the ordinary control.
The short exact-lowercase check still returned `True`. This confirms the
launch path, not a passing agent or exact-instruction quality evaluation.

The existing exact-file override can select this GGUF without changing a
built-in preset. Stop the gateway and other GPU workloads first. A direct
server does not participate in the gateway's one-allocation scheduler, and
this model cannot safely coexist with a second large resident model on the
tested host.

After installing and verifying the exact artifact, use its real absolute
path in place of the placeholder below. Keep a conservative context for the
first run.

```bash
./paracetamol run dwarfstar server \
  --model /ABSOLUTE/PATH/GLM-5.3-Flash-Q2.gguf \
  --context 32768 --output-tokens 1024 \
  --listen 127.0.0.1 --port 8000
```

This starts a direct loopback server on port 8000. It does not make GLM a
gateway model or add it to Pi/Maki's picker. Typed tool calls remain affected
by the parser problem described below. Do not pair it with DeepSeek's DSpark
support file. GLM's embedded MTP is a different upstream option and was
tested only through the isolated driver.

For a deterministic text-only check against that direct server,

```bash
curl http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"glm-5.3-flash","messages":[{"role":"user","content":"Reply exactly STREAM_OK and nothing else."}],"thinking":{"type":"disabled"},"temperature":0,"max_tokens":128}'
```

## Tool argument blocker

The tool schema declared `job` as an object containing a string array and a
boolean. The client received this decoded argument value instead.

```json
{"job":"{\"paths\": [\"alpha.go\", \"beta.go\"], \"readonly\": true}"}
```

The expected value was an object, not a JSON string containing an object.

```json
{"job":{"paths":["alpha.go","beta.go"],"readonly":true}}
```

The retained synthetic trace shows the model generated a JSON object inside
`<arg_value>`. The server then quoted that object in the outgoing arguments.

In the pinned
[server parser](https://github.com/antirez/ds4/blob/6289c516273979173abbc062209a81dd3706b804/ds4_server.c#L5850),
`parse_glm_generated_message_ex` passes a hardcoded string flag for every
argument. Its own regression fixture expects a numeric timeout as a string.
The live failure therefore has an identifiable server-side cause and should
not be counted as evidence that the model cannot generate a nested argument.

This matches upstream [issue 569](https://github.com/antirez/ds4/issues/569).
[PR 1016](https://github.com/antirez/ds4/pull/1016), at
`9db96f0e96928e2245664b83e0498145e86a4c25`, proposes schema-aware recovery and
was open on the test date. It was inspected, not applied or accepted on this
host. Blindly treating every JSON-looking string as a typed value would also
be wrong, since tools can legitimately require strings such as `001` or `true`.

## Revisit gate

Before advertising GLM through the gateway or generated agent catalogs,
repeat the typed-tool test with a reviewed upstream parser fix. Cover strings,
numbers, booleans, nested objects, arrays, malformed values, buffered and
streamed calls, and tool-result continuation. Preserve DeepSeek's existing
DSML behavior and verify its tools as a regression control.

Then test real Pi/Maki exchanges and model-specific sampler and reasoning
defaults. Adding a second DwarfStar model also requires deliberate model
selection and safe same-application replacement in the gateway's one resident
allocation. Do not advertise two resident models through a backend that has
actually loaded only one.

Quality at Q2, vision, concurrent serving, other GPU classes and fully populated
256K histories were not accepted by this feasibility screen. The result
answers whether the model can fit and execute, not whether it should replace
the default Qwen or DeepSeek paths.

## Retained evidence

The verified GGUF remains installed outside the checkout. The local content
pack, exact launch commands, model inspection, HTTP requests/responses,
synthetic trace, memory samples and kernel windows are retained under
`~/.local/share/paracetamol/apps/acceptance/results/20260910-glm53-flash/`.
See `result-manifest.md` there for the case map. Existing models and images
were not pruned, and the normal gateway was restored after testing.
