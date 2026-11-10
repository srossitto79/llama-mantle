package tuner

import (
	"fmt"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/gguf"
)

const miBytes = int64(1 << 20)

// newKV builds a header key/value map with arch-prefixed numeric keys.
func newKV(arch string, nums map[string]uint64) map[string]any {
	kv := map[string]any{"general.architecture": arch}
	for k, v := range nums {
		kv[arch+"."+k] = v
	}
	return kv
}

func TestTuner_ModelInfoHybridMoE(t *testing.T) {
	kv := newKV("qwen3next", map[string]uint64{"block_count": 48, "embedding_length": 2048, "attention.head_count": 16, "attention.head_count_kv": 2, "attention.key_length": 256, "attention.value_length": 256, "context_length": 262144, "expert_count": 512, "expert_used_count": 10})
	tensors := []gguf.Tensor{{Name: "token_embd.weight", Bytes: 1024 * miBytes}, {Name: "per_layer_token_embd.weight", Bytes: 4096 * miBytes}}
	for i := 0; i < 48; i++ {
		tensors = append(tensors, gguf.Tensor{Name: fmt.Sprintf("blk.%d.ffn_up_exps.weight", i), Bytes: 100 * miBytes})
		tensors = append(tensors, gguf.Tensor{Name: fmt.Sprintf("blk.%d.attn_norm.weight", i), Bytes: miBytes})
		if i%4 == 3 {
			tensors = append(tensors, gguf.Tensor{Name: fmt.Sprintf("blk.%d.attn_k.weight", i), Bytes: 2 * miBytes})
		}
	}

	mi := modelInfo("/models/m.gguf", kv, tensors, 2)

	if mi.Arch != "qwen3next" || mi.Layers != 48 || mi.Shards != 2 || mi.Path != "/models/m.gguf" {
		t.Fatalf("basic fields: %+v", mi)
	}
	if mi.KVLayers != 12 {
		t.Fatalf("KVLayers = %d, want 12", mi.KVLayers)
	}
	if mi.ExpertBytes != 48*100*miBytes {
		t.Fatalf("ExpertBytes = %d", mi.ExpertBytes)
	}
	// Input-layer embeddings stay in host memory and are not dense GPU weights.
	if mi.HostBytes != (1024+4096)*miBytes {
		t.Fatalf("HostBytes = %d", mi.HostBytes)
	}
	wantDense := 48*miBytes + 12*2*miBytes
	if mi.DenseBytes != wantDense {
		t.Fatalf("DenseBytes = %d, want %d", mi.DenseBytes, wantDense)
	}
	if mi.TotalBytes != mi.ExpertBytes+mi.DenseBytes+mi.HostBytes {
		t.Fatalf("TotalBytes = %d", mi.TotalBytes)
	}
	if !mi.IsMoE() {
		t.Fatal("IsMoE = false")
	}
	if !mi.HasTokenEmbd {
		t.Fatal("HasTokenEmbd = false")
	}
	if mi.TrainedContext != 262144 || mi.ExpertCount != 512 || mi.ExpertUsedCount != 10 {
		t.Fatalf("context/experts: %+v", mi)
	}
	want := int64(12) * int64(2*(256+256)) * 34 / 32
	if mi.KVBytesPerTokenQ8 != want {
		t.Fatalf("KVBytesPerTokenQ8 = %d, want %d", mi.KVBytesPerTokenQ8, want)
	}
}

func TestTuner_ModelInfoMTPOnlyDraft(t *testing.T) {
	kv := newKV("qwen3next", map[string]uint64{"block_count": 1, "embedding_length": 2048, "attention.head_count": 16, "attention.key_length": 256, "nextn_predict_layers": 1})
	tensors := []gguf.Tensor{
		{Name: "blk.0.attn_k.weight", Bytes: miBytes},
		{Name: "blk.0.nextn.eh_proj.weight", Bytes: miBytes},
	}
	mi := modelInfo("/models/mtp.gguf", kv, tensors, 1)
	if mi.HasTokenEmbd {
		t.Fatal("HasTokenEmbd = true for a file without token_embd")
	}
	if mi.NextNLayers != 1 {
		t.Fatalf("NextNLayers = %d, want 1", mi.NextNLayers)
	}
	if mi.IsMoE() {
		t.Fatal("IsMoE = true")
	}
}

func TestTuner_ModelInfoMLA(t *testing.T) {
	kv := newKV("deepseek2", map[string]uint64{"block_count": 4, "embedding_length": 2048, "attention.head_count": 16, "attention.head_count_kv": 1, "attention.kv_lora_rank": 512, "rope.dimension_count": 64})
	var tensors []gguf.Tensor
	for i := 0; i < 4; i++ {
		tensors = append(tensors, gguf.Tensor{Name: fmt.Sprintf("blk.%d.attn_kv_a_mqa.weight", i), Bytes: miBytes})
	}
	mi := modelInfo("/models/mla.gguf", kv, tensors, 1)
	if mi.KVLayers != 4 {
		t.Fatalf("KVLayers = %d, want 4", mi.KVLayers)
	}
	want := int64(4) * int64(512+64) * 34 / 32
	if mi.KVBytesPerTokenQ8 != want {
		t.Fatalf("KVBytesPerTokenQ8 = %d, want %d", mi.KVBytesPerTokenQ8, want)
	}
}

func TestTuner_ModelInfoHeadFallbacks(t *testing.T) {
	kv := newKV("llama", map[string]uint64{"block_count": 32, "embedding_length": 4096, "attention.head_count": 32})
	mi := modelInfo("/models/d.gguf", kv, nil, 1)
	if mi.HeadCountKV != 32 || mi.KeyLength != 128 || mi.ValueLength != 128 {
		t.Fatalf("fallbacks: %+v", mi)
	}
	// No attention tensors in the table: every layer is assumed to cache K/V.
	if mi.KVLayers != 32 {
		t.Fatalf("KVLayers = %d, want 32", mi.KVLayers)
	}
	if mi.IsMoE() {
		t.Fatal("dense model reported as MoE")
	}
}
