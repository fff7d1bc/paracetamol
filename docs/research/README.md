# Research and historical evidence

These records describe particular source, image, model, hardware and runtime
tuples. Old commands, retired model IDs and recommendations remain part of
their provenance. They are not the current setup guide, version list or a
promise that raw private fixtures ship in this repository.

Use [Applications](../guides/applications.md) for current model choices,
[hardware acceptance](../hardware-acceptance.md) for current coverage, and
[llama.cpp maintenance](../llama-cpp-maintenance.md) for the accepted baseline
and open regression gates. A later change does not inherit an earlier result
without the relevant acceptance checks.

## Integration and acceptance journals

- [Hardware acceptance history](hardware-acceptance-history.md) preserves
  dated host observations, accepted tuples, failures and deferred checks.
- [Upgrade history](upgrade-history.md) preserves external pin refreshes,
  patch investigations and their original limitations.
- [Bounded coding-quality screen](coding-quality-screen.md) records the
  retained private developer protocol and what a fresh clone can reproduce.
- [Maintainer acceptance kit qualification](llama-acceptance-kit-qualification.md)
  records clean-clone, sandbox and production-path checks of the tracked kit,
  including failed and deferred coverage.
- [Archived coding-agent evaluation](coding-agent-evaluation.md) describes
  frozen tasks and fixtures from the retired Pi runner.

## Flash-Next optimization and capacity studies

- [Tiled GDN prefill](qwen3.8-tiled-gdn-strix-halo-feasibility.md)
- [Direct embedding reads](qwen3.8-flash-next-direct-reader-feasibility.md)
- [Gufo source extraction and engine recheck](qwen3.8-flash-next-gufo-optimization-screen.md)
- [Strix fork reverse ablation](strix-llama-reverse-ablation.md)
- [IQ4_NL versus Q8 n-gram precision](qwen3.8-flash-next-ngram-precision-screen.md)
- [Vision overhead and DFlash2 compatibility](qwen3.8-flash-next-vision-feasibility.md)
- [DFlash2 measurements and dense-model retest](qwen3.8-dflash2-strix-halo-feasibility.md)
- [RDNA head-dimension-256 Flash Attention](qwen3.8-rdna-head-dim-256-flash-attention-strix-halo-feasibility.md)
- [Platform power policy](qwen3.8-strix-halo-platform-power-policy-feasibility.md)

The [Gufo developer recheck procedure](../../tools/gufo-recheck/README.md)
lives beside its tool. That explicit experiment is not a managed backend.

## Model and retired-runtime comparisons

- [Historical Unsloth Dynamic Q4-preview/MTP versus Q8 coding comparison](qwen3.8-dynamic-quant-comparison.md)
- [Swift retirement](swift-retirement.md)
- [DeepSeek V4 Flash llama.cpp feasibility](deepseek-v4-flash-llama-cpp-feasibility.md)
- [GLM-5.3 Flash DwarfStar feasibility](glm-5.3-flash-dwarfstar-feasibility.md)
- [Ling 3.0 Flash feasibility](ling-3.0-flash-llama-cpp-feasibility.md)
- [Muse Glimmer feasibility](muse-glimmer-llama-cpp-agent-feasibility.md)
- [Qwen3.6 Strix Halo tuning](qwen3.6-strix-halo-llama-cpp-tuning-feasibility.md)

Preserve measured inputs and outcomes when editing these pages. Add a dated
follow-up or a clear current-policy pointer instead of relabeling an old
measurement as evidence for a newer model, kernel or precision path. Keep
raw logs, prompts and personal infrastructure out of tracked files.
