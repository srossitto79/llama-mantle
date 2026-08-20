package mantle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/billziss-gh/golib/shlex"
	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/event"
	"github.com/mostlygeek/llama-swap/internal/shared"
)

// Handler bundles all mantle API handlers with their dependencies.
type Handler struct {
	tm          *TaskManager
	cfg         *config.Config
	configPath  string
	modelsDir   string
	backendsDir string
	buildScript string
	configMu    sync.Mutex
}

// NewHandler creates a new mantle API handler. tm is constructed once by the
// caller and outlives config reloads, so long-running tasks (builds,
// downloads) stay visible across a reload instead of being silently orphaned.
func NewHandler(tm *TaskManager, cfg *config.Config, configPath, modelsDir, backendsDir, buildScript string) *Handler {
	tm.StartStudioStagingCleanup(modelsDir)
	h := &Handler{
		tm:          tm,
		cfg:         cfg,
		configPath:  configPath,
		modelsDir:   modelsDir,
		backendsDir: backendsDir,
		buildScript: buildScript,
	}
	tm.SetStudioRegister(h.registerStudioModel)
	return h
}

func (h *Handler) jsonStudioTaskResponse(w http.ResponseWriter, r *http.Request, task *Task) {
	if err := h.tm.AssignStudioTaskProject(task, r.Header.Get("X-Studio-Project")); err != nil {
		h.tm.CancelTask(task.ID)
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusAccepted, task.Snapshot())
}

// RegisterRoutes adds all mantle API endpoints to the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// HF model browsing
	mux.HandleFunc("GET /api/mantle/models/search", h.handleSearchModels)
	mux.HandleFunc("GET /api/mantle/models/files", h.handleListModelFiles)

	// Download management
	mux.HandleFunc("POST /api/mantle/models/download", h.handleStartDownload)
	mux.HandleFunc("POST /api/mantle/models/download/repo", h.handleStartRepoDownload)
	mux.HandleFunc("DELETE /api/mantle/models/download/{id}", h.handleCancelDownload)
	mux.HandleFunc("GET /api/mantle/models/download/{id}/stream", h.handleDownloadProgress)

	// Local model management
	mux.HandleFunc("GET /api/mantle/models/local", h.handleListLocalModels)
	mux.HandleFunc("DELETE /api/mantle/models/local/{name...}", h.handleDeleteLocalModel)

	// Size estimates (weights + KV cache) for configured models
	mux.HandleFunc("GET /api/mantle/models/estimates", h.handleModelEstimates)

	// Llama Studio
	mux.HandleFunc("GET /api/mantle/studio/models/inspect", h.handleInspectStudioModel)
	mux.HandleFunc("GET /api/mantle/studio/datasets/inspect", h.handleInspectStudioDataset)
	mux.HandleFunc("GET /api/mantle/studio/datasets", h.handleListStudioDatasets)
	mux.HandleFunc("DELETE /api/mantle/studio/datasets/{name...}", h.handleDeleteStudioDataset)
	mux.HandleFunc("GET /api/mantle/studio/datasets/preview", h.handlePreviewStudioDataset)
	mux.HandleFunc("POST /api/mantle/studio/datasets/import", h.handleImportStudioDataset)
	mux.HandleFunc("GET /api/mantle/studio/datasets/hub/search", h.handleSearchHFDatasets)
	mux.HandleFunc("GET /api/mantle/studio/datasets/hub/files", h.handleListHFDatasetFiles)
	mux.HandleFunc("POST /api/mantle/studio/datasets/hub/download", h.handleDownloadHFDataset)
	mux.HandleFunc("POST /api/mantle/studio/quantize", h.handleStartQuantize)
	mux.HandleFunc("POST /api/mantle/studio/hash", h.handleStartHash)
	mux.HandleFunc("POST /api/mantle/studio/split", h.handleStartSplit)
	mux.HandleFunc("POST /api/mantle/studio/merge", h.handleStartMerge)
	mux.HandleFunc("POST /api/mantle/studio/prune", h.handleStartPrune)
	mux.HandleFunc("POST /api/mantle/studio/train/qlora", h.handleStartTrainQLoRA)
	mux.HandleFunc("POST /api/mantle/studio/distill", h.handleStartDistill)
	mux.HandleFunc("POST /api/mantle/studio/export/lora", h.handleStartExportLoRA)
	mux.HandleFunc("POST /api/mantle/studio/evaluate", h.handleStartEvaluate)
	mux.HandleFunc("POST /api/mantle/studio/utility", h.handleStartStudioUtility)
	mux.HandleFunc("POST /api/mantle/studio/pipelines", h.handleStartStudioPipeline)
	mux.HandleFunc("POST /api/mantle/studio/pipelines/{id}/retry", h.handleRetryStudioPipeline)
	mux.HandleFunc("GET /api/mantle/studio/pipeline-templates", h.handleListStudioPipelineTemplates)
	mux.HandleFunc("POST /api/mantle/studio/pipeline-templates", h.handleSaveStudioPipelineTemplate)
	mux.HandleFunc("DELETE /api/mantle/studio/pipeline-templates/{id}", h.handleDeleteStudioPipelineTemplate)
	mux.HandleFunc("POST /api/mantle/studio/register", h.handleRegisterStudioModel)
	mux.HandleFunc("GET /api/mantle/studio/artifacts", h.handleListStudioArtifacts)
	mux.HandleFunc("GET /api/mantle/studio/lineage", h.handleStudioLineage)
	mux.HandleFunc("PATCH /api/mantle/studio/artifacts/annotation", h.handleSaveStudioArtifactAnnotation)
	mux.HandleFunc("POST /api/mantle/studio/artifacts/verify", h.handleVerifyStudioArtifact)
	mux.HandleFunc("POST /api/mantle/studio/artifacts/verify-bulk", h.handleVerifyStudioArtifacts)
	mux.HandleFunc("POST /api/mantle/studio/artifacts/cleanup", h.handleCleanupStudioArtifact)
	mux.HandleFunc("POST /api/mantle/studio/artifacts/retention/preview", h.handlePreviewStudioRetention)
	mux.HandleFunc("POST /api/mantle/studio/artifacts/retention/apply", h.handleApplyStudioRetention)
	mux.HandleFunc("GET /api/mantle/studio/evaluations", h.handleListStudioEvaluations)
	mux.HandleFunc("DELETE /api/mantle/studio/jobs/{id}", h.handleCancelStudioJob)
	mux.HandleFunc("GET /api/mantle/studio/scheduler", h.handleStudioScheduler)
	mux.HandleFunc("POST /api/mantle/studio/preflight", h.handleStudioPreflight)
	mux.HandleFunc("GET /api/mantle/studio/resources", h.handleListStudioResources)
	mux.HandleFunc("GET /api/mantle/studio/projects", h.handleListStudioProjects)
	mux.HandleFunc("POST /api/mantle/studio/projects", h.handleSaveStudioProject)
	mux.HandleFunc("PUT /api/mantle/studio/projects/{id}/resources", h.handleSetStudioProjectResources)
	mux.HandleFunc("DELETE /api/mantle/studio/projects/{id}", h.handleDeleteStudioProject)

	// Config management
	mux.HandleFunc("GET /api/mantle/config", h.handleGetConfig)
	mux.HandleFunc("PUT /api/mantle/config", h.handlePutConfig)
	mux.HandleFunc("GET /api/mantle/config/models", h.handleListConfigModels)
	mux.HandleFunc("PUT /api/mantle/config/models/{name}", h.handlePutConfigModel)
	mux.HandleFunc("DELETE /api/mantle/config/models/{name}", h.handleDeleteConfigModel)
	mux.HandleFunc("GET /api/mantle/config/groups", h.handleListConfigGroups)
	mux.HandleFunc("PUT /api/mantle/config/groups/{name}", h.handlePutConfigGroup)
	mux.HandleFunc("DELETE /api/mantle/config/groups/{name}", h.handleDeleteConfigGroup)

	// Command string <-> argv helpers, for the guided flag editor
	mux.HandleFunc("POST /api/mantle/cmd/tokenize", h.handleTokenizeCmd)
	mux.HandleFunc("POST /api/mantle/cmd/build", h.handleBuildCmd)

	// Backend builds
	mux.HandleFunc("POST /api/mantle/backends/build", h.handleStartBuild)
	mux.HandleFunc("DELETE /api/mantle/backends/build/{id}", h.handleCancelBuild)
	mux.HandleFunc("GET /api/mantle/backends/build/{id}/stream", h.handleBuildProgress)
	mux.HandleFunc("GET /api/mantle/backends", h.handleListBackends)
	mux.HandleFunc("GET /api/mantle/backends/{name}/schema", h.handleGetBackendSchema)
	mux.HandleFunc("POST /api/mantle/backends/{name}/update", h.handleUpdateBackend)
	mux.HandleFunc("DELETE /api/mantle/backends/{name...}", h.handleDeleteBackend)

	// Task status
	mux.HandleFunc("GET /api/mantle/tasks", h.handleListTasks)
	mux.HandleFunc("GET /api/mantle/tasks/{id}", h.handleGetTask)
	mux.HandleFunc("GET /api/mantle/tasks/{id}/stream", h.handleTaskProgress)
}

func (h *Handler) handleInspectStudioModel(w http.ResponseWriter, r *http.Request) {
	inspection, err := InspectStudioModel(h.modelsDir, r.URL.Query().Get("name"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, inspection)
}

func (h *Handler) handleStudioPreflight(w http.ResponseWriter, r *http.Request) {
	var req StudioPreflightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	report, err := h.tm.StudioPreflight(h.modelsDir, req)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, report)
}

func (h *Handler) handleListStudioResources(w http.ResponseWriter, _ *http.Request) {
	resources, err := h.tm.ListStudioResources(h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, resources)
}

func (h *Handler) handleListStudioProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := h.tm.ListStudioProjects()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, projects)
}
func (h *Handler) handleSaveStudioProject(w http.ResponseWriter, r *http.Request) {
	var project StudioProject
	if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	saved, err := h.tm.SaveStudioProject(project)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, saved)
}
func (h *Handler) handleSetStudioProjectResources(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.tm.SetStudioProjectResources(r.PathValue("id"), req.Paths, h.modelsDir); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) handleDeleteStudioProject(w http.ResponseWriter, r *http.Request) {
	deleted, err := h.tm.DeleteStudioProject(r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !deleted {
		jsonError(w, http.StatusNotFound, "project not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleInspectStudioDataset(w http.ResponseWriter, r *http.Request) {
	inspection, err := InspectStudioDataset(h.modelsDir, r.URL.Query().Get("name"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, inspection)
}

func (h *Handler) handleListStudioDatasets(w http.ResponseWriter, _ *http.Request) {
	datasets, err := ListStudioDatasets(h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, datasets)
}

func (h *Handler) handleDeleteStudioDataset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "dataset name is required")
		return
	}
	if err := DeleteStudioDataset(h.modelsDir, name); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "deleted"})
}

func (h *Handler) handlePreviewStudioDataset(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	preview, err := PreviewStudioDataset(h.modelsDir, r.URL.Query().Get("name"), limit)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, preview)
}

func (h *Handler) handleImportStudioDataset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid or oversized upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "dataset file is required")
		return
	}
	defer file.Close()
	destination := strings.TrimSpace(r.FormValue("destination"))
	if destination == "" {
		destination = filepath.Base(header.Filename)
	}
	dataset, err := ImportStudioDataset(h.modelsDir, destination, file)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusCreated, dataset)
}

func (h *Handler) handleSearchHFDatasets(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		jsonError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	datasets, err := SearchHFDatasets(query, limit, r.URL.Query().Get("sort"))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, datasets)
}

func (h *Handler) handleListHFDatasetFiles(w http.ResponseWriter, r *http.Request) {
	datasetID := strings.TrimSpace(r.URL.Query().Get("dataset"))
	if datasetID == "" {
		jsonError(w, http.StatusBadRequest, "dataset is required")
		return
	}
	files, err := ListHFDatasetFiles(datasetID)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, files)
}

func (h *Handler) handleDownloadHFDataset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DatasetID string `json:"datasetID"`
		Filename  string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartHFDatasetDownload(req.DatasetID, req.Filename, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartQuantize(w http.ResponseWriter, r *http.Request) {
	var req QuantizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartQuantize(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartHash(w http.ResponseWriter, r *http.Request) {
	var req HashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartHash(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartSplit(w http.ResponseWriter, r *http.Request) {
	var req SplitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartSplit(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartMerge(w http.ResponseWriter, r *http.Request) {
	var req MergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartMerge(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartPrune(w http.ResponseWriter, r *http.Request) {
	var req PruneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartPrune(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartTrainQLoRA(w http.ResponseWriter, r *http.Request) {
	var req TrainQLoRARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartTrainQLoRA(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartDistill(w http.ResponseWriter, r *http.Request) {
	var req DistillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartDistill(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartExportLoRA(w http.ResponseWriter, r *http.Request) {
	var req ExportLoRARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartExportLoRA(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartEvaluate(w http.ResponseWriter, r *http.Request) {
	var req EvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.StartEvaluate(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartStudioUtility(w http.ResponseWriter, r *http.Request) {
	var req StudioUtilityRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid utility request: "+err.Error())
		return
	}
	task, err := h.tm.StartStudioUtility(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleStartStudioPipeline(w http.ResponseWriter, r *http.Request) {
	var req StudioPipelineRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid pipeline request: "+err.Error())
		return
	}
	if req.ProjectID == "" {
		req.ProjectID = r.Header.Get("X-Studio-Project")
	}
	task, err := h.tm.StartStudioPipeline(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleRetryStudioPipeline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromStep int `json:"fromStep"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task, err := h.tm.RetryStudioPipeline(r.PathValue("id"), req.FromStep, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleListStudioPipelineTemplates(w http.ResponseWriter, _ *http.Request) {
	templates, err := h.tm.ListStudioPipelineTemplates()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, templates)
}

func (h *Handler) handleSaveStudioPipelineTemplate(w http.ResponseWriter, r *http.Request) {
	var template StudioPipelineTemplate
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&template); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid pipeline template: "+err.Error())
		return
	}
	if template.ProjectID == "" {
		template.ProjectID = r.Header.Get("X-Studio-Project")
	}
	saved, err := h.tm.SaveStudioPipelineTemplate(template)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, saved)
}

func (h *Handler) handleDeleteStudioPipelineTemplate(w http.ResponseWriter, r *http.Request) {
	deleted, err := h.tm.DeleteStudioPipelineTemplate(r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !deleted {
		jsonError(w, http.StatusNotFound, "pipeline template not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "deleted"})
}

func (h *Handler) handleCancelStudioJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task := h.tm.GetTask(id)
	if task == nil || task.Type != "studio" || !h.tm.CancelTask(id) {
		jsonError(w, http.StatusNotFound, "running Studio job not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "cancelled"})
}

func (h *Handler) handleStudioScheduler(w http.ResponseWriter, _ *http.Request) {
	jsonResponse(w, http.StatusOK, h.tm.StudioSchedulerStatus())
}

func (h *Handler) handleTaskProgress(w http.ResponseWriter, r *http.Request) {
	task := h.tm.GetTask(r.PathValue("id"))
	if task == nil {
		jsonError(w, http.StatusNotFound, "task not found")
		return
	}
	h.streamProgress(w, r, task)
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

// --- HF Model Search ---

func (h *Handler) handleSearchModels(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		jsonError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	sort := r.URL.Query().Get("sort")
	kind := r.URL.Query().Get("kind")
	models, err := SearchHFModels(query, limit, sort, kind)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, models)
}

func (h *Handler) handleListModelFiles(w http.ResponseWriter, r *http.Request) {
	modelID := r.URL.Query().Get("model")
	if modelID == "" {
		jsonError(w, http.StatusBadRequest, "query parameter 'model' is required")
		return
	}
	files, err := ListHFFiles(modelID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, files)
}

// --- Download ---

type downloadRequest struct {
	ModelID  string `json:"modelID"`
	Filename string `json:"filename"`
}

func (h *Handler) handleStartDownload(w http.ResponseWriter, r *http.Request) {
	var req downloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ModelID == "" || req.Filename == "" {
		jsonError(w, http.StatusBadRequest, "modelID and filename are required")
		return
	}
	task := h.tm.StartDownload(req.ModelID, req.Filename, h.modelsDir)
	jsonResponse(w, http.StatusAccepted, task)
}

type repoDownloadRequest struct {
	ModelID string `json:"modelID"`
}

func (h *Handler) handleStartRepoDownload(w http.ResponseWriter, r *http.Request) {
	var req repoDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ModelID == "" {
		jsonError(w, http.StatusBadRequest, "modelID is required")
		return
	}
	task := h.tm.StartRepoDownload(req.ModelID, h.modelsDir)
	jsonResponse(w, http.StatusAccepted, task)
}

func (h *Handler) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.tm.CancelTask(id) {
		jsonError(w, http.StatusNotFound, "task not found or already completed")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "cancelled"})
}

func (h *Handler) handleDownloadProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task := h.tm.GetTask(id)
	if task == nil {
		jsonError(w, http.StatusNotFound, "task not found")
		return
	}
	h.streamProgress(w, r, task)
}

// --- Local Models ---

func (h *Handler) handleListLocalModels(w http.ResponseWriter, r *http.Request) {
	models, err := ListLocalModels(h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, models)
}

// handleModelEstimates returns weight + KV-cache size estimates for every
// configured model, keyed by model ID. Models whose GGUF cannot be read are
// omitted rather than failing the whole response.
func (h *Handler) handleModelEstimates(w http.ResponseWriter, r *http.Request) {
	estimates := make(map[string]*ModelEstimate)
	for id, mc := range h.cfg.Models {
		est, err := EstimateModel(mc.Cmd, h.modelsDir)
		if err != nil {
			continue
		}
		estimates[id] = est
	}
	jsonResponse(w, http.StatusOK, estimates)
}

func (h *Handler) handleDeleteLocalModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "model name is required")
		return
	}
	if err := DeleteLocalModel(h.modelsDir, name); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "deleted"})
}

// --- Config ---

func (h *Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

func (h *Handler) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if err := h.applyConfigBytes(body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "config updated and reloaded"})
}

// applyConfigBytes validates a full config YAML document, writes it to disk,
// and hot-reloads the running config. Shared by the raw config PUT and the
// structured models/groups editing routes below, which produce their new
// full-document bytes via targeted yaml.Node surgery (see config_edit.go)
// rather than a client-supplied full document.
func (h *Handler) applyConfigBytes(body []byte) error {
	h.configMu.Lock()
	defer h.configMu.Unlock()

	// Validate by attempting to parse
	newCfg, err := config.LoadConfigFromReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Preserve runtime paths
	newCfg.ConfigPath = h.configPath
	newCfg.ModelsDir = h.modelsDir
	newCfg.BackendsDir = h.backendsDir
	newCfg.BuildScript = h.buildScript

	// Write to config file
	if err := os.WriteFile(h.configPath, body, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Emit config changed event so the server hot-reloads
	event.Emit(shared.ConfigFileChangedEvent{State: shared.ReloadingStateStart})
	*h.cfg = newCfg
	event.Emit(shared.ConfigFileChangedEvent{State: shared.ReloadingStateEnd})
	return nil
}

func (h *Handler) handleListConfigModels(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, ListModels(h.cfg))
}

func (h *Handler) handlePutConfigModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "model name is required")
		return
	}
	var model config.ModelConfig
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))
		return
	}
	newRaw, err := UpsertModelYAML(raw, name, model)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.applyConfigBytes(newRaw); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "model saved"})
}

func (h *Handler) handleDeleteConfigModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "model name is required")
		return
	}
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))
		return
	}
	newRaw, err := DeleteModelYAML(raw, name)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.applyConfigBytes(newRaw); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "model deleted"})
}

func (h *Handler) handleListConfigGroups(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, ListGroups(h.cfg))
}

func (h *Handler) handlePutConfigGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "group name is required")
		return
	}
	var group config.GroupConfig
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))
		return
	}
	newRaw, err := UpsertGroupYAML(raw, name, group)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.applyConfigBytes(newRaw); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "group saved"})
}

func (h *Handler) handleDeleteConfigGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "group name is required")
		return
	}
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))
		return
	}
	newRaw, err := DeleteGroupYAML(raw, name)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.applyConfigBytes(newRaw); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "group deleted"})
}

func (h *Handler) handleTokenizeCmd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	argv, err := config.SanitizeCommand(req.Cmd)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"argv": argv})
}

func (h *Handler) handleBuildCmd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Argv []string `json:"argv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"cmd": config.BuildCommandString(req.Argv)})
}

func (h *Handler) handleRegisterStudioModel(w http.ResponseWriter, r *http.Request) {
	var req RegisterStudioModelRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid registration request: "+err.Error())
		return
	}
	task, err := h.tm.StartRegisterStudioModel(req, h.modelsDir, h.registerStudioModel)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleListStudioArtifacts(w http.ResponseWriter, r *http.Request) {
	limit := 250
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	artifacts, err := h.tm.ListStudioCatalogArtifacts(h.modelsDir, r.URL.Query().Get("kind"), limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, artifacts)
}

func (h *Handler) handleStudioLineage(w http.ResponseWriter, r *http.Request) {
	edges, err := h.tm.StudioLineage(r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, edges)
}

func (h *Handler) handleSaveStudioArtifactAnnotation(w http.ResponseWriter, r *http.Request) {
	var req StudioArtifactAnnotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid annotation request")
		return
	}
	if err := h.tm.SaveStudioArtifactAnnotation(req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "saved"})
}

func (h *Handler) handleVerifyStudioArtifact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid verification request")
		return
	}
	task, err := h.tm.StartVerifyStudioArtifact(req.Path, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleVerifyStudioArtifacts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid bulk verification request")
		return
	}
	task, err := h.tm.StartVerifyStudioArtifacts(req.Paths, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleCleanupStudioArtifact(w http.ResponseWriter, r *http.Request) {
	var req StudioArtifactCleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid cleanup request")
		return
	}
	task, err := h.tm.StartCleanupStudioArtifact(req, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handlePreviewStudioRetention(w http.ResponseWriter, r *http.Request) {
	var policy StudioRetentionPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid retention policy")
		return
	}
	preview, err := h.tm.PreviewStudioRetention(h.modelsDir, policy)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, preview)
}

func (h *Handler) handleApplyStudioRetention(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Policy StudioRetentionPolicy `json:"policy"`
		Token  string                `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid retention apply request")
		return
	}
	task, err := h.tm.StartApplyStudioRetention(req.Policy, req.Token, h.modelsDir)
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	h.jsonStudioTaskResponse(w, r, task)
}

func (h *Handler) handleListStudioEvaluations(w http.ResponseWriter, r *http.Request) {
	evaluations, err := h.tm.ListStudioEvaluations(r.URL.Query().Get("model"))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, evaluations)
}

func (h *Handler) registerStudioModel(req RegisterStudioModelRequest, modelPath string) error {
	h.configMu.Lock()
	defer h.configMu.Unlock()
	body, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	updated, err := addStudioModelToConfig(body, req, modelPath)
	if err != nil {
		return err
	}
	newCfg, err := config.LoadConfigFromReader(bytes.NewReader(updated))
	if err != nil {
		return fmt.Errorf("validate updated config: %w", err)
	}
	newCfg.ConfigPath = h.configPath
	newCfg.ModelsDir = h.modelsDir
	newCfg.BackendsDir = h.backendsDir
	newCfg.BuildScript = h.buildScript
	if err := os.WriteFile(h.configPath, updated, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	event.Emit(shared.ConfigFileChangedEvent{State: shared.ReloadingStateStart})
	*h.cfg = newCfg
	event.Emit(shared.ConfigFileChangedEvent{State: shared.ReloadingStateEnd})
	return nil
}

// --- Backend Builds ---

type buildRequest struct {
	BackendName string   `json:"backendName"`
	Repo        string   `json:"repo"`
	Branch      string   `json:"branch"`
	CMakeArgs   []string `json:"cmakeArgs"`
	CMakeFlags  string   `json:"cmakeFlags"`
}

func (h *Handler) handleStartBuild(w http.ResponseWriter, r *http.Request) {
	var req buildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Repo == "" {
		jsonError(w, http.StatusBadRequest, "repo is required")
		return
	}
	if req.BackendName != "" && !isSafeBackendName(req.BackendName) {
		jsonError(w, http.StatusBadRequest, "backendName may only contain letters, numbers, dot, underscore, and hyphen")
		return
	}
	cmakeArgs := req.CMakeArgs
	if req.CMakeFlags != "" {
		var parsed []string
		if runtime.GOOS == "windows" {
			parsed = shlex.Windows.Split(req.CMakeFlags)
		} else {
			parsed = shlex.Posix.Split(req.CMakeFlags)
		}
		if parsed == nil {
			jsonError(w, http.StatusBadRequest, "invalid CMake flags")
			return
		}
		cmakeArgs = append(cmakeArgs, parsed...)
	}
	task := h.tm.StartBuild(req.Repo, req.Branch, req.BackendName, h.buildScript, h.backendsDir, cmakeArgs, false)
	jsonResponse(w, http.StatusAccepted, task)
}

func (h *Handler) handleCancelBuild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.tm.CancelTask(id) {
		jsonError(w, http.StatusNotFound, "task not found or already completed")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "cancelled"})
}

func (h *Handler) handleBuildProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task := h.tm.GetTask(id)
	if task == nil {
		jsonError(w, http.StatusNotFound, "task not found")
		return
	}
	h.streamProgress(w, r, task)
}

// --- Backend Listing ---

func (h *Handler) handleListBackends(w http.ResponseWriter, r *http.Request) {
	backends, err := ListBackends(h.backendsDir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, backends)
}

func (h *Handler) handleGetBackendSchema(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || !isSafeBackendName(name) {
		jsonError(w, http.StatusBadRequest, "invalid backend name")
		return
	}
	schema, err := LoadOrBuildBackendSchema(h.backendsDir, name)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, schema)
}

func (h *Handler) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "backend name is required")
		return
	}
	if !isSafeBackendName(name) {
		jsonError(w, http.StatusBadRequest, "invalid backend name")
		return
	}
	if err := DeleteBackend(h.backendsDir, name); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "deleted"})
}

type updateBackendRequest struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

// handleUpdateBackend rebuilds an existing backend from its source, replacing
// it in place. Backends built before source tracking was added (or installed
// by other means) have no meta.json; the caller can supply repo/branch in the
// request body to adopt them, after which future updates no longer need it.
func (h *Handler) handleUpdateBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, http.StatusBadRequest, "backend name is required")
		return
	}
	if !isSafeBackendName(name) {
		jsonError(w, http.StatusBadRequest, "invalid backend name")
		return
	}

	var req updateBackendRequest
	if r.ContentLength != 0 {
		json.NewDecoder(r.Body).Decode(&req)
	}

	if req.Repo == "" {
		metaData, err := os.ReadFile(filepath.Join(h.backendsDir, name, "meta.json"))
		if err != nil {
			jsonError(w, http.StatusNotFound, "backend has no build metadata; provide repo/branch to adopt it")
			return
		}
		var meta struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
		}
		if err := json.Unmarshal(metaData, &meta); err != nil || meta.Repo == "" {
			jsonError(w, http.StatusInternalServerError, "failed to read build metadata")
			return
		}
		req.Repo, req.Branch = meta.Repo, meta.Branch
	}

	// The old backend is left in place until the new build succeeds -
	// StartBuild builds into a staging dir and swaps it in atomically, so a
	// failed rebuild doesn't lose the working binary.
	task := h.tm.StartBuild(req.Repo, req.Branch, name, h.buildScript, h.backendsDir, nil, true)
	jsonResponse(w, http.StatusAccepted, task)
}

// --- Task listing ---

func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, h.tm.ListTasks())
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task := h.tm.GetTask(id)
	if task == nil {
		jsonError(w, http.StatusNotFound, "task not found")
		return
	}
	jsonResponse(w, http.StatusOK, task.Snapshot())
}

// streamProgress sends SSE events for a task's progress.
// It subscribes to the appropriate progress events and forwards them.
func (h *Handler) streamProgress(w http.ResponseWriter, r *http.Request, task *Task) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// Send initial state
	task.mu.Lock()
	lastUpdated := task.UpdatedAt
	initialTerminal := isTerminalTaskState(task.State)
	initial := map[string]any{
		"id":      task.ID,
		"type":    task.Type,
		"state":   task.State,
		"message": task.Message,
		"pct":     task.Pct,
	}
	task.mu.Unlock()
	data, _ := json.Marshal(initial)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
	if initialTerminal {
		return
	}

	// Subscribe to events
	ctx := r.Context()
	eventCh := make(chan any, 64)

	var cancel1, cancel2 context.CancelFunc

	if task.Type == "build" {
		cancel1 = event.SubscribeTo(event.Default, shared.BackendBuildProgressEventID,
			func(ev shared.BackendBuildProgressEvent) {
				if ev.TaskID != task.ID {
					return
				}
				select {
				case eventCh <- ev:
				case <-ctx.Done():
				default:
				}
			})
	} else if task.Type == "download" {
		cancel2 = event.SubscribeTo(event.Default, shared.ModelDownloadProgressEventID,
			func(ev shared.ModelDownloadProgressEvent) {
				if ev.TaskID != task.ID {
					return
				}
				select {
				case eventCh <- ev:
				case <-ctx.Done():
				default:
				}
			})
	}

	defer func() {
		if cancel1 != nil {
			cancel1()
		}
		if cancel2 != nil {
			cancel2()
		}
	}()

	// Also subscribe to generic task state changes for task completion
	taskDone := task.Done()
	var ticker *time.Ticker
	var tickerCh <-chan time.Time
	if task.Type == "studio" {
		ticker = time.NewTicker(250 * time.Millisecond)
		tickerCh = ticker.C
		defer ticker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-taskDone:
			// Final state
			task.mu.Lock()
			final := map[string]any{
				"id":      task.ID,
				"state":   task.State,
				"message": task.Message,
				"pct":     task.Pct,
			}
			task.mu.Unlock()
			data, _ := json.Marshal(final)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		case ev := <-eventCh:
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-tickerCh:
			task.mu.Lock()
			if !task.UpdatedAt.After(lastUpdated) {
				task.mu.Unlock()
				continue
			}
			lastUpdated = task.UpdatedAt
			update := map[string]any{
				"id":        task.ID,
				"state":     task.State,
				"message":   task.Message,
				"pct":       task.Pct,
				"logs":      append([]string(nil), task.Logs...),
				"exitCode":  task.ExitCode,
				"artifacts": append([]Artifact(nil), task.Artifacts...),
			}
			terminal := isTerminalTaskState(task.State)
			task.mu.Unlock()
			data, _ := json.Marshal(update)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if terminal {
				return
			}
		}
	}
}
