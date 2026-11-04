// OS tabs for the install bar. Without this script every panel stays visible as a list.
const root = document.querySelector("[data-os-tabs]");

function guessOs() {
  const platform = (navigator.userAgentData?.platform || navigator.platform || "").toLowerCase();
  if (platform.includes("win")) return "windows";
  if (platform.includes("mac")) return "macos";
  return "linux";
}

if (root) {
  const panels = [...root.querySelectorAll("[data-os]")];
  const list = document.createElement("div");
  list.setAttribute("role", "tablist");
  list.setAttribute("aria-label", "Operating system");
  const tabs = panels.map((panel) => {
    const tab = document.createElement("button");
    tab.type = "button";
    tab.setAttribute("role", "tab");
    tab.id = `tab-${panel.dataset.os}`;
    tab.setAttribute("aria-controls", panel.id);
    tab.textContent = panel.dataset.label;
    panel.setAttribute("role", "tabpanel");
    panel.setAttribute("aria-labelledby", tab.id);
    list.append(tab);
    return tab;
  });

  const select = (index, focus) => {
    panels.forEach((panel, i) => {
      panel.hidden = i !== index;
      tabs[i].setAttribute("aria-selected", String(i === index));
      tabs[i].tabIndex = i === index ? 0 : -1;
    });
    if (focus) tabs[index].focus();
  };

  list.addEventListener("keydown", (event) => {
    const now = tabs.indexOf(document.activeElement);
    if (now < 0) return;
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (step) {
      event.preventDefault();
      select((now + step + tabs.length) % tabs.length, true);
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      select(event.key === "Home" ? 0 : tabs.length - 1, true);
    }
  });
  tabs.forEach((tab, i) => tab.addEventListener("click", () => select(i, false)));

  panels[0].before(list);
  root.classList.add("is-tabbed");
  const wanted = panels.findIndex((panel) => panel.dataset.os === guessOs());
  select(Math.max(wanted, 0), false);
}
