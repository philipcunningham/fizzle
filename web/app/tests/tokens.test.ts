// WCAG AA contrast pass over the design tokens (Q5, Q6). Parses the
// token stylesheet so the assertion tracks the real values; a palette
// edit that breaks contrast fails here. Text pairs are held to 4.5;
// borders, control fills, and state marks carry meaning without being
// text, so they are held to the 3.0 of WCAG 1.4.11.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const css = readFileSync(join(__dirname, "../src/shell/tokens.css"), "utf8");
const mockup = readFileSync(join(__dirname, "../src/shell/mockup.css"), "utf8");

function token(name: string): string {
  const m = new RegExp(`--fz-${name}:\\s*(#[0-9a-fA-F]{6})`).exec(css);
  if (!m?.[1]) throw new Error(`token --fz-${name} not found or not a 6-digit hex`);
  return m[1];
}

/** One declaration's value, read from the real stylesheet. */
function styleValue(sheet: string, selector: string, property: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const rule = new RegExp(`(?:^|})\\s*${escaped}\\s*\\{([^}]*)}`, "m").exec(sheet);
  if (!rule?.[1]) throw new Error(`no rule for ${selector}`);
  const declared = new RegExp(`(?:^|;)\\s*${property}:\\s*([^;]+)`).exec(rule[1]);
  if (!declared?.[1]) throw new Error(`${selector} declares no ${property}`);
  return declared[1].trim();
}

function luminance(hex: string): number {
  const chan = (i: number) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * chan(1) + 0.7152 * chan(3) + 0.0722 * chan(5);
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

/**
 * What the eye actually receives from an element drawn at less than
 * full opacity: the browser composites in sRGB, so the ratio has to be
 * measured on the blend rather than on the token.
 */
function over(fg: string, bg: string, alpha: number): string {
  const mix = (i: number) => {
    const f = parseInt(fg.slice(i, i + 2), 16);
    const b = parseInt(bg.slice(i, i + 2), 16);
    return Math.round(f * alpha + b * (1 - alpha))
      .toString(16)
      .padStart(2, "0");
  };
  return `#${mix(1)}${mix(3)}${mix(5)}`;
}

const TEXT = 4.5;
const NON_TEXT = 3.0;

// Every foreground token that carries text, against every background
// it sits on, at the AA threshold for normal text.
const FOREGROUNDS = ["fg", "fg-dim", "fg-faint", "accent-bright", "warning", "error", "ok"];
const BACKGROUNDS = ["bg", "bg-raised", "bg-panel"];

describe("design token contrast (WCAG AA)", () => {
  for (const fg of FOREGROUNDS) {
    for (const bg of BACKGROUNDS) {
      it(`${fg} on ${bg} is at least 4.5:1`, () => {
        expect(contrast(token(fg), token(bg))).toBeGreaterThanOrEqual(TEXT);
      });
    }
  }

  it("defines the monospace stack once", () => {
    expect(css).toMatch(/--fz-font:\s*[^;]*monospace/);
  });
});

describe("non-text contrast (WCAG 1.4.11)", () => {
  // The edges are the whole of what identifies a control here. An
  // input's black fill is 1.14 against the raised panel it sits in and
  // the active tab is 1.14 against the page, so nothing but the border
  // says where either begins. It has to read on all three surfaces.
  for (const edge of ["border-faint", "border", "accent"]) {
    for (const bg of BACKGROUNDS) {
      it(`${edge} on ${bg} is at least 3:1`, () => {
        expect(contrast(token(edge), token(bg))).toBeGreaterThanOrEqual(NON_TEXT);
      });
    }
  }

  // The row buttons rest at reduced opacity and brighten on hover, so
  // what the eye gets at rest is the border token blended into the row
  // behind it, not the token itself.
  it("a resting row button's edge is at least 3:1 over the row", () => {
    const alpha = Number(styleValue(mockup, ".rowbtn", "opacity"));
    expect(alpha).toBeGreaterThan(0);
    expect(alpha).toBeLessThanOrEqual(1);
    const edge = over(token("border"), token("bg-raised"), alpha);
    expect(contrast(edge, token("bg-raised"))).toBeGreaterThanOrEqual(NON_TEXT);
  });
});

// The piano is the one light surface in a dark app, so its marks need
// their own values: a mark that reads on a black key disappears on a
// white one.
describe("on screen keyboard contrast (Q5)", () => {
  const FILLS = ["key-white", "key-white-on", "key-black", "key-black-on"];

  it("the in range state reads against a plain black key", () => {
    expect(contrast(token("key-black-on"), token("key-black"))).toBeGreaterThanOrEqual(NON_TEXT);
  });

  it("the in range state reads against a plain white key", () => {
    expect(contrast(token("key-white-on"), token("key-white"))).toBeGreaterThanOrEqual(NON_TEXT);
  });

  it("the root mark reads on every dark key it is drawn on", () => {
    for (const fill of ["key-white-on", "key-black", "key-black-on"]) {
      expect(contrast(token("key-root"), token(fill))).toBeGreaterThanOrEqual(NON_TEXT);
    }
  });

  it("the root mark reads on a plain white key", () => {
    expect(contrast(token("key-root-light"), token("key-white"))).toBeGreaterThanOrEqual(NON_TEXT);
  });

  // No single stroke clears 3:1 on all four fills, so the ring is two
  // strokes and the test asks that one of them always reads.
  for (const fill of FILLS) {
    it(`the focus ring reads on ${fill}`, () => {
      const best = Math.max(
        contrast(token("key-focus-dark"), token(fill)),
        contrast(token("key-focus-light"), token(fill)),
      );
      expect(best).toBeGreaterThanOrEqual(NON_TEXT);
    });
  }

  it("the two focus ring strokes read against each other", () => {
    expect(contrast(token("key-focus-dark"), token("key-focus-light"))).toBeGreaterThanOrEqual(
      NON_TEXT,
    );
  });

  it("the octave label is text, so it clears 4.5 on both white keys", () => {
    expect(contrast(token("key-label"), token("key-white"))).toBeGreaterThanOrEqual(TEXT);
    expect(contrast(token("key-label-on"), token("key-white-on"))).toBeGreaterThanOrEqual(TEXT);
  });
});

describe("hierarchy from brightness and colour (Q6)", () => {
  // Which file is open was a 1.09 step of hue and nothing else. Colour
  // may carry it, but never alone (WCAG 1.4.1).
  it("marks the open file row with weight as well as hue", () => {
    expect(styleValue(mockup, ".filerow.selected", "font-weight")).toBe("700");
  });
});

// The keyboard's in-range keys must read as a lit range against the
// surface they actually sit on (the keyboard bar is bg-raised, not
// the page black), never as a void: a fill too close to its surface
// makes the key range invisible, a meaning carried by colour alone.
describe("keyboard range visibility", () => {
  it("the highlighted range reads against its bar", () => {
    expect(contrast(token("key-white-on"), token("bg-raised"))).toBeGreaterThanOrEqual(NON_TEXT);
    expect(contrast(token("key-black-on"), token("bg-raised"))).toBeGreaterThanOrEqual(NON_TEXT);
  });

  // The amber root dot and the plain key fills pin both in-range
  // fills into one narrow luminance band, so the fills cannot also
  // separate white keys from black keys; the in-range edge stroke is
  // the boundary that carries that meaning, and it needs 3:1 against
  // both fills it separates.
  it("the in-range key edge separates white keys from black keys", () => {
    expect(contrast(token("key-edge-on"), token("key-white-on"))).toBeGreaterThanOrEqual(NON_TEXT);
    expect(contrast(token("key-edge-on"), token("key-black-on"))).toBeGreaterThanOrEqual(NON_TEXT);
  });
});

// Text needs air inside its box: the drop zone's browse button was
// flush against the prompt line, and the matrix controller labels sat
// on the cell border.
describe("labels get room from their boxes", () => {
  it("the drop zone separates its prompt from the browse button", () => {
    expect(
      parseInt(styleValue(mockup, ".dropzone .btn", "margin-top"), 10),
    ).toBeGreaterThanOrEqual(12);
  });

  it("the matrix controller labels clear the cell border", () => {
    const padding = styleValue(mockup, "table.term.matrix td.matrixrow", "padding").split(" ");
    expect(parseInt(padding[0] ?? "", 10)).toBeGreaterThanOrEqual(4);
    expect(parseInt(padding[1] ?? padding[0] ?? "", 10)).toBeGreaterThanOrEqual(8);
  });
});
