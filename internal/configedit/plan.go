package configedit

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// lineRange is a 1-based inclusive line range of a mapping entry.
type lineRange struct{ start, end int }

// insertion is a key appended to an original mapping during the plan.
type insertion struct {
	parent *yaml.Node
	key    *yaml.Node
}

// keyRename is a rename of an original key, applied as a text edit on its line.
type keyRename struct {
	key      *yaml.Node
	from, to string
}

// planState tracks one plan: the mutable tree, the original text and which
// original entries must be re-emitted.
type planState struct {
	root  *yaml.Node
	lines []string
	// Original entries, keyed by their key node.
	ranges   map[*yaml.Node]lineRange
	parentOf map[*yaml.Node]*yaml.Node   // key node -> key node of the enclosing entry (nil at root)
	ownerOf  map[*yaml.Node]*yaml.Node   // value node -> its key node
	keyOfMap map[*yaml.Node]*yaml.Node   // mapping node -> key node owning it
	children map[*yaml.Node][]*yaml.Node // original mapping -> its original keys, in order
	indent   int

	dirty      map[*yaml.Node]bool
	removed    []*yaml.Node
	removedSet map[*yaml.Node]bool
	inserted   []insertion
	renames    []keyRename
	index      int // index of the change being applied, for errors
}

func newPlanState(norm []byte, doc *yaml.Node) *planState {
	p := &planState{
		root:       doc.Content[0],
		lines:      strings.Split(string(norm), "\n"),
		ranges:     map[*yaml.Node]lineRange{},
		parentOf:   map[*yaml.Node]*yaml.Node{},
		ownerOf:    map[*yaml.Node]*yaml.Node{},
		keyOfMap:   map[*yaml.Node]*yaml.Node{},
		children:   map[*yaml.Node][]*yaml.Node{},
		dirty:      map[*yaml.Node]bool{},
		removedSet: map[*yaml.Node]bool{},
		indent:     2,
	}
	// A trailing newline yields an empty last element; it is not a line.
	if n := len(p.lines); n > 0 && p.lines[n-1] == "" {
		p.lines = p.lines[:n-1]
	}
	p.indexMapping(p.root, nil, len(p.lines))
	p.indent = detectIndent(p.root)
	return p
}

func detectIndent(root *yaml.Node) int {
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if v.Kind == yaml.MappingNode && len(v.Content) > 0 && v.Style&yaml.FlowStyle == 0 {
			if d := v.Content[0].Column - k.Column; d > 0 && d <= 8 {
				return d
			}
		}
	}
	return 2
}

// isTrailingFiller reports whether line is blank or a comment indented at or
// left of col (0-based), i.e. belongs to what follows rather than the entry.
func isTrailingFiller(line string, col int) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "#") {
		return len(line)-len(trimmed) <= col
	}
	return false
}

// indexMapping records the line range of every entry in m (and below).
// limit is the last line the mapping may extend to.
// Entries of flow mappings ({a: 1}) get no range: they share lines, so a
// change inside one re-emits the nearest enclosing block entry.
func (p *planState) indexMapping(m *yaml.Node, parentKey *yaml.Node, limit int) {
	p.keyOfMap[m] = parentKey
	flow := m.Style&yaml.FlowStyle != 0
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if flow {
			p.parentOf[k] = parentKey
			p.ownerOf[v] = k
			p.indexValue(v, k, limit)
			continue
		}
		p.children[m] = append(p.children[m], k)
		next := limit + 1
		if i+2 < len(m.Content) {
			next = m.Content[i+2].Line
		}
		end := next - 1
		for end > k.Line && isTrailingFiller(p.lines[end-1], k.Column-1) {
			end--
		}
		p.ranges[k] = lineRange{k.Line, end}
		p.parentOf[k] = parentKey
		p.ownerOf[v] = k
		p.indexValue(v, k, end)
	}
}

func (p *planState) indexValue(v, key *yaml.Node, limit int) {
	switch v.Kind {
	case yaml.MappingNode:
		p.indexMapping(v, key, limit)
	case yaml.SequenceNode:
		for i, item := range v.Content {
			p.ownerOf[item] = key
			if item.Kind != yaml.MappingNode {
				continue
			}
			itemLimit := limit
			if i+1 < len(v.Content) {
				itemLimit = v.Content[i+1].Line - 1
			}
			p.indexMapping(item, key, itemLimit)
		}
	}
}

func (p *planState) isOriginal(key *yaml.Node) bool {
	_, ok := p.ranges[key]
	return ok
}

// markDirty schedules an original entry for re-emission. New keys need no
// marking: their insertion emits their current state.
// Keys without a range (inside flow mappings) climb to the nearest ranged
// ancestor.
func (p *planState) markDirty(key *yaml.Node) {
	for k := key; k != nil; k = p.parentOf[k] {
		if p.isOriginal(k) {
			p.dirty[k] = true
			return
		}
		if _, known := p.parentOf[k]; !known {
			return
		}
	}
}

// markOwner marks the entry owning node n (a value or sequence item).
func (p *planState) markOwner(n *yaml.Node) {
	if k, ok := p.ownerOf[n]; ok {
		p.markDirty(k)
	} else if k, ok := p.keyOfMap[n]; ok && k != nil {
		p.markDirty(k)
	}
}

func (p *planState) errf(code, anchor, format string, args ...any) error {
	return &ChangeError{Index: p.index, Code: code, Anchor: anchor, Message: fmt.Sprintf(format, args...)}
}

// copyNode deep-copies n without anchors, so a detached copy never redefines
// an anchor. Aliases inside stay aliases.
func copyNode(n *yaml.Node) *yaml.Node {
	c := *n
	c.Anchor = ""
	if n.Kind == yaml.AliasNode {
		return &c
	}
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = copyNode(child)
	}
	return &c
}

func encodeValue(v any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	return &n, nil
}

func newKey(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
}

func equalValues(a, b any) bool {
	return reflect.DeepEqual(normalize(a), normalize(b))
}

// appendKey adds key/value to mapping m and records the insertion.
func (p *planState) appendKey(m *yaml.Node, name string, value *yaml.Node) *yaml.Node {
	k := newKey(name)
	m.Content = append(m.Content, k, value)
	p.inserted = append(p.inserted, insertion{parent: m, key: k})
	return k
}

// removeKey removes key name from mapping m.
func (p *planState) removeKey(m *yaml.Node, name string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Value != name {
			continue
		}
		m.Content = append(m.Content[:i], m.Content[i+2:]...)
		if p.isOriginal(k) {
			p.removed = append(p.removed, k)
			p.removedSet[k] = true
		}
		return true
	}
	return false
}

func findIndex(m *yaml.Node, name string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == name {
			return i
		}
	}
	return -1
}

// detachSlot replaces an alias in *slot with a local copy when allowed.
func (p *planState) detachSlot(slot **yaml.Node, owner *yaml.Node, detach bool, where string) error {
	cur := *slot
	if cur.Kind != yaml.AliasNode {
		return nil
	}
	if !detach {
		return p.errf(CodeAliased, cur.Value, "%s is inherited from anchor %q", where, cur.Value)
	}
	*slot = copyNode(resolve(cur))
	if owner != nil {
		p.markDirty(owner)
	} else {
		p.markOwner(cur)
	}
	return nil
}

// assign sets *slot to v, changing as little of the tree as possible: equal
// values are left alone (including their aliases), mappings and sequences are
// merged element by element, scalars keep their comments and quoting style.
func (p *planState) assign(slot **yaml.Node, owner *yaml.Node, key string, v any, detach bool, where string) error {
	cur := *slot
	curVal := decodeNode(cur)
	if equalValues(curVal, v) {
		return nil
	}
	if red, changed := redactValue(key, curVal); changed && equalValues(red, v) {
		return nil
	}
	if err := p.detachSlot(slot, owner, detach, where); err != nil {
		return err
	}
	cur = *slot

	switch nv := v.(type) {
	case map[string]any:
		if cur.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(cur.Content); {
				name := cur.Content[i].Value
				if _, keep := nv[name]; keep || (name == "<<" && cur.Content[i].Tag == "!!merge") {
					i += 2
					continue
				}
				p.removeKey(cur, name)
			}
			names := make([]string, 0, len(nv))
			for name := range nv {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				idx := findIndex(cur, name)
				if idx < 0 {
					if containsRedacted(nv[name]) {
						return p.errf(CodeRedacted, "", "%s.%s contains a redacted value", where, name)
					}
					n, err := encodeValue(nv[name])
					if err != nil {
						return p.errf(CodeInvalid, "", "%s.%s: %v", where, name, err)
					}
					p.appendKey(cur, name, n)
					continue
				}
				if err := p.assign(&cur.Content[idx+1], cur.Content[idx], name, nv[name], detach, where+"."+name); err != nil {
					return err
				}
			}
			return nil
		}
	case []any:
		if cur.Kind == yaml.SequenceNode {
			for i := 0; i < len(nv) && i < len(cur.Content); i++ {
				if err := p.assign(&cur.Content[i], owner, key, nv[i], detach, fmt.Sprintf("%s[%d]", where, i)); err != nil {
					return err
				}
			}
			if len(nv) != len(cur.Content) {
				if len(nv) < len(cur.Content) {
					cur.Content = cur.Content[:len(nv)]
				} else {
					for _, item := range nv[len(cur.Content):] {
						if containsRedacted(item) {
							return p.errf(CodeRedacted, "", "%s contains a redacted value", where)
						}
						n, err := encodeValue(item)
						if err != nil {
							return p.errf(CodeInvalid, "", "%s: %v", where, err)
						}
						cur.Content = append(cur.Content, n)
					}
				}
				p.markDirty(owner)
			}
			return nil
		}
	}

	if containsRedacted(v) {
		return p.errf(CodeRedacted, "", "%s contains a redacted value", where)
	}
	fresh, err := encodeValue(v)
	if err != nil {
		return p.errf(CodeInvalid, "", "%s: %v", where, err)
	}
	if cur.Kind == yaml.ScalarNode && fresh.Kind == yaml.ScalarNode {
		if s, ok := v.(string); ok {
			switch {
			case strings.Contains(s, "\n"):
				fresh.Style = yaml.LiteralStyle
			case cur.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
				fresh.Style = cur.Style
			}
		}
	}
	fresh.HeadComment, fresh.LineComment, fresh.FootComment = cur.HeadComment, cur.LineComment, cur.FootComment
	fresh.Anchor = cur.Anchor
	// Assign in place so aliases pointing at cur see the new value.
	*cur = *fresh
	p.markDirty(owner)
	if owner == nil {
		p.markOwner(cur)
	}
	return nil
}

// sectionMapping returns the mapping holding the entries of section,
// creating it when create is true.
func (p *planState) sectionMapping(section string, create bool) (*yaml.Node, error) {
	if section == SectionRoot {
		return p.root, nil
	}
	idx := findIndex(p.root, section)
	if idx < 0 {
		if !create {
			return nil, p.errf(CodeNotFound, "", "section %q not found", section)
		}
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		p.appendKey(p.root, section, m)
		return m, nil
	}
	slot := &p.root.Content[idx+1]
	if (*slot).Kind == yaml.AliasNode {
		return nil, p.errf(CodeAliased, (*slot).Value, "section %q is an alias", section)
	}
	if (*slot).Kind == yaml.ScalarNode && (*slot).Tag == "!!null" {
		*slot = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: (*slot).Line, Column: (*slot).Column}
		p.markDirty(p.root.Content[idx])
	}
	if (*slot).Kind != yaml.MappingNode {
		return nil, p.errf(CodeInvalid, "", "section %q is not a mapping", section)
	}
	return *slot, nil
}

func (p *planState) modelIDsAndAliases() map[string]string {
	out := map[string]string{}
	_, models := lookup(p.root, SectionModels)
	models = resolve(models)
	if models == nil || models.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(models.Content); i += 2 {
		id := models.Content[i].Value
		out[id] = id
		if _, aliases := lookup(resolve(models.Content[i+1]), "aliases"); aliases != nil {
			if list, ok := decodeNode(aliases).([]any); ok {
				for _, a := range list {
					if s, ok := a.(string); ok {
						out[s] = id
					}
				}
			}
		}
	}
	return out
}

// modelReferenceLists returns sequences that list model IDs: group members
// and hooks.on_startup.preload. Aliases are resolved, so a shared list is
// edited at its anchor.
func (p *planState) modelReferenceLists() []*yaml.Node {
	var lists []*yaml.Node
	addMembers := func(groups *yaml.Node) {
		groups = resolve(groups)
		if groups == nil || groups.Kind != yaml.MappingNode {
			return
		}
		for i := 1; i < len(groups.Content); i += 2 {
			if _, members := lookup(resolve(groups.Content[i]), "members"); members != nil {
				lists = append(lists, resolve(members))
			}
		}
	}
	_, groups := lookup(p.root, SectionGroups)
	addMembers(groups)
	_, routing := lookup(p.root, "routing")
	_, router := lookup(resolve(routing), "router")
	_, settings := lookup(resolve(router), "settings")
	_, nested := lookup(resolve(settings), "groups")
	addMembers(nested)
	_, hooks := lookup(p.root, "hooks")
	_, onStartup := lookup(resolve(hooks), "on_startup")
	if _, preload := lookup(resolve(onStartup), "preload"); preload != nil {
		lists = append(lists, resolve(preload))
	}
	seen := map[*yaml.Node]bool{}
	out := lists[:0]
	for _, l := range lists {
		if l != nil && l.Kind == yaml.SequenceNode && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

// updateModelReferences renames (to != "") or removes (to == "") model id in
// every reference list.
func (p *planState) updateModelReferences(id, to string) {
	for _, list := range p.modelReferenceLists() {
		changed := false
		kept := list.Content[:0]
		for _, item := range list.Content {
			if item.Kind == yaml.ScalarNode && item.Value == id {
				changed = true
				if to == "" {
					continue
				}
				item.Value = to
			}
			kept = append(kept, item)
		}
		list.Content = kept
		if changed {
			p.markOwner(list)
		}
	}
}

func (p *planState) apply(c Change) error {
	if c.Op != "add" && c.Op != "set" && c.Op != "unset" && c.Op != "delete" && c.Op != "rename" {
		return p.errf(CodeInvalid, "", "unknown op %q", c.Op)
	}
	if c.Section != SectionRoot && c.Section != SectionMacros && !isEntrySection(c.Section) {
		return p.errf(CodeInvalid, "", "unknown section %q", c.Section)
	}
	if strings.TrimSpace(c.ID) == "" {
		return p.errf(CodeInvalid, "", "id is required")
	}
	section, err := p.sectionMapping(c.Section, c.Op == "add")
	if err != nil {
		return err
	}
	idx := findIndex(section, c.ID)
	where := strings.TrimPrefix(c.Section+"."+c.ID, ".")

	switch c.Op {
	case "add":
		if idx >= 0 {
			return p.errf(CodeExists, "", "%s already exists", where)
		}
		if c.Section == SectionModels {
			if owner, ok := p.modelIDsAndAliases()[c.ID]; ok {
				return p.errf(CodeExists, "", "%q is already used by model %s", c.ID, owner)
			}
		}
		if _, isMap := c.Value.(map[string]any); isEntrySection(c.Section) && !isMap {
			return p.errf(CodeInvalid, "", "%s: value must be a mapping", where)
		}
		if containsRedacted(c.Value) {
			return p.errf(CodeRedacted, "", "%s contains a redacted value", where)
		}
		n, err := encodeValue(c.Value)
		if err != nil {
			return p.errf(CodeInvalid, "", "%s: %v", where, err)
		}
		p.appendKey(section, c.ID, n)
		return nil

	case "delete":
		if idx < 0 {
			return p.errf(CodeNotFound, "", "%s not found", where)
		}
		p.removeKey(section, c.ID)
		if c.Section == SectionModels {
			p.updateModelReferences(c.ID, "")
		}
		return nil

	case "rename":
		if idx < 0 {
			return p.errf(CodeNotFound, "", "%s not found", where)
		}
		if strings.TrimSpace(c.NewID) == "" {
			return p.errf(CodeInvalid, "", "newId is required")
		}
		if c.NewID == c.ID {
			return nil
		}
		if findIndex(section, c.NewID) >= 0 {
			return p.errf(CodeExists, "", "%s already exists", strings.TrimPrefix(c.Section+"."+c.NewID, "."))
		}
		if c.Section == SectionModels {
			if owner, ok := p.modelIDsAndAliases()[c.NewID]; ok && owner != c.ID {
				return p.errf(CodeExists, "", "%q is already used by model %s", c.NewID, owner)
			}
		}
		k := section.Content[idx]
		if p.isOriginal(k) {
			p.renames = append(p.renames, keyRename{key: k, from: k.Value, to: c.NewID})
		}
		k.Value = c.NewID
		if c.Section == SectionModels {
			p.updateModelReferences(c.ID, c.NewID)
		}
		return nil
	}

	// set / unset
	if idx < 0 {
		return p.errf(CodeNotFound, "", "%s not found", where)
	}
	slot := &section.Content[idx+1]
	owner := section.Content[idx]
	if len(c.Path) == 0 {
		if c.Op == "unset" {
			return p.errf(CodeInvalid, "", "unset needs a path; use delete to remove %s", where)
		}
		return p.assign(slot, owner, c.ID, c.Value, c.Detach, where)
	}
	for i, seg := range c.Path {
		last := i == len(c.Path)-1
		if err := p.detachSlot(slot, owner, c.Detach, where); err != nil {
			return err
		}
		node := *slot
		if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
			*node = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: node.Line, Column: node.Column}
			p.markDirty(owner)
		}
		if node.Kind != yaml.MappingNode {
			return p.errf(CodeInvalid, "", "%s is not a mapping", where)
		}
		fi := findIndex(node, seg)
		childWhere := where + "." + seg
		if c.Op == "unset" && last {
			if fi < 0 {
				if inheritedFrom(node, seg) != "" {
					return p.errf(CodeInherited, inheritedFrom(node, seg), "%s is inherited from anchor %q", childWhere, inheritedFrom(node, seg))
				}
				return nil
			}
			p.removeKey(node, seg)
			return nil
		}
		if fi < 0 {
			if c.Op == "unset" {
				return nil
			}
			if last {
				if containsRedacted(c.Value) {
					return p.errf(CodeRedacted, "", "%s contains a redacted value", childWhere)
				}
				n, err := encodeValue(c.Value)
				if err != nil {
					return p.errf(CodeInvalid, "", "%s: %v", childWhere, err)
				}
				p.appendKey(node, seg, n)
				return nil
			}
			m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			owner = p.appendKey(node, seg, m)
			slot = &node.Content[len(node.Content)-1]
			where = childWhere
			continue
		}
		owner = node.Content[fi]
		slot = &node.Content[fi+1]
		where = childWhere
		if last {
			return p.assign(slot, owner, seg, c.Value, c.Detach, where)
		}
	}
	return nil
}

// inheritedFrom returns the anchor a merge key in m provides name from.
func inheritedFrom(m *yaml.Node, name string) string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Value != "<<" || k.Tag != "!!merge" {
			continue
		}
		sources := []*yaml.Node{v}
		if v.Kind == yaml.SequenceNode {
			sources = v.Content
		}
		for _, s := range sources {
			if _, found := lookup(resolve(s), name); found != nil {
				return s.Value
			}
		}
	}
	return ""
}

// emitEntry renders key: value at the given column using the file's indent.
// Head and foot comments of the entry itself stay in the original text.
func (p *planState) emitEntry(key, value *yaml.Node, column int, prefix string) ([]string, error) {
	k := *key
	k.HeadComment, k.FootComment = "", ""
	v := *value
	v.FootComment = ""
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{&k, &v}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(p.indent)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	pad := strings.Repeat(" ", column)
	for i := range out {
		if i == 0 {
			out[i] = prefix + out[i]
		} else if out[i] != "" {
			out[i] = pad + out[i]
		}
	}
	return out, nil
}

// hasDirtyAncestor reports whether an ancestor of key is re-emitted or removed.
func (p *planState) hasDirtyAncestor(key *yaml.Node) bool {
	for a := p.parentOf[key]; a != nil; a = p.parentOf[a] {
		if p.dirty[a] || p.removedSet[a] {
			return true
		}
	}
	return false
}

func containsKey(m, key *yaml.Node) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i] == key {
			return true
		}
	}
	return false
}

type textEdit struct {
	start, end int // 1-based inclusive; end = start-1 for a pure insertion before start
	lines      []string
}

// splice renders the mutated tree into the original text, touching only the
// entries the plan changed.
func (p *planState) splice() (string, error) {
	lines := append([]string(nil), p.lines...)
	var edits []textEdit

	// Locate each original entry's current value through its parent mapping.
	valueOf := func(key *yaml.Node) (*yaml.Node, bool) {
		var parentMap *yaml.Node
		if pk := p.parentOf[key]; pk == nil {
			parentMap = p.root
		} else {
			parentMap = findValueOfKey(p.root, pk)
		}
		if parentMap == nil {
			return nil, false
		}
		parentMap = resolve(parentMap)
		// Entries inside sequence items: search the items too.
		for _, m := range mappingsUnder(parentMap) {
			for i := 0; i+1 < len(m.Content); i += 2 {
				if m.Content[i] == key {
					return m.Content[i+1], true
				}
			}
		}
		return nil, false
	}

	for key := range p.dirty {
		if p.removedSet[key] || p.hasDirtyAncestor(key) {
			continue
		}
		value, ok := valueOf(key)
		if !ok {
			continue
		}
		r := p.ranges[key]
		first := lines[r.start-1]
		col := key.Column - 1
		if col > len(first) {
			col = len(first)
		}
		emitted, err := p.emitEntry(key, value, col, first[:col])
		if err != nil {
			return "", err
		}
		edits = append(edits, textEdit{r.start, r.end, emitted})
	}

	for _, key := range p.removed {
		if p.hasDirtyAncestor(key) {
			continue
		}
		r := p.ranges[key]
		start := r.start
		// Take the entry's own head comment with it.
		for start > 1 {
			prev := lines[start-2]
			trimmed := strings.TrimLeft(prev, " ")
			if !strings.HasPrefix(trimmed, "#") || len(prev)-len(trimmed) != key.Column-1 {
				break
			}
			start--
		}
		edits = append(edits, textEdit{start, r.end, nil})
	}

	// Group insertions by parent, in insertion order.
	type group struct {
		parent *yaml.Node
		keys   []*yaml.Node
	}
	var groups []*group
	byParent := map[*yaml.Node]*group{}
	for _, ins := range p.inserted {
		if !containsKey(ins.parent, ins.key) {
			continue
		}
		g := byParent[ins.parent]
		if g == nil {
			g = &group{parent: ins.parent}
			byParent[ins.parent] = g
			groups = append(groups, g)
		}
		g.keys = append(g.keys, ins.key)
	}
	for _, g := range groups {
		parentKey, isOriginalMap := p.keyOfMap[g.parent]
		if !isOriginalMap {
			// A mapping created in this plan: emitted by its own insertion.
			continue
		}
		if parentKey != nil && (p.dirty[parentKey] || p.removedSet[parentKey] || p.hasDirtyAncestor(parentKey)) {
			continue
		}
		// Insert after the last original child (still present or removed).
		kids := p.children[g.parent]
		if len(kids) == 0 {
			if parentKey == nil {
				// Empty root: append at end of file.
				var out []string
				for _, k := range g.keys {
					_, v := lookupKey(g.parent, k)
					emitted, err := p.emitEntry(k, v, 0, "")
					if err != nil {
						return "", err
					}
					out = append(out, emitted...)
				}
				edits = append(edits, textEdit{len(lines) + 1, len(lines), out})
				continue
			}
			p.markDirty(parentKey)
			return p.splice()
		}
		lastOrig := kids[len(kids)-1]
		col := lastOrig.Column - 1
		var out []string
		if parentKey == nil {
			out = append(out, "")
		}
		for _, k := range g.keys {
			_, v := lookupKey(g.parent, k)
			emitted, err := p.emitEntry(k, v, col, strings.Repeat(" ", col))
			if err != nil {
				return "", err
			}
			out = append(out, emitted...)
		}
		at := p.ranges[lastOrig].end + 1
		edits = append(edits, textEdit{at, at - 1, out})
	}

	// Renames that are not covered by a re-emitted entry: edit the key text.
	for _, rn := range p.renames {
		if p.dirty[rn.key] || p.removedSet[rn.key] || p.hasDirtyAncestor(rn.key) {
			continue
		}
		i := rn.key.Line - 1
		col := rn.key.Column - 1
		line := lines[i]
		if col <= len(line) && strings.HasPrefix(line[col:], rn.from+":") {
			lines[i] = line[:col] + rn.to + line[col+len(rn.from):]
			continue
		}
		p.markDirty(rn.key)
		return p.splice()
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		tail := append([]string(nil), lines[e.end:]...)
		lines = append(append(lines[:e.start-1], e.lines...), tail...)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func lookupKey(m, key *yaml.Node) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i] == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// mappingsUnder returns m itself if it is a mapping, or the mapping items of
// a sequence.
func mappingsUnder(n *yaml.Node) []*yaml.Node {
	switch n.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{n}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range n.Content {
			if item.Kind == yaml.MappingNode {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

// findValueOfKey searches the tree for key node k and returns its value.
func findValueOfKey(n, k *yaml.Node) *yaml.Node {
	seen := map[*yaml.Node]bool{}
	var walk func(n *yaml.Node) *yaml.Node
	walk = func(n *yaml.Node) *yaml.Node {
		if n == nil || seen[n] || n.Kind == yaml.AliasNode {
			return nil
		}
		seen[n] = true
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i] == k {
					return n.Content[i+1]
				}
			}
		}
		for _, c := range n.Content {
			if r := walk(c); r != nil {
				return r
			}
		}
		return nil
	}
	return walk(n)
}

// Plan implements Planner.
func (e *editor) Plan(raw []byte, baseHash string, changes []Change) (Plan, error) {
	hash := Hash(raw)
	if baseHash != "" && baseHash != hash {
		return Plan{}, &ChangeError{Index: -1, Code: CodeStale, Message: "config file changed since it was read"}
	}
	norm, crlf := normalizeNewlines(raw)
	doc, err := parseDocument(norm)
	if err != nil {
		return Plan{}, &ChangeError{Index: -1, Code: CodeInvalid, Message: "current config is not valid YAML: " + err.Error()}
	}
	p := newPlanState(norm, doc)
	for i, c := range changes {
		p.index = i
		if err := p.apply(c); err != nil {
			return Plan{}, err
		}
	}

	out, err := p.splice()
	if err != nil {
		return Plan{}, err
	}
	// Self-check: the spliced text must parse to exactly the edited tree.
	if !sameDocument(out, doc) {
		full, err := encodeDocument(doc, p.indent)
		if err != nil {
			return Plan{}, err
		}
		out = full
	}
	if string(norm) == out || (len(changes) == 0) {
		out = string(norm)
	}
	result := []byte(out)
	if crlf {
		result = bytes.ReplaceAll(result, []byte("\n"), []byte("\r\n"))
	}
	if len(changes) == 0 {
		result = raw
	}
	plan := Plan{BaseHash: hash, Hash: Hash(result), YAML: string(result), Problems: []Problem{}}
	if e.validator != nil {
		plan.Problems = e.validator.Validate(result)
	}
	return plan, nil
}

func sameDocument(text string, doc *yaml.Node) bool {
	var got any
	if err := yaml.Unmarshal([]byte(text), &got); err != nil {
		return false
	}
	return equalValues(normalize(got), decodeNode(doc.Content[0]))
}

func encodeDocument(doc *yaml.Node, indent int) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
