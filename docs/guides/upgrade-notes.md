# Upgrade notes

[Documentation index](../README.md)

Paracetamol is pre-release. Commands, model selectors and checkpoint formats
can change before stable interfaces are promised. Read the notes relevant to
your existing setup before updating. Updates never automatically delete old
models, application state, client sessions or evaluation results.

## Update an existing checkout

Preserve local work and back up irreplaceable application data before updating.
If the checkout still runs DwarfStar, stop it using that old checkout first.

```bash
git pull --ff-only
./paracetamol doctor
./paracetamol build all
```

Rerun the normal content installer for the recipes or bundles you use when
their required artifacts change. Verified unchanged files are reused. Restart
the gateway to refresh its frozen inventory, then refresh client configuration.
See [content verification](content.md) and [native Maki setup](remote-maki.md).
On suitable target hardware, follow the
[smoke acceptance walkthrough](operations.md#target-hardware-smoke-acceptance).
A build or CPU startup alone is not GPU inference acceptance.

## Model IDs and required companions

Public text model IDs now begin with the publisher of the pinned GGUF, such
as `unsloth-` or `huihui-ai-`. Update literal `model` IDs in OpenAI-compatible
clients and exact bundle selectors. Inspect the current names with:

```bash
./paracetamol content list models
```

An ID change alone does not mean the weight bytes changed. Follow the normal
installer's verification and destination checks. When paths change, the
[local mirror workflow](content.md#local-mirror-migration) can reuse matching
bytes without downloading them again.

Flash-Next's regular and Huihui presets now require their matching pinned
vision projector, including for text-only use. Run the selected bundle's
normal installer again. Existing verified target shards are reused. The
projector retains an explicit `NOASSERTION` license gate. There is no separate
vision switch or new model ID. The [application guide](applications.md#llamacpp)
covers image input limits and setup.

Qwen3.6, Muse Glimmer, the Swift adaptations and dense Qwen MTP presets are
retired from the managed catalog and client exports. Existing downloads remain
untouched. Choose a current model from the
[managed preset guide](applications.md#choosing-a-managed-qwen-preset), update
literal IDs and refresh generated client configuration.

## Native clients replace managed launchers

Managed Pi support, the Pi and Maki launchers, their bubblewrap sandbox and
`benchmark agent` are retired. Install Maki separately and use the
[gateway configuration export](remote-maki.md). Native Maki has normal user
access, not the former launcher's filesystem or network isolation. Review
project configuration and client permissions accordingly.

Existing runtime, session, credential and evaluation trees are left in place.
They are not automatically imported into native Maki. Back them up before
manual cleanup. See [archived client data](operations.md#coding-agent-evaluation)
and the [historical evaluation protocol](../coding-agent-evaluation.md).

The exported `init.lua` now starts new sessions at explicit `xhigh`, including
Huihui. Merge `always_thinking = "xhigh"` into an existing configuration to
adopt it. Adaptive and lower levels remain selectable. Qwen HTTP requests that omit
reasoning still use the model's `medium` fallback. The
[native client guide](applications.md#native-clients) keeps the complete
model preferences, reasoning mappings and sampling policy.

## Retired DwarfStar application

llama.cpp is now the only managed text-inference engine. Before updating from
a checkout that includes DwarfStar, stop any running instance with that
checkout. Remove `dwarfstar` from saved application selections and user-owned
content packs. Its existing models, state and images remain untouched.

## State identity and checkpoint compatibility

The Paracetamol command, configuration prefix, default data directory, image
and container names and ownership labels use the Paracetamol identity. A
differently named checkout has no automatic aliases or state migration. To
reuse managed content, explicitly select the intended `data_dir` and run the
normal installer so it can validate the bytes. Existing files are not deleted.
See [persistent data](operations.md#persistent-data) before moving any state.

The Go transition changed command shapes and checkpoint formats. `acceptance`
is a direct leaf command. Benchmarks use `comfyui run`, `comfyui suite`,
`llama-cpp throughput` and `llama-cpp speculative`. Older benchmark and
acceptance JSON remains historical evidence but cannot resume a current run.
