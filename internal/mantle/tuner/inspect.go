package tuner

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mostlygeek/llama-swap/internal/gguf"
)

// NewModelInspector returns the GGUF-backed inspector.
func NewModelInspector() ModelInspector { return ggufInspector{} }

type ggufInspector struct{}

var blkRe = regexp.MustCompile(`^blk\.(\d+)\.`)

// hostTensors are input-layer tensors: llama.cpp keeps them in host memory
// and reads rows from them, so they take no VRAM whatever -ngl says. The
// per-layer embedding table (Gemma 3n style, also in qwen4exp) can be larger
// than all the other dense weights together.
var hostTensors = map[string]bool{
	"token_embd.weight":           true,
	"per_layer_token_embd.weight": true,
}

func (ggufInspector) Inspect(path string) (ModelInfo, error) {
	m, err := gguf.ReadModel(path)
	if err != nil {
		return ModelInfo{}, err
	}
	return modelInfo(path, m.KV(), m.Tensors(), len(m.Shards)), nil
}

// modelInfo derives ModelInfo from a header; split out for tests.
func modelInfo(path string, kv map[string]any, tensors []gguf.Tensor, shards int) ModelInfo {
	arch, _ := gguf.String(kv, "general.architecture")
	u := func(key string) int {
		v, _ := gguf.Uint(kv, arch+"."+key)
		return int(v)
	}
	mi := ModelInfo{
		Path:            path,
		Arch:            arch,
		Layers:          u("block_count"),
		EmbeddingLength: u("embedding_length"),
		HeadCountKV:     u("attention.head_count_kv"),
		KeyLength:       u("attention.key_length"),
		ValueLength:     u("attention.value_length"),
		TrainedContext:  u("context_length"),
		ExpertCount:     u("expert_count"),
		ExpertUsedCount: u("expert_used_count"),
		NextNLayers:     u("nextn_predict_layers"),
		Shards:          shards,
	}
	if mi.HeadCountKV == 0 {
		mi.HeadCountKV = u("attention.head_count")
	}
	if mi.KeyLength == 0 {
		if h := u("attention.head_count"); h > 0 {
			mi.KeyLength = mi.EmbeddingLength / h
		}
	}
	if mi.ValueLength == 0 {
		mi.ValueLength = mi.KeyLength
	}

	// Layers with attention K/V. Hybrid models (SSM or linear attention
	// interleaved with full attention) cache K/V on only some layers, so the
	// count comes from the tensor table rather than block_count.
	kvLayers := map[int]bool{}
	for _, t := range tensors {
		mi.TotalBytes += t.Bytes
		if strings.Contains(t.Name, "_exps") {
			mi.ExpertBytes += t.Bytes
		}
		if t.Name == "token_embd.weight" {
			mi.HasTokenEmbd = true
		}
		if hostTensors[t.Name] {
			mi.HostBytes += t.Bytes
		}
		if sm := blkRe.FindStringSubmatch(t.Name); sm != nil {
			rest := t.Name[len(sm[0]):]
			if strings.HasPrefix(rest, "attn_k.") || strings.HasPrefix(rest, "attn_kv_a_mqa.") || strings.HasPrefix(rest, "attn_k_b.") {
				n, _ := strconv.Atoi(sm[1])
				kvLayers[n] = true
			}
		}
	}
	mi.DenseBytes = mi.TotalBytes - mi.ExpertBytes - mi.HostBytes
	mi.KVLayers = len(kvLayers)
	if mi.KVLayers == 0 {
		mi.KVLayers = mi.Layers
	}
	// NextN (MTP) layers carry attention too but are only cached when drafting.
	if mi.NextNLayers > 0 && mi.KVLayers > mi.NextNLayers {
		mi.KVLayers -= mi.NextNLayers
	}

	// Per-token K/V elements per layer. MLA (DeepSeek-style) caches the
	// compressed latent plus the RoPE part instead of full heads.
	perLayer := mi.HeadCountKV * (mi.KeyLength + mi.ValueLength)
	if rank := u("attention.kv_lora_rank"); rank > 0 {
		perLayer = rank + u("rope.dimension_count")
	}
	// q8_0 stores 32 elements in 34 bytes. Sliding-window layers are counted
	// at full context, which overestimates (the safe side) for SWA models.
	mi.KVBytesPerTokenQ8 = int64(mi.KVLayers) * int64(perLayer) * 34 / 32
	return mi
}
