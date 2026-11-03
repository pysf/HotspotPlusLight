// Small form helpers, all driven by data attributes (no inline handlers):
//   [data-reveal="id"]       toggles the password field #id between hidden and visible
//   [data-select-all="sel"]  a checkbox that ticks every checkbox matching sel
//   [data-clear="id"]        a button that empties the element #id
//   [data-bulk-guard]        a form: htmx does not send it when nothing is ticked
//   [aria-busy]              a region that htmx updates: aria-busy is "true" while the request runs
document.addEventListener("click", (e) => {
  const reveal = e.target.closest("[data-reveal]");
  if (reveal) {
    const input = document.getElementById(reveal.getAttribute("data-reveal"));
    if (!input) { return; }
    input.type = input.type === "password" ? "text" : "password";
    reveal.textContent = input.type === "password" ? "Reveal" : "Hide";
    return;
  }
  const clear = e.target.closest("[data-clear]");
  if (clear) { document.getElementById(clear.getAttribute("data-clear"))?.replaceChildren(); }
});

document.addEventListener("change", (e) => {
  const all = e.target.closest("[data-select-all]");
  if (!all) { return; }
  for (const box of document.querySelectorAll(all.getAttribute("data-select-all"))) { box.checked = all.checked; }
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
