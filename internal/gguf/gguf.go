// Package gguf reads the header of a GGUF file: the metadata key/values and
// the tensor table, without reading tensor data. Split models are read shard
// by shard with ReadModel.
//
// internal/mantle/gguf.go has an older metadata-only reader used by the size
// estimates; it can move onto this package later.
package gguf

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

const magic uint32 = 0x46554747 // "GGUF" little-endian

const (
	typeUint8 uint32 = iota
	typeInt8
	typeUint16
	typeInt16
	typeUint32
	typeInt32
	typeFloat32
	typeBool
	typeString
	typeArray
	typeUint64
	typeInt64
	typeFloat64
)

// maxArrayKeep bounds how many array elements are kept in memory; the rest
// are consumed and dropped (vocabularies run to hundreds of thousands).
const maxArrayKeep = 1024

// Tensor is one entry of the tensor table.
type Tensor struct {
	Name   string
	Dims   []uint64
	Type   uint32
	Offset uint64 // relative to the data section
	Bytes  int64  // derived from the next tensor's offset (includes alignment padding)
}

// File is one GGUF file's header.
type File struct {
	Path    string
	Version uint32
	KV      map[string]any
	Tensors []Tensor
}

// Model is a whole model: one file, or every shard of a split file.
type Model struct {
	Shards []*File
}

var splitRe = regexp.MustCompile(`^(.*)-(\d{5})-of-(\d{5})\.gguf$`)

// ShardPaths returns every shard path of a split model given any shard, or
// the path itself when it is not a split file.
func ShardPaths(path string) []string {
	m := splitRe.FindStringSubmatch(path)
	if m == nil {
		return []string{path}
	}
	n, _ := strconv.Atoi(m[3])
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%s-%05d-of-%s.gguf", m[1], i, m[3]))
	}
	return out
}

// ReadModel reads the header of every shard of the model at path.
func ReadModel(path string) (*Model, error) {
	paths := ShardPaths(path)
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s: no shards", filepath.Base(path))
	}
	var m Model
	for _, p := range paths {
		f, err := ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		m.Shards = append(m.Shards, f)
	}
	return &m, nil
}

// KV returns the metadata of the first shard, which carries the model keys.
func (m *Model) KV() map[string]any { return m.Shards[0].KV }

// Tensors returns the tensors of all shards.
func (m *Model) Tensors() []Tensor {
	var out []Tensor
	for _, s := range m.Shards {
		out = append(out, s.Tensors...)
	}
	return out
}

// countingReader tracks the byte position so the data section start can be
// computed after the tensor table.
type countingReader struct {
	r *bufio.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ReadFile reads one GGUF file's metadata and tensor table.
func ReadFile(path string) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	r := &countingReader{r: bufio.NewReaderSize(fh, 1<<20)}

	var mg, version uint32
	if err := binary.Read(r, binary.LittleEndian, &mg); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if mg != magic {
		return nil, fmt.Errorf("not a GGUF file (bad magic 0x%08x)", mg)
	}
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}
	if version != 2 && version != 3 {
		return nil, fmt.Errorf("unsupported GGUF version %d", version)
	}
	tensorCount, err := readU64(r)
	if err != nil {
		return nil, fmt.Errorf("read tensor_count: %w", err)
	}
	kvCount, err := readU64(r)
	if err != nil {
		return nil, fmt.Errorf("read kv_count: %w", err)
	}
	if tensorCount > 1<<24 || kvCount > 1<<20 {
		return nil, fmt.Errorf("implausible header counts (%d tensors, %d keys)", tensorCount, kvCount)
	}

	kv := make(map[string]any, kvCount)
	for i := uint64(0); i < kvCount; i++ {
		key, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("read key #%d: %w", i, err)
		}
		var t uint32
		if err := binary.Read(r, binary.LittleEndian, &t); err != nil {
			return nil, fmt.Errorf("read type of %q: %w", key, err)
		}
		v, err := readValue(r, t)
		if err != nil {
			return nil, fmt.Errorf("read value of %q: %w", key, err)
		}
		kv[key] = v
	}

	tensors := make([]Tensor, 0, tensorCount)
	for i := uint64(0); i < tensorCount; i++ {
		name, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("read tensor #%d name: %w", i, err)
		}
		var nd uint32
		if err := binary.Read(r, binary.LittleEndian, &nd); err != nil {
			return nil, err
		}
		if nd > 8 {
			return nil, fmt.Errorf("tensor %q has %d dims", name, nd)
		}
		dims := make([]uint64, nd)
		for d := range dims {
			if dims[d], err = readU64(r); err != nil {
				return nil, err
			}
		}
		var tt uint32
		if err := binary.Read(r, binary.LittleEndian, &tt); err != nil {
			return nil, err
		}
		off, err := readU64(r)
		if err != nil {
			return nil, err
		}
		tensors = append(tensors, Tensor{Name: name, Dims: dims, Type: tt, Offset: off})
	}

	align := int64(32)
	if a, ok := Uint(kv, "general.alignment"); ok && a > 0 {
		align = int64(a)
	}
	dataStart := (r.n + align - 1) / align * align
	sizeTensors(tensors, st.Size()-dataStart)

	return &File{Path: path, Version: version, KV: kv, Tensors: tensors}, nil
}

// sizeTensors fills Tensor.Bytes from the gaps between sorted offsets.
func sizeTensors(ts []Tensor, dataBytes int64) {
	idx := make([]int, len(ts))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return ts[idx[a]].Offset < ts[idx[b]].Offset })
	for k, i := range idx {
		end := dataBytes
		if k+1 < len(idx) {
			end = int64(ts[idx[k+1]].Offset)
		}
		if b := end - int64(ts[i].Offset); b > 0 {
			ts[i].Bytes = b
		}
	}
}

func readU64(r io.Reader) (uint64, error) {
	var v uint64
	err := binary.Read(r, binary.LittleEndian, &v)
	return v, err
}

func readString(r io.Reader) (string, error) {
	n, err := readU64(r)
	if err != nil {
		return "", err
	}
	if n > 64<<20 {
		return "", fmt.Errorf("string length %d too large", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readValue(r io.Reader, t uint32) (any, error) {
	switch t {
	case typeUint8:
		var v uint8
		err := binary.Read(r, binary.LittleEndian, &v)
		return uint64(v), err
	case typeInt8:
		var v int8
		err := binary.Read(r, binary.LittleEndian, &v)
		return int64(v), err
	case typeUint16:
		var v uint16
		err := binary.Read(r, binary.LittleEndian, &v)
		return uint64(v), err
	case typeInt16:
		var v int16
		err := binary.Read(r, binary.LittleEndian, &v)
		return int64(v), err
	case typeUint32:
		var v uint32
		err := binary.Read(r, binary.LittleEndian, &v)
		return uint64(v), err
	case typeInt32:
		var v int32
		err := binary.Read(r, binary.LittleEndian, &v)
		return int64(v), err
	case typeFloat32:
		var v float32
		err := binary.Read(r, binary.LittleEndian, &v)
		return float64(v), err
	case typeBool:
		var v uint8
		err := binary.Read(r, binary.LittleEndian, &v)
		return v != 0, err
	case typeString:
		return readString(r)
	case typeUint64:
		var v uint64
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeInt64:
		var v int64
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeFloat64:
		var v float64
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeArray:
		var et uint32
		if err := binary.Read(r, binary.LittleEndian, &et); err != nil {
			return nil, err
		}
		n, err := readU64(r)
		if err != nil {
			return nil, err
		}
		if n > 1<<28 {
			return nil, fmt.Errorf("array length %d too large", n)
		}
		keep := n
		if keep > maxArrayKeep {
			keep = maxArrayKeep
		}
		arr := make([]any, 0, keep)
		for i := uint64(0); i < n; i++ {
			v, err := readValue(r, et)
			if err != nil {
				return nil, err
			}
			if i < keep {
				arr = append(arr, v)
			}
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unknown value type %d", t)
	}
}

// Uint returns kv[key] as a uint64. For per-layer arrays it returns the
// largest element, which is the value that sizes buffers.
func Uint(kv map[string]any, key string) (uint64, bool) {
	return toUint(kv[key])
}

func toUint(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case float64:
		if n < 0 || math.IsNaN(n) {
			return 0, false
		}
		return uint64(n), true
	case []any:
		var best uint64
		found := false
		for _, e := range n {
			if u, ok := toUint(e); ok {
				found = true
				if u > best {
					best = u
				}
			}
		}
		return best, found
	default:
		return 0, false
	}
}

// String returns kv[key] as a string.
func String(kv map[string]any, key string) (string, bool) {
	s, ok := kv[key].(string)
	return s, ok
}
