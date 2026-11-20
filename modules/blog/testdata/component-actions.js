async function waitForComponent(condition, description) {
  const deadline = Date.now() + 12000;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error('Timed out waiting for ' + description);
    await new Promise(resolve => setTimeout(resolve, 20));
  }
}

function closeComponentSettings(form) {
  form.querySelector('.component-dialog[open] [data-component-done]')?.click();
}

function componentEditor(form, type) {
  return form.querySelector('[data-component-id="' + type + '"], [data-post-component="' + type + '"]:not([data-component-id])');
}

function componentRowIn(form, type) {
  return form.querySelector('[data-component-row="' + type + '"]:not([hidden])');
}

async function addTestComponent(button) {
  const form = button.form;
  closeComponentSettings(form);
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
  if (section.hasAttribute('data-component-pending')) closeComponentSettings(form);
  if (!section.matches('.component-dialog:modal .component-panel-active')) {
    closeComponentSettings(form);
    const type = section.dataset.componentId || section.dataset.postComponent;
    await waitForComponent(() => componentRowIn(form, type), 'component row');
    componentRowIn(form, type).querySelector('[data-component-edit]').click();
  }
  button.click();
  await waitForComponent(() => section.hidden, 'component removal');
}

async function undoTestComponent(button) {
  const form = button.form;
  closeComponentSettings(form);
  button.click();
  await waitForComponent(() => componentEditor(form, button.dataset.componentUndo).matches('.component-dialog:modal .component-panel-active'), 'restored component settings');
}

async function moveTestComponent(section, direction) {
  const form = section.closest('form');
  closeComponentSettings(form);
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
