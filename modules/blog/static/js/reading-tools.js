const copyIcon = '<svg width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="7" y="7" width="10" height="10" rx="1.5"/><path d="M13 4.5A1.5 1.5 0 0 0 11.5 3h-7A1.5 1.5 0 0 0 3 4.5v7A1.5 1.5 0 0 0 4.5 13"/></svg>';

function addCopyButton(element, kind, label) {
  const wrapper = document.createElement('div');
  wrapper.className = kind === 'heading' ? 'reading-heading' : 'reading-code-block';
  const button = document.createElement('button');
  button.type = 'button';
  button.dataset.readingCopy = kind;
  button.innerHTML = kind === 'heading' ? '#' : copyIcon;
  button.setAttribute('aria-label', label);
  button.title = label;
  element.replaceWith(wrapper);
  wrapper.append(element, button);
}

function initialize(root) {
  if (!navigator.clipboard?.writeText) return;
  for (const heading of root.querySelectorAll('[data-reading-url] > :is(h1, h2, h3, h4, h5, h6)[id]')) {
    const label = heading.textContent.trim();
    addCopyButton(heading, 'heading', label ? `Copy link to ${label}` : 'Copy heading link');
  }
  for (const code of root.querySelectorAll('[data-reading-url] pre > code')) {
    if (!code.parentElement.closest('.reading-code-block')) {
      addCopyButton(code.parentElement, 'code', 'Copy code');
    }
  }
}

let resetFeedback;
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-reading-copy]');
  if (!button) return;
  const heading = button.dataset.readingCopy === 'heading';
  const content = button.previousElementSibling;
  let text = content.textContent;
  if (heading) {
    const url = new URL(button.closest('[data-reading-url]').dataset.readingUrl, location.href);
    url.hash = content.id;
    text = url.href;
  }
  let message, feedback;
  try {
    await navigator.clipboard.writeText(text);
    message = heading ? 'Heading link copied.' : 'Code copied.';
    feedback = '✓';
  } catch {
    message = heading ? 'Could not copy the heading link.' : 'Could not copy the code.';
    feedback = '!';
  }
  if (!button.isConnected) return;
  resetFeedback?.();
  const icon = button.firstChild;
  const status = document.querySelector('.reading-copy-status');
  status.textContent = message;
  button.textContent = feedback;
  button.title = message;
  const timer = setTimeout(() => resetFeedback(), 2200);
  resetFeedback = () => {
    clearTimeout(timer);
    status.textContent = '';
    button.replaceChildren(icon);
    button.title = button.getAttribute('aria-label');
    resetFeedback = null;
  };
});

document.addEventListener('htmx:before:swap', () => resetFeedback?.());
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
