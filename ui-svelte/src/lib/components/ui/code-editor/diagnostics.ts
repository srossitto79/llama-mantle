import type { Text } from "@codemirror/state";
import type { Diagnostic } from "@codemirror/lint";

export interface EditorProblem {
  line?: number;
  column?: number;
  message: string;
}

/**
 * Maps 1-based line/column problems to CodeMirror diagnostics. A problem
 * without a line marks the first line; one with a line but no column marks
 * the whole line. Out-of-range positions are clamped.
 */
export function problemsToDiagnostics(doc: Text, problems: EditorProblem[]): Diagnostic[] {
  return problems.map((p) => {
    const lineNo = Math.min(Math.max(p.line ?? 1, 1), doc.lines);
    const line = doc.line(lineNo);
    let from = line.from;
    let to = line.to;
    if (p.column && p.column > 0) {
      from = Math.min(line.from + p.column - 1, line.to);
      const rest = doc.sliceString(from, line.to);
      const word = /^\S+/.exec(rest);
      to = word ? from + word[0].length : line.to;
    }
    if (to === from) to = Math.min(from + 1, doc.length);
    return { from, to, severity: "error", message: p.message };
  });
}
