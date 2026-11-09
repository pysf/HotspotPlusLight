// Polled regions (data-poll): skip a poll while the tab is hidden or a dialog or menu is open.
// Fade regions (data-swap-fade): after a swap, only the child regions ([data-region]) whose content
// changed get the fade (u-swap-in), so unchanged values never flicker.
const FADE = "[data-swap-fade]";
let before = new Map();

document.body.addEventListener("htmx:beforeRequest", (e) => {
  if (!e.detail.elt.hasAttribute("data-poll")) { return; }
  if (document.hidden || document.querySelector("dialog[open], .c-menu:popover-open")) { e.preventDefault(); }
});

document.body.addEventListener("htmx:beforeSwap", (e) => {
  const root = e.detail.target;
  if (!(root instanceof HTMLElement) || !root.matches(FADE)) { return; }
  before = new Map([...root.querySelectorAll("[data-region]")].map((el) => [el.getAttribute("data-region"), el.innerHTML]));
});

document.body.addEventListener("htmx:afterSwap", () => {
  const root = document.querySelector(FADE);
  if (!root || before.size === 0) { return; }
  for (const el of root.querySelectorAll("[data-region]")) {
    const old = before.get(el.getAttribute("data-region"));
    if (old !== undefined && old !== el.innerHTML) { el.classList.add("u-swap-in"); }
  }
  before = new Map();
});
