// Password helpers.
// Generator: <button data-generate="new_password" data-generate-also="confirm_password">
// fills the named inputs with one random password (crypto.getRandomValues) and shows it in the first one.
// Rules: <form data-rules-form data-min-length="10"> with <li data-rule="length"> and <li data-rule="match">
// gets a tick on each rule that holds while the admin types. The server still checks everything.
// Letters and digits without look-alikes (no 0/O, 1/l/I), 16 characters.
const ALPHABET = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789";
const LENGTH = 16;

function generate() {
  // Rejection sampling keeps every character equally likely.
  const limit = 256 - (256 % ALPHABET.length);
  let out = "";
  while (out.length < LENGTH) {
    for (const b of crypto.getRandomValues(new Uint8Array(LENGTH))) {
      if (b < limit && out.length < LENGTH) { out += ALPHABET[b % ALPHABET.length]; }
    }
  }
  return out;
}

document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-generate]");
  if (!btn) { return; }
  const first = document.getElementById(btn.getAttribute("data-generate"));
  if (!first) { return; }
  const pw = generate();
  first.value = pw;
  first.type = "text";
  const eye = document.querySelector('[data-reveal="' + first.id + '"]');
  if (eye && eye.hasAttribute("aria-pressed")) {
    eye.setAttribute("aria-pressed", "true");
    eye.setAttribute("aria-label", "Hide password");
  }
  const also = document.getElementById(btn.getAttribute("data-generate-also") || "");
  if (also) { also.value = pw; }
  const form = first.closest("[data-rules-form]");
  if (form) { updateRules(form); }
});

// updateRules ticks or clears each rule of the form.
function updateRules(form) {
  const pw = form.querySelector('[name="new_password"]');
  const again = form.querySelector('[name="confirm_password"]');
  const min = Number(form.getAttribute("data-min-length")) || 10;
  const holds = {
    length: [...pw.value].length >= min,
    match: again.value !== "" && again.value === pw.value,
  };
  for (const li of form.querySelectorAll("[data-rule]")) {
    const ok = holds[li.getAttribute("data-rule")] === true;
    li.toggleAttribute("data-ok", ok);
    li.querySelector("use")?.setAttribute("href", ok ? "#i-check-circle" : "#i-circle");
  }
}

document.addEventListener("input", (e) => {
  const form = e.target.closest("[data-rules-form]");
  if (form) { updateRules(form); }
});
