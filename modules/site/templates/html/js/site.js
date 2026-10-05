document.addEventListener("htmx:beforeSwap", function (event) {
  const xhr = event.detail.xhr;
  if (xhr.status < 400 || xhr.status > 599) {
    return;
  }

  // Render error fragments, but never interpret a plain-text error as HTML.
  const contentType = (xhr.getResponseHeader("Content-Type") || "").split(";")[0].trim().toLowerCase();
  event.detail.shouldSwap = contentType === "text/html";
});

// Messages are escaped HTML text, never executable JavaScript.
document.addEventListener("DOMContentLoaded", function () {
  const levels = ["debug", "log", "info", "warn", "error"];
  document.querySelectorAll("[data-console-level]").forEach(function (element) {
    const level = levels.includes(element.dataset.consoleLevel) ? element.dataset.consoleLevel : "debug";
    console[level](element.textContent);
    element.remove();
  });
});
