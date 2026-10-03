// @ts-check

/** @typedef {'chrome'|'vivaldi'|'firefox'|'safari'|'apple'} Source */
/** @typedef {{source:Source, file:string, row:number, name:string, url:string, origin:string, username:string, password:string, note:string, otp:string}} Entry */
/** @typedef {{password:string, entries:Entry[]}} Candidate */
/** @typedef {{key:string, origin:string, username:string, entries:Entry[], candidates:Candidate[]}} Group */

export const SOURCES = /** @type {const} */ (['chrome', 'vivaldi', 'firefox', 'safari', 'apple']);
export const SOURCE_LABELS = Object.freeze({chrome:'Chrome', vivaldi:'Vivaldi', firefox:'Firefox', safari:'Safari', apple:'Apple Passwords'});
export const IMPORT_FIELDS = /** @type {const} */ (['name', 'url', 'username', 'password', 'note']);
export const DOWNLOAD_NAME = 'merged-passwords.csv';

export class InputError extends Error {}

/** @param {string} left @param {string} right */
function compare(left, right) { return left < right ? -1 : left > right ? 1 : 0; }

/** Parse CSV at the file boundary. Quoted field contents remain exact.
 * @param {string} input @returns {string[][]}
 */
export function parseCSV(input) {
  const text = input.startsWith('\uFEFF') ? input.slice(1) : input;
  /** @type {string[][]} */ const rows = [];
  /** @type {string[]} */ let row = [];
  let field = '';
  let quoted = false;
  let closed = false;
  let started = false;
  const endField = () => { row.push(field); field = ''; closed = false; started = false; };
  const endRow = () => { endField(); rows.push(row); row = []; };
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    if (char === '\0') throw new InputError('CSV contains a null character. Select the original UTF-8 export.');
    if (quoted) {
      if (char === '"') {
        if (text[index + 1] === '"') { field += '"'; index += 1; }
        else { quoted = false; closed = true; }
      } else { field += char; }
      continue;
    }
    if (char === ',') { endField(); continue; }
    if (char === '\r' || char === '\n') {
      endRow();
      if (char === '\r' && text[index + 1] === '\n') index += 1;
      continue;
    }
    if (closed) throw new InputError('CSV has text after a quoted field. Select the original export.');
    if (char === '"') {
      if (started) throw new InputError('CSV has an unexpected quote. Select the original export.');
      quoted = true; started = true;
    } else { field += char; started = true; }
  }
  if (quoted) throw new InputError('CSV has an unfinished quoted field. Select the original export.');
  if (started || closed || row.length) endRow();
  return rows.filter(values => values.length !== 1 || values[0] !== '');
}

/** @param {string} raw @returns {{origin:string, url:string}} */
function website(raw) {
  const trimmed = raw.trim();
  if (/\s/.test(trimmed)) throw new InputError('A record has an invalid website URL. Correct the source export.');
  let parsed;
  try { parsed = new URL(trimmed); }
  catch { throw new InputError('A record has an invalid website URL. Correct the source export.'); }
  if (!['https:', 'http:'].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password) {
    throw new InputError('A record needs an HTTP or HTTPS website URL without embedded credentials.');
  }
  return {origin:parsed.origin, url:parsed.href};
}

/** @param {string} text @param {Source} source @param {string} file @returns {Entry[]} */
export function parseExport(text, source, file) {
  const rows = parseCSV(text);
  if (!rows.length) throw new InputError(`${SOURCE_LABELS[source]} CSV has no header.`);
  const headers = rows[0].map(header => header.trim().toLowerCase());
  if (new Set(headers).size !== headers.length) throw new InputError(`${SOURCE_LABELS[source]} CSV has duplicate headers.`);
  if (!headers.includes('username') || !headers.includes('password') || !headers.includes('url')) {
    throw new InputError(`${SOURCE_LABELS[source]} CSV needs url, username, and password columns.`);
  }
  if (headers.includes('title') && headers.includes('name') || headers.includes('note') && headers.includes('notes')) {
    throw new InputError(`${SOURCE_LABELS[source]} CSV has ambiguous name or note columns.`);
  }
  return rows.slice(1).map((values, index) => {
    const row = index + 2;
    if (values.length !== headers.length) throw new InputError(`${SOURCE_LABELS[source]} CSV row ${row} has the wrong column count.`);
    const columns = Object.fromEntries(headers.map((header, position) => [header, values[position]]));
    if (columns.httprealm) throw new InputError(`${SOURCE_LABELS[source]} CSV row ${row} contains an HTTP authentication account. Export website accounts separately.`);
    const normalized = website(columns.url);
    return {source, file, row, name:columns.name ?? columns.title ?? '', ...normalized,
      username:columns.username, password:columns.password, note:columns.note ?? columns.notes ?? '', otp:columns.otpauth ?? ''};
  });
}

/** @param {Entry} entry */
function entryKey(entry) {
  return JSON.stringify([entry.source, entry.url, entry.name, entry.note, entry.otp, entry.file, entry.row]);
}

/** @param {Entry[]} entries @returns {Group[]} */
export function groupEntries(entries) {
  /** @type {Map<string, Entry[]>} */ const buckets = new Map();
  for (const entry of entries) {
    const key = JSON.stringify([entry.origin, entry.username]);
    const members = buckets.get(key) ?? [];
    members.push(entry); buckets.set(key, members);
  }
  return [...buckets.entries()].sort(([left], [right]) => compare(left, right)).map(([key, members]) => {
    const sorted = [...members].sort((left, right) => compare(entryKey(left), entryKey(right)));
    const passwords = [...new Set(sorted.map(entry => entry.password).filter(value => value !== ''))].sort(compare);
    const values = passwords.length ? passwords : [''];
    return {key, origin:sorted[0].origin, username:sorted[0].username, entries:sorted,
      candidates:values.map(password => ({password, entries:sorted.filter(entry => entry.password === password)}))};
  });
}

/** @param {Group[]} groups @param {Map<string, number>} selections @returns {string} */
export function mergedCSV(groups, selections) {
  const rows = groups.map(group => {
    const selectedIndex = group.candidates.length === 1 ? 0 : selections.get(group.key);
    if (selectedIndex === undefined) throw new InputError('Select one password for every conflict before download.');
    const candidate = group.candidates[selectedIndex];
    const selected = candidate.entries[0];
    const titles = [...new Set(group.entries.map(entry => entry.name).filter(Boolean))].sort(compare);
    const name = selected.name || titles[0] || group.origin;
    const notes = [...new Set(group.entries.map(entry => entry.note).filter(Boolean))].sort(compare);
    const urls = [...new Set(group.entries.map(entry => entry.url).filter(url => url !== selected.url))].sort(compare);
    const otherNames = titles.filter(title => title !== name);
    const otp = [...new Set(group.entries.map(entry => entry.otp).filter(Boolean))].sort(compare);
    if (otherNames.length) notes.push(`Other saved titles:\n${otherNames.join('\n')}`);
    if (urls.length) notes.push(`Other saved login URLs:\n${urls.join('\n')}`);
    if (otp.length) notes.push(`Verification code setup URI (import separately):\n${otp.join('\n')}`);
    return [name, selected.url, group.username, candidate.password, notes.join('\n\n')];
  });
  const quoted = rows.map(row => row.map(value => `"${value.replaceAll('"', '""')}"`).join(','));
  return `${IMPORT_FIELDS.join(',')}\r\n${quoted.join('\r\n')}\r\n`;
}
