import { writable, type Readable } from "svelte/store";

export type ToastKind = "info" | "success" | "error";

export interface ToastInput {
  kind?: ToastKind;
  title: string;
  description?: string;
  /** Milliseconds before auto-dismiss; 0 keeps it until dismissed. Errors default to 0. */
  duration?: number;
}

export interface ToastItem extends Required<Omit<ToastInput, "description">> {
  id: number;
  description?: string;
}

export interface Toaster {
  readonly items: Readable<ToastItem[]>;
  show(input: ToastInput): number;
  dismiss(id: number): void;
}

type Timers = {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
};

const defaultTimers: Timers = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

/** At most this many toasts are visible; the oldest is dropped first. */
export const MAX_TOASTS = 4;

export function createToaster(timers: Timers = defaultTimers): Toaster {
  const items = writable<ToastItem[]>([]);
  const handles = new Map<number, unknown>();
  let nextId = 1;

  function dismiss(id: number): void {
    const h = handles.get(id);
    if (h !== undefined) timers.clear(h);
    handles.delete(id);
    items.update((list) => list.filter((t) => t.id !== id));
  }

  function show(input: ToastInput): number {
    const kind = input.kind ?? "info";
    const item: ToastItem = {
      id: nextId++,
      kind,
      title: input.title,
      description: input.description,
      duration: input.duration ?? (kind === "error" ? 0 : 4000),
    };
    items.update((list) => {
      const next = [...list, item];
      while (next.length > MAX_TOASTS) {
        const dropped = next.shift()!;
        const h = handles.get(dropped.id);
        if (h !== undefined) timers.clear(h);
        handles.delete(dropped.id);
      }
      return next;
    });
    if (item.duration > 0) handles.set(item.id, timers.set(() => dismiss(item.id), item.duration));
    return item.id;
  }

  return { items: { subscribe: items.subscribe }, show, dismiss };
}

export const toast: Toaster = createToaster();
