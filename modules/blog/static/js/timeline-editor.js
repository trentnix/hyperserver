import {initializeComponentPanels, jsonComponentValue, registerComponent} from './post-components.js';
import {focusField} from './forms.js';
import {destroyMarkdownEditors, refreshCompactMarkdownEditors} from './markdown-editor.js';

const initialized = new WeakSet();
const field = (row, name) => row.querySelector(`[data-timeline-field="${name}"]`);
const validMilestone = item => item && typeof item.date === 'string' && typeof item.title === 'string' &&
  Object.entries(item).every(([key, value]) => ['date', 'title', 'detail'].includes(key) && typeof value === 'string');

function validDate(date) {
  if (!/^\d{4}(?:-\d{2}(?:-\d{2})?)?$/.test(date) || date.startsWith('0000')) return false;
  const [year, month = '01', day = '01'] = date.split('-');
  const full = `${year}-${month}-${day}`;
  const parsed = new Date(full + 'T00:00:00Z');
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === full;
}

function initialize(root) {
  for (const section of root.querySelectorAll('.timeline-editor')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const sourceLabel = source.labels[0];
    const fields = section.querySelector('[data-component-fields]');
    const heading = fields.querySelector('[data-timeline-heading]');
    const style = fields.querySelector('[data-timeline-style-picker]');
    const styles = new Set([...style.options].map(option => option.value));
    const dateHelp = fields.querySelector('[data-timeline-date-help]');
    const list = fields.querySelector('.timeline-editor-list');
    const add = fields.querySelector('[data-timeline-add]');
    const limit = fields.querySelector('[data-timeline-limit]');
    const template = section.querySelector('[data-timeline-row]');
    const maximum = Number(template.dataset.maxMilestones);
    let nextRow = 0;

    function refreshRows() {
      for (const [index, row] of [...list.children].entries()) {
        const remove = row.querySelector('[data-timeline-remove]');
        remove.title = 'Remove milestone ' + (index + 1);
        remove.setAttribute('aria-label', remove.title);
      }
      add.disabled = list.children.length >= maximum;
      limit.hidden = !add.disabled;
    }

    function rowValue(row) {
      const date = field(row, 'date');
      const title = field(row, 'title');
      const detail = field(row, 'detail').value;
      const item = {date: date.value.trim(), title: title.value.trim()};
      if (detail.trim()) item.detail = detail;
      date.setCustomValidity(validDate(item.date) ? '' : 'Enter a valid date in YYYY, YYYY-MM, or YYYY-MM-DD format.');
      title.setCustomValidity(item.title ? '' : 'Enter a milestone title.');
      return item;
    }

    function serialize() {
      const milestones = [...list.children].map(rowValue);
      const value = {milestones};
      const title = heading.value.trim();
      if (title) value.title = title;
      if (style.value) value.style = style.value;
      source.value = title || style.value || milestones.length ? JSON.stringify(value) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function newRow(item = {}) {
      const row = template.content.firstElementChild.cloneNode(true);
      const id = source.id + '-milestone-' + nextRow++;
      for (const control of row.querySelectorAll('[data-timeline-field]')) {
        const name = control.dataset.timelineField;
        control.id = id + '-' + name;
        control.value = item[name] ?? '';
        row.querySelector(`[data-timeline-label="${name}"]`).htmlFor = control.id;
      }
      field(row, 'date').setAttribute('aria-describedby', dateHelp.id);
      const detail = field(row, 'detail');
      const detailField = detail.parentElement;
      const addDetail = row.querySelector('[data-timeline-add-detail]');
      detailField.id = id + '-detail-field';
      detailField.hidden = !detail.value.trim();
      detail.setSelectionRange(0, 0);
      addDetail.hidden = !detailField.hidden;
      addDetail.setAttribute('aria-controls', detailField.id);
      addDetail.addEventListener('click', () => {
        detailField.hidden = false;
        addDetail.hidden = true;
        refreshCompactMarkdownEditors(detailField);
        focusField(detail);
      });
      row.querySelector('[data-timeline-remove]').addEventListener('click', () => {
        const neighbor = row.nextElementSibling || row.previousElementSibling;
        destroyMarkdownEditors(row);
        row.remove();
        refreshRows();
        serialize();
        (neighbor ? field(neighbor, 'date') : add).focus();
      });
      rowValue(row);
      return row;
    }

    function read() {
      destroyMarkdownEditors(list);
      let value;
      try { value = source.value.trim() ? JSON.parse(source.value) : {milestones: []}; }
      catch { /* Allow correcting malformed source directly. */ }
      const valid = Boolean(value && Array.isArray(value.milestones) && value.milestones.length <= maximum &&
        value.milestones.every(validMilestone) && Object.entries(value).every(([key, text]) => key === 'milestones' ||
          key === 'title' && typeof text === 'string' || key === 'style' && styles.has(text)));
      source.hidden = sourceLabel.hidden = valid;
      fields.hidden = !valid;
      fields.disabled = !valid || section.hidden;
      if (!valid) return;
      heading.value = value.title ?? '';
      style.value = value.style ?? '';
      list.replaceChildren(...(value.milestones.length ? value.milestones.map(newRow) : [newRow()]));
      refreshRows();
      refreshCompactMarkdownEditors(list);
    }

    add.addEventListener('click', () => {
      const row = newRow();
      list.append(row);
      refreshRows();
      serialize();
      field(row, 'date').focus();
    });
    fields.addEventListener('input', event => {
      if (event.target.matches('[data-timeline-field], [data-timeline-heading], [data-timeline-style-picker]')) serialize();
    });
    source.addEventListener('change', read);
    registerComponent(section, {
      label: () => source.hidden ? heading.value.trim() : '',
      normalize: jsonComponentValue,
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: value => { source.value = value; read(); },
      focus: () => (source.hidden ? field(list, 'date') || add : source).focus()
    });
    read();
  }
}

initializeComponentPanels(initialize);
