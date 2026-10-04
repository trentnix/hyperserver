document.addEventListener("htmx:beforeSwap", function (event) {
  const xhr = event.detail.xhr;
  if (xhr.status < 400 || xhr.status > 599) {
    return;
  }

  // Render error fragments, but never interpret a plain-text error as HTML.
  const contentType = (xhr.getResponseHeader("Content-Type") || "").split(";")[0].trim().toLowerCase();
  event.detail.shouldSwap = contentType === "text/html";
});
