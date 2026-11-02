// Copy button of ui.CodeBlock: <button data-copy-target="id"> copies the value of #id.
document.addEventListener("click", function (e) {
  var btn = e.target.closest("[data-copy-target]");
  if (!btn) { return; }
  var el = document.getElementById(btn.getAttribute("data-copy-target"));
  if (!el) { return; }
  el.select();
  if (navigator.clipboard) { navigator.clipboard.writeText(el.value); } else { document.execCommand("copy"); }
});

// Menu button of ui.Layout (small screens): shows or hides the sidebar.
// Closes on a click on the backdrop (the body itself), on Escape, and when the page is restored from the back/forward cache.
function setNav(open) {
  document.body.toggleAttribute("data-nav-open", open);
  var btn = document.querySelector("[data-nav-toggle]");
  if (btn) { btn.setAttribute("aria-expanded", open ? "true" : "false"); }
}
document.addEventListener("click", function (e) {
  if (e.target.closest("[data-nav-toggle]")) {
    setNav(!document.body.hasAttribute("data-nav-open"));
  } else if (e.target === document.body) {
    setNav(false);
  }
});
document.addEventListener("keydown", function (e) { if (e.key === "Escape") { setNav(false); } });
window.addEventListener("pageshow", function () { setNav(false); });
