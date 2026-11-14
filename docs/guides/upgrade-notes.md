# Upgrade notes

[Documentation index](../README.md)

Paracetamol is pre-release. Commands, model selectors and checkpoint formats
can change before stable interfaces are promised. Read the notes relevant to
your existing setup before updating. Updates never automatically delete old
models, application state, client sessions or evaluation results.

## Update an existing checkout

Preserve local work and back up irreplaceable application data before updating.
Record the working source revision and whether it has local changes:

```bash
git rev-parse HEAD
git status --short
./paracetamol --version
```

Do not discard local changes to make an update succeed. Stop running services
with the old checkout before switching source, especially a retired application
that the new checkout no longer manages. Keep the image identities from a live
runtime report and, if needed, an [image backup](operations.md#image-backups-and-transfer).

To follow `master` from a clean checkout on that branch:

```bash
git pull --ff-only
```

Alternatively, choose an inspected full commit rather than a moving branch.
Fetch it from the project's configured remote if it is not already local,
then substitute its complete commit hash for `REVISION`:

```bash
git fetch origin
git switch --detach REVISION
```

A full commit selects the controller, application source/dependency pins and
catalog together. It does not promise byte-identical rebuilt images, continued
availability of upstream bytes, or GPU acceptance on your host. After either
source-selection path, build from that checkout:

```bash
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

To return to a known working source revision, stop the current services,
preserve local changes, select its full commit and rebuild or import matching
managed images. Read the instructions in that revision. A source rollback does
not revert application state, content destinations, client configuration or
checkpoint formats. Restore those only from an appropriate backup, never by
assuming that an older checkout migrates them backward. See
[source and image identity](operations.md#source-and-image-identity) when
recording a reproducible report.

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

Qwen3.6, Muse Glimmer, the Swift adaptations, dense Qwen MTP presets,
KAT-Coder, Shisa V2.1, TranslateGemma and HY-MT1.5 are retired from the managed
catalog and client exports. Existing downloads remain untouched. Choose a
current model from the
[managed preset guide](applications.md#choosing-a-managed-qwen-preset), update
literal IDs and refresh generated client configuration.
The [Swift retirement record](../research/swift-retirement.md) preserves the observed
coding failure and the limits of that comparison.

## Native clients replace managed launchers

Managed Pi support, the Pi and Maki launchers, their bubblewrap sandbox and
`benchmark agent` are retired. Install Maki separately and use the
[gateway configuration export](remote-maki.md). Native Maki has normal user
access, not the former launcher's filesystem or network isolation. Review
project configuration and client permissions accordingly.

Existing runtime, session, credential and evaluation trees are left in place.
They are not automatically imported into native Maki. Back them up before
manual cleanup. See [archived client data](operations.md#coding-agent-evaluation)
and the [historical evaluation protocol](../research/coding-agent-evaluation.md).

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
