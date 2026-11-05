package mantle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/configedit"
	"github.com/mostlygeek/llama-swap/internal/event"
	"github.com/mostlygeek/llama-swap/internal/shared"
)

// Reload modes reported with a preview. With -watch-config, llama-swap
// rebuilds the whole server on any change to config.yaml and the old server's
// shutdown stops every running model, changed or not (llama-swap.go reload).
// Without it, the file is written and the change takes effect on restart.
const (
	reloadStopsAll  = "all"
	reloadOnRestart = "restart"
)

type configChangesRequest struct {
	BaseHash string              `json:"baseHash"`
	Changes  []configedit.Change `json:"changes"`
}

type configPreviewResponse struct {
	configedit.Plan
	Reload string `json:"reload"`
}

type configErrorResponse struct {
	Error    string                  `json:"error"`
	Code     string                  `json:"code,omitempty"`
	Change   *configedit.ChangeError `json:"change,omitempty"`
	Problems []configedit.Problem    `json:"problems,omitempty"`
}

func (h *Handler) reloadMode() string {
	if h.cfg.WatchConfig {
		return reloadStopsAll
	}
	return reloadOnRestart
}

func (h *Handler) readConfig() ([]byte, error) {
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	return raw, nil
}

// writeConfigLocked validates body, writes it in place and swaps the live
// config. The caller holds configMu. config.yaml is a single-file bind mount
// in Docker, so it is written in place rather than renamed over.
func (h *Handler) writeConfigLocked(body []byte) error {
	newCfg, err := config.LoadConfigFromReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid config: %s", h.validator.ScrubSecrets(body, err.Error()))
	}
	newCfg.ConfigPath = h.configPath
	newCfg.ModelsDir = h.modelsDir
	newCfg.BackendsDir = h.backendsDir
	newCfg.BuildScript = h.buildScript
	newCfg.WatchConfig = h.cfg.WatchConfig
	if err := os.WriteFile(h.configPath, body, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	event.Emit(shared.ConfigFileChangedEvent{State: shared.ReloadingStateStart})
	*h.cfg = newCfg
	event.Emit(shared.ConfigFileChangedEvent{State: shared.ReloadingStateEnd})
	return nil
}

// applyChanges plans changes against the current file and writes the result
// when it validates. The read, the stale check and the write happen under one
// lock, so two editors cannot overwrite each other.
func (h *Handler) applyChanges(baseHash string, changes []configedit.Change) (configedit.Plan, error) {
	h.configMu.Lock()
	defer h.configMu.Unlock()
	raw, err := h.readConfig()
	if err != nil {
		return configedit.Plan{}, err
	}
	plan, err := h.planner.Plan(raw, baseHash, changes)
	if err != nil {
		return configedit.Plan{}, err
	}
	if len(plan.Problems) > 0 {
		return plan, errInvalidPlan
	}
	if plan.Hash == plan.BaseHash {
		return plan, nil
	}
	return plan, h.writeConfigLocked([]byte(plan.YAML))
}

var errInvalidPlan = errors.New("config is invalid")

func (h *Handler) writeChangeError(w http.ResponseWriter, plan configedit.Plan, err error) {
	var ce *configedit.ChangeError
	switch {
	case errors.As(err, &ce):
		status := http.StatusBadRequest
		if ce.Code == configedit.CodeStale {
			status = http.StatusConflict
		}
		jsonResponse(w, status, configErrorResponse{Error: ce.Message, Code: ce.Code, Change: ce})
	case errors.Is(err, errInvalidPlan):
		jsonResponse(w, http.StatusBadRequest, configErrorResponse{Error: err.Error(), Code: configedit.CodeInvalid, Problems: plan.Problems})
	default:
		jsonError(w, http.StatusInternalServerError, err.Error())
	}
}

// GET /api/mantle/config returns the file as written. The ETag is the hash
// that PUT accepts as If-Match.
func (h *Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := h.readConfig()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("ETag", `"`+configedit.Hash(raw)+`"`)
	w.Write(raw)
}

// PUT /api/mantle/config replaces the file. With If-Match, it is refused when
// the file changed since it was read.
func (h *Handler) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	h.configMu.Lock()
	defer h.configMu.Unlock()
	if match := r.Header.Get("If-Match"); match != "" {
		raw, err := h.readConfig()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if match != `"`+configedit.Hash(raw)+`"` && match != configedit.Hash(raw) {
			jsonResponse(w, http.StatusConflict, configErrorResponse{Error: "config file changed since it was read", Code: configedit.CodeStale})
			return
		}
	}
	if problems := h.validator.Validate(body); len(problems) > 0 {
		jsonResponse(w, http.StatusBadRequest, configErrorResponse{Error: problems[0].Message, Code: configedit.CodeInvalid, Problems: problems})
		return
	}
	if err := h.writeConfigLocked(body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "config updated", "hash": configedit.Hash(body), "reload": h.reloadMode()})
}

// POST /api/mantle/config/validate checks a whole file without writing it.
func (h *Handler) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"problems": h.validator.Validate(body), "reload": h.reloadMode()})
}

// GET /api/mantle/config/source returns every section as written, secrets
// redacted.
func (h *Handler) handleGetConfigSource(w http.ResponseWriter, r *http.Request) {
	raw, err := h.readConfig()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	doc, err := h.source.ReadSource(raw)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, doc)
}

func decodeChanges(w http.ResponseWriter, r *http.Request) (configChangesRequest, bool) {
	var req configChangesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return req, false
	}
	return req, true
}

// POST /api/mantle/config/changes/preview returns the file the changes would
// produce and its validation result, without writing.
func (h *Handler) handlePreviewConfigChanges(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeChanges(w, r)
	if !ok {
		return
	}
	raw, err := h.readConfig()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, err := h.planner.Plan(raw, req.BaseHash, req.Changes)
	if err != nil {
		h.writeChangeError(w, plan, err)
		return
	}
	jsonResponse(w, http.StatusOK, configPreviewResponse{Plan: plan, Reload: h.reloadMode()})
}

// POST /api/mantle/config/changes/apply applies all changes as one write.
func (h *Handler) handleApplyConfigChanges(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeChanges(w, r)
	if !ok {
		return
	}
	plan, err := h.applyChanges(req.BaseHash, req.Changes)
	if err != nil {
		h.writeChangeError(w, plan, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"hash": plan.Hash, "reload": h.reloadMode()})
}

// --- Whole-entry routes used by the model config editor ---

// GET /api/mantle/config/models returns the effective models (macros
// expanded), with ${env.*} kept as references and literal secrets redacted.
func (h *Handler) handleListConfigModels(w http.ResponseWriter, r *http.Request) {
	raw, err := h.readConfig()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models, err := h.effective.EffectiveModels(raw)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		m := models[id]
		m["id"] = id
		out = append(out, m)
	}
	jsonResponse(w, http.StatusOK, out)
}

// PUT /api/mantle/config/models/{name} takes a whole model in effective form
// and writes only the fields that differ from the current effective model.
func (h *Handler) handlePutConfigModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("name")
	var posted map[string]any
	if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	delete(posted, "id")
	raw, err := h.readConfig()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models, err := h.effective.EffectiveModels(raw)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var current map[string]any
	if m, ok := models[id]; ok {
		current = m
	}
	changes := configedit.ChangesForModelUpdate(id, current, posted)
	plan, err := h.applyChanges(configedit.Hash(raw), changes)
	if err != nil {
		h.writeChangeError(w, plan, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "model saved", "hash": plan.Hash})
}

func (h *Handler) handleDeleteConfigModel(w http.ResponseWriter, r *http.Request) {
	h.deleteEntry(w, configedit.SectionModels, r.PathValue("name"))
}

func (h *Handler) handleDeleteConfigGroup(w http.ResponseWriter, r *http.Request) {
	h.deleteEntry(w, configedit.SectionGroups, r.PathValue("name"))
}

func (h *Handler) deleteEntry(w http.ResponseWriter, section, id string) {
	plan, err := h.applyChanges("", []configedit.Change{{Op: "delete", Section: section, ID: id}})
	if err != nil {
		h.writeChangeError(w, plan, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "deleted", "hash": plan.Hash})
}

type namedGroup struct {
	Name string `json:"name"`
	config.GroupConfig
}

// GET /api/mantle/config/groups returns the configured top-level groups.
func (h *Handler) handleListConfigGroups(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(h.cfg.Groups))
	for name := range h.cfg.Groups {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]namedGroup, 0, len(names))
	for _, name := range names {
		out = append(out, namedGroup{Name: name, GroupConfig: h.cfg.Groups[name]})
	}
	jsonResponse(w, http.StatusOK, out)
}

// PUT /api/mantle/config/groups/{name} merges a whole group into its entry:
// unchanged fields, including an aliased members list, keep their source.
func (h *Handler) handlePutConfigGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var group config.GroupConfig
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	members := make([]any, len(group.Members))
	for i, m := range group.Members {
		members[i] = m
	}
	value := map[string]any{"swap": group.Swap, "exclusive": group.Exclusive, "persistent": group.Persistent, "members": members}
	var changes []configedit.Change
	if current, exists := h.cfg.Groups[name]; !exists {
		changes = []configedit.Change{{Op: "add", Section: configedit.SectionGroups, ID: name, Value: value}}
	} else {
		// Only fields that differ, so defaults left out of the file stay out.
		differs := map[string]bool{
			"swap":       current.Swap != group.Swap,
			"exclusive":  current.Exclusive != group.Exclusive,
			"persistent": current.Persistent != group.Persistent,
			"members":    fmt.Sprint(current.Members) != fmt.Sprint(group.Members),
		}
		for _, k := range []string{"swap", "exclusive", "persistent", "members"} {
			if differs[k] {
				changes = append(changes, configedit.Change{Op: "set", Section: configedit.SectionGroups, ID: name, Path: []string{k}, Value: value[k]})
			}
		}
	}
	plan, err := h.applyChanges("", changes)
	if err != nil {
		h.writeChangeError(w, plan, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "group saved", "hash": plan.Hash})
}
