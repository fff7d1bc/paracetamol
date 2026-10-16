# Why the Swift presets were retired

On 2026-09-27 we removed the two UkisAI Swift 1.5 presets from the managed
catalog. This is a curation decision, not a claim that either conversion is
universally broken. The trusted dense Qwen3.8 27B default was unchanged by
that removal. On 2026-10-16, regular Unsloth Flash-Next became the harness
default at xhigh. See the [current model guide](guides/applications.md#choosing-a-managed-qwen-preset).

The deciding observation was a real Pi coding session on the acceptance host at `xhigh`.
The prompt asked for a small Python script that calculates pi accurately to
a user-supplied number of digits. The Unsloth Qwen3.8 Flash-Next Dynamic
Q4_K_XL session finished in about 38 minutes with a script and tests. The
Swift 1.5 Flash-Next Q4_K_M session was stopped after about 1 hour 40 minutes.
It had written and revised a script, but was still debugging correctness and
performance rather than handing back a completed result. Its last recorded
cross-check reported a 10,000-digit output mismatch at byte 10002.

This was not a controlled model benchmark. The sessions ran sequentially,
used different GGUF conversions, and the Swift session could see files left
in the working directory by the earlier run. The prompt and reasoning level
matched, but elapsed time includes the models' own tool use, tests, and
decisions. Earlier bounded Swift checks had passed, including a 256K ROCm
tool smoke for the 27B adaptation and high-context checks for Flash-Next.
Those checks established runtime feasibility, not dependable task completion.
The Swift 27B adaptation did not run this particular prompt.

For this project's small curated inventory, the long unfinished session and
the added licensing burden do not justify keeping the Swift alternatives.
Both the 27B and Flash-Next Swift artifacts, bundles, presets, recipes, and
Pi picker category were removed. This does not delete GGUFs already installed
on a host, Pi session records, or task files. The Unsloth Flash-Next preset
remained experimental at the time, with dense Qwen3.8 27B still the default.

The supporting Pi session records are retained privately and are not bundled
with this public repository.
