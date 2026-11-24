async function waitForComponent(condition, description) {
  const deadline = Date.now() + 12000;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error('Timed out waiting for ' + description);
    await new Promise(resolve => setTimeout(resolve, 20));
  }
}

async function closeComponentSettings(form) {
  const done = form.querySelector('.component-dialog[open] [data-component-done]');
  if (!done) return;
  done.click();
  await waitForComponent(() => !done.hasAttribute('aria-busy'), 'component settings validation');
}

function componentEditor(form, type) {
  return form.querySelector('[data-component-id="' + type + '"], [data-post-component="' + type + '"]:not([data-component-id])');
}

function componentRowIn(form, type) {
  return form.querySelector('[data-component-row="' + type + '"]:not([hidden])');
}

function setTestComponentSource(section, value) {
  const source = section.querySelector('textarea[name]');
  source.value = typeof value === 'string' ? value : JSON.stringify(value);
  source.dispatchEvent(new Event('change', {bubbles: true}));
}

async function addTestComponent(button) {
  const form = button.form;
  await closeComponentSettings(form);
  const picker = button.closest('details');
  if (!picker.open) picker.querySelector('summary').click();
  button.click();
  const panel = () => form.querySelector('.component-dialog:modal .component-panel-active[data-post-component="' + button.dataset.componentAdd + '"]');
  await waitForComponent(panel, 'added component settings');
  return panel();
}

async function removeTestComponent(button) {
  const form = button.form;
  const section = button.closest('[data-post-component]');
  if (section.hasAttribute('data-component-pending')) {
    await closeComponentSettings(form);
    if (section.hasAttribute('data-component-pending')) {
      section.closest('dialog').querySelector('[data-component-cancel]').click();
      return;
    }
  }
  if (!section.matches('.component-dialog:modal .component-panel-active')) {
    await closeComponentSettings(form);
    const type = section.dataset.componentId || section.dataset.postComponent;
    await waitForComponent(() => componentRowIn(form, type), 'component row');
    componentRowIn(form, type).querySelector('[data-component-edit]').click();
  }
  button.click();
  await waitForComponent(() => section.hidden, 'component removal');
}

async function undoTestComponent(button) {
  const form = button.form;
  await closeComponentSettings(form);
  button.click();
  await waitForComponent(() => componentEditor(form, button.dataset.componentUndo).matches('.component-dialog:modal .component-panel-active'), 'restored component settings');
}

async function moveTestComponent(section, direction) {
  const form = section.closest('form');
  await closeComponentSettings(form);
  const type = section.dataset.componentId || section.dataset.postComponent;
  const row = () => componentRowIn(form, type);
  await waitForComponent(row, 'component row');
  if (row().querySelector('.component-move-steps').hidden) row().querySelector('[data-component-place]').click();
  const step = () => row()?.querySelector('[data-component-shift="' + direction + '"]:not(:disabled)');
  await waitForComponent(step, 'move ' + direction);
  const before = form.elements.content.value;
  const order = form.elements.componentOrder.value;
  step().click();
  await waitForComponent(() => row() && (form.elements.content.value !== before || form.elements.componentOrder.value !== order), 'component movement');
  await waitForComponent(() => row()?.querySelector('[data-component-shift]:focus, [data-component-place]:focus'), 'focus after movement');
}
// Canvas resolves computed CSS colors such as color-mix() to sRGB bytes.
function testColorContrast(first, second) {
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = 1;
  const context = canvas.getContext('2d');
  function luminance(color) {
    context.clearRect(0, 0, 1, 1);
    context.fillStyle = color;
    context.fillRect(0, 0, 1, 1);
    const [red, green, blue] = context.getImageData(0, 0, 1, 1).data;
    return [red, green, blue].reduce((sum, value, index) => {
      const channel = value / 255;
      return sum + [.2126, .7152, .0722][index] *
        (channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4);
    }, 0);
  }
  const a = luminance(first) + .05;
  const b = luminance(second) + .05;
  return Math.max(a, b) / Math.min(a, b);
}
