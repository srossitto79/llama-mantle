import { describe, expect, it } from "vitest";
import { Text } from "@codemirror/state";
import { problemsToDiagnostics } from "./diagnostics";

const doc = Text.of(["models:", "  a:", "    cmd: run ${nope}"]);

describe("problemsToDiagnostics", () => {
  it("marks the word at line and column", () => {
    const [d] = problemsToDiagnostics(doc, [{ line: 3, column: 14, message: "unknown macro" }]);
    expect(doc.sliceString(d.from, d.to)).toBe("${nope}");
    expect(d.severity).toBe("error");
  });

  it("marks the whole line without a column and clamps out-of-range lines", () => {
    const [whole, clamped, none] = problemsToDiagnostics(doc, [
      { line: 2, message: "x" },
      { line: 99, message: "y" },
      { message: "z" },
    ]);
    expect(doc.sliceString(whole.from, whole.to)).toBe("  a:");
    expect(doc.lineAt(clamped.from).number).toBe(3);
    expect(doc.lineAt(none.from).number).toBe(1);
  });
});
