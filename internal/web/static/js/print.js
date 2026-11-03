// Print button of the voucher page: <button data-print>.
document.addEventListener("click", (e) => {
  if (e.target.closest("[data-print]")) { window.print(); }
});
