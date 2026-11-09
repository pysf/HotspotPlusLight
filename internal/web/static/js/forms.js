// Small form helpers, all driven by data attributes (no inline handlers):
//   [data-reveal="id"]       toggles the password field #id between hidden and visible (an eye button with
//                            aria-pressed keeps its icon and changes aria-pressed and aria-label; a text button changes its text)
//   [data-select-all="sel"]  a checkbox that ticks every checkbox matching sel
//   [data-generate-digits="id"]  a button that puts a random 6-digit number into the field #id
//   [data-bulk-bar]          the bar of a [data-bulk-guard] form: shown with the count while something is ticked;
//                            [data-bulk-clear] unticks everything
//   [data-clear="id"]        a button that empties the element #id
//   [data-bulk-guard]        a form: htmx does not send it when nothing is ticked
//   [aria-busy]              a region that htmx updates: aria-busy is "true" while the request runs
// randomDigits returns n random decimal digits from the browser's random source.
function randomDigits(n) {
  const buf = new Uint32Array(n);
  crypto.getRandomValues(buf);
  return Array.from(buf, (v) => v % 10).join("");
}

// syncBulk shows the bulk bar with the number of selected rows, or hides it when nothing is ticked.
// It also sets the header checkbox to ticked, half ticked or empty.
function syncBulk(form) {
  const bar = form.querySelector("[data-bulk-bar]");
  const rows = form.querySelectorAll("input[name=id]");
  const ticked = form.querySelectorAll("input[name=id]:checked").length;
  const all = form.querySelector("input[name=all_results]")?.checked;
  const head = form.querySelector("[data-select-all]");
  if (head) {
    head.checked = rows.length > 0 && ticked === rows.length;
    head.indeterminate = ticked > 0 && ticked < rows.length;
  }
  if (!bar) { return; }
  const count = all ? Number(bar.getAttribute("data-total")) : ticked;
  bar.hidden = count === 0;
  bar.querySelector("[data-bulk-count]").textContent = count + " selected";
}

document.addEventListener("click", (e) => {
  const reveal = e.target.closest("[data-reveal]");
  if (reveal) {
    const input = document.getElementById(reveal.getAttribute("data-reveal"));
    if (!input) { return; }
    input.type = input.type === "password" ? "text" : "password";
    const shown = input.type !== "password";
    if (reveal.hasAttribute("aria-pressed")) {
      reveal.setAttribute("aria-pressed", String(shown));
      reveal.setAttribute("aria-label", shown ? "Hide password" : "Show password");
    } else {
      reveal.textContent = shown ? "Hide" : "Reveal";
    }
    return;
  }
  const gen = e.target.closest("[data-generate-digits]");
  if (gen) {
    const field = document.getElementById(gen.getAttribute("data-generate-digits"));
    if (field) { field.value = randomDigits(6); field.dispatchEvent(new Event("input", { bubbles: true })); }
    return;
  }
  const clearBulk = e.target.closest("[data-bulk-clear]");
  if (clearBulk) {
    const form = clearBulk.closest("[data-bulk-guard]");
    for (const box of form.querySelectorAll("input[type=checkbox]")) { box.checked = false; }
    syncBulk(form);
    return;
  }
  const clear = e.target.closest("[data-clear]");
  if (clear) { document.getElementById(clear.getAttribute("data-clear"))?.replaceChildren(); }
});

document.addEventListener("change", (e) => {
  const all = e.target.closest("[data-select-all]");
  if (all) {
    for (const box of document.querySelectorAll(all.getAttribute("data-select-all"))) { box.checked = all.checked; }
  }
  const form = e.target.closest("[data-bulk-guard]");
  if (form) { syncBulk(form); }
});

document.addEventListener("htmx:configRequest", (e) => {
  const form = e.detail.elt.closest("[data-bulk-guard]");
  if (!form || e.detail.elt !== form) { return; }
  if (form.querySelector("input[name=id]:checked, input[name=all_results]:checked")) { return; }
  const note = document.getElementById("no-selection-notice");
  if (!note) { return; }
  e.preventDefault();
  document.getElementById("bulk-result")?.replaceChildren(note.content.cloneNode(true));
});

function setBusy(e, busy) {
  const t = e.detail.target;
  if (t instanceof HTMLElement && t.hasAttribute("aria-busy")) { t.setAttribute("aria-busy", busy ? "true" : "false"); }
}
document.body.addEventListener("htmx:beforeRequest", (e) => setBusy(e, true));
document.body.addEventListener("htmx:afterRequest", (e) => setBusy(e, false));
