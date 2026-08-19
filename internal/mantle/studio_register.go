package mantle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type RegisterStudioModelRequest struct {
	Model       string `json:"model"`
	ModelID     string `json:"modelID"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	ContextSize int    `json:"contextSize,omitempty"`
	GPULayers   int    `json:"gpuLayers,omitempty"`
	TTL         int    `json:"ttl,omitempty"`
	Overwrite   bool   `json:"overwrite,omitempty"`
}

func (tm *TaskManager) SetStudioRegister(register func(RegisterStudioModelRequest, string) error) {
	tm.mu.Lock()
	tm.studioRegister = register
	tm.mu.Unlock()
}

func (tm *TaskManager) studioRegisterFunc() func(RegisterStudioModelRequest, string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.studioRegister
}

func (tm *TaskManager) StartRegisterStudioModel(req RegisterStudioModelRequest, modelsDir string, register func(RegisterStudioModelRequest, string) error) (*Task, error) {
	modelPath, modelName, err := resolveStudioInput(modelsDir, req.Model, ".gguf")
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	if !isSafeBackendName(req.ModelID) {
		return nil, fmt.Errorf("modelID may only contain letters, numbers, dot, underscore, and hyphen")
	}
	if req.ContextSize < 0 || req.GPULayers < -1 || req.TTL < 0 {
		return nil, fmt.Errorf("registration numeric options are outside their supported range")
	}
	task := tm.newStudioTask("register", modelName, modelName, map[string]any{
		"modelID": req.ModelID, "name": req.Name, "contextSize": req.ContextSize,
		"gpuLayers": req.GPULayers, "ttl": req.TTL, "overwrite": req.Overwrite,
	})
	tm.enqueueStudioTask(task, StudioJobLight, func() {
		task.UpdateProgress(TaskRunning, "Registering model for serving...", 25)
		if err := register(req, modelPath); err != nil {
			task.UpdateProgress(TaskFailed, err.Error(), 0)
			return
		}
		info, _ := os.Stat(modelPath)
		if info != nil {
			task.AddArtifact(Artifact{Name: modelName, Path: modelName, Size: info.Size(), Kind: "served-model"})
		}
		task.UpdateProgress(TaskCompleted, "Model registered for serving", 100)
	})
	return task, nil
}

func addStudioModelToConfig(body []byte, req RegisterStudioModelRequest, modelPath string) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("parse config YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root must be a mapping")
	}
	root := document.Content[0]
	models := mappingValue(root, "models")
	if models == nil {
		models = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, scalarNode("models"), models)
	}
	if models.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config models must be a mapping")
	}
	index := mappingKeyIndex(models, req.ModelID)
	if index >= 0 && !req.Overwrite {
		return nil, fmt.Errorf("model ID %q is already registered", req.ModelID)
	}
	command := "llama-server --model " + strconv.Quote(filepath.Clean(modelPath)) + " --port ${PORT}"
	if req.ContextSize > 0 {
		command += " --ctx-size " + strconv.Itoa(req.ContextSize)
	}
	if req.GPULayers != 0 {
		command += " --n-gpu-layers " + strconv.Itoa(req.GPULayers)
	}
	model := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendMapping(model, "cmd", command)
	appendMapping(model, "proxy", "http://127.0.0.1:${PORT}")
	appendMapping(model, "checkEndpoint", "/health")
	if req.TTL > 0 {
		appendMapping(model, "ttl", strconv.Itoa(req.TTL))
		model.Content[len(model.Content)-1].Tag = "!!int"
	}
	if strings.TrimSpace(req.Name) != "" {
		appendMapping(model, "name", strings.TrimSpace(req.Name))
	}
	if strings.TrimSpace(req.Description) != "" {
		appendMapping(model, "description", strings.TrimSpace(req.Description))
	}
	if index >= 0 {
		models.Content[index+1] = model
	} else {
		models.Content = append(models.Content, scalarNode(req.ModelID), model)
	}
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return nil, fmt.Errorf("encode config YAML: %w", err)
	}
	return updated, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	index := mappingKeyIndex(mapping, key)
	if index < 0 {
		return nil
	}
	return mapping.Content[index+1]
}

func mappingKeyIndex(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func appendMapping(mapping *yaml.Node, key, value string) {
	mapping.Content = append(mapping.Content, scalarNode(key), scalarNode(value))
}
