// Toast: <div id="toast" class="c-toast" role="status">. Shows text from a "toast" event.
// Servers send it with the HX-Trigger header: {"toast": "Saved"}. Also reports failed requests.
// A message goes away after 4 s. An error (role="alert") stays until it is closed.
let timer;

function show(text, kind) {
  const el = document.getElementById("toast");
  if (!el || !text) { return; }
  const isError = kind === "error";
  el.querySelector("[data-toast-text]").textContent = text;
  el.setAttribute("role", isError ? "alert" : "status");
  el.querySelector(".c-toast__icon use")?.setAttribute("href", isError ? "#i-alert" : "#i-check-circle");
  if (isError) { el.setAttribute("data-kind", "error"); } else { el.removeAttribute("data-kind"); }
  el.setAttribute("data-open", "");
  clearTimeout(timer);
  if (!isError) { timer = setTimeout(() => el.removeAttribute("data-open"), 4000); }
}

document.addEventListener("click", (e) => {
  if (e.target.closest("[data-toast-close]")) { document.getElementById("toast")?.removeAttribute("data-open"); }
});

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

// <button data-toast="Saved." data-toast-kind="error"> shows that text (used by the dev gallery).
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-toast]");
  if (btn) { show(btn.getAttribute("data-toast"), btn.getAttribute("data-toast-kind")); }
});

// An error response that carried its own toast in HX-Trigger keeps that text.
document.body.addEventListener("htmx:responseError", (e) => {
  const xhr = e.detail.xhr;
  const trigger = (xhr && xhr.getResponseHeader("HX-Trigger")) || "";
  const redirecting = xhr && xhr.getResponseHeader("HX-Redirect"); // 401/403: the page is leaving
  if (!redirecting && !trigger.includes('"toast"')) { show("Something went wrong.", "error"); }
});
// A poll marked data-quiet-poll fails on purpose while the program restarts after an update: no toast.
document.body.addEventListener("htmx:sendError", (e) => {
  if (e.detail.elt && e.detail.elt.closest("[data-quiet-poll]")) { return; }
  show("Cannot reach the server.", "error");
});
