import { describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { createToaster, MAX_TOASTS } from "./toast";

function fakeTimers() {
  const pending = new Map<number, () => void>();
  let next = 1;
  return {
    timers: {
      set: (fn: () => void) => {
        const h = next++;
        pending.set(h, fn);
        return h;
      },
      clear: (h: unknown) => pending.delete(h as number),
    },
    fireAll: () => [...pending.values()].forEach((fn) => fn()),
    pending,
  };
}

describe("toaster", () => {
  it("auto-dismisses info toasts and keeps errors until dismissed", () => {
    const ft = fakeTimers();
    const t = createToaster(ft.timers);
    t.show({ title: "Saved", kind: "success" });
    const err = t.show({ title: "Failed", kind: "error" });
    expect(get(t.items)).toHaveLength(2);
    ft.fireAll();
    expect(get(t.items).map((i) => i.id)).toEqual([err]);
    t.dismiss(err);
    expect(get(t.items)).toHaveLength(0);
  });

  it("drops the oldest toast beyond the limit and clears its timer", () => {
    const ft = fakeTimers();
    const t = createToaster(ft.timers);
    for (let i = 0; i <= MAX_TOASTS; i++) t.show({ title: `t${i}` });
    expect(get(t.items).map((i) => i.title)).toEqual(["t1", "t2", "t3", "t4"]);
    expect(ft.pending.size).toBe(MAX_TOASTS);
  });
});
