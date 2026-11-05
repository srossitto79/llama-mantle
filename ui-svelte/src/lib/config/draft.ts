// Staged config changes. Edits across models, groups and macros accumulate
// here and are applied as one change set (configApi.apply), after review.
import { writable, type Readable } from "svelte/store";
import type { ChangeSection, ConfigChange } from "./configApi";

export interface DraftItem {
  /** Stable key; a later edit of the same target replaces the item in place. */
  key: string;
  change: ConfigChange;
}

export interface ConfigDraft extends Readable<DraftItem[]> {
  /** Stage a field edit. Equal to `original` removes the staged edit instead. */
  set(section: ChangeSection, id: string, path: string[], value: unknown, original: unknown, detach?: boolean): void;
  unset(section: ChangeSection, id: string, path: string[]): void;
  add(section: ChangeSection, id: string, value: unknown): void;
  remove(section: ChangeSection, id: string): void;
  rename(section: ChangeSection, id: string, newId: string): void;
  /** Discard one item and the items that depend on it. */
  discard(key: string): void;
  discardAll(): void;
  changes(): ConfigChange[];
  /** The id an entry has after the staged renames ("" when staged for deletion). */
  currentId(section: ChangeSection, id: string): string;
}

function fieldKey(section: string, id: string, path: string[]): string {
  return `${section}\u0000${id}\u0000${path.join("\u0000")}`;
}

export function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== typeof b || a === null || b === null || typeof a !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) {
    const bb = b as unknown[];
    return a.length === bb.length && a.every((v, i) => deepEqual(v, bb[i]));
  }
  const ao = a as Record<string, unknown>;
  const bo = b as Record<string, unknown>;
  const ak = Object.keys(ao);
  return ak.length === Object.keys(bo).length && ak.every((k) => k in bo && deepEqual(ao[k], bo[k]));
}

/**
 * The id an entry has after the staged renames in `items` ("" when staged for
 * deletion). Pure, so components can derive it from the draft store value.
 */
export function resolveId(items: DraftItem[], section: ChangeSection, id: string): string {
  let current = id;
  for (const { change: c } of items) {
    if (c.section !== section || c.id !== current) continue;
    if (c.op === "rename" && c.newId) current = c.newId;
    if (c.op === "delete") return "";
  }
  return current;
}

export function createConfigDraft(): ConfigDraft {
  const store = writable<DraftItem[]>([]);
  let items: DraftItem[] = [];
  let seq = 0;

  function commit(next: DraftItem[]): void {
    items = next;
    store.set(items);
  }

  function upsert(key: string, change: ConfigChange): void {
    const i = items.findIndex((it) => it.key === key);
    if (i >= 0) {
      const next = items.slice();
      next[i] = { key, change };
      commit(next);
    } else {
      commit([...items, { key, change }]);
    }
  }

  function removeKey(key: string): void {
    commit(items.filter((it) => it.key !== key));
  }

  // Items after index `from` that target section/id (or anything below it).
  function dependents(from: number, section: ChangeSection, id: string): string[] {
    return items
      .slice(from + 1)
      .filter((it) => it.change.section === section && (it.change.id === id || it.change.newId === id))
      .map((it) => it.key);
  }

  return {
    subscribe: store.subscribe,

    set(section, id, path, value, original, detach) {
      const key = fieldKey(section, id, path);
      if (deepEqual(value, original) && !items.some((it) => it.change.section === section && it.change.id === id && it.change.op === "add")) {
        removeKey(key);
        return;
      }
      // Editing inside an entry staged for addition: fold into the add.
      const addIdx = items.findIndex((it) => it.change.op === "add" && it.change.section === section && it.change.id === id);
      if (addIdx >= 0 && path.length > 0) {
        const add = items[addIdx];
        const value0 = structuredClone((add.change.value ?? {}) as Record<string, unknown>);
        let node = value0;
        for (const seg of path.slice(0, -1)) {
          if (typeof node[seg] !== "object" || node[seg] === null) node[seg] = {};
          node = node[seg] as Record<string, unknown>;
        }
        node[path[path.length - 1]] = value;
        const next = items.slice();
        next[addIdx] = { key: add.key, change: { ...add.change, value: value0 } };
        commit(next);
        return;
      }
      upsert(key, { op: "set", section, id, path, value, ...(detach ? { detach } : {}) });
    },

    unset(section, id, path) {
      upsert(fieldKey(section, id, path), { op: "unset", section, id, path });
    },

    add(section, id, value) {
      upsert(`add\u0000${section}\u0000${id}\u0000${seq++}`, { op: "add", section, id, value });
    },

    remove(section, id) {
      const addIdx = items.findIndex((it) => it.change.op === "add" && it.change.section === section && it.change.id === id);
      if (addIdx >= 0) {
        // Deleting something only staged: drop the add and everything on it.
        const drop = new Set([items[addIdx].key, ...dependents(addIdx, section, id)]);
        commit(items.filter((it) => !drop.has(it.key)));
        return;
      }
      // Field edits on a deleted entry are moot.
      const kept = items.filter((it) => !(it.change.section === section && it.change.id === id && (it.change.op === "set" || it.change.op === "unset")));
      commit([...kept, { key: `delete\u0000${section}\u0000${id}`, change: { op: "delete", section, id } }]);
    },

    rename(section, id, newId) {
      if (id === newId) return;
      const addIdx = items.findIndex((it) => it.change.op === "add" && it.change.section === section && it.change.id === id);
      if (addIdx >= 0) {
        commit(items.map((it, i) => (i >= addIdx && it.change.section === section && it.change.id === id ? { key: it.key, change: { ...it.change, id: newId } } : it)));
        return;
      }
      upsert(`rename\u0000${section}\u0000${id}\u0000${seq++}`, { op: "rename", section, id, newId });
    },

    discard(key) {
      const i = items.findIndex((it) => it.key === key);
      if (i < 0) return;
      const c = items[i].change;
      const drop = new Set([key]);
      if (c.op === "add") dependents(i, c.section, c.id).forEach((k) => drop.add(k));
      if (c.op === "rename" && c.newId) dependents(i, c.section, c.newId).forEach((k) => drop.add(k));
      commit(items.filter((it) => !drop.has(it.key)));
    },

    discardAll() {
      commit([]);
    },

    changes() {
      return items.map((it) => it.change);
    },

    currentId(section, id) {
      return resolveId(items, section, id);
    },
  };
}

/** The draft shared by every config page. */
export const configDraft = createConfigDraft();
