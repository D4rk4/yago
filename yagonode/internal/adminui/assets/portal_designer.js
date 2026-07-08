/* Portal design tabs (ADR-0033): progressively enhances each design form's
   plain textareas with a CodeMirror Handlebars/CSS editor and an optional
   GrapesJS visual canvas. Without JavaScript the textareas submit as-is. */
(function () {
  "use strict";

  var CSS_START = "/* grapes:start */";
  var CSS_END = "/* grapes:end */";

  function splitDocument(src) {
    var match = src.match(/^([\s\S]*?<body[^>]*>)([\s\S]*?)(<\/body>[\s\S]*)$/i);
    if (!match) {
      return { prefix: "", body: src, suffix: "" };
    }
    return { prefix: match[1], body: match[2], suffix: match[3] };
  }

  function mergeGrapesCss(existing, generated) {
    var start = existing.indexOf(CSS_START);
    var end = existing.indexOf(CSS_END);
    var base = existing;
    if (start >= 0 && end > start) {
      base = existing.slice(0, start) + existing.slice(end + CSS_END.length);
    }
    base = base.replace(/\s+$/, "");
    if (!generated) {
      return base + "\n";
    }
    return base + "\n" + CSS_START + "\n" + generated + "\n" + CSS_END + "\n";
  }

  function initDesigner(root) {
    if (root.classList.contains("designer-ready")) {
      return;
    }
    var form = root.querySelector("form");
    var tplArea = root.querySelector("textarea[data-designer-template]");
    var cssArea = root.querySelector("textarea[data-designer-styles]");
    var canvas = root.querySelector(".designer-canvas");
    var toggle = root.querySelector(".designer-toggle");
    if (!form || !tplArea || !cssArea || !canvas || !toggle ||
        typeof CodeMirror === "undefined") {
      return;
    }
    root.classList.add("designer-ready");

    var tplCM = CodeMirror.fromTextArea(tplArea, {
      mode: { name: "handlebars", base: "text/html" },
      lineNumbers: true,
      lineWrapping: true,
      viewportMargin: 30
    });
    var cssCM = CodeMirror.fromTextArea(cssArea, {
      mode: "css",
      lineNumbers: true,
      lineWrapping: true,
      viewportMargin: 30
    });

    var grapes = null;
    var docParts = null;
    var visual = false;
    if (typeof grapesjs !== "undefined") {
      toggle.hidden = false;
    }

    function syncFromVisual() {
      if (!grapes || !docParts) {
        return;
      }
      tplCM.setValue(docParts.prefix + grapes.getHtml() + docParts.suffix);
      cssCM.setValue(mergeGrapesCss(cssCM.getValue(), grapes.getCss() || ""));
    }

    function enterVisual() {
      if (typeof grapesjs === "undefined") {
        return;
      }
      docParts = splitDocument(tplCM.getValue());
      if (grapes) {
        grapes.setComponents(docParts.body);
        grapes.setStyle(cssCM.getValue());
      } else {
        var plugins = [];
        if (typeof window["grapesjs-preset-webpage"] !== "undefined") {
          plugins.push(window["grapesjs-preset-webpage"]);
        }
        grapes = grapesjs.init({
          container: canvas,
          height: "480px",
          fromElement: false,
          components: docParts.body,
          style: cssCM.getValue(),
          storageManager: false,
          plugins: plugins
        });
      }
      visual = true;
      root.classList.add("designer-visual");
      toggle.textContent = "Code editor";
    }

    function leaveVisual() {
      syncFromVisual();
      visual = false;
      root.classList.remove("designer-visual");
      toggle.textContent = "Visual editor";
      tplCM.refresh();
      cssCM.refresh();
    }

    toggle.addEventListener("click", function () {
      if (visual) {
        leaveVisual();
      } else {
        enterVisual();
      }
    });

    form.addEventListener("submit", function () {
      if (visual) {
        syncFromVisual();
      }
      tplCM.save();
      cssCM.save();
    });
  }

  function initAll() {
    var roots = document.querySelectorAll("[data-designer]");
    for (var i = 0; i < roots.length; i++) {
      initDesigner(roots[i]);
    }
  }

  initAll();
  document.body.addEventListener("htmx:afterSwap", initAll);
})();
