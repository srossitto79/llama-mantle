# Llama Studio tool capability plan

This document makes the executables in the power-user image the source of truth for
Studio operations. UI fields alone are not evidence of support: an option is complete
only when it is represented in the API, validated, translated to the real command,
covered by tests, and exercised against the container.

## Scope rules

Each executable and flag is classified before it is exposed:

- **Managed**: safe, durable Studio operation with catalog inputs, staging, atomic
  publication, jobs, logs, artifacts, project ownership, and validation.
- **Expert**: useful runtime control that remains typed but is hidden in an advanced
  section. Examples include device placement, CPU affinity, and cache configuration.
- **Existing surface**: already belongs to serving or the Playground and should not be
  duplicated in Studio.
- **Diagnostic**: useful only for troubleshooting and exposed, if at all, behind an
  explicit diagnostics page.
- **Excluded**: examples, benchmarks of implementation internals, PoCs, destructive
  modes, or flags that bypass Studio path and publication guarantees.

Shared llama.cpp argument parsers advertise generation, sampling, download, logging,
and model-loading flags even when an executable does not meaningfully use all of them.
The audit therefore records both advertised flags and flags relevant to that tool.
“All flags” does not mean blindly forwarding arbitrary argv.

## Current managed operations

| Studio operation | Executable | Coverage after audit | Remaining work |
| --- | --- | --- | --- |
| Quantize | `llama-quantize` | Managed | Tensor filters/types, rule files, pruning, split preservation, and metadata overrides are typed and validated. |
| Hash | `llama-gguf-hash` | Managed | Generation and manifest verification are supported. |
| Split | `llama-gguf-split` | Managed | Safe split and merge are supported; destructive source deletion remains excluded. |
| Model merge | `llama-model-merge` | Managed | TIES/evolution controls are supported. Config import remains excluded because it can replace Studio-managed paths. |
| Prune | `llama-prune` | Tool-specific flags represented | Verify every phase independently and conditionally show phase-relevant controls. Runtime placement belongs to the shared expert schema. |
| QLoRA training | `llama-finetune-qlora` | Tool-specific flags represented | Add a shared expert runtime section. Do not expose unrelated sampling/download flags merely because the common parser advertises them. |
| LoRA export | `llama-export-lora` | Managed | Normal and scaled adapter lists are supported. |
| Benchmark evaluation | `llama-bench` | Managed | Workload, warmup, priority, delay, device/cache/loading placement, embeddings, and fit controls are supported. Studio fixes output to JSON for metric capture. |
| Perplexity evaluation | `llama-perplexity` | Managed | Perplexity, HellaSwag, Winogrande, multiple-choice, and KL-divergence modes are supported with task metrics. |
| Register for serving | llama-swap configuration | Complete for current API | Keep separate from llama.cpp CLI flag coverage. |

## Installed executable classification

The current power-user image contains the following groups.

### Managed utility recipes

- Artifact jobs: `llama-imatrix`, `llama-cvector-generator`, `llama-lookup-create`,
  `llama-lookup-merge`, and `llama-finetune`.
- Report jobs: `llama-fit-params`, `llama-tokenize`, `llama-template-analysis`, and
  `llama-lookup-stats`. `llama-results` publishes its required results file.
- `llama-gguf` mutation remains excluded until individual metadata edits can be
  allow-listed and proven atomic; model metadata inspection already exists in Studio.
- Named `llama-template-analysis` test-suite templates are excluded because their
  source files are not shipped in the runtime image; cataloged custom template files
  are supported.

### Existing serving or Playground surfaces

- `llama-cli`, `llama-server`, `llama-completion`, `llama-embedding`, `llama-tts`.
- `llama-mtmd-cli` and multimodal inference.
- `llama-diffusion-cli`.

These capabilities should integrate through the existing Playground or serving model
configuration rather than appear as duplicate file-processing recipes.

### Diagnostics and examples

- `llama-batched`, `llama-batched-bench`, `llama-bench` diagnostic modes.
- `llama-debug`, `llama-mtmd-debug`, `llama-eval-callback`.
- `llama-idle`, `llama-lookahead`, `llama-lookup`, `llama-parallel`, `llama-passkey`,
  `llama-retrieval`, `llama-speculative`, `llama-speculative-simple`.
- `llama-convert-llama2c-to-ggml` for a legacy format.

These are not normal-user recipes. A tool moves out of this group only when it has a
clear user outcome, stable inputs and outputs, and a safe managed execution contract.

## Shared schemas to implement

Repeated flags must not be copied independently into every operation. Introduce typed,
reusable schemas and render them as collapsed expert sections:

1. **Compute placement**: threads, batch threads, CPU masks/ranges and priority,
   device list, GPU layers, split mode, tensor split, main GPU, MoE placement.
2. **Memory and loading**: context/batch/micro-batch, flash attention, KV offload and
   types, load mode, host buffers, operation offload, fit target/context, tensor checks.
3. **RoPE and context scaling**: scaling mode/factor, frequency base/scale, and YaRN
   controls where relevant.
4. **Logging**: verbosity and structured capture. User-selected arbitrary log paths
   remain excluded because Studio already owns job logs.
5. **Resource lists**: structured path plus optional scale for LoRA/control vectors,
   with every path resolved inside the configured model root.

Model download flags, arbitrary output paths, raw positional arguments, shell strings,
and flags that can delete source files are not accepted as generic passthrough.

## Implementation order

1. Complete the existing transformations: quantize, hash verification, split merge,
   model merge, and scaled LoRA export.
2. Complete evaluation modes for `llama-bench` and `llama-perplexity`.
3. Implement the shared compute/memory schema and apply it to evaluation, pruning, and
   training where the executable consumes it.
4. Add artifact-producing utilities: imatrix, fit parameters, tokenize/template
   analysis, control vectors, and lookup caches.
5. Add non-QLoRA fine-tuning as its own operation and recipe family.
6. Run each operation in the real container using tiny fixtures; verify command lines,
   cancellation, logs, atomic publication, artifact discovery, and project ownership.

## Definition of done for each operation

- The checked-in capability matrix matches the installed executable's `--help` for the
  pinned image revision.
- Every supported field has backend validation, API and TypeScript types, a suitable
  UI control, and command-construction tests.
- Resource paths use the Studio catalog and remain inside configured roots.
- Destructive or irrelevant flags are explicitly documented as excluded.
- A dry run or tiny real-container fixture proves the generated command is accepted.
- Jobs, artifacts, lineage, evaluations, recipes, and project scope remain intact.
