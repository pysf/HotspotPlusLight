// Copy buttons: one on every code block and one on every [data-copy] button.
const RESET_MS = 1400;

async function writeClipboard(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

function flash(button, label) {
  const old = button.dataset.label ?? button.textContent;
  button.dataset.label = old;
  button.textContent = label;
  setTimeout(() => {
    button.textContent = old;
  }, RESET_MS);
}

async function copyFrom(button, text) {
  flash(button, (await writeClipboard(text)) ? "Copied" : "Copy failed");
}

for (const pre of document.querySelectorAll("pre:not(.out)")) {
  const code = pre.querySelector("code");
  if (!code || pre.closest(".term")) continue;
  const button = document.createElement("button");
  button.type = "button";
  button.className = "copy-btn";
  button.textContent = "Copy";
  button.addEventListener("click", () => copyFrom(button, code.textContent.trim()));
  pre.append(button);
}

for (const button of document.querySelectorAll("button[data-copy]")) {
  button.hidden = false;
  button.addEventListener("click", () => copyFrom(button, button.dataset.copy));
}
