// What the smoke and the visual baselines both need from a driven
// page. Each takes the page once and returns the helper bound to it.

/** Commits a numeric field and waits for the core's answer to land in it. */
export const makeCommitField = (page) => async (label, value) => {
  const field = page.getByLabel(label);
  await field.fill(value);
  await field.press("Enter");
  await page.waitForFunction(
    ([l, v]) => document.querySelector(`[aria-label="${l}"]`)?.value === v,
    [label, value],
    { timeout: 5000 },
  );
};

/** The drawn loop region's fill, from inside wavesurfer's shadow root. */
export const makeRegionFill = (page) => () =>
  page.evaluate(() => {
    const host = document.querySelector('[data-testid="waveform"] div');
    const el = [...(host?.shadowRoot?.querySelectorAll("[part]") ?? [])].find((n) =>
      /^region region-/.test(n.getAttribute("part")),
    );
    return el ? getComputedStyle(el).backgroundColor : null;
  });
