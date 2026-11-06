document.addEventListener("DOMContentLoaded", async function () {
  function assertPage(page, path = page) {
    if (location.pathname !== "/test/history/" + path || document.querySelector("#history-page")?.dataset.page !== page) {
      throw new Error(`URL and displayed page disagree: expected ${page} at ${path}, got ${document.querySelector('#history-page')?.dataset.page} at ${location.pathname}`);
    }
    for (const selector of ["header", "main", "footer", "#history-page"]) {
      if (document.querySelectorAll(selector).length !== 1) {
        throw new Error("Missing or duplicated page structure: " + selector);
      }
    }
    if (document.querySelector("main header, main footer") || localStorage.getItem("htmx-history-cache") || sessionStorage.getItem("htmx-history-cache")) {
      throw new Error("Page shell leaked into a fragment or history snapshot was stored");
    }
  }

  async function navigate(eventName, action) {
    const completed = new Promise(resolve => document.addEventListener(eventName, resolve, { once: true }));
    action();
    return await completed;
  }

  try {
    await fetch("/test/history-ready", { method: "POST" });
    if (htmx.version !== "4.0.1" || !htmx.config.history) {
      throw new Error("Unexpected HTMX version or history policy");
    }
    assertPage("a");
    const initialLength = history.length;
    const header = document.querySelector("header");
    await navigate("htmx:finally:request", () => document.querySelector("#push-page").click());
    assertPage("b");
    if (history.length !== initialLength + 1 || document.querySelector("header") !== header) {
      throw new Error("Push did not add one entry and preserve the page shell");
    }
    await navigate("htmx:finally:request", () => document.querySelector("#replace-page").click());
    assertPage("c");
    if (history.length !== initialLength + 1 || document.querySelector("header") !== header) {
      throw new Error("Replace added an entry or replaced the page shell");
    }

    for (const [page, action] of [["a", () => history.back()], ["c", () => history.forward()]]) {
      const event = await navigate("htmx:finally:request", action);
      if (!event.detail.ctx.request.headers['HX-History-Restore-Request'] || !event.detail.ctx.text.toLowerCase().includes("<!doctype html>")) {
        throw new Error("History did not restore a full page from the server");
      }
      assertPage(page);
    }

    await navigate("htmx:finally:request", () => document.querySelector("#protected-page").click());
    assertPage("d");
    await navigate("htmx:finally:request", () => history.back());
    assertPage("c");
    await navigate("htmx:finally:request", () => history.forward());
    // The history loader follows the redirect, but retains the requested history URL.
    assertPage("login", "d");
    // hx-select requests a full document even outside history restoration.
    await htmx.ajax('GET', '/test/history/selected', {target: 'main', select: '#history-page'});
    assertPage('selected', 'd');
    await fetch("/test/history-result", { method: "POST", body: "PASS" });
  } catch (error) {
    await fetch("/test/history-result", { method: "POST", body: String(error) });
  }
});
