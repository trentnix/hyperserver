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

  async function request(path, status, source = button) {
    source.setAttribute("hx-get", "/test/browser/" + path);
    htmx.process(source);
    const response = new Promise((resolve) => {
      document.body.addEventListener("htmx:afterRequest", resolve, { once: true });
    });
    source.click();
    const event = await response;
    check(event.detail.xhr.status === status, `${path}: wrong status`);
    if (status !== 0) check(event.detail.failed === (status >= 400), `${path}: wrong error classification`);
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
    const fallback = document.querySelector('[data-request-errors]');
    check(fallback.querySelector('[data-request-error]')?.textContent.includes('<img'), "plain-text error was not displayed safely");

    // The request's form owns its error even when it swaps into a different form.
    const errorForm = document.createElement('form');
    errorForm.innerHTML = '<input value="Unsaved writing">';
    const input = errorForm.firstElementChild;
    button.type = 'button';
    errorForm.append(button);
    target.before(errorForm);
    target.innerHTML = '<form id="unrelated-form"><input value="Unrelated"></form>';
    for (const [path, status, message] of [['plain', 500, '<img'], ['json', 503, 'HTTP 503'], ['empty', 502, 'HTTP 502']]) {
      await request(path, status);
      const notice = errorForm.querySelector('[data-request-error]');
      check(notice?.textContent.includes(message) && notice.getAttribute('role') === 'alert', `${path}: missing accessible message`);
      check(!notice.querySelector('img') && !notice.textContent.includes('private detail'), `${path}: unsafe error content`);
      check(errorForm.querySelectorAll('[data-request-error]').length === 1, `${path}: duplicated alerts`);
      check(input === errorForm.querySelector('input') && input.value === 'Unsaved writing', `${path}: lost input`);
      check(!target.querySelector('[data-request-error]'), `${path}: error placed in unrelated form`);
    }

    // A successful retry can update a fragment or return 204 without replacing the form.
    for (const [path, status] of [['success', 200], ['no-content', 204]]) {
      await request('json', 503);
      check(errorForm.querySelector('[data-request-error]'), 'missing error before retry');
      await request(path, status);
      check(!errorForm.querySelector('[data-request-error]'), `${path}: retry left a stale error`);
      check(input === errorForm.querySelector('input') && input.value === 'Unsaved writing', `${path}: retry lost input`);
    }

    // Independent controls may share an error container. Retrying one must not
    // clear the other's failure or overwrite it when the retry also fails.
    const other = button.cloneNode(true);
    other.removeAttribute('id');
    errorForm.append(other);
    await request('plain', 500);
    await request('json', 503, other);
    check(errorForm.querySelectorAll('[data-request-error]').length === 2, 'unrelated failure replaced an error');
    await request('no-content', 204, other);
    check(errorForm.querySelectorAll('[data-request-error]').length === 1 &&
      errorForm.querySelector('[data-request-error]').textContent.includes('<img'), 'retry cleared an unrelated error');
    other.remove();

    const retained = errorForm.querySelector('[data-request-error]');
    button.addEventListener('htmx:beforeRequest', event => event.preventDefault(), {once: true});
    button.click();
    check(retained.isConnected, 'canceled request cleared an error');

    // htmx.ajax defaults its source to body. Different targets must stay independent.
    const first = document.createElement('div');
    const second = document.createElement('div');
    document.body.append(first, second);
    await htmx.ajax('GET', '/test/browser/json', {target: first});
    await htmx.ajax('GET', '/test/browser/empty', {target: second});
    await htmx.ajax('GET', '/test/browser/no-content', {target: second});
    const alerts = [...fallback.querySelectorAll('[data-request-error]')];
    check(alerts.some(alert => alert.textContent.includes('HTTP 503')) &&
      !alerts.some(alert => alert.textContent.includes('HTTP 502')), 'programmatic retry cleared the wrong error');
    await htmx.ajax('GET', '/test/browser/no-content', {target: first});

    // Response retargeting changes placement, not which request owns the alert.
    const originalForm = document.createElement('form');
    first.replaceWith(originalForm);
    originalForm.append(first);
    target.innerHTML = '<form id="retarget-errors"></form>';
    const alertCount = document.querySelectorAll('[data-request-error]').length;
    await htmx.ajax('GET', '/test/browser/retarget-error', {target: first});
    check(document.querySelectorAll('[data-request-error]').length === alertCount + 1, 'retargeted error was not displayed');
    check(target.querySelector('[data-request-error]')?.textContent.includes('Please retry') &&
      !originalForm.querySelector('[data-request-error]'), 'retargeted error appeared in the original target');
    await htmx.ajax('GET', '/test/browser/no-content', {target: second});
    check(document.querySelectorAll('[data-request-error]').length === alertCount + 1, 'unrelated retry cleared a retargeted error');
    await htmx.ajax('GET', '/test/browser/no-content', {target: first});
    check(document.querySelectorAll('[data-request-error]').length === alertCount, 'retry left a retargeted error behind');
    originalForm.remove();
    second.remove();

    // Each network failure starts clean and must emit its own expected event.
    for (const [path, name] of [['disconnect', 'htmx:sendError'], ['timeout', 'htmx:timeout']]) {
      errorForm.querySelector('[data-request-error]')?.remove();
      button.setAttribute('hx-request', JSON.stringify({timeout: path === 'timeout' ? 100 : 0}));
      let observed = false;
      const observe = event => { if (event.detail.elt === button) observed = true; };
      document.addEventListener(name, observe);
      await request(path, 0);
      document.removeEventListener(name, observe);
      check(observed, `${path}: expected ${name}`);
      check(errorForm.querySelector('[data-request-error]')?.textContent.includes('check whether it went through'), `${path}: missing uncertain-outcome warning`);
      check(input.value === 'Unsaved writing', `${path}: lost input`);
    }
    button.removeAttribute('hx-request');

    // A modal-local failure must remain visible above the modal backdrop.
    const dialog = document.createElement('dialog');
    document.body.append(dialog);
    dialog.append(button);
    dialog.showModal();
    await request('json', 503);
    check(dialog.querySelector('[data-request-error]')?.textContent.includes('HTTP 503'), 'modal error escaped the dialog');
    dialog.close();
    document.body.append(button);
    dialog.remove();
    errorForm.remove();

    // No designated notification area is required. Preserve unrelated content.
    target.textContent = 'Original content';
    fallback.removeAttribute('data-request-errors');
    await request('empty', 502);
    check(document.body.querySelector(':scope > [data-request-error]')?.textContent.includes('HTTP 502'), 'missing body fallback');
    document.body.querySelector(':scope > [data-request-error]').remove();
    fallback.setAttribute('data-request-errors', '');

    await request('no-content', 204);
    check(target.textContent === 'Original content', '204 changed the target');

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
