#!/bin/bash
# Supported llama.cpp executables grouped by Llama Studio build profile.
#
# "all" is the default power-user profile. Examples/demos, PoCs, the desktop
# app, build-time UI helpers, documentation generators, and deprecated
# architecture-specific mtmd wrappers are intentionally excluded from every
# profile.

llama_targets_for_profile() {
    local profile="${1:-all}"

    local runtime=(
        llama-cli
        llama-completion
        llama-embedding
        llama-lookahead
        llama-lookup
        llama-mtmd-cli
        llama-server
        llama-speculative
        llama-tts
    )
    local core=(
        llama-gguf
        llama-gguf-hash
        llama-gguf-split
        llama-imatrix
        llama-prune
        llama-quantize
        llama-template-analysis
        llama-tokenize
    )
    local training=(
        llama-convert-llama2c-to-ggml
        llama-export-lora
        llama-finetune
        llama-finetune-qlora
        llama-model-merge
    )
    local evaluation=(
        llama-batched-bench
        llama-bench
        llama-eval-callback
        llama-fit-params
        llama-perplexity
        llama-results
    )
    local advanced=(
        llama-batched
        llama-cvector-generator
        llama-debug
        llama-diffusion-cli
        llama-idle
        llama-lookup-create
        llama-lookup-merge
        llama-lookup-stats
        llama-mtmd-debug
        llama-parallel
        llama-passkey
        llama-retrieval
        llama-speculative-simple
    )

    case "${profile}" in
        runtime)           printf '%s\n' "${runtime[@]}" ;;
        studio-core)       printf '%s\n' "${core[@]}" ;;
        studio-training)   printf '%s\n' "${training[@]}" ;;
        studio-evaluation) printf '%s\n' "${evaluation[@]}" ;;
        studio-advanced)   printf '%s\n' "${advanced[@]}" ;;
        all)
            printf '%s\n' "${runtime[@]}" "${core[@]}" "${training[@]}" "${evaluation[@]}" "${advanced[@]}" |
                sort -u
            ;;
        *)
            echo "Unknown llama.cpp build profile: ${profile}" >&2
            return 1
            ;;
    esac
}
