// One shared confirm modal. htmx asks before it sends a request that has hx-confirm;
// we show the server-rendered text in <dialog id="confirm-modal"> instead of window.confirm.
// Also: close buttons ([data-close]) and sheets that open and close on server events.
// fillConfirm sets the text, title, icon and confirm button of the shared dialog from the asking element.
// Without data-confirm-* attributes the dialog shows its defaults.
function fillConfirm(modal, question, elt) {
  const ok = document.getElementById("confirm-ok");
  const icon = elt.getAttribute("data-confirm-icon") || "alert";
  modal.querySelector("[data-confirm-text]").textContent = question;
  document.getElementById("confirm-title").textContent = elt.getAttribute("data-confirm-title") || "Please confirm";
  document.getElementById("confirm-icon").replaceChildren(spriteIcon(icon, "l"));
  const label = elt.getAttribute("data-confirm-ok") || "Confirm";
  ok.replaceChildren(...(icon === "alert" ? [] : [spriteIcon(icon, "s")]), label);
}

// spriteIcon returns an <svg> that uses an icon of the page sprite.
function spriteIcon(name, size) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("class", "c-icon");
  svg.setAttribute("data-size", size);
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS(ns, "use");
  use.setAttribute("href", "#i-" + name);
  svg.append(use);
  return svg;
}

document.addEventListener("htmx:confirm", (e) => {
  const question = e.detail.question;
  const modal = document.getElementById("confirm-modal");
  if (!question || !modal) { return; }
  e.preventDefault();
  fillConfirm(modal, question, e.detail.elt);
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
// brings content, set the title from its data-sheet-title (and the line under it from data-sheet-sub) and open the dialog. A fragment is one
// root with data-sheet-title; an empty answer (a saved form) leaves the sheet as it is.
document.body.addEventListener("htmx:afterSwap", (e) => {
  const body = e.detail.target;
  if (!(body instanceof HTMLElement) || body.id !== "sheet-body") { return; }
  // htmx still reports a swap for a reply with HX-Reswap: none; nothing arrived, so do not open.
  if (e.detail.xhr && e.detail.xhr.getResponseHeader("HX-Reswap") === "none") { return; }
  const dlg = body.closest("dialog");
  if (!dlg || !body.firstElementChild) { return; }
  const title = body.querySelector("[data-sheet-title]");
  const head = document.getElementById(dlg.id + "-title");
  if (head) { head.textContent = title ? title.getAttribute("data-sheet-title") : "Details"; }
  const sub = document.getElementById(dlg.id + "-sub");
  const subText = title ? title.getAttribute("data-sheet-sub") || "" : "";
  if (sub) { sub.textContent = subText; sub.hidden = subText === ""; }
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

// Server events: HX-Trigger "close-sheet" closes every open sheet. The server sends it last. The
// host is emptied after the other events have fired, because events fired on a removed form never
// reach the body and the list would not refresh.
document.body.addEventListener("close-sheet", () => {
  for (const d of document.querySelectorAll("dialog.c-sheet[open]")) {
    d.close();
    // An old form left in the host would reopen the sheet.
    setTimeout(() => d.querySelector(".c-sheet__host")?.replaceChildren(), 0);
  }
});
