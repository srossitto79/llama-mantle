// Fails when classes that do not exist (leftovers of the old React UI) or raw
// palette colours come back. Colours must come from the theme tokens
// (bg-primary, text-muted-foreground, text-destructive, bg-success, ...).
import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = join(__dirname);

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return files(p);
    return /\.(svelte|ts)$/.test(name) && !name.endsWith(".test.ts") ? [p] : [];
  });
}

const palette =
  "red|green|blue|gray|grey|slate|zinc|neutral|stone|orange|amber|yellow|lime|emerald|teal|cyan|sky|indigo|violet|purple|fuchsia|pink|rose";
const paletteClass = new RegExp(
  `\\b(?:[a-z]+:)*(?:text|bg|border|ring|from|to|via|fill|stroke|outline|divide|placeholder|decoration|shadow|accent|caret)-(?:${palette})-\\d{2,3}\\b`,
  "g",
);
// Classes that were never defined in this UI.
const undefinedClass = /^(?:btn|btn--[\w-]+|card|input|text-txtsecondary|text-txtmain|bg-surface|bg-secondary-hover)$/;
const classAttr = /\bclass(?::[\w-]+)?=("[^"]*"|'[^']*'|\{[^}]*\})/g;
const styleBlock = /<style[^>]*>([\s\S]*?)<\/style>/g;
const hexColour = /#[0-9a-fA-F]{3,8}\b/g;

function violations(path: string): string[] {
  const src = readFileSync(path, "utf8");
  const out: string[] = [];
  for (const m of src.matchAll(paletteClass)) out.push(`raw palette colour ${m[0]}`);
  for (const m of src.matchAll(classAttr)) {
    for (const token of m[1].replace(/[{}"'`]/g, " ").split(/\s+/)) {
      if (undefinedClass.test(token)) out.push(`undefined class ${token}`);
    }
  }
  for (const m of src.matchAll(styleBlock)) {
    for (const h of m[1].matchAll(hexColour)) out.push(`hex colour ${h[0]} in <style>`);
  }
  return out;
}

describe("style guard", () => {
  it("uses only defined classes and theme colour tokens", () => {
    const found = files(root).flatMap((p) => violations(p).map((v) => `${relative(root, p)}: ${v}`));
    expect(found).toEqual([]);
  });
});
