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

// Server events: HX-Trigger "close-sheet" closes every open sheet.
document.body.addEventListener("close-sheet", () => {
  for (const d of document.querySelectorAll("dialog.c-sheet[open]")) { d.close(); }
});
