// @ts-check

/** @typedef {{data: string}} QRResult */
/** @typedef {(pixels: Uint8ClampedArray, width: number, height: number, options: {inversionAttempts: string}) => QRResult | null} QRDecoder */
/** @typedef {{accountName: string, issuer: string, setupKey: string, otpauthUri: string}} ConvertedAccount */
/** @typedef {{fieldNumber: number, wireType: number, fieldValue: number | Uint8Array}} ProtobufField */
/** @typedef {{secretBytes: Uint8Array | null, accountName: string, issuer: string, algorithm: string, digits: string, otpType: string, counter: number}} OTPParameter */

const algorithmNames = /** @type {Readonly<Record<number, string>>} */ (Object.freeze({1: 'SHA1', 2: 'SHA256', 3: 'SHA512', 4: 'MD5'}));
const digitCounts = /** @type {Readonly<Record<number, string>>} */ (Object.freeze({1: '6', 2: '8'}));
const otpTypes = /** @type {Readonly<Record<number, string>>} */ (Object.freeze({1: 'hotp', 2: 'totp'}));
const stages = Object.freeze(['start', 'export', 'save', 'upload', 'apple']);

const qrImages = /** @type {HTMLInputElement} */ (requireElement('qr-images', HTMLInputElement));
const status = requireElement('tool-status', HTMLElement, '[data-status]');
const results = requireElement('tool-results', HTMLElement, '[data-results]');
const qrCanvas = /** @type {HTMLCanvasElement} */ (requireElement('qr-canvas', HTMLCanvasElement, '[data-qr-canvas]'));
const canvasContext = qrCanvas.getContext('2d', {willReadFrequently: true});
if (!canvasContext) {
  throw new Error('The browser cannot read image pixels.');
}
/** @type {HTMLElement} */
const simulatorStage = requireElement('simulator-stage', HTMLElement, '[data-simulator-stage-container]');
/** @type {NodeListOf<HTMLElement>} */
const progressItems = document.querySelectorAll('[data-progress]');
let currentStage = 0;

qrImages.addEventListener('change', () => {
  const selectedFiles = qrImages.files ? Array.from(qrImages.files) : [];
  void convertSelectedFiles(selectedFiles);
});

renderSimulator();

/**
 * @param {string} id
 * @param {typeof HTMLElement} elementType
 * @param {string} [selector]
 * @returns {HTMLElement}
 */
function requireElement(id, elementType, selector = `#${id}`) {
  const element = document.querySelector(selector);
  if (!(element instanceof elementType)) {
    throw new Error(`Required tool element is missing: ${id}`);
  }
  return element;
}

/** @param {ConvertedAccount[]} accounts */
function renderConvertedAccounts(accounts) {
  results.replaceChildren();
  for (const [index, account] of accounts.entries()) {
    const card = document.createElement('article');
    card.className = 'result-card';
    const title = document.createElement('h3');
    title.textContent = account.issuer ? `${account.issuer}: ${account.accountName}` : account.accountName;
    card.append(title);
    appendSecretField(card, `setup-key-${index}`, 'Setup key for Apple Passwords', account.setupKey);
    appendSecretField(card, `otpauth-uri-${index}`, 'Standard otpauth URI', account.otpauthUri);
    results.append(card);
  }
}

/**
 * @param {HTMLElement} card
 * @param {string} id
 * @param {string} labelText
 * @param {string} value
 */
function appendSecretField(card, id, labelText, value) {
  const label = document.createElement('label');
  label.className = 'secret-label';
  label.htmlFor = id;
  label.textContent = labelText;
  const row = document.createElement('div');
  row.className = 'secret-row';
  const input = document.createElement('input');
  input.id = id;
  input.value = value;
  input.readOnly = true;
  const copyButton = document.createElement('button');
  copyButton.className = 'copy-button';
  copyButton.type = 'button';
  copyButton.textContent = 'Copy';
  copyButton.addEventListener('click', () => {
    void copyValue(value, copyButton);
  });
  row.append(input, copyButton);
  card.append(label, row);
}

/**
 * @param {string} value
 * @param {HTMLButtonElement} button
 */
async function copyValue(value, button) {
  const originalLabel = button.textContent;
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const temporaryInput = document.createElement('textarea');
    temporaryInput.value = value;
    temporaryInput.setAttribute('readonly', '');
    temporaryInput.style.position = 'fixed';
    temporaryInput.style.opacity = '0';
    document.body.append(temporaryInput);
    temporaryInput.select();
    document.execCommand('copy');
    temporaryInput.remove();
  }
  button.textContent = 'Copied';
  window.setTimeout(() => {
    button.textContent = originalLabel;
  }, 1200);
}

/** @param {File[]} selectedFiles */
async function convertSelectedFiles(selectedFiles) {
  results.replaceChildren();
  if (selectedFiles.length === 0) {
    setStatus('Choose one or more export QR screenshots.', '');
    return;
  }
  setStatus(`Reading ${selectedFiles.length} image${selectedFiles.length === 1 ? '' : 's'} locally…`, '');
  try {
    /** @type {ConvertedAccount[]} */
    const accounts = [];
    for (const selectedFile of selectedFiles) {
      const payload = await decodeQrPayloadFromFile(selectedFile);
      accounts.push(...convertQrPayload(payload));
    }
    const uniqueAccounts = deduplicateAccounts(accounts);
    renderConvertedAccounts(uniqueAccounts);
    setStatus(`Converted ${uniqueAccounts.length} account${uniqueAccounts.length === 1 ? '' : 's'}.`, 'success');
  } catch (error) {
    const message = error instanceof Error ? error.message : 'The QR image could not be converted.';
    setStatus(message, 'error');
  }
}

/**
 * @param {string} message
 * @param {string} statusName
 */
function setStatus(message, statusName) {
  status.textContent = message;
  if (statusName) {
    status.dataset.status = statusName;
  } else {
    delete status.dataset.status;
  }
}

/** @param {ConvertedAccount[]} accounts @returns {ConvertedAccount[]} */
function deduplicateAccounts(accounts) {
  const seen = new Set();
  return accounts.filter((account) => {
    const identity = `${account.issuer}\u0000${account.accountName}\u0000${account.setupKey}`;
    if (seen.has(identity)) {
      return false;
    }
    seen.add(identity);
    return true;
  });
}

/** @param {File} imageFile @returns {Promise<string>} */
function decodeQrPayloadFromFile(imageFile) {
  return new Promise((resolve, reject) => {
    const fileReader = new FileReader();
    fileReader.onload = () => {
      const image = new Image();
      image.onload = () => {
        const width = image.naturalWidth || image.width;
        const height = image.naturalHeight || image.height;
        qrCanvas.width = width;
        qrCanvas.height = height;
        canvasContext.clearRect(0, 0, width, height);
        canvasContext.drawImage(image, 0, 0, width, height);
        // @ts-expect-error jsQR is supplied by the pinned CDN script in index.html.
        const decoder = /** @type {QRDecoder | undefined} */ (window.jsQR);
        if (!decoder) {
          reject(new Error('The QR decoder is unavailable. Reload the page and try again.'));
          return;
        }
        const decoded = decoder(
          canvasContext.getImageData(0, 0, width, height).data,
          width,
          height,
          {inversionAttempts: 'attemptBoth'},
        );
        if (!decoded || !decoded.data) {
          reject(new Error('No QR code was found. Crop closer to the QR and try again.'));
          return;
        }
        resolve(decoded.data);
      };
      image.onerror = () => reject(new Error('The selected image could not be loaded.'));
      image.src = String(fileReader.result);
    };
    fileReader.onerror = () => reject(new Error('The selected image could not be read.'));
    fileReader.readAsDataURL(imageFile);
  });
}

/** @param {string} payload @returns {ConvertedAccount[]} */
function convertQrPayload(payload) {
  if (payload.startsWith('otpauth://')) {
    return [convertStandardOtpAuthUri(payload)];
  }
  if (!payload.startsWith('otpauth-migration://')) {
    throw new Error('This image is not a Google Authenticator export QR or a standard TOTP QR.');
  }
  const migrationURL = new URL(payload);
  const encodedData = migrationURL.searchParams.get('data');
  if (!encodedData) {
    throw new Error('The Google Authenticator QR has no data payload.');
  }
  const migrationFields = parseProtobufFields(decodeBase64ToBytes(encodedData));
  const accounts = [];
  for (const field of migrationFields) {
    if (field.fieldNumber === 1 && field.wireType === 2 && field.fieldValue instanceof Uint8Array) {
      accounts.push(buildConvertedAccount(parseOtpParameter(field.fieldValue)));
    }
  }
  if (accounts.length === 0) {
    throw new Error('No authenticator accounts were found in the migration QR.');
  }
  return accounts;
}

/** @param {string} value @returns {ConvertedAccount} */
function convertStandardOtpAuthUri(value) {
  const standardURL = new URL(value);
  const setupKey = standardURL.searchParams.get('secret');
  if (!setupKey) {
    throw new Error('This standard authenticator QR has no secret setup key.');
  }
  return {
    accountName: decodeURIComponent(standardURL.pathname.replace(/^\//, '')),
    issuer: standardURL.searchParams.get('issuer') || '',
    setupKey,
    otpauthUri: value,
  };
}

/** @param {Uint8Array} bytes @returns {OTPParameter} */
function parseOtpParameter(bytes) {
  const parameter = {
    secretBytes: null,
    accountName: '',
    issuer: '',
    algorithm: 'SHA1',
    digits: '6',
    otpType: 'totp',
    counter: 0,
  };
  for (const field of parseProtobufFields(bytes)) {
    if (field.fieldNumber === 1 && field.wireType === 2 && field.fieldValue instanceof Uint8Array) {
      parameter.secretBytes = field.fieldValue;
    } else if (field.fieldNumber === 2 && field.wireType === 2 && field.fieldValue instanceof Uint8Array) {
      parameter.accountName = decodeUtf8(field.fieldValue);
    } else if (field.fieldNumber === 3 && field.wireType === 2 && field.fieldValue instanceof Uint8Array) {
      parameter.issuer = decodeUtf8(field.fieldValue);
    } else if (field.fieldNumber === 4 && field.wireType === 0 && typeof field.fieldValue === 'number') {
      parameter.algorithm = algorithmNames[field.fieldValue] || 'SHA1';
    } else if (field.fieldNumber === 5 && field.wireType === 0 && typeof field.fieldValue === 'number') {
      parameter.digits = digitCounts[field.fieldValue] || '6';
    } else if (field.fieldNumber === 6 && field.wireType === 0 && typeof field.fieldValue === 'number') {
      parameter.otpType = otpTypes[field.fieldValue] || 'totp';
    } else if (field.fieldNumber === 7 && field.wireType === 0 && typeof field.fieldValue === 'number') {
      parameter.counter = field.fieldValue;
    }
  }
  if (!parameter.secretBytes) {
    throw new Error('One account in the QR is missing its secret.');
  }
  return parameter;
}

/** @param {OTPParameter} parameter @returns {ConvertedAccount} */
function buildConvertedAccount(parameter) {
  const setupKey = encodeBase32WithoutPadding(parameter.secretBytes);
  const accountName = parameter.accountName || 'Account';
  const issuer = parameter.issuer || '';
  const label = issuer ? `${issuer}:${accountName}` : accountName;
  const query = new URLSearchParams({
    secret: setupKey,
    algorithm: parameter.algorithm,
    digits: parameter.digits,
  });
  if (issuer) {
    query.set('issuer', issuer);
  }
  if (parameter.otpType === 'hotp') {
    query.set('counter', String(parameter.counter));
  } else {
    query.set('period', '30');
  }
  return {
    accountName,
    issuer,
    setupKey,
    otpauthUri: `otpauth://${parameter.otpType}/${encodeURIComponent(label)}?${query.toString()}`,
  };
}

/** @param {Uint8Array} bytes @returns {ProtobufField[]} */
function parseProtobufFields(bytes) {
  /** @type {ProtobufField[]} */
  const fields = [];
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readVarint(bytes, offset);
    offset = tag.nextOffset;
    const fieldNumber = tag.value >> 3;
    const wireType = tag.value & 7;
    if (wireType === 0) {
      const value = readVarint(bytes, offset);
      fields.push({fieldNumber, wireType, fieldValue: value.value});
      offset = value.nextOffset;
    } else if (wireType === 2) {
      const length = readVarint(bytes, offset);
      offset = length.nextOffset;
      const end = offset + length.value;
      if (end > bytes.length) {
        throw new Error('The QR payload ends before an account record is complete.');
      }
      fields.push({fieldNumber, wireType, fieldValue: bytes.slice(offset, end)});
      offset = end;
    } else {
      throw new Error(`Unsupported QR payload field type: ${wireType}.`);
    }
  }
  return fields;
}

/** @param {Uint8Array} bytes @param {number} startOffset @returns {{value: number, nextOffset: number}} */
function readVarint(bytes, startOffset) {
  let value = 0;
  let shift = 0;
  let offset = startOffset;
  while (offset < bytes.length) {
    const currentByte = bytes[offset];
    offset += 1;
    value += (currentByte & 127) * 2 ** shift;
    if ((currentByte & 128) === 0) {
      return {value, nextOffset: offset};
    }
    shift += 7;
    if (shift > 49) {
      throw new Error('The QR payload contains an unsupported value.');
    }
  }
  throw new Error('The QR payload ends during a value.');
}

/** @param {string} encodedValue @returns {Uint8Array} */
function decodeBase64ToBytes(encodedValue) {
  const normalized = encodedValue.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

/** @param {Uint8Array} bytes @returns {string} */
function encodeBase32WithoutPadding(bytes) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bitBuffer = 0;
  let bitCount = 0;
  let result = '';
  for (const byte of bytes) {
    bitBuffer = (bitBuffer << 8) | byte;
    bitCount += 8;
    while (bitCount >= 5) {
      result += alphabet[(bitBuffer >>> (bitCount - 5)) & 31];
      bitCount -= 5;
    }
  }
  if (bitCount > 0) {
    result += alphabet[(bitBuffer << (5 - bitCount)) & 31];
  }
  return result;
}

/** @param {Uint8Array} bytes @returns {string} */
function decodeUtf8(bytes) {
  return new TextDecoder('utf-8', {fatal: false}).decode(bytes);
}

function renderSimulator() {
  progressItems.forEach((item, index) => {
    item.dataset.active = String(index === Math.max(0, currentStage - 1));
  });
  simulatorStage.replaceChildren(buildSimulatorStage(stages[currentStage]));
}

/** @param {string} stage @returns {HTMLElement} */
function buildSimulatorStage(stage) {
  const section = document.createElement('section');
  section.dataset.simulatorStage = stage;
  section.className = 'simulator-stage-content';
  const device = document.createElement('div');
  device.className = 'simulated-device';
  if (stage === 'start') {
    device.classList.add('app-capture-frame');
  } else if (stage === 'export' || stage === 'apple') {
    device.classList.add('help-capture-frame');
  }
  device.append(buildSimulatedScreen(stage));
  const copy = document.createElement('div');
  copy.className = 'simulator-copy';
  const title = document.createElement('h3');
  title.textContent = simulatorTitle(stage);
  const text = document.createElement('p');
  text.textContent = simulatorDescription(stage);
  copy.append(title, text);
  if (stage === 'start') {
    copy.append(actionButton('start-simulator', 'Continue to the transfer steps'));
  } else if (stage === 'apple') {
    const list = document.createElement('ul');
    for (const item of ['Copy one setup key from the result card.', 'Open the matching account in Apple Passwords.', 'Choose Edit, Set Up Code, and Use Setup Key.']) {
      const listItem = document.createElement('li');
      listItem.textContent = item;
      list.append(listItem);
    }
    copy.append(list);
  } else {
    copy.append(actionButton('next-stage', stage === 'upload' ? 'Show the Apple Passwords step' : 'Continue'));
  }
  section.append(device, copy);
  return section;
}

/** @param {string} stage @returns {HTMLElement} */
function buildSimulatedScreen(stage) {
  if (stage === 'start') {
    return buildAppCapture(
      '/images/tools/google-authenticator/google-authenticator-simulator-onboarding.png',
      'Real Google Authenticator onboarding screen captured from an Android simulator.',
      'Google Authenticator 7.2 — Android simulator capture',
      'https://play.google.com/store/apps/details?id=com.google.android.apps.authenticator2',
    );
  }
  if (stage === 'export') {
    return buildHelpCapture(
      '/images/tools/google-authenticator/google-authenticator-transfer-help.png',
      'Google Account Help transfer instructions showing Menu, Transfer accounts, and Export accounts.',
      'Google Account Help: transfer your Google Authenticator codes',
      'https://support.google.com/accounts/answer/1066447?co=GENIE.Platform%3DiOS&hl=en',
    );
  }
  if (stage === 'apple') {
    return buildHelpCapture(
      '/images/tools/google-authenticator/apple-passwords-setup-key-help.png',
      'Apple iPhone User Guide instructions showing how to enter a setup key in Passwords.',
      'Apple iPhone User Guide: enter a setup key',
      'https://support.apple.com/en-mt/guide/iphone/ipha6173c19f/ios',
    );
  }
  const screen = document.createElement('div');
  screen.className = 'simulated-screen';
  if (stage === 'save') {
    const heading = document.createElement('h3');
    heading.textContent = 'Export accounts';
    const qr = document.createElement('div');
    qr.className = 'qr-placeholder';
    const label = document.createElement('span');
    label.textContent = 'SIMULATED QR';
    qr.append(label);
    screen.append(heading, qr);
  } else if (stage === 'upload') {
    const heading = document.createElement('h3');
    heading.textContent = 'Browser tool';
    const button = document.createElement('div');
    button.className = 'simulated-button';
    button.textContent = 'Choose QR screenshots';
    const note = document.createElement('p');
    note.textContent = 'Images stay in this browser.';
    screen.append(heading, button, note);
  } else {
    const heading = document.createElement('h3');
    heading.textContent = 'Passwords';
    for (const rowText of ['Example account', 'Edit', 'Set Up Code', 'Use Setup Key']) {
      const row = document.createElement('div');
      row.className = 'simulated-row';
      row.textContent = rowText;
      screen.append(row);
    }
  }
  return screen;
}

/** @param {string} imageSource @param {string} imageAlt @param {string} sourceLabel @param {string} sourceURL @returns {HTMLElement} */
function buildAppCapture(imageSource, imageAlt, sourceLabel, sourceURL) {
  const figure = document.createElement('figure');
  figure.className = 'app-capture-content';
  const image = document.createElement('img');
  image.src = imageSource;
  image.alt = imageAlt;
  image.width = 1080;
  image.height = 2400;
  const caption = document.createElement('figcaption');
  caption.append(`${sourceLabel} — `);
  const sourceLink = document.createElement('a');
  sourceLink.href = sourceURL;
  sourceLink.target = '_blank';
  sourceLink.rel = 'noopener noreferrer';
  sourceLink.textContent = 'open app listing';
  caption.append(sourceLink);
  figure.append(image, caption);
  return figure;
}

/** @param {string} imageSource @param {string} imageAlt @param {string} sourceLabel @param {string} sourceURL @returns {HTMLElement} */
function buildHelpCapture(imageSource, imageAlt, sourceLabel, sourceURL) {
  const figure = document.createElement('figure');
  figure.className = 'help-capture-content';
  const image = document.createElement('img');
  image.src = imageSource;
  image.alt = imageAlt;
  image.width = 1440;
  image.height = 1000;
  const caption = document.createElement('figcaption');
  caption.append(`${sourceLabel} — `);
  const sourceLink = document.createElement('a');
  sourceLink.href = sourceURL;
  sourceLink.target = '_blank';
  sourceLink.rel = 'noopener noreferrer';
  sourceLink.textContent = 'open source';
  caption.append(sourceLink);
  figure.append(image, caption);
  return figure;
}

/** @param {string} action @param {string} label @returns {HTMLButtonElement} */
function actionButton(action, label) {
  const button = document.createElement('button');
  button.className = 'simulator-action';
  button.type = 'button';
  button.dataset.action = action;
  button.textContent = label;
  button.addEventListener('click', () => {
    currentStage = action === 'start-simulator' ? 1 : Math.min(currentStage + 1, stages.length - 1);
    renderSimulator();
  });
  return button;
}

/** @param {string} stage @returns {string} */
function simulatorTitle(stage) {
  return ({
    start: 'Open Google Authenticator on the old device',
    export: 'Create the export QR code',
    save: 'Save every QR screen',
    upload: 'Choose the screenshots',
    apple: 'Finish in Apple Passwords',
  })[stage] || 'Follow the walkthrough';
}

/** @param {string} stage @returns {string} */
function simulatorDescription(stage) {
  return ({
    start: 'This is the real Google Authenticator app. Continue to Menu, Transfer accounts, and Export accounts on the old device.',
    export: 'Choose Menu, Transfer accounts, Export accounts, select the accounts, and tap Next.',
    save: 'Google Authenticator can show more than one QR code. Save one screenshot for each screen.',
    upload: 'Choose all QR screenshots in the real converter below. It decodes them without sending the images away.',
    apple: 'Copy each setup key to the matching Passwords account. Keep the old authenticator entry until the new code works.',
  })[stage] || '';
}

export {};
