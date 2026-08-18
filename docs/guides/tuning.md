# Tuning and benchmarks

[Documentation index](../README.md)

Start with the defaults. They are intentionally boring and are more useful
than a bag of copied environment variables. Change one thing only when a real
workload gives you a reason, then measure the same workload again.

The settings below are conservative starting points, not hardware acceptance
results.

## Host GPU access

Start with Doctor. It checks the selected device nodes, container policy, and
a real GPU operation when the managed PyTorch image is available:

```bash
./rocmplete doctor
```

GPU use needs read/write access to `/dev/kfd` and every selected
`/dev/dri/renderD*` node. On an enforcing SELinux host, Doctor also checks
`container_use_devices`. If it is off, enable the host policy once:

```bash
sudo setsebool -P container_use_devices 1
```

This permits the container domain to use device nodes that Podman explicitly
mounts. ROCmplete continues to pass only `/dev/kfd` and the selected render
nodes.

Ubuntu normally creates the GPU compute nodes as `0660 root:render`. Adding
the user to `render` fixes host access, but a rootless container also needs an
OCI runtime that preserves supplementary groups. Fedora and SteamOS commonly
use workstation-oriented `0666` permissions instead.

When access is missing, Doctor recommends a narrowly scoped persistent udev
rule matching those workstation permissions. That lets every local user
submit GPU work. On a shared machine, prefer an administrator-managed group
policy and a rootless runtime with supplementary-group support. Read the scope
Doctor prints before applying either approach.

Podman still exposes only the selected GPU nodes to a managed container. CPU
mode exposes none.

## Runtime policies

ComfyUI defaults to the kernel and memory policies below:

```text
--memory-policy balanced
--kernel-policy default
```

For ComfyUI, balanced preserves normal behavior and adds `--disable-mmap` on
the Strix Halo and Strix Point APU profiles. Conservative adds `--bf16-vae
--gpu-only --disable-smart-memory --cache-none`; both APU profiles still
disable mmap.

Balanced is the normal recommendation. Conservative is the first comparison
to try when a workflow nearly fits, retains too much memory, or behaves
unreliably under pressure.

Experimental kernels enable:

```text
TORCH_ROCM_AOTRITON_ENABLE_EXPERIMENTAL=1
TORCH_BLAS_PREFER_HIPBLASLT=1
```

Example:

```bash
./rocmplete run comfyui --profile strix-halo \
  --memory-policy conservative \
  --kernel-policy experimental
```

When exactly one render node exists it is selected automatically. On a
multi-GPU host, pass every node intended for one supported workload by
repeating the option:

```bash
./rocmplete doctor \
  --render-node /dev/dri/renderD128 \
  --render-node /dev/dri/renderD129
```

Do not infer GPU index from the `renderD` number. Doctor reports the devices
ROCm sees for the selected set. All cards in one workload must have the same
supported architecture.

## RDNA 3.5 APUs (`gfx1150` and `gfx1151`)

Strix Point and Strix Halo use physically shared CPU/GPU memory. The firmware
VRAM carve-out and dynamic GTT allowance are not a fixed RAM split: GTT is
system memory that the GPU maps on demand and remains available to Linux while
unmapped.

The short version is:

1. Keep the fixed BIOS VRAM reservation small.
2. Build the base or an application and run `./rocmplete doctor`.
3. If Doctor reports a small TTM/GTT ceiling, use the exact host recipe it
   prints and reboot.
4. Run with the defaults first. Try the conservative memory policy only when
   a real workload needs it.

Start with BIOS **iGPU Memory Size** at its minimum, normally 0.5 GiB on a
Framework Desktop and dependent on the Strix Point laptop firmware. Do not
create a large fixed firmware split for ROCm. AMD recommends a small
reservation and larger dynamic TTM/GTT limit; see
[AMD RDNA3.5 system
optimization](https://rocm.docs.amd.com/en/latest/reference/system-optimization/rdna3-5.html).

After building a PyTorch application, inspect the active module, RAM, TTM
ceiling, effective GTT pool, and the architecture PyTorch sees:

```bash
./rocmplete doctor
```

Doctor groups host state, GPU access, the live GPU probe, and shared-memory
capacity into aligned sections. Long paths and image tags remain ordinary
text so terminal wrapping does not corrupt a bordered table.

Doctor's GPU probe performs a small tensor operation on every selected GPU
rather than merely enumerating them, so a queue or memory-mapping failure does
not look like successful GPU readiness.

On either APU profile, doctor compares the effective pool with a tested
starting point:

| System RAM | Initial TTM/GTT ceiling | Approximate headroom |
| ---: | ---: | ---: |
| 48 GiB | 32 GiB | 16 GiB |
| 64 GiB | 48 GiB | 16 GiB |
| 128 GiB | 112 GiB | 16 GiB nominal |

Linux reports about 122.8 GiB usable on a typical nominal 128 GB host, so the
largest ceiling leaves closer to 11 GiB in the absolute worst case. It does
not reserve that memory. A manual DwarfStar run used the 112 GiB tier with its
128K server context on Strix Halo; the observed working set remained below the
ceiling. Formal acceptance remains tracked separately in the hardware matrix.

When tuning is warranted it prints a host recipe using the active module name
and host boot-management mechanism. On a current Ubuntu 128 GiB host, Doctor
keeps the parameter in a small owned GRUB drop-in:

```bash
printf '%s\n' \
  'GRUB_CMDLINE_LINUX_DEFAULT="${GRUB_CMDLINE_LINUX_DEFAULT} amdgpu.gttsize=114688 ttm.pages_limit=29360128 ttm.page_pool_size=29360128"' |
  sudo tee /etc/default/grub.d/70-rocmplete-ttm.cfg
sudo update-grub
sudo reboot
```

On an OSTree-booted host with rpm-ostree available, such as Fedora Kinoite,
Doctor instead keeps the parameter in the transactional deployment:

```bash
sudo rpm-ostree kargs --append-if-missing \
  'amdgpu.gttsize=114688' \
  --append-if-missing 'ttm.pages_limit=29360128' \
  --append-if-missing 'ttm.page_pool_size=29360128'
sudo reboot
```

On a conventional Fedora or RHEL-family installation with `grubby`, Doctor
updates every boot entry and replaces an older value for the active module:

```bash
sudo grubby --update-kernel=ALL \
  --remove-args='amdgpu.gttsize ttm.pages_limit ttm.page_pool_size' \
  --args='amdgpu.gttsize=114688 ttm.pages_limit=29360128 ttm.page_pool_size=29360128'
sudo reboot
```

Use the module and bootloader command reported by Doctor. Packaging may expose
TTM as `ttm`, `amd_ttm`, or `amdttm`. If a conventional host has no supported
GRUB drop-in or `grubby` mechanism, Doctor falls back to a module configuration
and the detected initramfs tool. ROCmplete does not execute these privileged
host changes.

These ceilings do not reserve their full values for the GPU. Leave headroom for
the OS, CPU-side allocations, caches, and other services. At the 112 GiB tier,
ROCmplete sets the GTT aperture, allocation limit, and page-pool limit together
because that configuration ran the large DwarfStar workload successfully in
manual testing. The smaller tiers need only the active TTM module's allocation
limit. ROCmplete does not require AMD's optional `amd-ttm` userspace helper.

`amd_iommu=off` is a separate performance experiment, not part of the memory
capacity fix. The
[kernel parameter](https://www.kernel.org/doc/html/latest/admin-guide/kernel-parameters.html)
disables the AMD IOMMU for the whole host, including its DMA isolation.
[Community Strix Halo
measurements](https://community.frame.work/t/tracking-will-the-ai-max-395-128gb-be-able-to-run-gpt-oss-120b/73280/28)
found about 6 percent faster raw memory reads but less than 2 percent
improvement in llama.cpp token generation. That is useful on the right
dedicated machine, but not enough to trade away isolation blindly. Keep the
default first. If the host does not need device passthrough or another
IOMMU-dependent workload, compare both boots with the same benchmark inputs
and repetitions before keeping it.

Use defaults first:

```bash
./rocmplete run comfyui
```

If one workflow has memory-pressure or retention problems, compare:

```bash
./rocmplete run comfyui --memory-policy conservative
```

The memory capacity only determines which models can fit. Strix Point has far
fewer GPU compute units and substantially less memory bandwidth than Strix
Halo, so large models that fit in a 128 GiB laptop can still be impractically
slow. Establish workload-specific acceptance before treating capacity as
useful support.

Strix Halo additionally needs
[two KFD fixes](https://rocm.docs.amd.com/en/latest/reference/system-optimization/rdna3-5.html).
One corrects `gfx1151` VGPR resource accounting when KFD admits a queue. The
other corrects the context-save/restore size reported to userspace. Without
them, queue creation can fail or ROCm workloads can hang. They are present in
Ubuntu 26.04's kernel and upstream Linux 6.18.4 or newer. Other distributions
may backport them under an older-looking version.

Doctor warns when a Strix Halo host is below that upstream baseline. It stays
quiet on newer kernels because the live GPU operation is the more useful
positive check. This extra KFD requirement is specific to the Ryzen AI Max /
`gfx1151` family and does not apply to Strix Point.

## Radeon RDNA 4 (`gfx1200` and `gfx1201`)

The Radeon AI PRO R9700 and RX 9070 family report `gfx1201`. The RX 9060
family reports `gfx1200`. These are dedicated-VRAM cards and use the same
`rdna4` profile. The R9700 has 32 GB, while capacity varies across the consumer
cards. Check the actual card instead of treating the profile as a memory-size
promise. Do not apply RDNA 3.5 APU firmware or TTM/GTT guidance. Start with
auto detection and balanced memory:

```bash
./rocmplete run comfyui
```

For a ComfyUI workflow that does not fit, try explicit offload:

```bash
./rocmplete run comfyui -- --lowvram
```

System-RAM offload crosses PCIe and is not equivalent to an RDNA 3.5 APU's
shared memory. Experimental kernel policy changes kernel selection; it does
not add VRAM.

For two discrete cards, the motherboard layout matters. Prefer equal-width
CPU-connected PCIe slots. If peer access behaves badly with IOMMU translation,
AMD documents `iommu=pt` as the first host setting to test. Treat it as a
measured hardware workaround, not a default ROCmplete requirement.

## Benchmarks

Managed ComfyUI benchmark results record image, content pins, profile, render
node, seed, cache mode, and both runtime policies:

```bash
./rocmplete benchmark run qwen-image-2512-bf16-base --dry-run
./rocmplete benchmark run qwen-image-2512-bf16-base
./rocmplete benchmark run qwen-image-2512-bf16-base \
  --cache-mode isolated

./rocmplete benchmark suite --family qwen --accept-license
./rocmplete benchmark suite --family qwen --accept-license \
  --resume ~/.local/share/rocmplete/apps/comfyui/benchmarks/suites/SUITE.json
./rocmplete benchmark report \
  ~/.local/share/rocmplete/apps/comfyui/benchmarks/suites/SUITE.json
```

A suite requires all selected bundles before its first GPU workload. It writes
incremental JSON and self-contained Markdown/HTML summaries. Compatible resume
skips completed entries.

Persistent cache mode starts a fresh ComfyUI process while retaining generated
compiler caches. Isolated mode redirects application and compiler caches to a
fresh per-run directory and removes it afterward.

llama.cpp uses its native `llama-bench`:

```bash
./rocmplete benchmark llama-cpp \
  --preset qwen3-0.6b-q8-0 --dry-run
./rocmplete benchmark llama-cpp \
  --preset qwen3-0.6b-q8-0 \
  --prompt-tokens 512 --generation-tokens 128 --repetitions 5
```

The one-shot container has no network, mounts only the selected model
directory read-only, and is removed on completion or interruption. Managed
preset results include immutable source metadata. Local `--model` results
record only resolved path, size, and modification time and are weaker
reproducibility evidence.

Speculative presets are intentionally rejected here. `llama-bench` measures
the main model without ROCmplete's speculative-decoding policy, so accepting
an MTP or DFlash preset would produce a clean-looking result for a different
runtime. Select a non-speculative control for a native `llama-bench`
comparison, or use the separate managed server sweep:

```bash
./rocmplete benchmark llama-cpp-speculative \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium --dry-run
./rocmplete benchmark llama-cpp-speculative \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium
```

For MTP, the default screen tests draft depths 1 through 8. It calibrates one
deterministic synthetic Go repository prompt at approximately 4K, 32K, 64K,
and 120K tokens, then issues three seeded 512-token requests per depth and
prompt size. Each request starts a fresh single-slot server, so no candidate
inherits a prompt cache. The model's reviewed sampling policy and selected
native reasoning condition remain fixed. The resulting 96 requests can take
hours; use `--draft-depth`, `--context-depth`, `--repetitions`, and
`--generation-tokens` for a bounded smoke before the default campaign.

Controlled tuning runs can additionally fix `--draft-probability-min`,
`--draft-backend-sampling`, `--ngram-simple`, `--graph-optimization`,
`--disable-graphs`, `--poll`, `--no-host`, `--flash-attn`, and the matching
`--cache-type-k`/`--cache-type-v` pair. These are recorded benchmark
conditions, not changes to the managed preset. Target cache overrides do not
change the separate draft cache. Quantized values require `--flash-attn on`;
keep one factor different per comparison and inspect response hashes as well
as throughput. `--batch-size` and `--ubatch-size` provide the same explicit
control for prompt-processing experiments. `--ngram-simple` asks llama.cpp's
minimal-overhead history matcher for a draft first and falls back to the
preset's MTP or DFlash strategy when no repeated sequence is found; it is most
relevant to source-code and other repetition-heavy workloads.

The checkpoint records immutable model and image identity, the complete
condition fingerprint, calibrated prompt hashes, server and request time,
prompt and generation rates, accepted and drafted tokens, finish reason, and
assistant response hashes and content. Aggregate generation throughput weights
llama.cpp's server-reported timed generation work by its generation interval;
it never sums or takes an unweighted average of reported rates. A three-percent
screen only names a candidate for a focused incumbent retest; the command
never edits a catalog preset. The synthetic task checks repeatability and
output divergence but is not model-quality acceptance.

Resume with the same options and the printed checkpoint path:

```bash
./rocmplete benchmark llama-cpp-speculative \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium --resume RESULT.json
```

Completed trials are skipped. A running, interrupted, or failed trial is
retried, and a changed source tree, image, model, sampler, context, depth set,
or prompt policy is rejected instead of silently mixing conditions.

The llama.cpp image contains both ROCm and Vulkan. Compare them unattended:

```bash
./rocmplete benchmark llama-cpp \
  --preset qwen3.6-27b-q8-0 \
  --compare-backends \
  --prompt-tokens 512 --generation-tokens 128 --repetitions 5
```

The command creates the two ordinary backend results plus a comparison JSON.
It reports prompt processing, token generation, and an estimated inference
time for the selected pp/tg ratio. The estimate combines the two measured
rates; it does not include model loading or warmups. If one backend fails, the
other still runs, the partial result is saved, and the command exits nonzero.
Ctrl-C stops the current run and uses the normal benchmark cleanup path.

Keep the image, model, profile, render-node set, token counts, and repetitions
fixed. Look at prompt processing and token generation separately because a
backend can win one without winning the other. Run the comparison again after
a llama.cpp, ROCm, Mesa, kernel, or firmware change instead of carrying an old
winner forward forever. Use `--backend rocm` or `--backend vulkan` for a
single-backend result.

Short empty-context runs are useful for a quick backend comparison, but they
do not describe an agent session after its context has grown. Populate the KV
cache before each measured prompt and generation run with `--context-depth`:

```bash
./rocmplete benchmark llama-cpp \
  --preset qwen3.6-27b-q8-0 \
  --compare-backends \
  --context-depth 32768 \
  --flash-attn on \
  --prompt-tokens 512 --generation-tokens 128 --repetitions 5
```

The default depth is zero. `--batch-size` and `--ubatch-size` expose the
logical and physical prompt batches used by `llama-bench`; keep them fixed
when comparing results. Flash Attention defaults to `auto` for compatibility,
but use explicit `on` and `off` runs when validating an upgrade because an
upstream default can change without the workload changing.

ROCmplete enables its reviewed f16 KV-contiguization patch automatically for
Vulkan on Strix Halo. It addresses the dense-model prompt-processing cliff
that otherwise appears as context grows. The path is deliberately not enabled
for Strix Point or RDNA 4 without matching hardware results. It does not
change ROCm behavior or make one backend the universal winner, so keep using
the comparison above for each model and workload.

To measure the memory and long-context effects of a quantized KV cache, set
`--cache-type-k` and `--cache-type-v` to `q8_0` or `q4_0`. Quantized values
require `--flash-attn on` and ROCmplete rejects an ambiguous or incompatible
combination before starting the container. Treat q4 cache measurements as
experimental and check task output as well as throughput. These flags affect
only the one-shot benchmark. A managed application preset changes cache type
only when its catalog entry contains an explicit profile-specific `kv_cache`
policy; inspect that policy with `content list --models --details`.

Treat the result as specific to that non-MTP preset too. Nearby variants are
not interchangeable performance evidence. Quantization, dense or
mixture-of-experts layout, and active parameter count can change the kernel and
memory workload enough for sparse Q4_K_XL to favor Vulkan while dense Q6_K
favors ROCm on the same GPU. Compare each preset you expect to run.

There is no equivalent backend comparison for ComfyUI, which uses ROCm.
