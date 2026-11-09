// One shared confirm modal. htmx asks before it sends a request that has hx-confirm;
// we show the server-rendered text in <dialog id="confirm-modal"> instead of window.confirm.
// Also: close buttons ([data-close]) and sheets that open and close on server events.
document.addEventListener("htmx:confirm", (e) => {
  const question = e.detail.question;
  const modal = document.getElementById("confirm-modal");
  if (!question || !modal) { return; }
  e.preventDefault();
  modal.querySelector("[data-confirm-text]").textContent = question;
  modal.returnValue = "";
  modal.showModal();
  modal.addEventListener("close", () => {
    if (modal.returnValue === "ok") { e.detail.issueRequest(true); }
  }, { once: true });
});

document.addEventListener("click", (e) => {
  const opener = e.target.closest("[data-open-dialog]");
  if (opener) {
    document.getElementById(opener.getAttribute("data-open-dialog"))?.showModal();
    return;
  }
  const closer = e.target.closest("[data-close]");
  if (closer) { closer.closest("dialog")?.close(closer.value); }
});

// A click on the dark area around a dialog closes it.
document.addEventListener("click", (e) => {
  const d = e.target;
  if (!(d instanceof HTMLDialogElement) || !d.open) { return; }
  const box = d.getBoundingClientRect();
  const outside = e.clientX < box.left || e.clientX > box.right || e.clientY < box.top || e.clientY > box.bottom;
  if (outside) { d.close(); }
});

// The sheet: pages load a form into <dialog id="sheet"> by targeting #sheet-body. When that swap
// brings content, set the title from its data-sheet-title and open the dialog. A fragment is one
// root with data-sheet-title; an empty answer (a saved form) leaves the sheet as it is.
document.body.addEventListener("htmx:afterSwap", (e) => {
  const body = e.detail.target;
  if (!(body instanceof HTMLElement) || body.id !== "sheet-body") { return; }
  const dlg = body.closest("dialog");
  if (!dlg || !body.firstElementChild) { return; }
  const title = body.querySelector("[data-sheet-title]");
  const head = document.getElementById(dlg.id + "-title");
  if (head) { head.textContent = title ? title.getAttribute("data-sheet-title") : "Details"; }
  if (!dlg.open) { dlg.showModal(); }
});

// A button inside the sheet that loads a new form into it (like "Create another") hides the old
// content at once, so old input such as a password never stays visible while the new form loads.
document.body.addEventListener("htmx:beforeRequest", (e) => {
  const body = document.getElementById("sheet-body");
  if (body && e.detail.target === body && body.contains(e.detail.elt)) { body.style.visibility = "hidden"; }
});
for (const name of ["htmx:afterSwap", "htmx:afterRequest"]) {
  document.body.addEventListener(name, () => {
    const body = document.getElementById("sheet-body");
    if (body) { body.style.visibility = ""; }
  });
}

// When the page's sheet host closes, empty its body, so old input never shows again.
document.addEventListener("close", (e) => {
  if (e.target instanceof HTMLDialogElement && e.target.hasAttribute("data-sheet-host")) {
    e.target.querySelector(".c-sheet__host")?.replaceChildren();
  }
}, true);

// Server events: HX-Trigger "close-sheet" closes every open sheet.
document.body.addEventListener("close-sheet", () => {
  for (const d of document.querySelectorAll("dialog.c-sheet[open]")) { d.close(); }
});
