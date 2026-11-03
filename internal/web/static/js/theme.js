// Theme button: toggles data-theme between air and air-dark and remembers it in the "theme" cookie.
// The server renders the same value on the next page load, so there is no flash.
// The allowed list comes from the server: data-themes="air air-dark" on the button.
function themes(btn) {
  const list = (btn.getAttribute("data-themes") || "").split(/\s+/).filter(Boolean);
  return list.length ? list : ["air", "air-dark"];
}

function apply(theme) {
  document.documentElement.setAttribute("data-theme", theme);
  document.cookie = "theme=" + theme + "; path=/; max-age=31536000; samesite=lax";
  for (const btn of document.querySelectorAll("[data-theme-toggle]")) {
    btn.setAttribute("aria-pressed", theme === "air-dark" ? "true" : "false");
  }
}

document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-theme-toggle]");
  if (!btn) { return; }
  const list = themes(btn);
  const now = document.documentElement.getAttribute("data-theme");
  apply(list[(list.indexOf(now) + 1) % list.length]);
});
