// Validated categorical palette (8 slots, fixed order — never cycled/regenerated).
// Each slot passes adjacent-pair CVD and normal-vision gates in both light and dark.
// See the data-viz skill's references/palette.md for the validation report.
const CATEGORICAL: { light: string; dark: string }[] = [
  { light: "#2a78d6", dark: "#3987e5" }, // blue
  { light: "#eb6834", dark: "#d95926" }, // orange
  { light: "#1baf7a", dark: "#199e70" }, // aqua
  { light: "#eda100", dark: "#c98500" }, // yellow
  { light: "#e87ba4", dark: "#d55181" }, // magenta
  { light: "#008300", dark: "#008300" }, // green
  { light: "#4a3aa7", dark: "#9085e9" }, // violet
  { light: "#e34948", dark: "#e66767" }, // red
];
// Past the 8th distinct entity, fold into this de-emphasis gray rather than
// generating a 9th hue (indistinguishable from an existing one under CVD).
const OTHER = { light: "#898781", dark: "#898781" };

export function chartChrome(dark: boolean) {
  return {
    surface: dark ? "#1a1a19" : "#fcfcfb",
    primary: dark ? "#ffffff" : "#0b0b0b",
    secondary: dark ? "#c3c2b7" : "#52514e",
    muted: "#898781",
    grid: dark ? "#2c2c2a" : "#e1e0d9",
    baseline: dark ? "#383835" : "#c3c2b7",
  };
}

export const STATUS = {
  good: "#0ca30c",
  warning: "#fab219",
  serious: "#ec835a",
  critical: "#d03b3b",
};

/** One outcome, one colour and one wording, on every page that shows per-item state. */
export const OUTCOME = {
  pass: { color: STATUS.good, label: "Full marks" },
  part: { color: STATUS.warning, label: "Partial" },
  fail: { color: STATUS.critical, label: "No marks" },
  none: { color: STATUS.serious, label: "No answer" },
  human: { color: "#2a78d6", label: "Awaiting score" },
  unsupported: { color: "#898781", label: "Unsupported" },
} as const;

export type Outcome = keyof typeof OUTCOME;

// Medal tiers for a "best at" leaderboard. Decorative accents, not a data
// encoding — rank is already carried by row position and the label text, so
// these don't need CVD separation from each other or from the categorical set.
export const RANK_TIERS = [
  { color: "#9c6b0a", label: "Gold" },
  { color: "#75766f", label: "Silver" },
  { color: "#8a5a2c", label: "Bronze" },
] as const;

/** Assigns a stable slot to each key in first-seen order, capped at 8; folds the rest to gray. */
export class CategoricalAssignment {
  private order: string[] = [];

  color(key: string, dark: boolean): string {
    let index = this.order.indexOf(key);
    if (index === -1) {
      index = this.order.length;
      this.order.push(key);
    }
    const slot = index < CATEGORICAL.length ? CATEGORICAL[index] : OTHER;
    return dark ? slot.dark : slot.light;
  }
}
