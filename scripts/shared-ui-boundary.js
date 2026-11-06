// @ts-check
// Inserted inside the native Playwright scenario before application navigation.
const sharedCandidateAssets = /* __SHARED_UI_CANDIDATE_ASSETS__ */ null;
if (sharedCandidateAssets !== null) {
  for (const [name, encoded] of Object.entries(sharedCandidateAssets)) {
    const body = await page.evaluate((encoded) => new TextDecoder().decode(
      Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))
    ), encoded);
    await page.route(
      `https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/${name}*`,
      (route) => route.fulfill({
        status: 200,
        contentType: name.endsWith('.css') ? 'text/css' : 'application/javascript',
        body
      })
    );
  }
}

await page.route('https://accounts.google.com/gsi/client', route => route.fulfill({
  contentType: 'application/javascript',
  body: `window.google={accounts:{id:{initialize(config){this.config=config},renderButton(host,options){if(options.type!=='icon'||options.size!=='small')throw new Error('Google fixture requires a small icon button');const button=document.createElement('button');button.dataset.fixtureGoogle='true';button.textContent='G';button.setAttribute('aria-label','Sign in with Google');Object.assign(button.style,{width:'30px',height:'30px',padding:'0',boxSizing:'border-box'});button.onclick=()=>{options.click_listener();this.config.callback({credential:'fixture-credential',state:options.state})};host.replaceChildren(button)},prompt(){},disableAutoSelect(){},cancel(){}}}}`
}));
await page.route('__BASE_URL__/auth/nonce', route => route.fulfill({json:{nonce:'fixture-nonce'}}));
