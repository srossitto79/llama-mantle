# HyperQwen chat template with reasoning continuation

`chat_template.jinja` is the chat template shipped with
`Swift-1.5-Qwen3.8-27B-W4A16-HyperQwen-INT4`, plus one branch: when the request sets
`chat_template_kwargs.continue_reasoning`, the last assistant message renders as
`<|im_start|>assistant\n<think>\n` followed by its `reasoning_content`, with the block left open.
Every other request renders exactly as with the original template.

The reasoning loop guard uses it with `continuation: vllm`: it resends the conversation with the
reasoning so far as a final assistant message and `add_generation_prompt: false`, so vLLM continues
inside the reasoning and its own reasoning and tool-call parsers still apply. The prompt up to the
reasoning is identical to the original request's, so the prefix cache is reused.

Use it from the model's `cmd` in `config.yaml`:

```
-v /mnt/w/llama-mantle/docker/hyperqwen:/mantle-templates:ro
-e EXTRA_ARGS="--chat-template /mantle-templates/chat_template.jinja"
```

and on the model entry:

```yaml
reasoningLoopGuard: { mode: recover, continuation: vllm }
```

When the model is updated, copy its new `chat_template.jinja` here and add the branch again.
