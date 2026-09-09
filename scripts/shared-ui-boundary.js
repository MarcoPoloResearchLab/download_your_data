// @ts-check
// Inserted inside the native Playwright scenario before application navigation.
await page.goto('__BASE_URL__/api/health');
const sharedCandidate = 'dd5bff9fdaf0e2c989624f9f6f75d3c55e460866';
const sharedDigests = {
  'mpr-ui-config.js': '3f56fbd212a516d2bd8b0b95f73ae7ad82952c10d8d5f4e6f8b44d3233f01304',
  'mpr-ui.js': 'a70b83442bc9693aec0421db0ef6dc6dba5654ff6f1497937eef4a2f321c4fd0',
  'mpr-ui.css': '31b92536df3a1584b7f19ac50eb61d6c7aff7c710ee92b84c46835194849e816'
};
for (const [name, expectedDigest] of Object.entries(sharedDigests)) {
  const response = await page.request.get(
    `https://raw.githubusercontent.com/MarcoPoloResearchLab/mpr-ui/${sharedCandidate}/${name}`
  );
  if (!response.ok()) throw new Error(`candidate ${name}: HTTP ${response.status()}`);
  const body = await response.body();
  const digest = await page.evaluate(async (bytes) => {
    const result = await crypto.subtle.digest('SHA-256', new Uint8Array(bytes));
    return Array.from(new Uint8Array(result), (byte) => byte.toString(16).padStart(2, '0')).join('');
  }, Array.from(body));
  if (digest !== expectedDigest) throw new Error(`candidate ${name}: digest mismatch`);
  await page.route(
    `https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/${name}*`,
    (route) => route.fulfill({
      status: 200,
      contentType: name.endsWith('.css') ? 'text/css' : 'application/javascript',
      body
    })
  );
}

await page.route('https://accounts.google.com/gsi/client', route => route.fulfill({
  contentType: 'application/javascript',
  body: `window.google={accounts:{id:{initialize(config){this.config=config},renderButton(host,options){const button=document.createElement('button');button.dataset.fixtureGoogle='true';button.textContent='Google';button.onclick=()=>{options.click_listener();this.config.callback({credential:'fixture-credential',state:options.state})};host.replaceChildren(button)},prompt(){},disableAutoSelect(){},cancel(){}}}}`
}));
await page.route('__BASE_URL__/auth/nonce', route => route.fulfill({json:{nonce:'fixture-nonce'}}));
