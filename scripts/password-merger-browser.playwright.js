async page => {
  let stage = 'initial public entrypoint';
  try {
  const baseURL = '__BASE_URL__';
  const sources = ['chrome', 'vivaldi', 'firefox', 'safari', 'apple'];
  const requests = [];
  const messages = [];
  const errors = [];
  const sockets = [];
  page.on('request', request => requests.push(request.url()));
  page.on('console', message => messages.push(message.text()));
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => sockets.push(socket.url()));
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const siteOrigin = url => url.split('/').slice(0, 3).join('/');
  const chromeHeader = ['name', 'url', 'username', 'password', 'note'];
  const secretA = 'synthetic-A,"quoted"\nsecond line';
  const secretB = 'synthetic-B';
  const secretC = 'synthetic-C';
  const secretD = 'synthetic-D';
  const secretE = 'synthetic-E';
  const uniqueSecret = 'synthetic-unicode-🔐';
  const allSecrets = [secretA, secretB, secretC, secretD, secretE, uniqueSecret, 'first-A', 'first-B', 'second-A', 'second-B', 'space-secret', 'plain-secret', 'idn-secret', 'ipv6-secret', 'otp-secret', 'realm-secret', 'bad-secret', 'synthetic-invalid', 'JBSWY3DPEHPK3PXP'];
  const fixtures = {
  "chrome": [
    {
      "name": "chrome-a.csv"
    },
    {
      "name": "chrome-b.csv"
    }
  ],
  "vivaldi": [
    {
      "name": "vivaldi.csv"
    }
  ],
  "firefox": [
    {
      "name": "firefox.csv"
    }
  ],
  "safari": [
    {
      "name": "safari.csv"
    }
  ],
  "apple": [
    {
      "name": "apple.csv"
    }
  ]
};
  const fixtureDirectory = '__FIXTURE_DIR__';
  const upload = async (reordered = false) => {
    for (const source of reordered ? [...sources].reverse() : sources) {
      const files = reordered ? [...fixtures[source]].reverse() : fixtures[source];
      const pending = page.waitForEvent('filechooser');
      const button = page.locator(`[data-pick-source="${source}"]`);
      if (source === 'firefox') { await button.focus(); await page.keyboard.press('Enter'); }
      else await button.click();
      const chooser = await pending;
      assert(chooser.isMultiple(), `${source} chooser must accept multiple files`);
      await chooser.setFiles(files.map(file => `${fixtureDirectory}${reordered ? '/reordered' : ''}/${file.name}`));
      const names = page.locator(`#selected-${source}`);
      assert((await names.innerText()) === files.map(file => file.name).join(', '), `${source} must display the selected filenames`);
      const position = await names.boundingBox();
      const buttonPosition = await button.boundingBox();
      assert(position && buttonPosition && position.x + position.width <= buttonPosition.x, `${source} filenames must appear left of the choose button`);
    }
    await page.locator('#merge').click();
    await page.locator('#conflicts fieldset[data-group]').first().waitFor();
  };
  // Independent parser for the downloaded public artifact, including quoted newlines.
  const parseCSV = text => {
    const rows = []; let row = []; let field = ''; let quoted = false;
    const input = text.replace(/^\ufeff/, '');
    for (let index = 0; index < input.length; index += 1) {
      const character = input[index];
      if (character === '"') {
        if (quoted && input[index + 1] === '"') { field += '"'; index += 1; }
        else quoted = !quoted;
      } else if (!quoted && character === ',') { row.push(field); field = ''; }
      else if (!quoted && (character === '\r' || character === '\n')) {
        if (character === '\r' && input[index + 1] === '\n') index += 1;
        row.push(field); rows.push(row); row = []; field = '';
      } else field += character;
    }
    assert(!quoted, 'download has an unterminated quoted field');
    if (field || row.length) { row.push(field); rows.push(row); }
    return rows;
  };
  const downloadCSV = async () => {
    const pending = page.waitForEvent('download');
    await page.locator('#download').click();
    const download = await pending;
    assert(download.suggestedFilename() === 'merged-passwords.csv', 'download must use the canonical filename');
    const stream = await download.createReadStream();
    assert(stream, 'download must expose readable CSV bytes');
    const chunks = [];
    for await (const chunk of stream) chunks.push(chunk);
    assert(chunks.length > 0, 'download must contain CSV data');
    return chunks[0].constructor.concat(chunks).toString('utf8');
  };
  await page.addInitScript(() => {
    window.__passwordMergerStorageWrites = 0;
    const setItem = Storage.prototype.setItem;
    Storage.prototype.setItem = function(key, value) { window.__passwordMergerStorageWrites += 1; return setItem.call(this, key, value); };
    const openDatabase = indexedDB.open.bind(indexedDB);
    indexedDB.open = function(...args) { window.__passwordMergerStorageWrites += 1; return openDatabase(...args); };
  });
  const response = await page.goto(`${baseURL}/tools/password-merger/`, {waitUntil: 'networkidle'});
  assert(response?.status() === 200, 'Password merger public entrypoint must return HTTP 200');
  stage = 'compact initial workflow';
  for (const size of [{width: 1280, height: 800}, {width: 390, height: 844}]) {
    await page.setViewportSize(size);
    const controlsFit = await page.evaluate(() => ['merge', 'reset', 'download'].every(id => {
      const bounds = document.getElementById(id).getBoundingClientRect();
      return bounds.width > 0 && bounds.top >= 0 && bounds.bottom <= innerHeight;
    }));
    assert(controlsFit, `${size.width}px initial viewport must show merge, clear, and download without scrolling`);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'compact workflow must not overflow horizontally');
  }
  await page.setViewportSize({width: 1280, height: 800});
  for (const id of ['export-guides', 'import-guides', 'apple-replacement', 'csv-details']) {
    assert(await page.locator(`#${id}`).evaluate(node => !node.open), `${id} must start collapsed`);
  }
  for (const guide of ['export', 'import']) {
    await page.locator(`#${guide}-guides > summary`).click();
    await page.locator(`[data-${guide}-guide="chrome"] > summary`).click();
    assert(await page.locator(`[data-${guide}-guide="chrome"] a`).isVisible(), `${guide} instructions must remain accessible`);
    await page.locator(`#${guide}-guides > summary`).click();
  }
  assert(await page.locator('input[type=file][data-source]').count() === 5, 'tool must offer all five file sources');
  stage = 'compact file controls';
  for (const source of sources) {
    assert(await page.locator(`#source-${source}[data-source="${source}"][multiple]`).count() === 1, `${source} must accept multiple CSVs`);
    assert(await page.locator(`#source-${source}`).isHidden(), `${source} must omit the native empty filename label`);
    assert(await page.locator(`#selected-${source}`).count() === 1 && (await page.locator(`#selected-${source}`).textContent()) === '', `${source} must leave the empty filename display blank`);
    assert(await page.getByRole('button', {name: `Choose files for ${source === 'apple' ? 'Apple Passwords' : source[0].toUpperCase() + source.slice(1)}`, exact: true}).count() === 1, `${source} must expose an accessible choose button`);
    const icon = page.locator(`label[for="source-${source}"] img`);
    assert(await icon.count() === 1 && await icon.evaluate(image => image.complete && image.naturalWidth >= 32 && image.src.startsWith(location.origin + '/')), `${source} must show a loaded local application icon`);
    for (const guide of ['export', 'import']) {
      const section = page.locator(`[data-${guide}-guide="${source}"]`);
      assert(await section.count() === 1 && (await section.textContent()).length > 60, `${source} ${guide} instructions are missing`);
      assert(await section.locator('a[href^="https://"]').count() > 0, `${source} ${guide} must link official help`);
      assert(await section.locator('summary img').count() === 1 && await section.locator('summary img').evaluate(image => image.complete && image.naturalWidth >= 32), `${source} ${guide} must show its application icon`);
    }
  }
  assert(await page.locator('#status[role=status]').count() === 1, 'status must expose accessible live feedback');
  assert(await page.locator('#download').isDisabled(), 'download must start disabled');
  assert(!(await page.locator('#apple-reset-steps').isVisible()), 'Apple deletion instructions must be gated by confirmation');
  await page.locator('#apple-replacement > summary').click();
  await page.locator('#apple-reset-confirm').check();
  assert(await page.locator('#apple-reset-steps').isVisible(), 'Apple confirmation must disclose reset steps');
  await page.locator('#apple-reset-confirm').uncheck();
  assert(!(await page.locator('#apple-reset-steps').isVisible()), 'clearing Apple confirmation must hide reset steps');
  await page.locator('#apple-replacement > summary').click();
  const networkBoundary = requests.length;
  await upload();
  assert((await page.locator('#files').innerText()).includes('chrome-a.csv') && (await page.locator('#files').innerText()).includes('chrome-b.csv'), 'both same-source file counts must remain visible');
  const summary = await page.locator('#summary').innerText();
  assert(/13 records/.test(summary) && /6 accounts/.test(summary) && /1 conflicts/.test(summary) && /1 empty-password accounts/.test(summary), 'summary must report 13 records, six accounts, one conflict and one empty-password account');
  await page.setViewportSize({width: 390, height: 844});
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'populated merger must fit a mobile viewport');
  await page.setViewportSize({width: 1440, height: 1000});
  const groups = page.locator('#conflicts fieldset[data-group]');
  assert(await groups.count() === 1, 'normalized origin/exact username must yield one conflict');
  const radios = groups.first().locator('input[type=radio]');
  assert(await radios.count() === 5, 'all five distinct nonempty passwords must be selectable; duplicates and empty copies must not conflict');
  const names = await radios.evaluateAll(nodes => nodes.map(node => node.name));
  assert(names.every(name => name && name === names[0]), 'conflict choices must be one semantic radio group');
  assert(await page.locator('#download').isDisabled(), 'unresolved conflicts must block download');
  const provenance = await groups.first().innerText();
  assert(await groups.first().locator('.candidate-sources img').evaluateAll(images => images.length === 6 && images.every(image => image.complete && image.naturalWidth >= 32)), 'conflict provenance must retain all six source icons');
  for (const source of sources) assert(provenance.toLowerCase().includes(source), `${source} provenance must appear in conflict review`);
  assert(/chrome-a\.csv/.test(provenance) && /row\s*2/i.test(provenance), 'candidate provenance must include source filename and physical CSV row');
  let selectedDownload = '';
  const exportedChoices = new Set();
  for (let index = 0; index < 5; index += 1) {
    await radios.nth(index).check();
    assert(await groups.first().locator('input[type=radio]:checked').count() === 1, 'one candidate must be selected per conflict');
    assert(!(await page.locator('#download').isDisabled()), 'resolved conflict must enable download');
    const text = await downloadCSV();
    const rows = parseCSV(text);
    assert(JSON.stringify(rows[0]) === JSON.stringify(chromeHeader), 'canonical output headers must be name,url,username,password,note');
    assert(rows.length === 7 && rows.every(row => row.length === 5), 'export must contain exactly six valid account rows');
    const accounts = new Set(rows.slice(1).map(row => `${siteOrigin(row[1])}\u0000${row[2]}`));
    assert(accounts.size === 6, 'export must contain one row per normalized origin/exact username');
    const conflict = rows.slice(1).find(row => siteOrigin(row[1]) === 'https://example.test' && row[2] === 'Alice');
    assert(conflict && allSecrets.slice(0, 5).includes(conflict[3]), 'conflict export must retain a chosen candidate exactly');
    exportedChoices.add(conflict[3]);
    if (conflict[3] === secretA) selectedDownload = text;
    for (const note of ['chrome note, with comma\nsecond note line', 'keep empty-copy metadata', 'same-secret metadata', 'vivaldi metadata', 'safari metadata', 'apple metadata']) {
      assert(conflict[4].includes(note), `merged notes must retain metadata: ${note}`);
    }
    const empty = rows.slice(1).find(row => siteOrigin(row[1]) === 'https://empty.test');
    assert(empty?.[3] === '' && empty[4].includes('empty-only metadata') && empty[4].includes('second-empty metadata'), 'empty-only account must survive with all metadata');
    for (const origin of ['https://sub.example.test', 'http://example.test', 'https://example.test:8443']) assert(rows.slice(1).some(row => siteOrigin(row[1]) === origin && row[3] === uniqueSecret), `${origin} must remain a distinct account`);
  }
  assert(exportedChoices.size === 5, 'each candidate choice must export its distinct password');
  assert(selectedDownload !== '', 'quoted multiline candidate must be exportable without corruption');
  const reveal = groups.first().locator('[data-reveal]').first();
  const initialCandidate = await groups.first().innerText();
  assert(!allSecrets.some(secret => initialCandidate.includes(secret)), 'password candidates must start hidden');
  await reveal.click();
  const revealedCandidate = await groups.first().innerText();
  assert(allSecrets.filter(secret => revealedCandidate.includes(secret)).length === 1, 'individual reveal must disclose exactly one candidate password');
  await reveal.click();
  const concealedCandidate = await groups.first().innerText();
  assert(!allSecrets.some(secret => concealedCandidate.includes(secret)), 'hide must conceal all passwords again');
  await page.locator('#reset').click();
  assert(await groups.count() === 0 && await page.locator('#download').isDisabled(), 'reset must clear conflicts and disable stale downloads');
  assert(await page.locator('input[type=file]').evaluateAll(inputs => inputs.every(input => input.files.length === 0)), 'reset must clear all source selections');
  assert(await page.locator('[data-selected-source]').evaluateAll(labels => labels.length === 5 && labels.every(label => label.textContent === '')), 'reset must clear displayed filenames');
  await upload(true);
  assert(await page.locator('#download').isDisabled(), 'reordered upload must not retain old selections');
  for (let index = 0; index < 5; index += 1) {
    await groups.first().locator('input[type=radio]').nth(index).check();
    const text = await downloadCSV();
    if (parseCSV(text).some(row => row[3] === secretA)) assert(text === selectedDownload, 'file and row order must not change canonical output');
  }
  await page.locator('#source-chrome').setInputFiles(fixtures.chrome.map(file => `${fixtureDirectory}/${file.name}`));
  assert(await page.locator('#download').isDisabled(), 'changing input files must invalidate resolved selections immediately');
  stage = 'atomic invalid CSV batch';
  for (const label of ['malformed.csv', 'duplicate-header.csv', 'invalid-url.csv', 'no-headers.csv', 'column-count.csv', 'unescaped-quote.csv', 'embedded-credentials.csv', 'invalid-utf8.csv']) {
    stage = `atomic rejection: ${label}`;
    await page.locator('#reset').click();
    await page.locator('#source-chrome').setInputFiles([`${fixtureDirectory}/chrome-a.csv`, `${fixtureDirectory}/${label}`]);
    await page.locator('#merge').click();
    await page.waitForFunction(filename => document.querySelector('#status')?.textContent?.includes(filename), label);
    assert(await page.locator('#download').isDisabled() && await groups.count() === 0, `${label} must fail atomically without partial output`);
    assert((await page.locator('#status').innerText()).includes(label), `${label} error must identify the failed file`);
  }
  await page.locator('#reset').click();
  stage = 'HTTP realm rejection';
  await page.locator('#source-firefox').setInputFiles(`${fixtureDirectory}/realm.csv`);
  await page.locator('#merge').click();
  await page.waitForFunction(() => document.querySelector('#status')?.textContent?.includes('realm.csv'));
  assert(await page.locator('#download').isDisabled() && await groups.count() === 0, 'unsupported HTTP realm must fail without silently dropping a record');
  await page.locator('#reset').click();
  stage = 'entirely empty-password batch';
  await page.locator('#source-chrome').setInputFiles(`${fixtureDirectory}/empty-only.csv`);
  await page.locator('#merge').click();
  await page.waitForFunction(() => !document.querySelector('#download').disabled);
  const emptyRows = parseCSV(await downloadCSV());
  assert(emptyRows.length === 2 && emptyRows[1][3] === '' && emptyRows[1][4].includes('retained-one') && emptyRows[1][4].includes('retained-two'), 'entirely empty-password inputs must retain a resolved account and both notes');
  await page.locator('#reset').click();
  stage = 'multiple conflicts';
  await page.locator('#source-chrome').setInputFiles(`${fixtureDirectory}/multiple-conflicts.csv`);
  await page.locator('#merge').click();
  await page.waitForFunction(() => document.querySelectorAll('#conflicts fieldset').length === 2);
  await groups.nth(0).locator('input[type=radio]').first().check();
  assert(await page.locator('#download').isDisabled(), 'one unresolved account must continue blocking download');
  await groups.nth(1).locator('input[type=radio]').last().check();
  const multipleRows = parseCSV(await downloadCSV());
  assert(multipleRows.length === 3 && multipleRows.slice(1).every(row => row[3]), 'independently resolved conflicts must each export one selected password');
  await page.locator('#reset').click();
  stage = 'normalization batch';
  await page.locator('#source-chrome').setInputFiles(`${fixtureDirectory}/normalization.csv`);
  await page.locator('#source-apple').setInputFiles(`${fixtureDirectory}/otp.csv`);
  await page.locator('#merge').click();
  await page.waitForFunction(() => !document.querySelector('#download').disabled);
  const normalizedRows = parseCSV(await downloadCSV());
  assert(await groups.count() === 0, 'same-password copies must resolve without manual conflict choices');
  for (const password of ['space-secret', 'plain-secret', 'idn-secret', 'ipv6-secret', 'otp-secret']) assert(normalizedRows.slice(1).filter(row => row[3] === password).length === 1, 'non-conflicting passwords must export exactly once without edits');
  assert(normalizedRows.length === 6, 'IDN and IPv6 copies must group while whitespace-distinct usernames remain separate');
  assert(normalizedRows.some(row => row[2] === ' spaced ') && normalizedRows.some(row => row[2] === 'spaced'), 'username whitespace must round-trip exactly');
  assert(normalizedRows.filter(row => siteOrigin(row[1]) === 'https://xn--bcher-kva.test').length === 1, 'Unicode and ASCII IDN origins must normalize together');
  assert(normalizedRows.filter(row => siteOrigin(row[1]) === 'https://[2001:db8::1]').length === 1, 'expanded and compressed IPv6 origins must normalize together');
  const otp = normalizedRows.find(row => row[2] === 'otp-user');
  assert(otp?.[4].includes('otpauth://totp/Synthetic?secret=JBSWY3DPEHPK3PXP&issuer=Synthetic'), 'OTP metadata must survive in the canonical note');
  await page.locator('#reset').click();
  stage = 'pending read invalidation';
  await page.evaluate(() => {
    const readFile = File.prototype.arrayBuffer;
    window.__passwordMergerReleaseRead = null;
    File.prototype.arrayBuffer = function() {
      if (this.name !== 'multiple-conflicts.csv') return readFile.call(this);
      return new Promise(resolve => { window.__passwordMergerReleaseRead = () => readFile.call(this).then(resolve); });
    };
  });
  stage = 'multiple conflicts';
  await page.locator('#source-chrome').setInputFiles(`${fixtureDirectory}/multiple-conflicts.csv`);
  await page.locator('#merge').click();
  await page.waitForFunction(() => typeof window.__passwordMergerReleaseRead === 'function');
  await page.locator('#reset').click();
  stage = 'entirely empty-password batch';
  await page.locator('#source-chrome').setInputFiles(`${fixtureDirectory}/empty-only.csv`);
  await page.locator('#merge').click();
  await page.waitForFunction(() => !document.querySelector('#download').disabled);
  await page.evaluate(async () => { await window.__passwordMergerReleaseRead(); });
  assert(await groups.count() === 0, 'late results after reset and new upload must not restore stale conflicts');
  const currentRows = parseCSV(await downloadCSV());
  assert(currentRows.length === 2 && currentRows[1][3] === '', 'stale async reads must not overwrite the current download');
  await page.locator('#reset').click();
  for (const size of [{width: 1440, height: 1000}, {width: 390, height: 844}]) {
    await page.setViewportSize(size);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${size.width}px viewport must not overflow horizontally`);
  }
  const storage = await page.evaluate(async () => ({local: localStorage.length, session: sessionStorage.length, databases: (await indexedDB.databases()).length, writes: window.__passwordMergerStorageWrites}));
  assert(storage.local === 0 && storage.session === 0 && storage.databases === 0 && storage.writes === 0, 'credentials must not be persisted in browser storage');
  assert(requests.length === networkBoundary, 'file upload, review, guide confirmation and download must make no network requests');
  assert(sockets.length === 0, 'local merger must not create websocket connections');
  assert(!messages.some(message => allSecrets.some(secret => message.includes(secret))), 'synthetic credentials must not appear in console messages');
  assert(errors.length === 0, `browser errors: ${errors.join(' | ')}`);
  } catch (error) { throw new Error(`${stage}: ${error.message}`); }
}
