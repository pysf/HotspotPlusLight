// Scroll-spy: marks the sidebar link of the section that is being read.
const links = [...document.querySelectorAll(".side .sub a[href^='#']")];
const sections = links
  .map((link) => document.getElementById(link.getAttribute("href").slice(1)))
  .filter(Boolean);

if (sections.length && "IntersectionObserver" in window) {
  const mark = (id) => {
    for (const link of links) {
      if (link.getAttribute("href") === `#${id}`) link.setAttribute("aria-current", "location");
      else link.removeAttribute("aria-current");
    }
  };
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) if (entry.isIntersecting) mark(entry.target.id);
    },
    { rootMargin: "-20% 0px -70% 0px" },
  );
  sections.forEach((section) => observer.observe(section));
}
