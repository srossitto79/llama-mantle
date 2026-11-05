import { describe, expect, it } from "vitest";
import { filterItems, groupByCategory, groupState, profileItems, runItems, toggleGroup, type SuiteItem } from "./intelligenceSelection";

const items: SuiteItem[] = [
  { id: "a1", title: "Add numbers", category: "math", profiles: ["quick", "default"] },
  { id: "a2", title: "Long division", category: "math", profiles: ["default"] },
  { id: "c1", title: "Fix the bug", category: "code", profiles: ["quick"] },
  { id: "x1", title: "Odd one", category: "unlisted", profiles: ["default"] },
];
const categories = [
  { id: "code", name: "Coding" },
  { id: "math", name: "Math" },
];

describe("intelligence test selection", () => {
  it("scopes items to the profile; full runs everything", () => {
    expect(profileItems(items, "quick").map((i) => i.id)).toEqual(["a1", "c1"]);
    expect(profileItems(items, "full")).toHaveLength(4);
  });

  it("groups in catalog order and keeps unnamed categories", () => {
    const groups = groupByCategory(items, categories);
    expect(groups.map((g) => [g.id, g.name, g.items.length])).toEqual([
      ["code", "Coding", 1],
      ["math", "Math", 2],
      ["unlisted", "unlisted", 1],
    ]);
  });

  it("toggles a category: partial selects all, all clears", () => {
    const [, math] = groupByCategory(items, categories);
    let sel = new Set(["a1"]);
    expect(groupState(math, sel)).toBe("some");
    sel = toggleGroup(math, sel);
    expect(groupState(math, sel)).toBe("all");
    sel = toggleGroup(math, sel);
    expect(groupState(math, sel)).toBe("none");
  });

  it("sends no items for the whole profile and drops ids outside it", () => {
    const quick = profileItems(items, "quick");
    expect(runItems(true, new Set(["a1"]), quick)).toBeUndefined();
    expect(runItems(false, new Set(["a1", "a2"]), quick)).toEqual(["a1"]);
  });

  it("filters on title, id and category", () => {
    expect(filterItems(items, "division").map((i) => i.id)).toEqual(["a2"]);
    expect(filterItems(items, "CODE").map((i) => i.id)).toEqual(["c1"]);
  });
});
