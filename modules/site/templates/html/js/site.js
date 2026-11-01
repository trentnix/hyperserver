// Messages are escaped HTML text, never executable JavaScript.
document.addEventListener("DOMContentLoaded", function () {
  const levels = ["debug", "log", "info", "warn", "error"];
  document.querySelectorAll("[data-console-level]").forEach(function (element) {
    const level = levels.includes(element.dataset.consoleLevel) ? element.dataset.consoleLevel : "debug";
    console[level](element.textContent);
    element.remove();
  });
});
