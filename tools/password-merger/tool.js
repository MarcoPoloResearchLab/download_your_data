// @ts-check
import {SOURCES, SOURCE_LABELS, DOWNLOAD_NAME, InputError, parseExport, groupEntries, mergedCSV} from './core.js';

/** @typedef {import('./core.js').Group} Group */
/** @typedef {import('./core.js').Source} Source */
/** @param {string} selector @returns {HTMLElement} */
function element(selector) {
  const node = document.querySelector(selector);
  if (!(node instanceof HTMLElement)) throw new Error(`Missing tool element: ${selector}`);
  return node;
}
const status = element('#status');
const summary = element('#summary');
const files = element('#files');
const conflicts = element('#conflicts');
const merge = /** @type {HTMLButtonElement} */ (element('#merge'));
const download = /** @type {HTMLButtonElement} */ (element('#download'));
const resetConfirmation = /** @type {HTMLInputElement} */ (element('#apple-reset-confirm'));
const resetSteps = element('#apple-reset-steps');
const inputs = SOURCES.map(source => ({
  source,
  input:/** @type {HTMLInputElement} */ (element(`#source-${source}`)),
  selected:element(`#selected-${source}`),
  choose:element(`[data-pick-source="${source}"]`),
}));
const sourceIcons = /** @type {Record<Source, HTMLImageElement>} */ (Object.fromEntries(inputs.map(({source, input}) => {
  const icon = input.closest('.source-picker')?.querySelector('img.source-icon');
  if (!(icon instanceof HTMLImageElement)) throw new Error(`Missing source icon: ${source}`);
  return [source, icon];
})));
/** @param {Source} source @returns {HTMLImageElement} */
function sourceIcon(source) { return /** @type {HTMLImageElement} */ (sourceIcons[source].cloneNode()); }
/** @type {Group[]} */ let groups = [];
/** @type {Map<string, number>} */ const selections = new Map();
let revision = 0;
let downloadURL = '';
let complete = false;

function clearResult() {
  revision += 1;
  groups = []; selections.clear(); complete = false;
  conflicts.replaceChildren(); summary.textContent = '';
  download.disabled = true; merge.disabled = false;
  resetConfirmation.checked = false; resetSteps.hidden = true;
  if (downloadURL) { URL.revokeObjectURL(downloadURL); downloadURL = ''; }
}

/** @param {Map<File, number>} [counts] */
function renderFiles(counts) {
  files.replaceChildren();
  for (const {source, input, selected} of inputs) {
    const selectedFiles = Array.from(input.files ?? []);
    selected.textContent = selectedFiles.map(file => file.name).join(', ');
    selected.title = selected.textContent;
    if (!selectedFiles.length) continue;
    const section = document.createElement('li');
    const label = document.createElement('strong');
    label.textContent = `${SOURCE_LABELS[source]}: ${selectedFiles.length} file${selectedFiles.length === 1 ? '' : 's'}`;
    label.prepend(sourceIcon(source));
    section.append(label);
    const listing = document.createElement('ul');
    for (const file of selectedFiles) {
      const item = document.createElement('li');
      const count = counts?.get(file);
      item.textContent = `${file.name}${count === undefined ? '' : ` — ${count} records`}`;
      listing.append(item);
    }
    section.append(listing); files.append(section);
  }
}

function updateDownload() {
  const remaining = groups.filter(group => group.candidates.length > 1 && !selections.has(group.key)).length;
  download.disabled = !complete || remaining !== 0;
  status.textContent = remaining
    ? `Select a password for ${remaining} unresolved conflict${remaining === 1 ? '' : 's'}.`
    : 'All accounts resolved. Download the merged CSV.';
}

function renderConflicts() {
  conflicts.replaceChildren();
  const fragment = document.createDocumentFragment();
  let groupNumber = 0;
  for (const group of groups.filter(group => group.candidates.length > 1)) {
    groupNumber += 1;
    const fieldset = document.createElement('fieldset');
    fieldset.dataset.group = String(groupNumber);
    const legend = document.createElement('legend');
    legend.textContent = `${group.origin} · ${group.username || '(empty username)'}`;
    fieldset.append(legend);
    const instruction = document.createElement('p');
    instruction.textContent = `${group.candidates.length} different passwords. Select the password you want to keep.`;
    fieldset.append(instruction);
    group.candidates.forEach((candidate, index) => {
      const container = document.createElement('div'); container.className = 'candidate';
      const label = document.createElement('label');
      const radio = document.createElement('input');
      radio.type = 'radio'; radio.name = `account-${groupNumber}`; radio.value = String(index);
      radio.addEventListener('change', () => { selections.set(group.key, index); updateDownload(); });
      const caption = document.createElement('span');
      caption.textContent = `Candidate ${index < 26 ? String.fromCharCode(65 + index) : index + 1}`;
      label.append(radio, caption); container.append(label);
      const provenance = document.createElement('ul'); provenance.className = 'candidate-sources';
      for (const entry of candidate.entries) {
        const item = document.createElement('li');
        item.textContent = `${SOURCE_LABELS[entry.source]} · ${entry.file} · row ${entry.row} · ${entry.url}`;
        item.prepend(sourceIcon(entry.source));
        provenance.append(item);
      }
      container.append(provenance);
      const reveal = document.createElement('button');
      reveal.type = 'button'; reveal.dataset.reveal = ''; reveal.textContent = 'Show password';
      reveal.setAttribute('aria-expanded', 'false');
      const password = document.createElement('code'); password.hidden = true;
      password.className = 'candidate-password'; password.id = `password-${groupNumber}-${index}`;
      reveal.setAttribute('aria-controls', password.id);
      reveal.addEventListener('click', () => {
        password.hidden = !password.hidden;
        password.textContent = password.hidden ? '' : candidate.password;
        reveal.textContent = password.hidden ? 'Show password' : 'Hide password';
        reveal.setAttribute('aria-expanded', String(!password.hidden));
      });
      container.append(reveal, password); fieldset.append(container);
    });
    fragment.append(fieldset);
  }
  conflicts.append(fragment);
}

for (const {input, choose} of inputs) {
  choose.addEventListener('click', () => input.click());
  input.addEventListener('change', () => {
    clearResult(); renderFiles(); status.textContent = 'Files changed. Merge files to review the current selection.';
  });
}

merge.addEventListener('click', async () => {
  clearResult(); const currentRevision = revision;
  /** @type {{source:Source, file:File}[]} */ const selectedFiles = [];
  for (const {source, input} of inputs) {
    for (const file of Array.from(input.files ?? [])) selectedFiles.push({source, file});
  }
  if (!selectedFiles.length) { status.textContent = 'Select at least one exported CSV file.'; return; }
  merge.disabled = true; status.textContent = 'Read and validate the selected files locally…';
  try {
    const parsed = await Promise.all(selectedFiles.map(async ({source, file}) => {
      let text;
      try { text = new TextDecoder('utf-8', {fatal:true}).decode(await file.arrayBuffer()); }
      catch { throw new InputError(`${file.name}: ${SOURCE_LABELS[source]} file could not be read as UTF-8 CSV.`); }
      try { return {file, entries:parseExport(text, source, file.name)}; }
      catch (error) {
        if (!(error instanceof InputError)) throw error;
        throw new InputError(`${file.name}: ${error.message}`);
      }
    }));
    if (revision !== currentRevision) return;
    const entries = parsed.flatMap(result => result.entries);
    if (!entries.length) throw new InputError('The selected files contain no accounts.');
    groups = groupEntries(entries); complete = true;
    renderFiles(new Map(parsed.map(result => [result.file, result.entries.length])));
    const conflictCount = groups.filter(group => group.candidates.length > 1).length;
    const emptyCount = groups.filter(group => group.candidates[0].password === '').length;
    summary.textContent = `${entries.length} records · ${groups.length} accounts · ${conflictCount} conflicts · ${emptyCount} empty-password accounts`;
    renderConflicts(); updateDownload();
  } catch (error) {
    if (revision !== currentRevision) return;
    if (!(error instanceof InputError)) throw error;
    clearResult(); renderFiles(); status.textContent = `Cannot merge: ${error.message} No new result was created.`;
  } finally {
    if (revision === currentRevision) merge.disabled = false;
  }
});

download.addEventListener('click', () => {
  const csv = mergedCSV(groups, selections);
  if (downloadURL) URL.revokeObjectURL(downloadURL);
  downloadURL = URL.createObjectURL(new Blob([csv], {type:'text/csv;charset=utf-8'}));
  const link = document.createElement('a');
  link.href = downloadURL; link.download = DOWNLOAD_NAME; link.referrerPolicy = 'no-referrer';
  link.click();
  URL.revokeObjectURL(downloadURL); downloadURL = '';
});

element('#reset').addEventListener('click', () => {
  clearResult();
  for (const {input} of inputs) input.value = '';
  renderFiles(); status.textContent = 'Files and results cleared. Select your exported CSV files.';
});
resetConfirmation.addEventListener('change', () => { resetSteps.hidden = !resetConfirmation.checked; });
window.addEventListener('pagehide', () => { clearResult(); for (const {input} of inputs) input.value = ''; renderFiles(); });
