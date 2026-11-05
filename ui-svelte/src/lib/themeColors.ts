// Theme colours for canvas charts. chart.js cannot parse oklch() or
// color-mix(), so any CSS colour expression (a token, var(), color-mix()) is
// resolved by the browser and read back as rgba() through a 1px canvas.

/** Resolves a CSS colour expression to rgba(); `fallback` when unavailable. */
export function resolveColor(expression: string, fallback: string): string {
  try {
    const el = document.createElement("span");
    el.style.color = expression;
    document.documentElement.appendChild(el);
    const computed = getComputedStyle(el).color;
    el.remove();
    const probe = document.createElement("canvas");
    probe.width = probe.height = 1;
    const ctx = probe.getContext("2d", { willReadFrequently: true });
    if (!ctx || !computed) return fallback;
    ctx.fillStyle = computed;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
    return `rgba(${r},${g},${b},${Math.round((a / 255) * 1000) / 1000})`;
  } catch {
    return fallback;
  }
}

/** Resolves a theme token such as "--card" to rgba(). */
export function tokenColor(name: string, fallback: string): string {
  return resolveColor(`var(${name})`, fallback);
}
