// Theme button: switches between light and dark and remembers the choice in the "theme" cookie.
// The server renders the cookie value into <html data-theme> (or "auto" when there is none, then the OS decides),
// so the page never flashes. The allowed values come from the server: data-themes="light dark" on the button.
function themes(btn) {
  const list = (btn.getAttribute("data-themes") || "").split(/\s+/).filter(Boolean);
  return list.length ? list : ["light", "dark"];
}

// current returns the theme on screen: the chosen one, or what the OS shows for "auto".
function current() {
  const now = document.documentElement.getAttribute("data-theme");
  if (now === "light" || now === "dark") { return now; }
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function setPressed() {
  for (const btn of document.querySelectorAll("[data-theme-toggle]")) {
    btn.setAttribute("aria-pressed", current() === "dark" ? "true" : "false");
  }
}

function apply(theme) {
  document.documentElement.setAttribute("data-theme", theme);
  document.cookie = "theme=" + theme + "; path=/; max-age=31536000; samesite=lax";
  setPressed();
}

document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-theme-toggle]");
  if (!btn) { return; }
  const list = themes(btn);
  apply(list[(list.indexOf(current()) + 1) % list.length]);
});

setPressed();
