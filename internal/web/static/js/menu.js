// Puts a row menu (popover) next to its button when it opens. Esc and outside click close it natively.
document.addEventListener("toggle", (e) => {
  const menu = e.target;
  if (!(menu instanceof HTMLElement) || !menu.classList.contains("c-menu") || e.newState !== "open") { return; }
  const btn = document.querySelector('[popovertarget="' + menu.id + '"]');
  if (!btn) { return; }
  const r = btn.getBoundingClientRect();
  menu.style.top = Math.min(r.bottom + 4, window.innerHeight - menu.offsetHeight - 8) + "px";
  menu.style.left = Math.max(8, r.right - menu.offsetWidth) + "px";
}, true);

// Choosing an item closes the menu (a popover does not close itself on a click inside).
document.addEventListener("click", (e) => {
  const item = e.target.closest(".c-menu__item");
  if (item) { item.closest(".c-menu")?.hidePopover(); }
});
