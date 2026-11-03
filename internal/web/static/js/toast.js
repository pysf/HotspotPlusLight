// Toast: <div id="toast" class="c-toast" role="status">. Shows text from a "toast" event.
// Servers send it with the HX-Trigger header: {"toast": "Saved"}. Also reports failed requests.
let timer;

function show(text) {
  const el = document.getElementById("toast");
  if (!el || !text) { return; }
  el.textContent = text;
  el.setAttribute("data-open", "");
  clearTimeout(timer);
  timer = setTimeout(() => el.removeAttribute("data-open"), 2200);
}

document.body.addEventListener("toast", (e) => {
  const d = e.detail;
  show(typeof d === "string" ? d : d && d.value);
});

// <button data-toast="Saved."> shows that text (used by the dev gallery).
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-toast]");
  if (btn) { show(btn.getAttribute("data-toast")); }
});

document.body.addEventListener("htmx:responseError", () => show("Something went wrong."));
document.body.addEventListener("htmx:sendError", () => show("Cannot reach the server."));
