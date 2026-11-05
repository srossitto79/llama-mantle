// Test (item) selection for an Intelligence run. The service accepts an
// `items` list of item ids on POST /run and runs only those that also belong
// to the chosen profile; an absent or empty list runs the whole profile.

export interface SuiteItem {
  id: string;
  title: string;
  category: string;
  profiles?: string[];
  requires?: string | null;
}

export interface SuiteCategory {
  id: string;
  name: string;
}

export interface CategoryGroup {
  id: string;
  name: string;
  items: SuiteItem[];
}

/** Items the profile runs. "full" runs every item. */
export function profileItems(items: SuiteItem[], profile: string): SuiteItem[] {
  if (profile === "full") return items;
  return items.filter((i) => (i.profiles ?? []).includes(profile));
}

/** Items grouped by category, in the suite's category order; empty groups dropped. */
export function groupByCategory(items: SuiteItem[], categories: SuiteCategory[]): CategoryGroup[] {
  const byCat = new Map<string, SuiteItem[]>();
  for (const item of items) {
    const list = byCat.get(item.category);
    if (list) list.push(item);
    else byCat.set(item.category, [item]);
  }
  const groups: CategoryGroup[] = [];
  for (const c of categories) {
    const list = byCat.get(c.id);
    if (list?.length) groups.push({ id: c.id, name: c.name, items: list });
    byCat.delete(c.id);
  }
  // Categories the catalog does not name still show, by id.
  for (const [id, list] of byCat) groups.push({ id, name: id, items: list });
  return groups;
}

/** Items matching a filter on title, id or category. */
export function filterItems(items: SuiteItem[], query: string): SuiteItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter((i) => i.title.toLowerCase().includes(q) || i.id.toLowerCase().includes(q) || i.category.toLowerCase().includes(q));
}

export type GroupState = "all" | "some" | "none";

export function groupState(group: CategoryGroup, selected: ReadonlySet<string>): GroupState {
  const n = group.items.filter((i) => selected.has(i.id)).length;
  return n === 0 ? "none" : n === group.items.length ? "all" : "some";
}

/** Selects every item of the group, or clears them when all were selected. */
export function toggleGroup(group: CategoryGroup, selected: ReadonlySet<string>): Set<string> {
  const next = new Set(selected);
  if (groupState(group, selected) === "all") for (const i of group.items) next.delete(i.id);
  else for (const i of group.items) next.add(i.id);
  return next;
}

/**
 * The `items` value for POST /run: undefined runs the whole profile. Ids not
 * in the profile are dropped, since the service would skip them anyway.
 */
export function runItems(allTests: boolean, selected: ReadonlySet<string>, inProfile: SuiteItem[]): string[] | undefined {
  if (allTests) return undefined;
  return inProfile.filter((i) => selected.has(i.id)).map((i) => i.id);
}
