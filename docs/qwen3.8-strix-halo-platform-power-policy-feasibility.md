# Qwen3.8 Strix Halo platform power-policy feasibility snapshot

This maintainer research record captures a 2026-08-17 power-policy screen on
the Fedora 44 Strix Halo host. It tests whether the host ACPI platform profile
or AMD P-State energy-performance preference materially improves the managed
Qwen3.8 27B Q8 MTP path.

This document is machine-specific evidence, not a global performance or host
configuration recommendation. Always inspect `Containerfile`, the catalog,
source, and current hardware-acceptance records for the active pins and
policies.

## Conclusion

Retain the host defaults:

```text
/sys/firmware/acpi/platform_profile = balanced
/sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference = balance_performance
```

The ACPI `performance` profile improved weighted prompt processing by 6.0%
and weighted generation by 1.1% across the two EPP conditions. Complete
measured request time fell by only 2.0% to 2.6%. High-load package power rose
from approximately 101.5 W to 117.9 W, the maximum observed CPU temperature
rose from 79.0 C to 93.8 C, and the maximum observed GPU temperature rose
from 80.0 C to 88.0 C. The monitored fan reached approximately 1810 RPM
instead of 1295 RPM or less. The operator found the resulting acoustic change
intrusive while the normal balanced profile was barely audible.

For this always-powered desktop, removing battery-life concerns therefore did
not change the decision: less than three percent request-time improvement was
not worth the additional heat and noise.

Forcing CPU EPP to `performance` was neutral. Its mean within-platform effect
was approximately +0.02% generation and -0.49% prompt processing, which is
noise at this sample size. Do not combine an EPP override with the retained
balanced platform policy.

If a future firmware, kernel, ROCm, llama.cpp, or cooling change warrants a
retest, compare only ACPI `balanced` and `performance`. Use a longer 32K or
64K prompt and a thermally soaked agent-shaped workload. There is no evidence
here for repeating the EPP matrix.

## Snapshot under test

- Paracetamol source commit:
  `579739aae340cf094bfb4d73c576f01ba8ce69de`
- llama.cpp image:
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r25`
- image ID:
  `55b2ed6796687891d18e77b86bf8d1ded883ce3a03c5e97769da92ddb21a803c`
- llama.cpp commit:
  `3cb7ffb1a1f612d5e4a46244ae5a3c77ad934a70`
- ROCm: 7.14.0
- host: Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64`, Ryzen AI Max+ 395,
  128 GB LPDDR5X-8000, Strix Halo `gfx1151`
- render node: `/dev/dri/renderD128`
- managed preset: `qwen3.8-27b-mtp-ud-q8-k-xl`
- model source: `unsloth/Qwen3.8-27B-GGUF` revision
  `4604b899a826000505a834e623272db5b7fd62f6`
- model SHA-256:
  `af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377`
- model size: 31,457,991,680 bytes

The host began with ACPI `balanced`, AMD P-State EPP `balance_performance` on
all 32 logical CPUs, the `amd-pstate-epp` driver, and the `powersave` scaling
governor. No managed application container was running. The test restored
those exact values and left no benchmark container or sampler running.

## Workload and measurement rules

The controlled server-side speculative benchmark used the managed ROCm path,
medium native Qwen3.8 reasoning effort, MTP draft depth three, and the preset's
sampling and runtime policy. Each condition received the same deterministic
4136-token synthetic Go prompt and three fresh-server, 512-token requests with
seeds 42 through 44. The single-slot server context was 131072, with logical
batch 2048 and physical batch 512.

The four conditions ran in this order:

1. ACPI `balanced`, EPP `balance_performance`;
2. ACPI `performance`, EPP `balance_performance`;
3. ACPI `performance`, EPP `performance`; and
4. ACPI `balanced`, EPP `performance`.

Starting a fresh server for every request prevented prompt-cache carryover.
The benchmark's weighted rates use the server-reported prompt and generation
intervals rather than summing per-request token rates. Request time below is
the sum of the three measured HTTP requests and excludes model startup.

All twelve requests generated 512 tokens and stopped at the length limit. For
each seed, the response SHA-256 was identical under all four conditions. Every
condition accepted the same 950 of 1743 MTP proposals, or 54.50%. This keeps
the comparison free of output-length, content, and speculation-acceptance
changes.

## Performance results

| ACPI profile | CPU EPP | Generate | Prompt | Three-request time |
| --- | --- | ---: | ---: | ---: |
| `balanced` | `balance_performance` | 13.298 t/s | 315.38 t/s | 154.652 s |
| `performance` | `balance_performance` | 13.440 t/s | 331.31 t/s | 151.531 s |
| `balanced` | `performance` | 13.292 t/s | 311.02 t/s | 155.242 s |
| `performance` | `performance` | 13.453 t/s | 332.67 t/s | 151.271 s |

Changing only the ACPI profile improved generation by 1.07% with normal EPP
and 1.21% with performance EPP. Prompt processing improved by 5.05% and 6.96%
respectively. The matching total request-time reductions were 2.02% and
2.56%.

Changing only EPP moved generation by -0.04% under ACPI balanced and +0.09%
under ACPI performance. Its prompt effect changed sign as well: -1.38% and
+0.41%. Those small inconsistent movements provide no actionable EPP result.

The two balanced-profile measurements bracketed the two performance-profile
conditions in time and agreed on generation within 0.05%. The two performance
conditions agreed within 0.10%. That repeatability supports the direction of
the ACPI result despite the small trial count and non-randomized order.

## Power, temperature, and stability

Telemetry sampled the host every two seconds. The table below includes samples
at or above 80 W package power to exclude idle and most transition periods.
Temperatures and power are averages with the observed maximum in parentheses.
`Fan max` is the highest value from the fan channel that reported consistently.

| ACPI profile | CPU EPP | Samples | Package power | CPU | GPU | Fan max |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| `balanced` | `balance_performance` | 68 | 101.55 W (114.1) | 72.15 C (78.1) | 71.72 C (78.0) | 1222 RPM |
| `performance` | `balance_performance` | 70 | 118.04 W (138.0) | 87.89 C (93.8) | 74.94 C (88.0) | 1810 RPM |
| `balanced` | `performance` | 68 | 101.37 W (114.0) | 73.12 C (79.0) | 72.19 C (80.0) | 1295 RPM |
| `performance` | `performance` | 71 | 117.74 W (141.1) | 87.20 C (92.0) | 73.79 C (88.0) | 1810 RPM |

The performance-profile requests retained their small throughput advantage
through all three repetitions, so this bounded screen did not show thermal
throttling. It also did not establish long-duration thermal stability. The
kernel journal recorded no warning, AMDGPU mapping or page fault, GPU reset,
process trap, hardware error, or thermal-throttle message during the test.

Active-load ROCm SMI clock fields were incomplete, so this record does not
attribute the prompt-processing change to a memory-controller, GPU, or SoC
clock. The power, temperature, fan, and application timing measurements are
the usable evidence.

## Retained results

The machine-specific files remain outside the source tree below the managed
llama.cpp benchmark directory:

```text
20260817-power-policy-A-balanced-balance-performance.json
20260817-power-policy-B-performance-balance-performance.json
20260817-power-policy-C-balanced-performance.json
20260817-power-policy-D-performance-performance.json
20260817-power-policy-telemetry.csv
```

The JSON checkpoints contain the immutable model and image identities,
complete conditions, individual timing records, MTP counters, and response
hashes. The normalized CSV contains 451 telemetry samples, including the final
cooldown after restoring the original policy.
