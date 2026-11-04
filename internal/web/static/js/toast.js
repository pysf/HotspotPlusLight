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

// A page that redirects after a change leaves the text in the short-lived "hsp_toast" cookie.
const saved = document.cookie.split("; ").find((c) => c.startsWith("hsp_toast="));
if (saved) {
  document.cookie = "hsp_toast=; path=/admin; max-age=0"; // clear first, so a bad value never stays
  try {
    show(decodeURIComponent(saved.slice("hsp_toast=".length)));
  } catch (_) { /* bad cookie text: ignore it */ }
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
