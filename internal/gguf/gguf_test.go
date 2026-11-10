package gguf

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func putU32(b *bytes.Buffer, v uint32) { _ = binary.Write(b, binary.LittleEndian, v) }
func putU64(b *bytes.Buffer, v uint64) { _ = binary.Write(b, binary.LittleEndian, v) }

func putStr(b *bytes.Buffer, s string) {
	putU64(b, uint64(len(s)))
	b.WriteString(s)
}

// writeTestGGUF writes a GGUF v3 file with three tensors at data offsets 0,
// 64 and 192, followed by dataBytes of tensor data.
func writeTestGGUF(t *testing.T, path, arch string, names [3]string, dataBytes int) {
	t.Helper()
	var b bytes.Buffer
	putU32(&b, magic)
	putU32(&b, 3) // version
	putU64(&b, 3) // tensor_count
	putU64(&b, 4) // kv_count

	putStr(&b, "general.architecture")
	putU32(&b, typeString)
	putStr(&b, arch)

	putStr(&b, arch+".block_count")
	putU32(&b, typeUint32)
	putU32(&b, 12)

	putStr(&b, arch+".feed_forward_length")
	putU32(&b, typeArray)
	putU32(&b, typeUint32)
	putU64(&b, 4)
	for _, v := range []uint32{0, 4, 8, 2} {
		putU32(&b, v)
	}

	putStr(&b, "general.alignment")
	putU32(&b, typeUint32)
	putU32(&b, 32)

	offsets := [3]uint64{0, 64, 192}
	for i, n := range names {
		putStr(&b, n)
		putU32(&b, 2) // dims
		putU64(&b, uint64(8*(i+1)))
		putU64(&b, 4)
		putU32(&b, 0) // type f32
		putU64(&b, offsets[i])
	}
	for b.Len()%32 != 0 {
		b.WriteByte(0)
	}
	b.Write(make([]byte, dataBytes))
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGGUF_ReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.gguf")
	names := [3]string{"token_embd.weight", "blk.0.attn_k.weight", "blk.0.ffn_up_exps.weight"}
	writeTestGGUF(t, path, "foo", names, 256)

	f, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 3 {
		t.Fatalf("version = %d", f.Version)
	}
	if a, ok := String(f.KV, "general.architecture"); !ok || a != "foo" {
		t.Fatalf("architecture = %q, %v", a, ok)
	}
	if n, ok := Uint(f.KV, "foo.block_count"); !ok || n != 12 {
		t.Fatalf("block_count = %d, %v", n, ok)
	}
	if n, ok := Uint(f.KV, "general.alignment"); !ok || n != 32 {
		t.Fatalf("alignment = %d, %v", n, ok)
	}
	arr, ok := f.KV["foo.feed_forward_length"].([]any)
	if !ok || len(arr) != 4 {
		t.Fatalf("array = %#v", f.KV["foo.feed_forward_length"])
	}
	if len(f.Tensors) != 3 {
		t.Fatalf("tensors = %d", len(f.Tensors))
	}
	wantBytes := []int64{64, 128, 256 - 192}
	for i, ts := range f.Tensors {
		if ts.Name != names[i] {
			t.Errorf("tensor %d name = %q, want %q", i, ts.Name, names[i])
		}
		if want := []uint64{uint64(8 * (i + 1)), 4}; !reflect.DeepEqual(ts.Dims, want) {
			t.Errorf("tensor %d dims = %v, want %v", i, ts.Dims, want)
		}
		if ts.Bytes != wantBytes[i] {
			t.Errorf("tensor %d bytes = %d, want %d", i, ts.Bytes, wantBytes[i])
		}
	}
}

func TestGGUF_ReadFileBadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gguf")
	if err := os.WriteFile(path, []byte("NOPE0000000000000000"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected an error for a bad magic")
	}
}

func TestGGUF_ReadFileUnsupportedVersion(t *testing.T) {
	var b bytes.Buffer
	putU32(&b, magic)
	putU32(&b, 9)
	putU64(&b, 0)
	putU64(&b, 0)
	path := filepath.Join(t.TempDir(), "v9.gguf")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected an error for version 9")
	}
}

func TestGGUF_ShardPaths(t *testing.T) {
	got := ShardPaths("/m/model-00002-of-00003.gguf")
	want := []string{
		"/m/model-00001-of-00003.gguf",
		"/m/model-00002-of-00003.gguf",
		"/m/model-00003-of-00003.gguf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("split: got %v, want %v", got, want)
	}
	single := "/m/model-Q4_K_M.gguf"
	if got := ShardPaths(single); !reflect.DeepEqual(got, []string{single}) {
		t.Fatalf("non-split: got %v", got)
	}
}

func TestGGUF_ReadModelTwoShards(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "m-00001-of-00002.gguf")
	second := filepath.Join(dir, "m-00002-of-00002.gguf")
	writeTestGGUF(t, first, "foo", [3]string{"a0", "a1", "a2"}, 256)
	writeTestGGUF(t, second, "foo", [3]string{"b0", "b1", "b2"}, 512)

	// Any shard path reads the whole model.
	m, err := ReadModel(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Shards) != 2 {
		t.Fatalf("shards = %d", len(m.Shards))
	}
	if a, _ := String(m.KV(), "general.architecture"); a != "foo" {
		t.Fatalf("architecture = %q", a)
	}
	ts := m.Tensors()
	if len(ts) != 6 {
		t.Fatalf("tensors = %d", len(ts))
	}
	var names []string
	var total int64
	for _, x := range ts {
		names = append(names, x.Name)
		total += x.Bytes
	}
	if want := []string{"a0", "a1", "a2", "b0", "b1", "b2"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v", names)
	}
	if total != 256+512 {
		t.Fatalf("total bytes = %d, want %d", total, 256+512)
	}
}

func TestGGUF_ReadModelMissingShard(t *testing.T) {
	dir := t.TempDir()
	writeTestGGUF(t, filepath.Join(dir, "m-00001-of-00002.gguf"), "foo", [3]string{"a", "b", "c"}, 256)
	if _, err := ReadModel(filepath.Join(dir, "m-00001-of-00002.gguf")); err == nil {
		t.Fatal("expected an error when a shard is missing")
	}
}

func TestGGUF_UintTakesMaxOfArray(t *testing.T) {
	kv := map[string]any{
		"per_layer": []any{uint64(0), uint64(8), int64(3)},
		"scalar":    uint64(7),
		"signed":    int64(5),
		"negative":  int64(-1),
		"text":      "x",
	}
	cases := []struct {
		key  string
		want uint64
		ok   bool
	}{
		{"per_layer", 8, true},
		{"scalar", 7, true},
		{"signed", 5, true},
		{"negative", 0, false},
		{"text", 0, false},
		{"missing", 0, false},
	}
	for _, c := range cases {
		got, ok := Uint(kv, c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("Uint(%q) = %d, %v; want %d, %v", c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestGGUF_UintFromFileArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.gguf")
	writeTestGGUF(t, path, "foo", [3]string{"a", "b", "c"}, 64)
	f, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := Uint(f.KV, "foo.feed_forward_length"); !ok || n != 8 {
		t.Fatalf("Uint of array = %d, %v; want 8", n, ok)
	}
}

func TestGGUF_StringMissing(t *testing.T) {
	if _, ok := String(map[string]any{"a": uint64(1)}, "a"); ok {
		t.Fatal("non-string value reported as string")
	}
}
