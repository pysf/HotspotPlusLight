// "/" focuses the search box (the first [data-search] input), unless you are typing somewhere.
document.addEventListener("keydown", (e) => {
  if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey) { return; }
  const t = e.target;
  if (t instanceof HTMLElement && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) { return; }
  const search = document.querySelector("[data-search]");
  if (!search) { return; }
  e.preventDefault();
  search.focus();
});
