// @ts-check

const INSTRUCTION_LINK_HOSTS = Object.freeze({
  netflix: Object.freeze(['help.netflix.com']),
  openai: Object.freeze(['chatgpt.com']),
  facebook: Object.freeze(['accountscenter.facebook.com', 'www.facebook.com']),
  instagram: Object.freeze(['accountscenter.instagram.com', 'www.facebook.com']),
  whatsapp: Object.freeze(['faq.whatsapp.com']),
  threads: Object.freeze(['www.facebook.com']),
  linkedin: Object.freeze(['www.linkedin.com']),
  tiktok: Object.freeze(['support.tiktok.com']),
  x: Object.freeze(['x.com']),
  youtube: Object.freeze(['takeout.google.com']),
  google: Object.freeze(['takeout.google.com']),
  amazon: Object.freeze(['www.amazon.com'])
});

export function instructionLinkURL(providerID, href) {
  if (typeof href !== 'string') {
    throw new Error(`${providerID} instruction link must be a string`);
  }
  let target;
  try {
    target = new URL(href);
  } catch {
    throw new Error(`${providerID} instruction link must be an absolute URL`);
  }
  const allowedHosts = INSTRUCTION_LINK_HOSTS[providerID];
  if (
    target.protocol !== 'https:' ||
    target.username ||
    target.password ||
    !allowedHosts ||
    !allowedHosts.includes(target.hostname)
  ) {
    throw new Error(`${providerID} instruction link must use an approved first-party HTTPS host`);
  }
  return target;
}
