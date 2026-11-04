// Password generator: <button data-generate="new_password" data-generate-also="confirm_password">
// fills the named inputs with one random password (crypto.getRandomValues) and shows it in the first one.
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
  const also = document.getElementById(btn.getAttribute("data-generate-also") || "");
  if (also) { also.value = pw; }
});
