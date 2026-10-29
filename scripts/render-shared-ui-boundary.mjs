// @ts-check

import {readFile} from 'node:fs/promises';
import {resolve} from 'node:path';
import {createHash} from 'node:crypto';

const baseURL = new URL(process.argv[2]).origin;
const candidateRoot = process.env.DOWNLOAD_YOUR_DATA_MPR_UI_ROOT;
const assetNames = ['mpr-ui-config.js', 'mpr-ui.js', 'mpr-ui.css'];
let candidateAssets = null;
if (candidateRoot !== undefined) {
  if (!candidateRoot.trim()) {
    throw new Error('DOWNLOAD_YOUR_DATA_MPR_UI_ROOT must select a shared UI checkout');
  }
  candidateAssets = Object.fromEntries(await Promise.all(assetNames.map(async (name) => {
    const body = await readFile(resolve(candidateRoot, name));
    const digest = createHash('sha256').update(body).digest('hex');
    process.stderr.write(`Shared UI candidate ${name}: sha256:${digest}\n`);
    return [name, body.toString('base64')];
  })));
}
const template = await readFile(new URL('./shared-ui-boundary.js', import.meta.url), 'utf8');
process.stdout.write(template
  .replace('/* __SHARED_UI_CANDIDATE_ASSETS__ */ null', JSON.stringify(candidateAssets))
  .replaceAll('__BASE_URL__', baseURL));
