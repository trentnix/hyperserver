const policyViolations = [];
document.addEventListener("securitypolicyviolation", function (event) {
  policyViolations.push(event.violatedDirective + ": " + event.blockedURI);
});

const consoleMessages = [];
const originalDebug = console.debug;
console.debug = function (message) {
  consoleMessages.push(message);
  originalDebug.call(console, message);
};

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
    const ready = await fetch("/test/browser/ready", { method: "POST" });
    check(ready.ok, "Could not report browser readiness");
    check(typeof htmx !== "undefined", "HTMX did not load");
    check(!htmx.config.allowEval && !htmx.config.allowScriptTags && !htmx.config.includeIndicatorStyles && htmx.config.historyCacheSize === 0, "HTMX security settings are missing");
    check(consoleMessages.includes("</script><script>window.systemMessageInjected=true</script>"), "console message was not logged as text");
    check(!window.systemMessageInjected, "console message executed as JavaScript");
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
    check(!target.querySelector("script") && !window.fragmentScriptExecuted, "HTMX accepted a fragment script");

    const form = document.getElementById("htmx-form");
    const submitted = new Promise((resolve) => {
      form.addEventListener("htmx:afterRequest", resolve, { once: true });
    });
    form.requestSubmit();
    const submission = await submitted;
    check(submission.detail.xhr.status === 200 && !submission.detail.failed, "HTMX form POST failed");
    check(document.querySelector("#form-target #form-success")?.textContent === "Form saved", "HTMX form response did not swap");
    check(policyViolations.length === 0, "unexpected CSP violations: " + policyViolations.join(", "));

    // Prove the browser enforces the response policy, not just HTMX's settings.
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("inline script was not blocked by CSP")), 2000);
      function blocked(event) {
        if (event.blockedURI === "inline" && event.effectiveDirective.startsWith("script-src")) {
          clearTimeout(timeout);
          document.removeEventListener("securitypolicyviolation", blocked);
          resolve();
        }
      }
      document.addEventListener("securitypolicyviolation", blocked);
      const script = document.createElement("script");
      script.textContent = "window.inlineScriptExecuted=true";
      document.head.appendChild(script);
      script.remove();
    });
    check(!window.inlineScriptExecuted, "CSP allowed inline JavaScript");

    // Submit through the browser's native form machinery, not fetch or HTMX.
    // The receiving handler validates the POST and reports the final result.
    console.debug = originalDebug;
    document.getElementById("native-form").requestSubmit();
    return;
  } catch (error) {
    report = error.message;
  }

  console.debug = originalDebug;

  await fetch("/test/browser/result", { method: "POST", body: report });
});
