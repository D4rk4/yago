// Accessible search-box autocomplete for the admin console (WAI-ARIA combobox
// over a listbox), fetching suggestions from this console's suggest endpoint.
// Served as a static asset because the console CSP forbids inline scripts.
(function () {
  "use strict";
  var input = document.getElementById("q");
  var list = document.getElementById("ac-list");
  if (!input || !list) return;
  var form = input.form, timer = null, options = [], active = -1;
  function close() {
    list.hidden = true; list.textContent = ""; options = []; active = -1;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
  }
  function pick(i) { input.value = options[i].textContent; close(); form.submit(); }
  function highlight(i) {
    if (active >= 0) options[active].removeAttribute("aria-selected");
    active = i;
    if (i >= 0) {
      options[i].setAttribute("aria-selected", "true");
      input.setAttribute("aria-activedescendant", options[i].id);
    } else input.removeAttribute("aria-activedescendant");
  }
  function render(items) {
    close();
    if (!items.length) return;
    items.forEach(function (text, i) {
      var li = document.createElement("li");
      li.id = "ac-opt-" + i;
      li.setAttribute("role", "option");
      li.textContent = text;
      li.addEventListener("mousedown", function (e) { e.preventDefault(); pick(i); });
      list.appendChild(li);
      options.push(li);
    });
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
  }
  input.addEventListener("input", function () {
    clearTimeout(timer);
    var q = input.value.trim();
    if (q.length < 2) { close(); return; }
    timer = setTimeout(function () {
      fetch("/admin/search/suggest?q=" + encodeURIComponent(q))
        .then(function (r) { return r.json(); })
        .then(function (data) { render((data && data[1]) || []); })
        .catch(close);
    }, 200);
  });
  input.addEventListener("keydown", function (e) {
    if (list.hidden) return;
    if (e.key === "ArrowDown") { e.preventDefault(); highlight((active + 1) % options.length); }
    else if (e.key === "ArrowUp") { e.preventDefault(); highlight((active - 1 + options.length) % options.length); }
    else if (e.key === "Enter" && active >= 0) { e.preventDefault(); pick(active); }
    else if (e.key === "Escape") { close(); }
  });
  input.addEventListener("blur", function () { setTimeout(close, 120); });
})();
