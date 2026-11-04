/* Optional. Shows a spinner on the Log in button and stops a second click. The page works without this file. */
(function () {
  var form = document.querySelector("form[data-busy]");
  if (!form) return;
  var button = form.querySelector(".btn");
  form.addEventListener("submit", function (event) {
    if (button.getAttribute("aria-busy") === "true") {
      event.preventDefault();
      return;
    }
    button.setAttribute("aria-busy", "true");
  });
  /* Back button: the page may come from the cache with the spinner still on. */
  window.addEventListener("pageshow", function () {
    button.removeAttribute("aria-busy");
  });
})();
