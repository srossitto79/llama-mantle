// Line diff for the review step: before/after config.yaml as hunks.

export type DiffLine =
  | { kind: "context"; text: string; oldNo: number; newNo: number }
  | { kind: "removed"; text: string; oldNo: number }
  | { kind: "added"; text: string; newNo: number };

export interface DiffHunk {
  oldStart: number;
  newStart: number;
  lines: DiffLine[];
}

/** Above this many cells the middle section is shown as a block replacement. */
const MAX_LCS_CELLS = 4_000_000;

function splitLines(s: string): string[] {
  const lines = s.replace(/\r\n/g, "\n").split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** Full line-by-line edit script between a and b. */
export function diffLines(a: string, b: string): DiffLine[] {
  const A = splitLines(a);
  const B = splitLines(b);
  let pre = 0;
  while (pre < A.length && pre < B.length && A[pre] === B[pre]) pre++;
  let suf = 0;
  while (suf < A.length - pre && suf < B.length - pre && A[A.length - 1 - suf] === B[B.length - 1 - suf]) suf++;

  const out: DiffLine[] = [];
  for (let i = 0; i < pre; i++) out.push({ kind: "context", text: A[i], oldNo: i + 1, newNo: i + 1 });

  const a0 = pre;
  const b0 = pre;
  const am = A.slice(pre, A.length - suf);
  const bm = B.slice(pre, B.length - suf);
  if (am.length * bm.length > MAX_LCS_CELLS) {
    am.forEach((t, i) => out.push({ kind: "removed", text: t, oldNo: a0 + i + 1 }));
    bm.forEach((t, j) => out.push({ kind: "added", text: t, newNo: b0 + j + 1 }));
  } else {
    // LCS table, filled from the end.
    const n = am.length;
    const m = bm.length;
    const w = m + 1;
    const L = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        L[i * w + j] = am[i] === bm[j] ? L[(i + 1) * w + j + 1] + 1 : Math.max(L[(i + 1) * w + j], L[i * w + j + 1]);
      }
    }
    let i = 0;
    let j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && am[i] === bm[j]) {
        out.push({ kind: "context", text: am[i], oldNo: a0 + i + 1, newNo: b0 + j + 1 });
        i++;
        j++;
      } else if (i < n && (j === m || L[(i + 1) * w + j] >= L[i * w + j + 1])) {
        // Removals first on ties, so a replaced line reads old then new.
        out.push({ kind: "removed", text: am[i], oldNo: a0 + i + 1 });
        i++;
      } else {
        out.push({ kind: "added", text: bm[j], newNo: b0 + j + 1 });
        j++;
      }
    }
  }

  for (let k = 0; k < suf; k++) {
    const ai = A.length - suf + k;
    const bi = B.length - suf + k;
    out.push({ kind: "context", text: A[ai], oldNo: ai + 1, newNo: bi + 1 });
  }
  return out;
}

/** Changed regions with `context` lines around each, merged when they touch. */
export function diffHunks(a: string, b: string, context = 3): DiffHunk[] {
  const lines = diffLines(a, b);
  const changed = lines.map((l, i) => (l.kind === "context" ? -1 : i)).filter((i) => i >= 0);
  const hunks: DiffHunk[] = [];
  let start = -1;
  let end = -1;
  const flush = () => {
    if (start < 0) return;
    const slice = lines.slice(start, end + 1);
    const first = slice[0];
    hunks.push({
      oldStart: "oldNo" in first ? first.oldNo : (slice.find((l) => "oldNo" in l) as { oldNo: number } | undefined)?.oldNo ?? 0,
      newStart: "newNo" in first ? first.newNo : (slice.find((l) => "newNo" in l) as { newNo: number } | undefined)?.newNo ?? 0,
      lines: slice,
    });
  };
  for (const i of changed) {
    const s = Math.max(0, i - context);
    const e = Math.min(lines.length - 1, i + context);
    if (start >= 0 && s <= end + 1) {
      end = Math.max(end, e);
    } else {
      flush();
      start = s;
      end = e;
    }
  }
  flush();
  return hunks;
}

export function diffStats(hunks: DiffHunk[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const h of hunks) for (const l of h.lines) {
    if (l.kind === "added") added++;
    else if (l.kind === "removed") removed++;
  }
  return { added, removed };
}
