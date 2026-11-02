// Copy button of ui.CodeBlock: <button data-copy-target="id"> copies the value of #id.
document.addEventListener("click", function (e) {
  var btn = e.target.closest("[data-copy-target]");
  if (!btn) { return; }
  var el = document.getElementById(btn.getAttribute("data-copy-target"));
  if (!el) { return; }
  el.select();
  if (navigator.clipboard) { navigator.clipboard.writeText(el.value); } else { document.execCommand("copy"); }
});
