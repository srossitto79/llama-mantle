package configedit

import "sort"

// derivedModelFields are present in the effective view but not owned by the
// model: healthCheckTimeout is copied from the global setting, and macros are
// edited as macros, not through a whole-model update.
var derivedModelFields = map[string]bool{"healthCheckTimeout": true, "macros": true}

// ChangesForModelUpdate turns a whole model posted in effective form (the
// shape EffectiveModels returns) into changes for only the fields that
// differ from current. Untouched fields keep their source text: macro
// references, aliases, ${env.*} references and comments.
//
// A field missing from posted is left alone rather than unset: the effective
// JSON omits empty values, so absence does not mean the user cleared it.
// current is nil for a new model; the result is then a single "add".
func ChangesForModelUpdate(id string, current, posted map[string]any) []Change {
	if current == nil {
		fields := map[string]any{}
		for k, v := range posted {
			if !derivedModelFields[k] && !isEmpty(v) {
				fields[k] = v
			}
		}
		return []Change{{Op: "add", Section: SectionModels, ID: id, Value: fields}}
	}
	names := make([]string, 0, len(posted))
	for k := range posted {
		names = append(names, k)
	}
	sort.Strings(names)
	var changes []Change
	for _, k := range names {
		if derivedModelFields[k] || equalValues(current[k], posted[k]) {
			continue
		}
		if isEmpty(posted[k]) {
			if _, had := current[k]; had {
				changes = append(changes, Change{Op: "unset", Section: SectionModels, ID: id, Path: []string{k}})
			}
			continue
		}
		changes = append(changes, Change{Op: "set", Section: SectionModels, ID: id, Path: []string{k}, Value: posted[k]})
	}
	return changes
}

func isEmpty(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	}
	return false
}
