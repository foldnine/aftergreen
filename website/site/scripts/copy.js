// Adds a "Copy" button to every <pre data-copy>. Without JavaScript the code
// stays visible and selectable; the button only exists when it can work.
(function () {
  "use strict";

  var live = document.createElement("p");
  live.className = "visually-hidden";
  live.setAttribute("role", "status");
  document.body.appendChild(live);

  function fallbackCopy(text) {
    var area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    var ok = false;
    try { ok = document.execCommand("copy"); } catch (e) { ok = false; }
    document.body.removeChild(area);
    return ok ? Promise.resolve() : Promise.reject(new Error("copy failed"));
  }

  function copy(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text).catch(function () { return fallbackCopy(text); });
    }
    return fallbackCopy(text);
  }

  document.querySelectorAll("pre[data-copy]").forEach(function (pre) {
    var title = pre.parentElement && pre.parentElement.querySelector(".code__title");
    var label = title ? title.textContent.trim() : "code";
    var button = document.createElement("button");
    var timer;

    button.type = "button";
    button.className = "copy";
    button.textContent = "Copy";
    button.setAttribute("aria-label", "Copy: " + label);

    button.addEventListener("click", function () {
      copy(pre.textContent).then(function () {
        button.textContent = "Copied";
        button.dataset.state = "done";
        live.textContent = "Copied to clipboard.";
      }, function () {
        button.textContent = "Select";
        live.textContent = "Copy failed. Select the text and copy it manually.";
        var range = document.createRange();
        range.selectNodeContents(pre);
        var sel = window.getSelection();
        sel.removeAllRanges();
        sel.addRange(range);
      });
      clearTimeout(timer);
      timer = setTimeout(function () {
        button.textContent = "Copy";
        delete button.dataset.state;
        live.textContent = "";
      }, 2000);
    });

    (title || pre.parentElement).appendChild(button);
  });
})();
