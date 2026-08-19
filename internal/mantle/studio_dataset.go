package mantle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxDatasetInspectionRecords = 1000
	maxDatasetRecordBytes       = 8 * 1024 * 1024
	maxDatasetPreviewRecords    = 50
)

var studioDatasetExtensions = map[string]bool{
	".jsonl": true, ".json": true, ".txt": true, ".text": true, ".csv": true, ".parquet": true,
}

type StudioDataset struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	Format     string    `json:"format"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type DatasetPreview struct {
	DatasetInspection
	Records []json.RawMessage `json:"records"`
}

type HFDataset struct {
	ID        string   `json:"id"`
	Downloads int64    `json:"downloads"`
	Likes     int64    `json:"likes"`
	UpdatedAt string   `json:"updatedAt"`
	Tags      []string `json:"tags"`
}

func ListStudioDatasets(modelsDir string) ([]StudioDataset, error) {
	root := filepath.Join(modelsDir, "datasets")
	var datasets []StudioDataset
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !studioDatasetExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(modelsDir, path)
		if err != nil {
			return err
		}
		datasets = append(datasets, StudioDataset{Name: entry.Name(), Path: filepath.ToSlash(rel), Size: info.Size(), Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), "."), ModifiedAt: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list datasets: %w", err)
	}
	sort.Slice(datasets, func(i, j int) bool { return datasets[i].ModifiedAt.After(datasets[j].ModifiedAt) })
	return datasets, nil
}

func PreviewStudioDataset(modelsDir, name string, limit int) (*DatasetPreview, error) {
	if limit <= 0 || limit > maxDatasetPreviewRecords {
		limit = 10
	}
	path, cleanName, err := resolveStudioInput(modelsDir, name, "")
	if err != nil {
		return nil, err
	}
	if strings.ToLower(filepath.Ext(path)) != ".jsonl" {
		return nil, fmt.Errorf("preview currently supports JSONL datasets")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("preview dataset: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("preview dataset: %w", err)
	}
	defer file.Close()
	result := &DatasetPreview{DatasetInspection: DatasetInspection{Name: cleanName, Size: info.Size(), Formats: map[string]int{}}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("dataset line %d is not valid JSON: %w", line, err)
		}
		format := datasetRecordFormat(record)
		if format == "" {
			return nil, fmt.Errorf("dataset line %d must contain messages, text, or prompt and response", line)
		}
		result.Formats[format]++
		result.RecordsScanned++
		copyRecord := append(json.RawMessage(nil), scanner.Bytes()...)
		result.Records = append(result.Records, copyRecord)
		if len(result.Records) >= limit {
			result.Truncated = scanner.Scan()
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan dataset: %w", err)
	}
	if len(result.Records) == 0 {
		return nil, fmt.Errorf("dataset contains no records")
	}
	return result, nil
}

func ImportStudioDataset(modelsDir, destination string, source multipart.File) (*StudioDataset, error) {
	if !strings.HasPrefix(filepath.ToSlash(destination), "datasets/") {
		destination = "datasets/" + filepath.ToSlash(destination)
	}
	if !studioDatasetExtensions[strings.ToLower(filepath.Ext(destination))] {
		return nil, fmt.Errorf("unsupported dataset file extension")
	}
	path, clean, err := resolveStudioOutput(modelsDir, destination, "")
	if err != nil {
		return nil, err
	}
	if clean != "datasets" && !strings.HasPrefix(clean, "datasets/") {
		return nil, fmt.Errorf("dataset destination must remain inside datasets/")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".dataset-*.part")
	if err != nil {
		return nil, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	written, copyErr := io.Copy(temp, source)
	closeErr := temp.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("import dataset: %w", copyErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written == 0 {
		return nil, fmt.Errorf("dataset is empty")
	}
	if err := os.Rename(tempName, path); err != nil {
		return nil, fmt.Errorf("publish dataset: %w", err)
	}
	info, _ := os.Stat(path)
	return &StudioDataset{Name: filepath.Base(clean), Path: filepath.ToSlash(clean), Size: written, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(clean)), "."), ModifiedAt: info.ModTime()}, nil
}

func SearchHFDatasets(query string, limit int, sortBy string) ([]HFDataset, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	params := url.Values{"search": {query}, "limit": {fmt.Sprint(limit)}}
	if field := hfSortParam(sortBy); field != "" {
		params.Set("sort", field)
		params.Set("direction", "-1")
	}
	resp, err := http.Get("https://huggingface.co/api/datasets?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("HF API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HF API returned status %d", resp.StatusCode)
	}
	var raw []struct {
		ID           string   `json:"id"`
		Downloads    int64    `json:"downloads"`
		Likes        int64    `json:"likes"`
		LastModified string   `json:"lastModified"`
		Tags         []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("HF API decode failed: %w", err)
	}
	result := make([]HFDataset, 0, len(raw))
	for _, item := range raw {
		result = append(result, HFDataset{ID: item.ID, Downloads: item.Downloads, Likes: item.Likes, UpdatedAt: item.LastModified, Tags: item.Tags})
	}
	return result, nil
}

func ListHFDatasetFiles(datasetID string) ([]HFFile, error) {
	resp, err := http.Get(fmt.Sprintf("https://huggingface.co/api/datasets/%s/tree/main?recursive=true", datasetID))
	if err != nil {
		return nil, fmt.Errorf("HF API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HF API returned status %d", resp.StatusCode)
	}
	var raw []struct {
		Type, Path string
		Size       int64
		LFS        *struct {
			Size int64 `json:"size"`
		} `json:"lfs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("HF API decode failed: %w", err)
	}
	var files []HFFile
	for _, item := range raw {
		if item.Type == "directory" {
			continue
		}
		size := item.Size
		if item.LFS != nil && item.LFS.Size > 0 {
			size = item.LFS.Size
		}
		if studioDatasetExtensions[strings.ToLower(filepath.Ext(item.Path))] {
			files = append(files, HFFile{Path: item.Path, Size: size})
		}
	}
	return files, nil
}

func (tm *TaskManager) StartHFDatasetDownload(datasetID, filename, modelsDir string) (*Task, error) {
	if strings.TrimSpace(datasetID) == "" || strings.ContainsAny(datasetID, "?#\\") || strings.Contains(datasetID, "..") {
		return nil, fmt.Errorf("invalid Hugging Face dataset ID")
	}
	filename = filepath.ToSlash(filepath.Clean(filepath.FromSlash(filename)))
	if filename == "." || strings.HasPrefix(filename, "../") || filepath.IsAbs(filename) || !studioDatasetExtensions[strings.ToLower(filepath.Ext(filename))] {
		return nil, fmt.Errorf("invalid dataset filename")
	}
	destination := filepath.ToSlash(filepath.Join("datasets", strings.ReplaceAll(datasetID, "/", "_"), filepath.FromSlash(filename)))
	localPath, clean, err := resolveStudioOutput(modelsDir, destination, "")
	if err != nil {
		return nil, err
	}
	task := tm.newStudioTask("download-dataset", datasetID+"/"+filename, clean, map[string]any{"datasetID": datasetID, "filename": filename})
	tm.enqueueStudioTask(task, StudioJobIO, func() {
		task.UpdateProgress(TaskRunning, "Downloading "+filename, 0)
		downloaded, downloadErr := downloadFile(task, fmt.Sprintf("https://huggingface.co/datasets/%s/resolve/main/%s", datasetID, filename), localPath, func(done, total int64) {
			pct := -1
			if total > 0 {
				pct = int(done * 100 / total)
			}
			task.UpdateProgress(TaskRunning, fmt.Sprintf("Downloading %s (%d MiB)", filename, done/(1024*1024)), pct)
		})
		if downloadErr == errCancelled {
			return
		}
		if downloadErr != nil {
			task.UpdateProgress(TaskFailed, downloadErr.Error(), 0)
			return
		}
		task.AddArtifact(Artifact{Name: filepath.Base(clean), Path: clean, Size: downloaded, Kind: "dataset"})
		task.UpdateProgress(TaskCompleted, "Downloaded dataset to "+clean, 100)
	})
	return task, nil
}

type DatasetInspection struct {
	Name           string         `json:"name"`
	Size           int64          `json:"size"`
	RecordsScanned int            `json:"recordsScanned"`
	Formats        map[string]int `json:"formats"`
	Truncated      bool           `json:"truncated"`
}

func InspectStudioDataset(modelsDir, name string) (*DatasetInspection, error) {
	path, cleanName, err := resolveStudioInput(modelsDir, name, "")
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect dataset: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("inspect dataset: %w", err)
	}
	defer file.Close()

	result := &DatasetInspection{Name: cleanName, Size: info.Size(), Formats: map[string]int{}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("dataset line %d is not valid JSON: %w", line, err)
		}
		format := datasetRecordFormat(record)
		if format == "" {
			return nil, fmt.Errorf("dataset line %d must contain messages, text, or prompt and response", line)
		}
		result.Formats[format]++
		result.RecordsScanned++
		if result.RecordsScanned >= maxDatasetInspectionRecords {
			result.Truncated = scanner.Scan()
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan dataset: %w", err)
	}
	if result.RecordsScanned == 0 {
		return nil, fmt.Errorf("dataset contains no records")
	}
	return result, nil
}

func datasetRecordFormat(record map[string]json.RawMessage) string {
	if value, ok := record["messages"]; ok {
		var messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}
		if json.Unmarshal(value, &messages) == nil && len(messages) > 0 {
			return "messages"
		}
	}
	if value, ok := record["text"]; ok {
		var text string
		if json.Unmarshal(value, &text) == nil && text != "" {
			return "text"
		}
	}
	if prompt, ok := record["prompt"]; ok {
		response, hasResponse := record["response"]
		var promptText, responseText string
		if hasResponse && json.Unmarshal(prompt, &promptText) == nil && json.Unmarshal(response, &responseText) == nil && promptText != "" && responseText != "" {
			return "prompt-response"
		}
	}
	return ""
}
