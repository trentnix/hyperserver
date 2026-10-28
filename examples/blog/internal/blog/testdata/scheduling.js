// Exercise scheduling through the real HTMX editor, including local recovery.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const edit = async path => {
    await htmx.ajax('GET', path, '#dialog-host');
    await wait(() => document.querySelector('dialog[open] .CodeMirror'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    return document.querySelector('#post-form');
  };
  const choose = (form, value) => {
    form.elements.published.value = value;
    form.elements.published.dispatchEvent(new Event('change', {bubbles: true}));
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('schedule-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('schedule-stage', 'create');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'create') {
      const form = await edit('/post/create');
      const field = form.querySelector('[data-publication-time]');
      assert(field.hidden, 'A new draft must not show a scheduling field.');
      assert(form.elements.publishDate.disabled && form.elements.publishTime.disabled, 'Unused scheduling fields must not block a draft save.');
      choose(form, 'scheduled');
      assert(form.elements.published.selectedOptions[0].textContent === 'Schedule', 'The scheduling choice must be labeled Schedule.');
      assert(!field.hidden && field.textContent.includes('UTC'), 'Scheduling must expose its time zone.');
      assert(form.elements.publishDate.type === 'date' && form.elements.publishTime.type === 'text', 'Scheduling should offer a date picker and a single editable time.');
      const timeChoices = form.querySelector('[data-time-choice]');
      const timeMenu = timeChoices.closest('details');
      const timeToggle = timeMenu.querySelector('summary');
      const dialog = form.closest('dialog');
      const frame = () => new Promise(resolve => requestAnimationFrame(resolve));
      assert(!timeMenu.hidden && !timeMenu.open, 'Scheduling must expose a closed time menu.');
      const closedScrollHeight = dialog.scrollHeight;
      timeToggle.click();
      await new Promise(resolve => setTimeout(resolve, 100));
      assert(timeMenu.open && timeChoices.getClientRects().length > 0, 'The time list must stay open after a completed click without holding the mouse.');
      assert(dialog.scrollHeight === closedScrollHeight, 'Opening the time menu must not extend the dialog scroll area.');
      const suggestions = [...timeChoices.options].slice(1).map(option => option.value);
      assert(suggestions.length === 48 && new Set(suggestions).size === 48 && suggestions[0] === '12:00 AM' && suggestions[24] === '12:00 PM' && suggestions[47] === '11:30 PM', 'Time suggestions must cover the full day in half-hour steps.');
      // Keep the time field visible and scroll within the list, as a user would
      // when picking a late time. Track transient movement, not just its final position.
      form.elements.publishTime.scrollIntoView({block: 'center'});
      timeChoices.focus({preventScroll: true});
      timeChoices.scrollTop = timeChoices.scrollHeight;
      await frame();
      const scrollTop = dialog.scrollTop;
      const pageTop = window.scrollY;
      let jumped = false;
      const recordScroll = () => { jumped ||= Math.abs(dialog.scrollTop - scrollTop) > 1 || Math.abs(window.scrollY - pageTop) > 1; };
      dialog.addEventListener('scroll', recordScroll);
      window.addEventListener('scroll', recordScroll);
      const focus = timeToggle.focus;
      let preventedScroll = false;
      timeToggle.focus = function(options) {
        preventedScroll = options?.preventScroll === true;
        focus.call(this, options);
      };
      timeChoices.value = suggestions[19];
      timeChoices.dispatchEvent(new Event('change', {bubbles: true}));
      await frame();
      await frame();
      recordScroll();
      dialog.removeEventListener('scroll', recordScroll);
      window.removeEventListener('scroll', recordScroll);
      delete timeToggle.focus;
      assert(preventedScroll && !jumped, 'Selecting a time must return focus without scrolling the dialog or page: ' + JSON.stringify({preventedScroll, scrollTop, after: dialog.scrollTop, pageTop, pageAfter: window.scrollY, maximum: dialog.scrollHeight - dialog.clientHeight}));
      assert(form.elements.publishTime.value === '9:30 AM' && form.elements.publishTime.checkValidity() && !timeMenu.open, 'Selecting a time must update the field and close the menu.');
      form.elements.publishTime.value = '14:17';
      form.elements.publishTime.dispatchEvent(new Event('input', {bubbles: true}));
      assert(form.elements.publishTime.checkValidity(), 'Typed times must not be restricted to the suggestions.');
      timeToggle.click();
      assert(timeMenu.open && timeChoices.options.length === 49 && [...timeChoices.options].slice(1).every(option => !option.hidden && !option.disabled), 'Typing must leave the full time list available.');
      timeChoices.focus();
      const escape = new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true});
      timeChoices.dispatchEvent(escape);
      assert(!timeMenu.open && escape.defaultPrevented && document.activeElement === timeToggle && form.closest('dialog').open, 'Escape must close only the time menu and return focus to its toggle.');
      timeToggle.click();
      form.elements.publishDate.click();
      assert(!timeMenu.open, 'Clicking outside the time menu must dismiss it.');
      timeToggle.click();
      form.elements.publishTime.focus();
      assert(!timeMenu.open, 'Moving keyboard focus out of the time menu must dismiss it.');
      timeToggle.click();
      timeChoices.value = '12:00 AM';
      timeChoices.dispatchEvent(new Event('change', {bubbles: true}));
      assert(form.elements.publishTime.value === '12:00 AM' && timeChoices.value === '' && document.activeElement === timeToggle, 'Selecting after typing must update the time and return focus to the toggle without opening the mobile keyboard.');
      await wait(() => form.querySelector('[data-draft-status]').textContent === 'Saved locally. Use Save to apply changes.');
      // Larger mobile text makes the saved message wrap while "Unsaved changes."
      // still fits on one line. The footer must not shrink between those states.
      if (innerWidth < 768) form.querySelector('[data-draft-status]').style.fontSize = '18px';
      dialog.scrollTop = dialog.scrollHeight;
      await frame();
      const bottomScroll = dialog.scrollTop;
      const bottomHeight = dialog.scrollHeight;
      const bottomPositions = [];
      const recordBottom = () => bottomPositions.push({top: dialog.scrollTop, height: dialog.scrollHeight, menuTop: timeChoices.getBoundingClientRect().top, dialogTop: dialog.getBoundingClientRect().top});
      dialog.addEventListener('scroll', recordBottom);
      timeToggle.click();
      timeChoices.focus({preventScroll: true});
      timeChoices.scrollTop = timeChoices.scrollHeight;
      await frame();
      recordBottom();
      const listBounds = timeChoices.getBoundingClientRect();
      const dialogBounds = dialog.getBoundingClientRect();
      assert(listBounds.top >= dialogBounds.top && listBounds.bottom <= dialogBounds.bottom, 'The time list must fit inside the visible dialog so native option clicks cannot scroll it.');
      // External browser drivers can supply a real pointer click as well.
      if (window.testPickTime) await window.testPickTime('6:30 AM');
      else {
        timeChoices.value = '6:30 AM';
        timeChoices.dispatchEvent(new Event('change', {bubbles: true}));
      }
      recordBottom();
      await frame();
      recordBottom();
      await wait(() => form.querySelector('[data-draft-status]').textContent === 'Saved locally. Use Save to apply changes.');
      await frame();
      recordBottom();
      dialog.removeEventListener('scroll', recordBottom);
      assert(bottomPositions.every(position => Math.abs(position.top - bottomScroll) <= 1), 'Selection at the bottom must preserve scroll position through recovery status updates: ' + JSON.stringify({bottomScroll, bottomHeight, bottomPositions}));
      assert(bottomPositions.every(position => position.height === bottomHeight), 'Updating recovery status must not resize the dialog scroll area: ' + JSON.stringify({bottomHeight, bottomPositions}));
      form.elements.publishTime.value = '';
      assert(form.elements.publishDate.validity.valueMissing && form.elements.publishTime.validity.valueMissing, 'Scheduling must require the complete date and time.');
      const dateBox = form.elements.publishDate.getBoundingClientRect();
      const timeBox = form.elements.publishTime.getBoundingClientRect();
      const scheduleBox = field.getBoundingClientRect();
      const choiceBox = timeToggle.getBoundingClientRect();
      const timeField = form.querySelector('.post-time-field');
      const fieldBox = timeField.getBoundingClientRect();
      assert(timeToggle.getAttribute('aria-label') === 'Choose a time in UTC', 'The time menu must have an accessible toggle.');
      assert(choiceBox.width === 44 && Math.abs(choiceBox.left - timeBox.right) < 1 && choiceBox.height === timeBox.height, 'The arrow must form a compact, aligned end to the time field.');
      assert(getComputedStyle(timeField).outlineStyle === 'solid' && getComputedStyle(form.elements.publishTime).outlineStyle === 'none', 'Focus must outline the whole time field, not just its text input.');
      assert(choiceBox.right <= scheduleBox.right + 1 && timeBox.width >= 90, 'The editable time and menu must fit at narrow widths.');
      assert(dateBox.left >= scheduleBox.left && timeBox.right <= scheduleBox.right + 1, 'Date/time controls overflow their section.');
      assert(matchMedia('(max-width: 480px)').matches ? fieldBox.top > dateBox.bottom : Math.abs(fieldBox.top - dateBox.top) < 1, 'Date/time controls must stack on mobile and align on desktop.');
      const date = new Date(Date.now() + 86400000).toISOString().slice(0, 16);
      sessionStorage.setItem('schedule-date', date);
      form.elements.title.value = 'Scheduled browser post';
      form.elements.publishDate.value = date.slice(0, 10);
      const hour = Number(date.slice(11, 13));
      const time = `${hour % 12 || 12}:${date.slice(14, 16)} ${hour < 12 ? 'AM' : 'PM'}`;
      sessionStorage.setItem('schedule-time', time);
      form.elements.publishTime.value = time;
      form.querySelector('.CodeMirror').CodeMirror.setValue('Not public yet.');
      form.elements.publishTime.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => form.querySelector('[data-draft-status]').textContent === 'Saved locally. Use Save to apply changes.');
      const prefix = 'hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':/post:';
      assert(Object.keys(localStorage).some(key => {
        if (!key.startsWith(prefix)) return false;
        const fields = JSON.parse(localStorage.getItem(key)).fields;
        return fields.published === 'scheduled' && fields.publishDate === date.slice(0, 10) && fields.publishTime === time;
      }), 'Local recovery must preserve the complete schedule.');
      sessionStorage.setItem('schedule-stage', 'scheduled');
      form.requestSubmit();
      return;
    }
    if (stage === 'scheduled') {
      assert(document.querySelector('.post-heading').textContent.includes('Scheduled'), 'Scheduled preview needs its publication state.');
      assert((await fetch(location.pathname, {credentials: 'omit'})).status === 404, 'Scheduled post is public too early.');
      const form = await edit(document.querySelector('a[href^="/post/update/"]').getAttribute('href'));
      assert(form.elements.published.value === 'scheduled' && form.elements.publishDate.value === sessionStorage.getItem('schedule-date').slice(0, 10) && form.elements.publishTime.value === sessionStorage.getItem('schedule-time'), 'Reopening changed the saved UTC schedule.');
      assert(!form.querySelector('[data-publication-time]').hidden, 'Reopening hid the schedule.');

      // An editor can stay open while publication happens. Only the warning's
      // browser clock changes here. Stop submission before any server request.
      const originalSlug = form.elements.slug.value;
      const now = Date.now;
      const confirm = window.blogConfirm;
      let warnings = 0;
      window.blogConfirm = () => { warnings++; };
      const stopSave = event => { event.preventDefault(); event.stopImmediatePropagation(); };
      form.addEventListener('submit', stopSave, true);
      try {
        const publication = Date.parse(sessionStorage.getItem('schedule-date') + 'Z');
        form.elements.slug.value = 'changed-scheduled-url';
        for (const [instant, expected] of [[publication - 1, 0], [publication, 1]]) {
          Date.now = () => instant;
          form.requestSubmit();
          assert(warnings === expected, 'An open scheduled editor must warn about URL changes only after publication.');
        }
      } finally {
        Date.now = now;
        window.blogConfirm = confirm;
        form.removeEventListener('submit', stopSave, true);
        form.elements.slug.value = originalSlug;
      }
      choose(form, '');
      assert(form.querySelector('[data-publication-time]').hidden, 'Canceling should hide the date field.');
      assert(form.elements.publishDate.disabled && form.elements.publishTime.disabled, 'Canceling must remove date/time constraints from submission.');
      assert(!form.elements.publishTime.required, 'Hidden time must not block saving a draft.');
      assert(!form.querySelector('.post-time-menu').open, 'Draft mode must close the time menu.');
      sessionStorage.setItem('schedule-stage', 'draft');
      form.requestSubmit();
      return;
    }
    if (stage === 'draft') {
      assert(document.querySelector('.post-heading').textContent.includes('Draft preview'), 'Canceling must return to a private draft.');
      const form = await edit(document.querySelector('a[href^="/post/update/"]').getAttribute('href'));
      assert(form.elements.publishDate.value === '' && form.elements.publishTime.value === '' && form.elements.published.querySelector('[value="scheduled"]'), 'Canceled draft must allow a fresh schedule.');
      choose(form, 'on');
      sessionStorage.setItem('schedule-stage', 'published');
      form.requestSubmit();
      return;
    }
    if (stage === 'published') {
      assert((await fetch(location.pathname, {credentials: 'omit'})).ok, 'Publish now must make the post public.');
      const path = document.querySelector('a[href^="/post/update/"]').getAttribute('href');
      let form = await edit(path);
      assert(!form.elements.published.querySelector('[value="scheduled"]') && !form.elements.publishDate && !form.elements.publishTime, 'Previously published posts must not offer scheduling.');

      // A copy saved before publication must not turn an unavailable Schedule
      // option into an empty selection, which the server would treat as Draft.
      const fields = Object.fromEntries(new FormData(form));
      delete fields.version;
      const key = 'hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':' + new URL(form.action).pathname + ':scheduled-copy';
      localStorage.setItem(key, JSON.stringify({saved: Date.now(), baseline: JSON.stringify(fields), fields: {
        ...fields, content: 'Recovered scheduled writing.', published: 'scheduled', publishAt: sessionStorage.getItem('schedule-date')
      }}));
      form.querySelector('[data-close-dialog]').click();
      form = await edit(path);
      assert(!form.querySelector('[data-draft-recovery]').hidden, 'The old scheduled copy must remain recoverable.');
      assert(form.querySelector('[data-draft-description]').textContent.includes('Your current publication setting will be kept.'), 'Recovery must explain why it cannot restore the schedule.');
      form.querySelector('[data-draft-restore]').click();
      assert(form.elements.published.value === 'on', 'Restoring an unavailable schedule changed the current publication setting.');
      assert(form.querySelector('.CodeMirror').CodeMirror.getValue() === 'Recovered scheduled writing.', 'Keeping publication must not discard recovered content.');
      sessionStorage.setItem('schedule-stage', 'recovered');
      form.requestSubmit();
      return;
    }
    if (stage === 'recovered') {
      const response = await fetch(location.pathname, {credentials: 'omit'});
      assert(response.ok && (await response.text()).includes('Recovered scheduled writing.'), 'Saving recovered content must keep the already-published post public.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
