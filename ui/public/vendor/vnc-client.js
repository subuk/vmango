import UI from './noVNC-1.7.0/app/ui.js';

let started = false;
window.addEventListener('message', async (event) => {
  if (event.origin !== location.origin || event.source !== parent || event.data?.type !== 'vmango-console') return;
  const data = event.data;
  const theme = data.theme === 'dark' ? 'dark' : 'light';
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
  if (started) return;
  if (typeof data.cloud !== 'string' || typeof data.vm !== 'string' || typeof data.token !== 'string' || !data.token || data.token.length > 8192) return;
  started = true;
  // No query, fragment, localStorage setting or parent-provided URL chooses a host.
  const path = '/console/' + encodeURIComponent(data.cloud) + '/' + encodeURIComponent(data.vm) + '/vnc/ws';
  UI.vmangoProtocols = ['binary', 'vmango.token.' + data.token];
  await UI.start({ settings: { mandatory: { host: '', port: '', path, encrypt: location.protocol === 'https:', password: '', repeaterID: '', shared: true, autoconnect: true }, defaults: { resize: 'scale' } } });
});
parent.postMessage({ type: 'vmango-console-ready' }, location.origin);
