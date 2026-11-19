import {registerComponent, jsonComponentValue} from './post-components.js';
import {markField} from './forms.js';
import './media.js';

const initialized = new WeakSet();
const timedFields = ['startTime', 'endTime', 'timeZone'];
const optional = [...timedFields, 'endDate', 'location', 'mapURL', 'url', 'imageID', 'imageAlt'];
const imageID = /^[a-f0-9]{64}\.(png|jpeg|gif)$/;

function comparableEvent(source) {
  const comparable = jsonComponentValue(source, {strings: optional});
  try {
    const value = JSON.parse(comparable);
    if (value?.allDay === !value?.startTime) delete value.allDay;
    return JSON.stringify(value);
  } catch {
    return comparable;
  }
}

const zoneLabel = name => name.replaceAll('_', ' ').replaceAll('/', ' / ');
const zoneSearchText = name => name.toLowerCase().replace(/[\s_/]+/g, ' ').trim();

function setupZonePicker(trigger, zone, siteZone, select) {
  const field = trigger.closest('.event-zone');
  const panel = field.querySelector('.event-zone-picker');
  const search = panel.querySelector('input');
  const results = panel.querySelector('[data-event-zone-results]');
  const empty = panel.querySelector('[data-event-zone-empty]');
  const defaults = [...new Set([siteZone, 'UTC'])];
  let knownZones;

  function render() {
    const query = zoneSearchText(search.value);
    let matches = defaults.filter(name => name !== zone.value);
    if (query) {
      knownZones ??= [...new Set([...defaults, ...(Intl.supportedValuesOf?.('timeZone') ?? [])])];
      matches = [...new Set([zone.value, ...knownZones])].filter(name => zoneSearchText(name).includes(query));
    }
    // A browser's list may omit valid aliases. Accept a full IANA identifier too.
    const identifier = search.value.trim();
    if (!matches.length && identifier.includes('/')) {
      try {
        const zone = new Intl.DateTimeFormat('en', {timeZone: identifier}).resolvedOptions().timeZone;
        matches.push(zone);
      } catch {
        // Show the empty state for unrecognized identifiers.
      }
    }
    results.replaceChildren(...matches.map(name => {
      const item = document.createElement('li');
      const button = document.createElement('button');
      button.type = 'button';
      button.value = name;
      button.textContent = zoneLabel(name);
      if (name === zone.value) button.setAttribute('aria-current', 'true');
      item.append(button);
      return item;
    }));
    results.scrollTop = 0;
    results.hidden = matches.length === 0;
    empty.hidden = !query || matches.length !== 0;
  }

  function close(focus = false) {
    if (panel.hidden) return;
    panel.hidden = true;
    trigger.setAttribute('aria-expanded', 'false');
    trigger.setAttribute('aria-label', 'Change time zone');
    trigger.textContent = 'Change';
    if (focus) trigger.focus({preventScroll: true});
  }

  trigger.addEventListener('click', () => {
    if (!panel.hidden) {
      close(true);
      return;
    }
    search.value = '';
    render();
    panel.hidden = false;
    trigger.setAttribute('aria-expanded', 'true');
    trigger.setAttribute('aria-label', 'Cancel time zone change');
    trigger.textContent = 'Cancel';
    search.focus();
  });
  search.addEventListener('input', event => {
    event.stopPropagation();
    render();
  });
  search.addEventListener('keydown', event => {
    if (!['Enter', 'ArrowDown'].includes(event.key) || event.isComposing) return;
    event.preventDefault();
    const first = results.querySelector('button');
    if (event.key === 'Enter' && results.childElementCount === 1) first.click();
    else first?.focus();
  });
  results.addEventListener('click', event => {
    const button = event.target.closest('button');
    if (!button) return;
    close(true);
    select(button.value);
  });
  field.addEventListener('keydown', event => {
    if (event.key !== 'Escape' || event.isComposing || panel.hidden) return;
    event.preventDefault();
    close(true);
  });
  field.addEventListener('focusout', event => {
    if (event.relatedTarget && !field.contains(event.relatedTarget)) close();
  });
  return close;
}

function initialize(root) {
  for (const section of root.querySelectorAll('[data-event-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const sourceLabel = section.querySelector('[data-event-source-label]');
    const controls = section.querySelector('[data-component-fields]');
    const fields = new Map([...controls.querySelectorAll('[data-event-field]')].map(input => [input.dataset.eventField, input]));
    const zone = fields.get('timeZone');
    const zoneTrigger = section.querySelector('#event-zone-change');
    const zoneName = section.querySelector('#event-zone-name');
    const allDay = section.querySelector('#event-all-day');
    const defaultZone = source.form.dataset.timeZone || 'UTC';
    const picker = section.querySelector('[data-image-picker]');
    const selectedImage = fields.get('imageID');
    const imageAlt = fields.get('imageAlt');
    const preview = picker.querySelector('[data-image-preview]');
    const description = picker.querySelector('[data-event-image-description]');
    const clearImage = picker.querySelector('[data-image-reset]');
    let displayedImageID;
    let errorControl;
    picker.querySelector('.image-upload-controls').hidden = false;

    function updateSchedule() {
      for (const key of timedFields) fields.get(key).disabled = allDay.checked;
      zoneTrigger.disabled = allDay.checked || Boolean(source.form.dataset.uploading);
      if (zoneTrigger.disabled) closeZone();
      zoneName.textContent = zoneLabel(zone.value);
    }

    function showImage() {
      const id = selectedImage.value;
      displayedImageID = id;
      preview.hidden = description.hidden = clearImage.hidden = !id;
      if (id) preview.src = '/images/' + id;
      else preview.removeAttribute('src');
    }

    function updateRequirements() {
      for (const [name, required] of [
        ['endTime', !allDay.checked && Boolean(fields.get('endDate').value)],
        ['location', Boolean(fields.get('mapURL').value.trim())]
      ]) {
        const field = fields.get(name);
        if (field.required === required) continue;
        field.required = required;
        field.labels[0].querySelector('[data-requirement]').textContent = required ? ' (required)' : ' (optional)';
        if (!required && !field.value.trim() && field.dataset.errorDescription === field.id + '-error') markField(field, false);
      }
    }

    function syncError() {
      if (errorControl) markField(errorControl, false);
      errorControl = null;
      if (!source.hidden || source.getAttribute('aria-invalid') !== 'true') return;
      const summary = document.getElementById(source.dataset.errorDescription);
      const names = summary?.querySelector('[data-error-fields]')?.dataset.errorFields.split(' ') || [];
      const name = names.find(name => name.startsWith('event.'))?.slice('event.'.length);
      errorControl = name === 'timeZone' ? zoneTrigger : fields.get(name);
      if (errorControl?.matches(':disabled')) errorControl = null;
      if (errorControl) markField(errorControl, true, source.id + '-component-error');
    }

    function read() {
      closeZone();
      let valid = false;
      try {
        const value = source.value.trim() ? JSON.parse(source.value) : {};
        if (!value || Array.isArray(value) || typeof value !== 'object') throw new Error();
        for (const [key, text] of Object.entries(value)) {
          if (key === 'allDay' ? typeof text !== 'boolean' : !fields.has(key) || typeof text !== 'string') throw new Error();
        }
        if (value.imageID && !imageID.test(value.imageID)) throw new Error();
        for (const [key, field] of fields) {
          const text = value[key] ?? '';
          field.value = text;
          if (field.value !== text) throw new Error();
        }
        allDay.checked = value.allDay ?? (Boolean(source.value.trim()) && !value.startTime);
        zone.value ||= defaultZone;
        updateSchedule();
        updateRequirements();
        showImage();
        valid = true;
      } catch {
        // Leave unfamiliar source visible and intact for correction.
      }
      source.hidden = sourceLabel.hidden = valid;
      controls.hidden = !valid;
      controls.disabled = !valid || section.hidden;
      syncError();
    }

    function write() {
      const value = {allDay: allDay.checked};
      for (const [key, field] of fields) {
        if (field.value && !(allDay.checked && timedFields.includes(key))) value[key] = field.value;
      }
      if (!value.imageID) delete value.imageAlt;
      source.value = JSON.stringify(value);
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    controls.addEventListener('input', event => {
      if (event.target === allDay) updateSchedule();
      else if (!event.target.matches('[data-event-field]')) return;
      if (event.target.dataset.errorDescription === event.target.id + '-error') markField(event.target, false);
      updateRequirements();
      if (event.target === selectedImage) {
        if (selectedImage.value !== displayedImageID) imageAlt.value = '';
        showImage();
      }
      write();
    });
    const closeZone = setupZonePicker(zoneTrigger, zone, defaultZone, name => {
      zone.value = name;
      updateSchedule();
      write();
    });
    source.form.addEventListener('blog:uploads-finished', updateSchedule);
    source.addEventListener('change', read);
    source.addEventListener('blog:field-validation', syncError);
    registerComponent(section, {
      normalize: comparableEvent,
      snapshot: () => ({source: source.value, times: timedFields.map(key => fields.get(key).value)}),
      clear: () => { source.value = ''; read(); },
      restore: copy => {
        source.value = copy.source;
        read();
        if (!source.hidden) return;
        timedFields.forEach((key, index) => { fields.get(key).value = copy.times[index]; });
        updateSchedule();
      },
      focus: () => (errorControl || (source.hidden ? fields.get('title') : source)).focus()
    });
    read();
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
