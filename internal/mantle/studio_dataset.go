package mantle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

const (
	maxDatasetInspectionRecords = 1000
	maxDatasetRecordBytes       = 8 * 1024 * 1024
)

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
