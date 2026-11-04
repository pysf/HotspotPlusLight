// Docs search: a small hand-made index, opened with "/" or Ctrl/Cmd+K.
import index from "./search-index.js";

const form = document.querySelector("form.search");
const input = form?.querySelector("input");
const list = form?.querySelector(".results");
// Entry urls are relative to the site root, which is two folders above this file.
const siteRoot = new URL("../../", import.meta.url);

if (form && input && list) {
  form.hidden = false;
  let items = [];
  let active = -1;

  const match = (entry, words) => {
    const hay = `${entry.page} ${entry.heading} ${entry.keywords}`.toLowerCase();
    return words.every((word) => hay.includes(word));
  };

  const close = () => {
    list.hidden = true;
    input.setAttribute("aria-expanded", "false");
    active = -1;
  };

  const highlight = () => {
    items.forEach((li, i) => li.setAttribute("aria-selected", String(i === active)));
  };

  const render = () => {
    const words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return close();
    const found = index.filter((entry) => match(entry, words)).slice(0, 8);
    list.replaceChildren();
    if (!found.length) {
      const none = document.createElement("li");
      none.className = "none";
      none.textContent = "No results";
      list.append(none);
    }
    for (const entry of found) {
      const li = document.createElement("li");
      li.setAttribute("role", "option");
      const a = document.createElement("a");
      a.href = new URL(entry.url, siteRoot).href;
      a.textContent = entry.heading || entry.page;
      if (entry.heading) {
        const small = document.createElement("small");
        small.textContent = entry.page;
        a.append(small);
      }
      li.append(a);
      list.append(li);
    }
    items = [...list.querySelectorAll("li[role=option]")];
    active = -1;
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
  };

  input.setAttribute("aria-controls", list.id);
  input.setAttribute("aria-expanded", "false");
  input.addEventListener("input", render);
  input.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      close();
      input.blur();
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (!items.length) return;
      event.preventDefault();
      active = (active + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
      highlight();
    } else if (event.key === "Enter") {
      event.preventDefault();
      const target = items[Math.max(active, 0)]?.querySelector("a");
      if (target) location.href = target.href;
    }
  });
  form.addEventListener("submit", (event) => event.preventDefault());
  document.addEventListener("click", (event) => {
    if (!form.contains(event.target)) close();
  });
  document.addEventListener("keydown", (event) => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName ?? "");
    const shortcut = event.key === "/" ? !typing : event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey);
    if (shortcut) {
      event.preventDefault();
      input.focus();
      input.select();
    }
  });
}
