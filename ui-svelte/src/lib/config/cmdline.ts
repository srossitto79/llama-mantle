// Command lines as llama-swap reads them (config.SanitizeCommand, POSIX rules):
// "#" comment lines are dropped, a trailing backslash joins lines, words split
// on whitespace with single quotes, double quotes and backslash escapes.
// Edits work on the token list in place, so flag order and ${macro} tokens
// survive a round trip.

/** Whether the command has "#" comment lines, which a token edit would drop. */
export function hasCommentLines(cmd: string): boolean {
  return cmd.split("\n").some((l) => l.trim().startsWith("#"));
}

/** Splits a command into words the way llama-swap does on Linux. */
export function splitCommand(cmd: string): string[] {
  const lines: string[] = [];
  for (const line of cmd.split("\n")) {
    const t = line.trim();
    if (t.startsWith("#")) continue;
    lines.push(t.endsWith("\\") ? t.slice(0, -1) + " " : line);
  }
  const s = lines.join("\n");
  const out: string[] = [];
  let cur = "";
  let inWord = false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (/\s/.test(c)) {
      if (inWord) out.push(cur);
      cur = "";
      inWord = false;
    } else if (c === "'") {
      inWord = true;
      const end = s.indexOf("'", i + 1);
      const stop = end < 0 ? s.length : end;
      cur += s.slice(i + 1, stop);
      i = stop;
    } else if (c === '"') {
      inWord = true;
      i++;
      while (i < s.length && s[i] !== '"') {
        if (s[i] === "\\" && i + 1 < s.length && '"\\$`\n'.includes(s[i + 1])) i++;
        cur += s[i];
        i++;
      }
    } else if (c === "\\" && i + 1 < s.length) {
      inWord = true;
      cur += s[++i];
    } else {
      inWord = true;
      cur += c;
    }
  }
  if (inWord) out.push(cur);
  return out;
}

/** Quotes a word so splitCommand reads it back unchanged. */
export function quoteWord(word: string): string {
  if (word !== "" && /^[^\s'"\\#]+$/.test(word[0]) && /^[^\s'"\\]+$/.test(word)) return word;
  return "'" + word.replace(/'/g, `'"'"'`) + "'";
}

/**
 * Joins words into a command. `multiline` puts each flag (a word starting
 * with "-") on its own line, matching the usual block style in config.yaml.
 */
export function joinCommand(words: string[], multiline: boolean): string {
  if (!multiline) return words.map(quoteWord).join(" ");
  const lines: string[] = [];
  for (const w of words) {
    const q = quoteWord(w);
    if (lines.length === 0 || w.startsWith("-")) lines.push(q);
    else lines[lines.length - 1] += " " + q;
  }
  return lines.join("\n");
}

export type FlagKind = "boolean" | "value";

export interface FlagRef {
  /** Canonical name (first of the spec's names). */
  name: string;
  names: string[];
  kind: FlagKind;
}

export interface ParsedCommand {
  /** Canonical flag name -> index of the flag word. */
  flags: Map<string, number>;
  /** Indices of words that are no known flag or flag value (argv[0] excluded). */
  extra: number[];
}

/** Locates known flags in `words` (argv[0] is the binary and is skipped). */
export function parseCommand(words: string[], byName: Map<string, FlagRef>): ParsedCommand {
  const flags = new Map<string, number>();
  const extra: number[] = [];
  for (let i = 1; i < words.length; i++) {
    const ref = byName.get(words[i]);
    if (!ref) {
      extra.push(i);
      continue;
    }
    flags.set(ref.name, i);
    if (ref.kind === "value") i++;
  }
  return { flags, extra };
}

/** Value of a flag: true/false for booleans, the value word ("" when absent) otherwise. */
export function flagValue(words: string[], parsed: ParsedCommand, ref: FlagRef): string | boolean {
  const at = parsed.flags.get(ref.name);
  if (ref.kind === "boolean") return at !== undefined;
  return at === undefined ? "" : (words[at + 1] ?? "");
}

/**
 * Sets a flag in place: an existing flag keeps its position, a new one goes at
 * the end, "" or false removes it.
 */
export function setFlag(words: string[], byName: Map<string, FlagRef>, ref: FlagRef, value: string | boolean): string[] {
  const next = words.slice();
  const at = parseCommand(words, byName).flags.get(ref.name);
  if (ref.kind === "boolean") {
    if (value && at === undefined) next.push(ref.name);
    if (!value && at !== undefined) next.splice(at, 1);
    return next;
  }
  const v = String(value);
  if (at === undefined) {
    if (v !== "") next.push(ref.name, v);
    return next;
  }
  if (v === "") next.splice(at, at + 1 < words.length ? 2 : 1);
  else if (at + 1 < next.length) next[at + 1] = v;
  else next.push(v);
  return next;
}

/** Replaces the extra words with those typed in `text` (split like a command). */
export function setExtra(words: string[], byName: Map<string, FlagRef>, text: string): string[] {
  const drop = new Set(parseCommand(words, byName).extra);
  return [...words.filter((_, i) => !drop.has(i)), ...splitCommand(text)];
}
