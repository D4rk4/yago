// Progressive-enhancement tablist for the admin console. Without JavaScript
// every tab panel is shown in sequence under its own heading (a plain, readable
// long page); with JavaScript the panels collapse into a single active tab. No
// framework, no dependencies — it wires the ARIA tab pattern over the markup the
// server already renders.
(function () {
  "use strict";

  function activate(tabs, panels, index) {
    tabs.forEach(function (tab, i) {
      var selected = i === index;
      tab.setAttribute("aria-selected", selected ? "true" : "false");
      tab.tabIndex = selected ? 0 : -1;
      panels[i].hidden = !selected;
    });
  }

  function wire(container) {
    var list = container.querySelector('[role="tablist"]');
    if (!list) {
      return;
    }
    var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
    var panels = tabs.map(function (tab) {
      return document.getElementById(tab.getAttribute("aria-controls"));
    });
    if (!tabs.length || panels.indexOf(null) !== -1) {
      return;
    }
    container.classList.add("js-tabs");

    var current = 0;
    tabs.forEach(function (tab, i) {
      tab.addEventListener("click", function () {
        current = i;
        activate(tabs, panels, current);
      });
      tab.addEventListener("keydown", function (event) {
        var next = current;
        if (event.key === "ArrowRight" || event.key === "ArrowDown") {
          next = (current + 1) % tabs.length;
        } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
          next = (current - 1 + tabs.length) % tabs.length;
        } else if (event.key === "Home") {
          next = 0;
        } else if (event.key === "End") {
          next = tabs.length - 1;
        } else {
          return;
        }
        event.preventDefault();
        current = next;
        activate(tabs, panels, current);
        tabs[current].focus();
      });
    });
    activate(tabs, panels, current);
  }

  function init() {
    Array.prototype.slice
      .call(document.querySelectorAll("[data-tabs]"))
      .forEach(wire);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
  // htmx swaps replace the main content; re-wire any tabs it brings in.
  document.addEventListener("htmx:afterSwap", init);
})();
