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

  function preservationToken(src) {
    var used = Object.create(null);
    var pattern = /yago-preserved-markup-(\d+)-marker/g;
    var match = null;
    while ((match = pattern.exec(src)) !== null) {
      used[match[1]] = true;
    }
    var sequence = 0;
    while (used[sequence]) {
      sequence++;
    }
    return "yago-preserved-markup-" + sequence + "-marker";
  }

  function preserveScripts(src) {
    var token = preservationToken(src);
    var blocks = [];
    var document = src.replace(
      /<script\b[^>]*>[\s\S]*?<\/script\s*>/gi,
      function (block) {
        var index = blocks.length;
        blocks.push(block);
        return '<template data-yago-preserved-script="' + token + ":" +
          index + '"></template>';
      }
    );
    return { document: document, blocks: blocks, token: token };
  }

  function restoreScripts(document, preservation) {
    var restored = [];
    var marker = /<template\b(?=[^>]*\bdata-yago-preserved-script=["'](yago-preserved-markup-\d+-marker):(\d+)["'])[^>]*>[\s\S]*?<\/template\s*>/gi;
    var html = document.replace(marker, function (placeholder, token, rawIndex) {
      if (token !== preservation.token) {
        return placeholder;
      }
      var index = Number(rawIndex);
      if (!Number.isInteger(index) || !preservation.blocks[index] || restored[index]) {
        return "";
      }
      restored[index] = true;
      return preservation.blocks[index];
    });
    var missing = "";
    for (var j = 0; j < preservation.blocks.length; j++) {
      if (!restored[j]) {
        missing += preservation.blocks[j];
      }
    }
    if (missing) {
      var bodyEnd = html.toLowerCase().lastIndexOf("</body>");
      if (bodyEnd === -1) {
        html += missing;
      } else {
        html = html.slice(0, bodyEnd) + missing + html.slice(bodyEnd);
      }
    }
    return html;
  }

  function visualComponents(editor) {
    var wrapper = editor.getWrapper();
    if (wrapper && typeof wrapper.getInnerHTML === "function") {
      return wrapper.getInnerHTML();
    }
    return editor.getHtml();
  }

  function splitPortalStyles(existing) {
    var start = existing.indexOf(CSS_START);
    var contentStart = start + CSS_START.length;
    var end = start >= 0 ? existing.indexOf(CSS_END, contentStart) : -1;
    if (start < 0 || end < contentStart) {
      return {
        frame: existing,
        visual: "",
        before: null,
        after: null
      };
    }

    return {
      frame: existing.slice(0, start) + existing.slice(end + CSS_END.length),
      visual: existing.slice(contentStart, end).replace(/^\r?\n|\r?\n$/g, ""),
      before: existing.slice(0, start),
      after: existing.slice(end + CSS_END.length)
    };
  }

  function mergeGrapesCss(existing, generated) {
    var parts = splitPortalStyles(existing);
    var visual = (generated || "").replace(/^\s+|\s+$/g, "");
    var block = visual ? CSS_START + "\n" + visual + "\n" + CSS_END : "";
    if (parts.before !== null) {
      return parts.before + block + parts.after;
    }
    if (!block) {
      return existing;
    }
    return existing + (existing && !/\r?\n$/.test(existing) ? "\n" : "") + block + "\n";
  }

  function initDesigner(root) {
    if (root.classList.contains("designer-ready")) {
      return;
    }
    var form = root.matches("form") ? root : root.querySelector("form");
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
      tplCM.setValue(restoreScripts(
        docParts.prefix + visualComponents(grapes) + docParts.suffix,
        docParts.preservation
      ));
      cssCM.setValue(mergeGrapesCss(
        cssCM.getValue(),
        grapes.getCss({ avoidProtected: true, keepUnusedStyles: true }) || ""
      ));
    }

    function enterVisual() {
      if (typeof grapesjs === "undefined") {
        return;
      }
      var preservedScripts = preserveScripts(tplCM.getValue());
      docParts = splitDocument(preservedScripts.document);
      docParts.preservation = preservedScripts;
      var styleParts = splitPortalStyles(cssCM.getValue());
      var plugins = [];
      if (typeof window["grapesjs-preset-webpage"] !== "undefined") {
        var webpagePreset = window["grapesjs-preset-webpage"];
        var webpagePlugin = typeof webpagePreset === "function" ?
          webpagePreset : webpagePreset.default;
        plugins.push(function (editor) {
          webpagePlugin(editor, { useCustomTheme: false });
        });
      }
      grapes = grapesjs.init({
        container: canvas,
        height: "480px",
        fromElement: false,
        components: docParts.body,
        style: styleParts.visual,
        protectedCss: "",
        canvas: { frameStyle: styleParts.frame },
        cssIcons: "/admin/assets/vendor/font-awesome.min.css?v=4.7.0",
        storageManager: false,
        plugins: plugins
      });
      visual = true;
      root.classList.add("designer-visual");
      canvas.setAttribute("aria-hidden", "false");
      toggle.textContent = "Code editor";
      grapes.refresh();
    }

    function leaveVisual() {
      syncFromVisual();
      visual = false;
      root.classList.remove("designer-visual");
      canvas.setAttribute("aria-hidden", "true");
      toggle.textContent = "Visual editor";
      grapes.destroy();
      grapes = null;
      docParts = null;
      canvas.textContent = "";
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
