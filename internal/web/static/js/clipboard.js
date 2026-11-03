// Copy buttons. <button data-copy-target="id"> copies the value of #id (an input or textarea).
// <button data-copy-text="..."> copies that text.
// navigator.clipboard only exists on HTTPS or localhost, so plain HTTP uses a hidden textarea.
function legacyCopy(text) {
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.setAttribute("aria-hidden", "true");
  area.style.position = "fixed";
  area.style.insetBlockStart = "0";
  area.style.insetInlineStart = "-9999px";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  let ok = false;
  try { ok = document.execCommand("copy"); } catch { ok = false; }
  area.remove();
  return ok;
}

async function copy(text) {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch { /* fall through to the old way */ }
  }
  return legacyCopy(text);
}

document.addEventListener("click", async (e) => {
  const btn = e.target.closest("[data-copy-target], [data-copy-text]");
  if (!btn) { return; }
  const id = btn.getAttribute("data-copy-target");
  const el = id ? document.getElementById(id) : null;
  const text = el ? el.value : btn.getAttribute("data-copy-text");
  if (text == null) { return; }
  const ok = await copy(text);
  document.body.dispatchEvent(new CustomEvent("toast", { detail: { value: ok ? "Copied" : "Copy failed" } }));
});
