# Applications

[Documentation index](../README.md)

Every application follows the same basic path:

```text
build  ->  content install  ->  run
```

Use the [README quick start](../../README.md#quick-start) when you just want one
working setup. This page is for choosing between the applications and
understanding the bits that differ once they are running.
Network examples use the documentation address `192.0.2.10` and hostname
`gateway.example`; substitute your server's real address.

For a shorter walkthrough made of copyable commands, use the built-in guide:

```bash
./paracetamol guide
./paracetamol guide comfyui
./paracetamol guide llama-cpp
```

## ComfyUI

ComfyUI is the flexible option. Use it for graph workflows, the broadest
Paracetamol model selection, and imported models or workflows.

Prepare and run the default image-generation recipe:

```bash
./paracetamol build comfyui
./paracetamol content install comfyui image
./paracetamol run comfyui
```

The image includes pinned ComfyUI-GGUF, rgthree-comfy, and the exact Manager
version required by the pinned ComfyUI source. These copies move with the
locally built image, so a routine Paracetamol update does not depend on a custom
node updating itself at startup. Enable Manager through ComfyUI's upstream
flag after the argument separator:

```bash
./paracetamol run comfyui -- --enable-manager
./paracetamol run comfyui \
  --listen 192.0.2.10 \
  -- --enable-manager
```

Arguments before `--` belong to Paracetamol; arguments after it are passed to
ComfyUI. Paracetamol rejects forwarded versions of options it owns, including
listen addresses, ports, data directories, and `--cpu`.

The image remains read-only. Manager stores custom-node source under
`apps/comfyui/custom_nodes/` and installs its Python dependencies into the
separate persistent environment at `apps/comfyui/custom-node-python/`. That
environment can override an image package for ComfyUI runs, but cannot modify
the pinned environment inside the image. Custom nodes still execute arbitrary
third-party code, so review what Manager proposes before installing it.

A persistent custom node with the same directory name as a bundled node takes
precedence. The startup summary reports it under `persistent overrides`.
This keeps an existing Manager-installed copy usable and avoids loading two
copies of the same node. Remove or relocate that persistent directory when
you want the image-pinned copy to take over again.

Manager decides whether installation is safe from the host address Paracetamol
publishes, rather than ComfyUI's unavoidable wildcard bind inside the private
container network. Registered custom-node installation is available when the
host publication is loopback. A non-loopback listener can run already
installed nodes, but Manager keeps software installation disabled because
Paracetamol does not add authentication to ComfyUI.

For remote administration, keep the default loopback publication and carry it
through SSH:

```bash
# On the GPU host:
./paracetamol run comfyui -- --enable-manager

# On the workstation where the browser runs:
ssh -N -L 8188:127.0.0.1:8188 gateway.example
```

Then open `http://127.0.0.1:8188`. Stop ComfyUI before switching between this
loopback administration run and a deliberately exposed non-loopback run.

Other ComfyUI options can be forwarded in the same way:

```bash
./paracetamol run comfyui --profile rdna4 -- --lowvram
./paracetamol run comfyui --disable-bundled-extensions
```

`--disable-bundled-extensions` disables both ComfyUI-GGUF and rgthree-comfy.
It does not disable nodes installed persistently through Manager.

To expose two cards to one ComfyUI process, select both explicitly:

```bash
./paracetamol run comfyui \
  --render-node /dev/dri/renderD128 \
  --render-node /dev/dri/renderD129
```

Simply exposing both cards does not change an existing graph. Use ComfyUI's
built-in nodes under `Advanced > multigpu` to make the graph place work:

- `Select Model Device`, `Select CLIP Device`, and `Select VAE Device` put
  those components on chosen cards. Splitting components this way is the
  useful option when the complete workflow is too large for one card.
- `MultiGPU CFG Split` distributes CFG work across cards. Add it after LoRA
  loaders and other model-changing nodes so every worker sees the final model.
  It clones the model to each selected card, so it is a throughput tool rather
  than pooled VRAM for one oversized diffusion model.

Use only cards with the same supported architecture in one process. Paracetamol
checks that condition at startup.

See [Content](content.md#comfyui-content) for curated and imported workflows,
model families, and exact bundle installation.

## llama.cpp

Use llama.cpp for local text models, an OpenAI-compatible API, or an
interactive terminal session.

For the dense model on smaller hosts:

```bash
./paracetamol build llama-cpp
./paracetamol content install llama-cpp qwen3.8
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-27b-mtp-ud-q8-k-xl
```

Startup prints the matching inspection command. While the server is running,
use it to produce one block suitable for a bug report or a discussion of the
exact model configuration:

```bash
./paracetamol status llama-cpp \
  --model unsloth-qwen3.8-27b-mtp-ud-q8-k-xl
```

This reports the live image, resolved GPU profile, GGUF provenance, context,
template, reasoning and sampling ownership, MTP or DFlash depth, cache and
Flash Attention choices, model-load policy, and the exact running llama.cpp
command. Gateway sampling policy is identified separately from native engine
defaults. Inspect `status gateway --requests N` for the values actually
supplied on a request.

For the Muse Glimmer comparison:

```bash
./paracetamol content install llama-cpp muse-glimmer
./paracetamol run llama-cpp server \
  --preset meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k
```

The dense Qwen3.8 Q8 MTP preset starts at 262144 tokens and verifies up to
three tokens from its embedded prediction heads. Flash-Next also starts at
262144 tokens, without MTP. Every managed preset retains F16 target K/V
cache. `--context` overrides the catalog default. This is a ceiling, not an
amount of prompt text fed to the model. Request work follows the tokens sent.

Use 128K or 64K when memory matters more than the full native window:

```bash
# Smaller working set:
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-27b-mtp-ud-q8-k-xl \
  --context 65536

# Reduced but still substantial working set:
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-27b-mtp-ud-q8-k-xl \
  --context 131072
```

In router mode, `--context` overrides every loaded preset, not only the model
named by the next request. That is reasonable for a router containing only
256K agent presets. Omit it for a mixed router so smaller-context presets
retain their catalog-owned limits.

The generated agent-client model maps describe the 256K catalog default. Keep
the server at that default when using a launcher; a manual server override
does not silently rewrite client context metadata.

### Models, presets, and request settings

A model and a Paracetamol preset are related, but they are not interchangeable:

| Layer | What it owns | Examples |
| --- | --- | --- |
| GGUF model | Architecture, trained weights, and quantization | Dense Qwen3.8 27B Dynamic Q8_K_XL or Flash-Next Dynamic Q4_K_XL |
| Paracetamol preset | Model artifact, context size, and required model runtime policy | Chat template, Jinja, profile-specific Flash Attention, K/V cache and model loading, MTP or DFlash settings |
| Server launch | Machine and service policy for this run | ROCm or Vulkan, hardware profile, render nodes, listen address, port |
| API request or client | The current task and generation behavior | System message, conversation history, temperature, top-p, maximum tokens |

The API calls the router selector `model`, but its value is a Paracetamol preset
identifier. It selects both the GGUF and the runtime policy Paracetamol has
validated for it. The task instructions and output rules remain part of the
request messages.

Text preset IDs start with the pinned GGUF publisher's Hugging Face account.
For example, `unsloth-` identifies Unsloth's Qwen conversion, and `meta-models-`
identifies Meta's Muse conversion. The prefix
names the artifact source, not necessarily the base-model developer. Old
unprefixed API model IDs and exact bundle selectors no longer work. Installed
artifacts and verification receipts keep their original paths and IDs.

### Choosing a managed Qwen preset

Regular Unsloth Flash-Next at `xhigh` is the exported Maki default when it is
advertised by the gateway. Dense Qwen3.8 27B remains a smaller alternative.
Its MTP Q8 preset is the fallback when regular Flash-Next is unavailable.
Qwen3.6 has been retired from the catalog, recipes and managed clients.
Existing Qwen3.6 files are not deleted automatically.

| Preset | What changes | Good use |
| --- | --- | --- |
| `qwen-qwen3-0.6b-q8-0` | Tiny 0.6B model | Startup, API, and GPU-offload smoke tests |
| `unsloth-qwen3.8-27b-ud-q8-k-xl` | Dense 27B Dynamic Q8_K_XL | Qwen3.8 non-speculative control |
| `huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl` | Huihui modified weights from Unsloth's Dynamic Q8_K_XL | Optional non-MTP dense variant with the same runtime policy |
| `unsloth-qwen3.8-27b-mtp-ud-q8-k-xl` | Same GGUF using its embedded MTP heads | Dense coding-agent alternative |
| `unsloth-qwen3.8-27b-ud-q4-k-xl` | Dense 27B Dynamic v3 Q4_K_XL at 128K | Optional smaller non-speculative control |
| `unsloth-qwen3.8-27b-mtp-ud-q4-k-xl` | Same Dynamic v3 Q4_K_XL GGUF using its embedded MTP heads | Optional smaller agent preset |
| `unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl` | Sparse 125B-A6B Dynamic Q4_K_XL, no MTP | Harness default at xhigh on the accepted 128 GB Strix Halo path |
| `huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl` | Huihui abliterated v2, Unsloth-derived Dynamic Q4_K_XL | Optional modified weights with the same Flash-Next runtime policy |

Keep whichever model succeeds on representative tasks rather than choosing
from the parameter count or quantization name alone.

The guided `qwen3.8` recipe intentionally installs only Dynamic Q8_K_XL. Use
`./paracetamol content install llama-unsloth-qwen3.8-27b-ud-q4-k-xl` when the 16.35 GiB
Dynamic v3 Q4_K_XL capacity and throughput tradeoff is useful. The Q4 presets
do not change the recommended model or any client default.

Huihui's dense 27B variant is a separate 29.30 GiB download, not a patch to
the installed Unsloth model:

```bash
./paracetamol content install llama-huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl
```

Select `huihui-ai-qwen3.8-27b-abliterated-ud-q8-k-xl` in Maki or an HTTP
request after restarting the gateway. It shares the regular dense model's
managed template, off/low/medium/xhigh reasoning, preserved history,
mode-dependent sampling, 256K context and F16 KV. This preset does not use
MTP. The [pinned model card](https://huggingface.co/huihui-ai/Huihui-Qwen3.8-27B-abliterated-GGUF/blob/1c69098e9c32db6dc06fc48f1d6cc75ad07da89e/README.md)
identifies its Unsloth UD lineage and declares Apache-2.0. It is not the
Swift-derived variant. Modified weights can change task quality even when
the serving policy is identical. Regular Flash-Next remains the default.
The [acceptance record](../hardware-acceptance.md#huihui-dense-qwen-and-catalog-retirement-2026-10-22)
includes a native Maki xhigh task that produced the correct file but the wrong
final answer. API compatibility is not a claim of equal coding reliability.

The previously curated Swift adaptations were retired after a long Pi coding
session failed to finish while the Unsloth Flash-Next completed the same
request. See the [retirement record](../swift-retirement.md) for the observed
behavior and limits of that comparison. Installed Swift files are not removed
automatically.

The Flash-Next family has its own guided recipe because it has a different
architecture and operating envelope. The recipe installs the accepted
103.7 GiB, four-shard Dynamic Q4_K_XL variant:

```bash
./paracetamol content install llama-cpp qwen3.8-flash-next \
  --accept-license --acknowledge-license-risk
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl
```

Flash-Next has 125B total parameters, about 6B active model parameters, and a
large ngram token embedding, as described by the pinned
[official model card](https://huggingface.co/Qwen/Qwen3.8-Flash-Next/blob/de4b8e4d43b917e7706784d8bb445c9af86a3540/README.md).
On the accepted Strix Halo profile, ROCm uses buffered CPU reads for
`per_layer_token_embd.weight`. The rest of the weights use device allocation
with inherited unified-memory allocation disabled for this model process.
The policy also selects a 2048-token microbatch and no RAM prompt archive.
Four request slots share a 262144-token KV pool by default. This allows
concurrent conversations, not four independent 256K allocations. A lone
conversation can still use the full ceiling. `--slots 1` restores the
single-request, non-unified-cache policy. Reuse of the current live prefix
still works. F16 K/V precision and Flash Attention are retained. See
[gateway concurrency](../gateway.md) for the distinction from `--models-max`.
On `gfx1151` ROCm, the patched llama.cpp also uses selected-key HIP attention
for Flash-Next's F16 QSA shape. A compact single-sequence graph intersects
selected IDs with upstream's causal and sequence visibility mask without
building a dense selection mask for every layer. Scoped matrix kernels
also accelerate this model without changing K/V precision, quantization or
sampling. Unsupported cache layouts keep the ordinary attention graph.
Vulkan and older Qwen families retain their existing paths.
Upstream llama.cpp owns the pooled indexer-key cache and its state lifecycle.
There is no additional private derived-key cache.
The scoped pipeline also avoids full convolution-history copies during
prefill, combines normalization and injection work, and groups compatible
decode projections. Single-token decode also combines convolution, recurrent
state updates and normalization, and folds HC gates into the existing
normalization path. These changes retain F32 residuals and accumulation.
The fork's reduced-precision matrix and residual paths are not enabled.
The [hardware record](../hardware-acceptance.md)
contains the paired prefill measurements and correctness limits.

Direct startup selects ROCm on Strix Halo, and a ROCm router or gateway
includes the verified preset. Keep `--models-max 1` on a 128 GB host so the
native router replaces its resident model when you switch. The model count
does not predict whether two models fit. Other models retain their normal
allocation, microbatch and cache settings, including after a switch back
from Flash-Next.

`--backend vulkan` keeps the earlier CPU lazy-mmap embedding path. The ROCm
exception is restricted to Strix Halo, validated on the host's exact selected
render node and again inside the container. Unknown hardware detection does
not enable it. Ordinary ROCm allocation still corrupts meaningful output for
this model, so do not transfer these results to a different allocation recipe.
Native `benchmark llama-cpp throughput` refuses this ROCm preset because llama-bench
does not apply the accepted server/CLI allocation policy. The [acceptance
record](../qwen3.8-flash-next-direct-reader-feasibility.md) explains the
memory saving and modest high-context prefill gain, without claiming faster
decode or general coding quality.

The preset starts at the model's native 262144-token context and exposes off,
low, medium, and xhigh reasoning. The Maki example starts regular Flash-Next at
`xhigh`. The shared template and gateway fallback remain `medium` for HTTP
callers that omit reasoning. Thinking uses
temperature 1.0, top-p 0.95, top-k 20, and min-p 0.05. Off uses temperature
0.7, top-p 0.8, top-k 20, min-p 0, and presence penalty 1.5. Current pinned
upstream llama.cpp supports the target architecture but not its MTP heads, so
Paracetamol deliberately provides no speculative alias. Q4_K_XL is preferred on the
accepted 128 GB Strix Halo host because it improves Unsloth's quant-fidelity
measurements over IQ4_XS while retaining 34 GiB of available memory after a
199872-token retrieval run. The Q5_K_XL payload exceeds the measured remaining
capacity margin. The 87.2 GiB IQ4_XS conversion was retired because it still
exceeds practical 32 and 64 GB device classes while Q4_K_XL fits the accepted
128 GB system and provides the stronger quantization.

Huihui's abliterated v2 is a separate weight option based on Unsloth's
UD-Q4_K_XL conversion, not the Swift-derived series in the same repository.
It uses the same `qwen3.8.jinja`, native reasoning choices, preserved history,
gateway sampling policy, 256K context and F16 KV. The ROCm reader and all
Flash-Next optimizations are shared. There is no Huihui-specific engine patch.

```bash
./paracetamol content install llama-huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl \
  --acknowledge-license-risk
```

Select `huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl`
in Maki or an HTTP request. It stays non-default and keeps the medium
HTTP fallback. Choose its reasoning level in the client. It does not imply
equal coding quality to the original.
The [pinned model card](https://huggingface.co/huihui-ai/Huihui-Qwen3.8-Flash-Next-abliterated-v2-GGUF/blob/43b4c1a8871afcc27e37bf996a431528b3d957e5/README.md)
describes modified weights with reduced refusal behavior. Its linked license
file is absent, so the download retains the explicit `NOASSERTION` gate.
The [hardware acceptance log](../hardware-acceptance.md) records the tested
scope rather than treating protocol compatibility as a quality benchmark.

On the accepted Fedora Strix Halo host, start the optional dense MTP preset with the
normal ROCm backend unless the local workload favors Vulkan:

```bash
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-27b-mtp-ud-q4-k-xl
```

The current Dynamic v3 artifact passed ROCm startup, embedded-MTP, off,
medium, `xhigh`, nested-tool, and tool-result-continuation checks on Strix
Halo. Against the earlier preview artifact, a matched ROCm screen improved
aggregate MTP generation by 3.3% (17.73 versus 17.16 tokens/s) while reducing
prompt processing by 1.9% (197.65 versus 201.54 tokens/s). Native decoding
improved 2.4%, while native `pp512` fell 7.8%. Five fresh Pi 0.84.2 tasks at
128K and medium solved three first attempts. Both missed tasks were rerun with
the earlier artifact: it reproduced the same cancellation failure and took
43.5 minutes before also failing the hard SELinux task, versus 17.2 minutes
for Dynamic v3. This accepts the updated bytes without claiming a broad
quality improvement from five stochastic tasks.

The earlier preview artifact produced the backend observations below. At a 4K
populated prompt, depth-three Vulkan generated 22.22 tokens/s and
finished the two requests in 78.19 seconds; ROCm generated 16.98 tokens/s and
took 86.04 seconds. At 32K, Vulkan generated faster (20.07 versus 15.44
tokens/s) but slower prefill made its whole request 3.5% slower (170.29 versus
164.59 seconds). The mixed result does not justify a backend override.

Against the retired Q4_K_M path, the preview Dynamic Q4_K_XL stayed within
12.4% on these whole requests. Against the dense Dynamic Q8_K_XL on matched
ROCm requests, it generated 25% faster at 4K and 30% faster at 32K. A
medium-effort Pi run at
64K and a nested-tool router probe both passed. Two later hidden-graded
medium-effort Pi runs at the reviewed 128K default also passed without
compaction. This is useful single-host evidence, not an equivalent-quality
claim against Dynamic Q8_K_XL or proof that the model fits a dedicated 32 GiB
card.

For the quality side of that choice, see the dedicated
[Qwen3.8 Dynamic Q4 versus Q8 coding-agent
comparison](qwen3.8-dynamic-quant-comparison.md). It reports hidden-graded
medium and `xhigh` results task by task, separates strict completion from a
correct patch that timed out, and explains why the findings apply to these
exact Unsloth Dynamic files rather than every model carrying a Q4 or Q8 label.

Qwen3.8 uses Paracetamol's reviewed copy of the pinned official base-model Jinja
template instead of the template embedded in the Unsloth GGUF. It keeps Qwen's
official message and tool format, but the Unsloth copy silently aliases generic
`high` to native `xhigh`. Qwen3.8's real effort levels are low, medium, and
xhigh, plus a separate off toggle. The managed template changes the
omitted-effort fallback from upstream xhigh to medium; it does not invent a
high level. Upstream `xhigh` adds a short system steering instruction asking
for careful validation and alternatives, while `medium` adds no reasoning
instruction. The prompt addition itself is small; the practical risk is the
much longer reasoning it can trigger.
`reasoning_effort: none` disables thinking. The gateway resolves that
mode before applying sampling defaults. Low, medium, and xhigh use the official
thinking policy: temperature 1.0, top-p 0.95, top-k 20, min-p 0, presence
penalty 0, and repeat penalty 1. Off uses Qwen's non-thinking policy:
temperature 0.7, top-p 0.8, top-k 20, min-p 0, presence penalty 1.5, and repeat
penalty 1. Explicit request sampling fields override the corresponding gateway
defaults. Direct llama.cpp endpoints bypass this policy and use native engine
defaults unless the request supplies values.

The pinned
[official Qwen3.8 model card](https://huggingface.co/Qwen/Qwen3.8-27B/blob/1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0/README.md)
reports large improvements over Qwen3.6-27B on its coding and agent harnesses.
Treat those numbers as candidate-selection evidence, not acceptance of this
quantization or local client behavior.
The accepted medium-effort tool run and the neutral omitted-effort behavior
support keeping dense Qwen3.8 MTP as the smaller managed-client alternative. The
catalog currently serves text only; native vision needs the optional projector
and separate multimodal acceptance.

Initial Strix Halo acceptance measured 19.12 generated tokens/s for the
depth-three MTP preset versus 7.20 for its non-speculative control on one fixed
256-token workload. The first Pi run selected generic `high`, which the old
Unsloth template silently mapped to native `xhigh`; it solved `re-align` but
took 1,514 seconds and 26 tool calls. Under normalized native defaults,
Qwen3.8 medium solved the same task in 609.8 seconds and 16 tool calls. The two
runs are evidence that the reasoning setting materially changes this model,
not a before-and-after performance benchmark.

Early community experience remains unusually sensitive to the exact template
and reasoning defaults. The release-day
[Qwen3.8 megathread](https://www.reddit.com/r/LocalLLaMA/comments/1voojjz/megathread_qwen_38_27b_release_day/)
collects configurations, template alternatives, and linked experience reports;
the rapidly evolving
[Qwen3.8 27B release discussion](https://www.reddit.com/r/LocalLLaMA/comments/1voa3ch/qwen_38_27b_released_please_share_your_experience/)
is useful for finding failure modes, but its different quantizations, engines,
templates, and settings are not comparable benchmark evidence. One prominent
[community template](https://huggingface.co/froggeric/Qwen-Fixed-Chat-Templates/tree/9f14778c92c3b5ed3e0738085694c0d3452802dd)
claims fixes for empty historical thinking, stringified tool arguments,
mid-conversation instructions, and agent-loop recovery. It also changes prompt
policy well beyond Paracetamol's medium-default adaptation; even its `high`
choice aliases to Qwen3.8 `xhigh`. Paracetamol therefore measures the pinned
official-template adaptation first. A community-template run belongs in a
later, explicitly labelled A/B with the same model bytes and runtime inputs.

MTP is a decoding optimization, not a reasoning mode or a more capable model.
Its prediction heads propose several future tokens and the main model verifies
them. Accepted drafts can reduce generation time; poor acceptance can erase
the gain. MTP does not accelerate prompt ingestion, and the client's
`--thinking` switch is independent of it.

For an MTP comparison, use the server API and measure complete response wall
time, output validity, and draft acceptance on the same prompts. The native
`benchmark llama-cpp throughput` path deliberately rejects managed speculative presets
because `llama-bench` does not apply their MTP or DFlash policy. A matching
non-speculative preset remains the useful control.

See the launch policy and catalog footprint for every installed managed
preset:

```bash
./paracetamol content list models --details
```

Paracetamol presets do not store a general system prompt or persona. Ordinary
task sampling belongs in the caller because it can change without changing the
model's safe runtime setup. Qwen3.8 is a deliberate model-policy
exception. Its gateway presets select model- and mode-specific defaults
for each Chat Completions request. Direct engine endpoints do not apply that
catalog policy. Send a `system`
message and any sampling overrides in the OpenAI-compatible request, and send
the complete message history when the conversation should continue:

```json
{
  "model": "unsloth-qwen3.8-27b-mtp-ud-q8-k-xl",
  "messages": [
    {
      "role": "system",
      "content": "You translate technical Japanese into concise English."
    },
    {
      "role": "user",
      "content": "Translate this text..."
    }
  ],
  "temperature": 0.2,
  "max_tokens": 1024
}
```

Use an OpenAI-compatible API client or a small task-specific harness for
automation, system messages, sampling controls, and conversation history. A
new managed preset makes sense when a model requires stable context, template,
Jinja, Flash Attention, or speculative-decoding policy. A reusable writing
persona or summarization instruction does not need a catalog preset. Do not
add a hardware profile for task tuning either. In Paracetamol, profiles describe
GPUs such as Strix Halo or RDNA 4.

For Qwen3.8, the gateway selects the catalog's thinking or non-thinking
sampling policy, so native clients receive the same defaults without
duplicating them. Explicit request fields remain authoritative.

For a quick human check without an API client, run the model directly in a
terminal:

```bash
./paracetamol run llama-cpp cli --preset unsloth-qwen3.8-27b-mtp-ud-q8-k-xl
```

CLI mode owns an interactive conversation in that terminal. API callers must
send the complete message history when they want a conversation to continue.

List the exact managed variants and their launch policy before installing a
large model:

```bash
./paracetamol content list models --details

# Install every managed llama.cpp model in one resumable operation.
./paracetamol content install llama-cpp all

# Or install the tiny startup and GPU-offload smoke test.
./paracetamol content install llama-qwen-qwen3-0.6b-q8-0
./paracetamol run llama-cpp server --preset qwen-qwen3-0.6b-q8-0
```

The Gemma 4 preset starts at its native 256K and is maintained for agent use:

```bash
./paracetamol content install llama-ggml-org-gemma4-31b-it-q8-0-mtp
./paracetamol run llama-cpp server --preset ggml-org-gemma4-31b-it-q8-0-mtp
```

Use `--context 131072` or `--context 65536` when the full KV cache leaves too
little memory for the model, runtime buffers, or other workloads. Its
official embedded template handles developer instructions, structured tool
calls, tool results, and reasoning turns through llama.cpp's chat-template
path. It appears in both generated client model maps and supports the same
function-tool protocol. Paracetamol does not advertise a model-native reasoning
selector for Gemma 4.

### Tool-using clients

Managed Qwen, Gemma 4, and Muse Glimmer agent presets enable llama.cpp's Jinja
engine. This is required for structured OpenAI-style tool calls. Every Qwen3.8
preset, including Huihui, selects the same managed `qwen3.8.jinja` and retains
reasoning history. Gemma 4
uses Google's canonical embedded template. Muse uses Meta's later pinned ATEM
template from the base repository because its unchanged official GGUF still
embeds the original release template. Paracetamol does not replace these
model-specific protocols with a generic one.

The Qwen3 0.6B preset follows the same protocol and is useful for a cheap API
smoke test, but it is too small to treat as a dependable repository agent.
Template support means a client and server can exchange tool calls; it does
not make every Qwen variant equally capable of choosing and using tools.

### Inference gateway

Agents and small application integrations normally send a model name with
every request. Use the native gateway as their shared, least-ambiguous entry
point:

```bash
./paracetamol run gateway
```

With no application option, the gateway discovers llama.cpp when its image is
built and at least one compatible verified model is present. Select it
explicitly with `-a` or `--application` to require it:

```bash
./paracetamol run gateway -a llama-cpp
```

The gateway advertises a frozen receipt-verified snapshot, but starts no
backend until the first Chat Completions request. It keeps one private llama.cpp
router allocation resident. The foreground terminal
follows the active container's output with an application prefix. `status
gateway` reports the snapshot, current allocation, and process-lifetime
privacy-safe usage totals. Add `--requests N` for recent session and request
details. Direct llama.cpp server commands remain useful for engine diagnostics, isolated
benchmarks, and API features outside the gateway's intentionally small
surface. The complete lifecycle and failure policy is in the [gateway
design](../gateway.md).

The gateway starts its llama.cpp router with one loaded model allowed. The
limit counts model children and is not a memory-fit calculation. Use
`--models-max 2` only when the exact pair of weights and contexts is known to
fit; otherwise the one-model default unloads the idle child before loading a
different preset.

### Native clients

Maki is the maintained configuration example. Install Maki 0.6.3 or newer
separately on macOS or Linux, then download the native provider and startup
preferences from the gateway. The [Maki setup guide](remote-maki.md) covers
fresh configuration, merging into an existing setup, credentials and refreshes.
No Paracetamol installation is needed on the client.

The export lists only verified coding-capable models from the running gateway's
frozen inventory. It includes their context limits, output ceilings and native
reasoning choices. Refresh the files explicitly when that inventory changes.
Newly installed content first requires a gateway restart.

Publish on one trusted LAN address and restrict its port with the host
firewall. The same port remains available on local IPv4 loopback.

```bash
./paracetamol run gateway --listen 192.0.2.10 --port 7455
```

Using `--port` without `--listen` selects loopback for that invocation, even
when configuration normally publishes another address. Use both flags when
changing a LAN publication. Optional [Bearer authentication](../gateway.md#optional-authentication)
protects both inference and configuration downloads. Plain HTTP still exposes
credentials and prompts to the network.

Native Maki runs with normal user access. There is no Paracetamol filesystem
or network sandbox, runtime installation, credential store or session manager.
Maki's permission prompts and folder trust are not equivalent to the former
bubblewrap boundary. Keep backups and review executable project configuration.

The example prefers regular Unsloth Flash-Next at xhigh, then dense Qwen3.8
27B MTP Q8, then the first available coding-capable model. It limits concurrent
task subagents to two per Maki process but does not change saved tier
assignments or other providers. Use Maki's model picker and `/thinking` to
choose another model or reasoning mode.

| Family | Native control | Choices | Default |
| --- | --- | --- | --- |
| Qwen3.8 27B | reasoning effort | off, low, medium, xhigh | medium |
| Regular Qwen3.8 Flash-Next | reasoning effort | off, low, medium, xhigh | xhigh in the Maki example, medium when omitted over HTTP |
| Huihui Flash-Next abliterated v2 | reasoning effort | off, low, medium, xhigh | medium |
| Muse Glimmer 30B | reasoning strength | low, medium, high, xhigh | high |

Qwen3.8 has no native high level. The reviewed template uses medium when HTTP
reasoning is omitted. The client preference sends xhigh explicitly for regular
Flash-Next. Maki maps unsupported high to medium and clamps Muse off to low.
Adaptive follows the selected model's exported default.

The provider sends Qwen's `reasoning_effort` or Muse's `reasoning_strength`,
not a numeric thinking budget. Qwen off is `reasoning_effort: none`.
The exported configuration adds no sampler overrides. For Qwen, the gateway
fills missing or null sampler fields according to the requested thinking mode.

| Model family / condition | Temperature | Top-p | Top-k | Min-p | Presence penalty | Repeat penalty |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Qwen3.8 27B, thinking | 1.0 | 0.95 | 20 | 0 | 0 | 1 |
| Qwen3.8 27B, off | 0.7 | 0.8 | 20 | 0 | 1.5 | 1 |
| Flash-Next, regular and Huihui, thinking | 1.0 | 0.95 | 20 | 0.05 | 0 | 1 |
| Flash-Next, regular and Huihui, off | 0.7 | 0.8 | 20 | 0 | 1.5 | 1 |

Explicit request values take precedence field by field. Models without a catalog
sampling policy retain backend defaults. Direct engine endpoints also use
native defaults. See [gateway sampling policy](../gateway.md#model-aware-sampling-defaults).

Maki and other OpenAI-compatible clients use `/v1/chat/completions` for function
tools. Configure the real starting context rather than advertising a larger
limit. The export caps long-context llama.cpp models at 16K output per turn,
leaving more room for history before compaction. This is not a reasoning budget.

Before granting a new model unattended write access, test a complete read,
edit, command and tool-result loop. A plain chat response does not validate
tool handling. Other clients can use the same API without the Maki example.
Clients may send a UUID in `X-Paracetamol-Session-ID` for correlation, but the
export deliberately does not insert a shared or invented session identifier.

### Backend selection

ROCm is the default llama.cpp backend. The same locally built image also has
Vulkan, so a server, terminal session, or benchmark can select it explicitly:

```bash
./paracetamol run llama-cpp cli \
  --preset unsloth-qwen3.8-27b-ud-q8-k-xl \
  --backend vulkan
```

If you do not know which one to use, run them back to back:

```bash
./paracetamol benchmark llama-cpp throughput \
  --preset unsloth-qwen3.8-27b-ud-q8-k-xl \
  --compare-backends
```

The comparison runs unattended, shows the prompt and generation winners
separately, and estimates the combined time for the selected pp/tg ratio. Use
the winning backend with `run llama-cpp ... --backend`.

This is useful for measuring the machine in front of you, not a claim that one
backend always wins. Benchmark every preset you plan to use instead of carrying
one result across a model family. Sparse Q4_K_XL can favor Vulkan while dense
Q6_K favors ROCm on the same machine. Quantization, dense or
mixture-of-experts layout, and active parameter count all change the work each
backend sees.

Keep the model, context, profile, and render-node set the same when comparing
them. The switch belongs to llama.cpp. ComfyUI uses PyTorch and remains a
ROCm application.

### Multiple GPUs

llama.cpp can divide one model across an explicitly selected GPU set. Repeat
the option once per card:

```bash
./paracetamol run llama-cpp server \
  --model /path/to/large-model.gguf \
  --render-node /dev/dri/renderD128 \
  --render-node /dev/dri/renderD129
```

Paracetamol enables llama.cpp's compatible layer split automatically whenever
more than one card is selected. The same selection works with `cli`, the
managed router, and `benchmark llama-cpp throughput`. It is still worth leaving capacity
on every card for the KV cache and runtime buffers. Paracetamol does not enable
the experimental tensor split or add an RCCL dependency.

### MTP models

The `-mtp` presets enable llama.cpp's `draft-mtp` speculative decoder from
catalog-owned settings. Qwen GGUFs contain their MTP heads; Gemma installs a
separate pinned draft. Performance and output behavior depend on the llama.cpp
revision, context, backend, and hardware. Use the server API for end-to-end MTP
measurements as described in the Qwen section.

Dense Qwen3.8 MTP presets use three draft tokens and retain F16 target K/V,
as do the other managed models. Flash-Next has no managed MTP preset.
Inspect the resolved policy with `content list models --details`
instead of inferring cache precision from the weight quantization in a preset
name.

### Muse Glimmer and DFlash

Muse Glimmer is a separate 30B model family rather than another Qwen variant.
The recipe installs [Meta's official Dynamic Q4_K_XL target and DFlash
draft](https://huggingface.co/meta-models/Muse-Glimmer-30B-GGUF/blob/93769bc7ab5ad1e9cd22d857e3138cf5d977ae81/README.md),
approximately 19.82 GiB in total:

```bash
./paracetamol content install llama-cpp muse-glimmer
./paracetamol run llama-cpp server \
  --preset meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k
```

The forced-256K DFlash preset is the Muse recipe launch. It uses twelve draft
tokens on ROCm or four on Vulkan. The 128K DFlash control uses
fifteen on ROCm, matching the draft's 16-token block.
The dynamic bundle also exposes
`meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl` as a non-speculative control. This
makes it possible to compare output, draft acceptance, wall time, and memory
without changing the target GGUF.

The forced-window policy needs no separate download:

```bash
./paracetamol run llama-cpp server \
  --preset meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k
```

That preset sets both `muse-glimmer.context_length` and
`dflash.context_length` to 262144 and disables llama.cpp automatic fitting.
On ROCm it uses twelve draft tokens, selected by a controlled Strix Halo
comparison on the pinned llama.cpp build; the 128K XL preset retains fifteen.
Vulkan continues to use its separately accepted depth of four.
Meta's pinned target and DFlash metadata declare 131072 tokens. Treat 256K as
forced extrapolation until retrieval, quality, memory, and draft acceptance
pass beyond 128K. Making it the Muse recipe default is a deliberate local
policy, not evidence that Meta declared a 256K window.
`--context 0` is intentionally refused for the forced preset; a positive
override such as `--context 196608` applies to both target and draft metadata.

On one `gfx1151` host, a fixed pp512/tg128 ROCm benchmark measured the
official K-quant at 341.17 prompt and 10.32 generated tokens/s. The previously
managed Unsloth Q8 target measured 376.80 and 7.35 tokens/s, while a 51.90 GiB
BF16 conversion measured 483.19 and 4.11 tokens/s. The K-quant also started a
fresh 128K DFlash server in 14.0 seconds at about 30.64 GB of container memory,
versus 37.5 seconds and 66.77 GB for BF16. These are one-host observations,
not cross-hardware promises. The complete immutable inputs and caveats are in
the [maintainer feasibility record](../muse-glimmer-llama-cpp-agent-feasibility.md).

The target's base, 128K DFlash, and forced-256K DFlash presets are available
in the native Maki export when advertised by the gateway. The export leaves
sampling to the backend for Muse. Its upstream recommendation is temperature
1.0, top-p 0.95 and top-k 64, which callers may send explicitly.
Historical Maki tasks completed substantial repository
reviews with official K-quant at 128K, the former Q8 target at both 128K and
forced 256K, and BF16 at 128K. An earlier retired-client answer was a shallow
turn, not a server crash: repository-review depth remains sensitive to the
client scaffold and prompt. Test the actual workflow before granting
unattended write access.

All three presets enable llama.cpp reasoning preservation so parsed reasoning
remains available to multi-turn history. Direct launches and router presets
set this policy explicitly, and `status llama-cpp` reports it under
`Reasoning history`. It controls retained conversation history, not whether
the current reply thinks. Muse does not have a native off mode.
It exposes low, medium, high, and xhigh `Reasoning strength`, defaulting to
high. Maki maps its selector to Muse's
native strength field, and adaptive selects the managed high default.

Paracetamol previously installed
`Muse-Glimmer-30B-UD-Q8_K_XL.gguf` for this recipe. Upgrading the catalog does
not delete persistent model content. After the official replacement installs
and verifies successfully, remove the obsolete file manually if it is no
longer needed. The forced-256K entry remains available for pre-release field
testing, but its long-context quality and DFlash acceptance are experimental.

### Router mode and local GGUF files

Expose every completely installed preset through the multi-model router:

```bash
./paracetamol run llama-cpp server \
  --router --models-max 2
```

Inspect the router itself, or select one configured child policy:

```bash
./paracetamol status llama-cpp
./paracetamol status llama-cpp --model unsloth-qwen3.8-27b-mtp-ud-q8-k-xl
```

Select a preset using the OpenAI API `model` field. Missing models are skipped;
partial managed installs stop startup. The generated router INI and model
partition are mounted read-only.

An ordinary Chat Completions request selects the router preset through
`model`:

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "unsloth-qwen3.8-27b-mtp-ud-q8-k-xl",
    "messages": [{"role": "user", "content": "Explain unified memory."}],
    "max_tokens": 512
  }' | jq -r '.choices[0].message.content'
```

Any local regular `.gguf` file can also be used:

```bash
./paracetamol content list models --scan /path/to/model-directory
./paracetamol build llama-cpp
./paracetamol run llama-cpp server --model /path/to/model.gguf

./paracetamol run llama-cpp cli \
  --model /path/to/model.gguf \
  --prompt 'Explain unified memory briefly.'
```

The model's parent directory is mounted read-only, supporting multi-file GGUF
shards. A CLI invocation with `--prompt` answers once and exits; omit it for
an interactive terminal conversation. The server exposes an OpenAI-compatible
API on port 8080. Remote model fetching and the mutable upstream embedded web
UI are disabled. Non-loopback publication has no authentication unless
`--api-key-file` names a readable key file.

On Strix Halo and Strix Point the entrypoint enables unified-memory policy and
non-mmap model loading. RDNA 4 discrete GPUs use normal automatic layer
placement.
