document.addEventListener("DOMContentLoaded", async function () {
  const target = document.getElementById("browser-target");
  const button = document.getElementById("browser-request");
  let report = "PASS";

  function check(condition, message) {
    if (!condition) throw new Error(message);
  }

  async function request(path, status) {
    button.setAttribute("hx-get", "/test/browser/" + path);
    htmx.process(button);
    const response = new Promise((resolve) => {
      document.body.addEventListener("htmx:afterRequest", resolve, { once: true });
    });
    button.click();
    const event = await response;
    check(event.detail.xhr.status === status, `${path}: wrong status`);
    check(event.detail.failed === (status >= 400), `${path}: wrong error classification`);
    check(document.getElementById("untouched").textContent === "Outside the target", `${path}: changed the wrong target`);
    check(!document.getElementById("error-injection") && !window.errorInjected, `${path}: injected HTML`);
  }

  try {
    check(typeof htmx !== "undefined", "HTMX did not load");
    for (const [path, status] of [["bad-request", 400], ["forbidden", 403], ["unavailable", 503]]) {
      target.textContent = "Original content";
      await request(path, status);
      check(target.querySelector(".error-message")?.textContent.includes("Public error <img"), `${path}: missing escaped error fragment`);
    }

    target.textContent = "Original content";
    await request("plain", 500);
    check(target.textContent === "Original content", "plain-text error replaced the target");

    await request("success", 200);
    check(target.querySelector("#success")?.textContent === "Saved", "successful response did not swap");
  } catch (error) {
    report = error.message;
  }

  await fetch("/test/browser/result", { method: "POST", body: report });
});
