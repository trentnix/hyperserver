document.addEventListener("DOMContentLoaded", async function () {
  function check(condition, message) {
    if (!condition) throw new Error(message);
  }

  async function report(message) {
    await fetch("/test/responses/result", { method: "POST", body: message });
  }

  try {
    if (location.pathname === "/test/responses/destination") {
      check(location.search === "?from=post", "redirect lost its query string");
      check(document.getElementById("destination")?.textContent === "Arrived", "destination page did not load");
      check(!window.responseDispatchStarted && !document.getElementById("original-target"), "redirect swapped content instead of navigating");
      await report("PASS");
      return;
    }

    window.responseDispatchStarted = true;
    const ready = await fetch("/test/responses/ready", { method: "POST" });
    check(ready.ok, "Could not report browser readiness");
    check(typeof htmx !== "undefined", "HTMX did not load");
    const form = document.getElementById("update-form");
    const originalTarget = document.getElementById("original-target");
    const untouched = document.getElementById("untouched");

    // Cover the default validation status and the site's configured HTML error swaps.
    for (const [value, invalid, status] of [["person@example.invalid", false, 200], ["<img id=injected src=x>", true, 200], ["<img id=injected src=x>", true, 422]]) {
      form.elements.email.value = value;
      form.elements.status.value = status;
      const oldEditor = document.getElementById("response-editor");
      const oldNotice = document.getElementById("response-notice");
      // afterSettle observes the completed swap, not just a received response.
      const settled = new Promise((resolve) => {
        document.body.addEventListener("htmx:afterSettle", resolve, { once: true });
      });
      form.requestSubmit();
      const event = await settled;
      check(event.detail.xhr.status === status, "update returned the wrong status");
      check(event.detail.xhr.getResponseHeader("HX-Retarget") === "#response-editor", "missing retarget instruction");
      check(event.detail.xhr.getResponseHeader("HX-Reswap") === "outerHTML settle:0", "missing reswap instruction");

      const editor = document.getElementById("response-editor");
      check(editor && editor !== oldEditor && !oldEditor.isConnected, "outerHTML did not replace the selected editor");
      check(document.querySelectorAll("#response-editor").length === 1, "reswap nested or duplicated the editor");
      check(editor.querySelector("#submitted-value")?.textContent === value, "submitted input was not preserved as text");
      check(!document.getElementById("injected"), "submitted input became markup");
      check(originalTarget === document.getElementById("original-target") && originalTarget.textContent === "Original target", "retarget changed the original target");
      check(untouched === document.getElementById("untouched") && untouched.textContent === "Unrelated content", "update changed unrelated content");

      const notice = document.getElementById("response-notice");
      check(notice && notice !== oldNotice && !oldNotice.isConnected, "out-of-band notification was not replaced");
      check(notice.textContent === (invalid ? "<invalid>" : "<saved>"), "notification was missing or not escaped");
      check(!editor.contains(notice) && document.querySelectorAll("#response-notice").length === 1, "notification appeared inside the main swap");
      const error = editor.querySelector(".field-error");
      check(invalid ? error?.textContent === "Enter a valid email address." : !error, "validation errors did not match the submission");
    }

    // Only the newly loaded destination document can report success.
    document.getElementById("redirect-form").requestSubmit();
  } catch (error) {
    await report(error.message);
  }
});
