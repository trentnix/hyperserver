document.addEventListener("DOMContentLoaded", async function () {
  function assertPage(page, path = page) {
    if (location.pathname !== "/test/history/" + path || document.querySelector("#history-page")?.dataset.page !== page) {
      throw new Error("URL and displayed page disagree: " + page);
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
    if (htmx.version !== "2.0.11" || htmx.config.historyCacheSize !== 0) {
      throw new Error("Unexpected HTMX version or history policy");
    }
    assertPage("a");
    const initialLength = history.length;
    const header = document.querySelector("header");
    await navigate("htmx:afterSettle", () => document.querySelector("#push-page").click());
    assertPage("b");
    if (history.length !== initialLength + 1 || document.querySelector("header") !== header) {
      throw new Error("Push did not add one entry and preserve the page shell");
    }
    await navigate("htmx:afterSettle", () => document.querySelector("#replace-page").click());
    assertPage("c");
    if (history.length !== initialLength + 1 || document.querySelector("header") !== header) {
      throw new Error("Replace added an entry or replaced the page shell");
    }

    for (const [page, action] of [["a", () => history.back()], ["c", () => history.forward()]]) {
      const event = await navigate("htmx:historyRestore", action);
      if (!event.detail.cacheMiss || !event.detail.serverResponse.toLowerCase().includes("<!doctype html>")) {
        throw new Error("History did not restore a full page from the server");
      }
      assertPage(page);
    }

    await navigate("htmx:afterSettle", () => document.querySelector("#protected-page").click());
    assertPage("d");
    await navigate("htmx:historyRestore", () => history.back());
    assertPage("c");
    await navigate("htmx:historyRestore", () => history.forward());
    // The history loader follows the redirect, but retains the requested history URL.
    assertPage("login", "d");
    await fetch("/test/history-result", { method: "POST", body: "PASS" });
  } catch (error) {
    await fetch("/test/history-result", { method: "POST", body: String(error) });
  }
});
