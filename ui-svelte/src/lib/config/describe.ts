// One-line labels for staged changes, shown in the review list.
import type { ConfigChange } from "./configApi";

const sectionNoun: Record<string, string> = {
  models: "model",
  groups: "group",
  profiles: "profile",
  peers: "peer",
  macros: "macro",
  "": "setting",
};

export function describeChange(c: ConfigChange): string {
  const noun = sectionNoun[c.section] ?? c.section;
  const field = c.path && c.path.length ? ` · ${c.path.join(".")}` : "";
  switch (c.op) {
    case "add":
      return `Add ${noun} ${c.id}`;
    case "delete":
      return `Delete ${noun} ${c.id}`;
    case "rename":
      return `Rename ${noun} ${c.id} → ${c.newId}`;
    case "unset":
      return `${c.id}${field} · remove`;
    default:
      return `${c.id}${field}${c.detach ? " · detach" : ""}`;
  }
}
