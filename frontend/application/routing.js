// @ts-check

export const WORKSPACE_PROVIDER_IDS = Object.freeze(['netflix', 'openai']);
export const GUIDE_ONLY_PROVIDER_IDS = Object.freeze([
  'facebook',
  'instagram',
  'whatsapp',
  'threads',
  'linkedin',
  'tiktok',
  'x',
  'youtube',
  'google',
  'amazon'
]);

export function parseRoute() {
  const raw = window.location.hash.replace(/^#/, '');
  if (raw.startsWith('app/')) {
    const provider = raw.slice('app/'.length);
    if (WORKSPACE_PROVIDER_IDS.includes(provider)) {
      return {name: provider};
    }
  }
  if (raw === 'credits') {
    return {name: 'credits'};
  }
  if (raw.startsWith('guide/')) {
    const provider = raw.slice('guide/'.length);
    if (
      WORKSPACE_PROVIDER_IDS.includes(provider) ||
      GUIDE_ONLY_PROVIDER_IDS.includes(provider)
    ) {
      return {name: 'guide', provider};
    }
  }
  return {name: 'catalog'};
}

export function navigate(route, provider = '') {
  if (WORKSPACE_PROVIDER_IDS.includes(route)) {
    window.location.hash = `app/${route}`;
  } else if (route === 'guide') {
    window.location.hash = `guide/${provider}`;
  } else if (route === 'credits') {
    window.location.hash = 'credits';
  } else {
    window.location.hash = 'catalog';
  }
}

export function isWorkspaceRoute(route) {
  return WORKSPACE_PROVIDER_IDS.includes(route.name);
}
