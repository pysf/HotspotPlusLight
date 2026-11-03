// Menu button of the app shell (small screens): shows or hides the sidebar.
// It closes on a click on the dark area (the body itself), on Escape,
// and when the page comes back from the back/forward cache.
function setNav(open) {
  document.body.toggleAttribute("data-nav-open", open);
  const btn = document.querySelector("[data-nav-toggle]");
  if (btn) { btn.setAttribute("aria-expanded", open ? "true" : "false"); }
}

document.addEventListener("click", (e) => {
  if (e.target.closest("[data-nav-toggle]")) {
    setNav(!document.body.hasAttribute("data-nav-open"));
  } else if (e.target === document.body) {
    setNav(false);
  }
});
document.addEventListener("keydown", (e) => { if (e.key === "Escape") { setNav(false); } });
window.addEventListener("pageshow", () => setNav(false));
