import RFB from './noVNC-1.7.0/core/rfb.js';

const token = location.hash.slice(1);
// Retain the credential only in this page's memory after initial navigation.
history.replaceState(null, '', location.pathname);
const screen = document.getElementById('screen');
const status = document.getElementById('status');
const endpoint = new URL(location.pathname + '/ws', location.origin);
endpoint.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
const protocols = ['binary', 'vmango.token.' + token];
const serial = location.pathname.endsWith('/serial');
let connection;
let terminal;
function connect() {
    if (!token) { status.textContent = 'Missing console link credential.'; return; }
    if (connection) { serial ? connection.close() : connection.disconnect(); }
    if (terminal) terminal.dispose();
    screen.replaceChildren();
    status.textContent = 'Connecting…';
    if (serial) {
        terminal = new Terminal({cols: 117, rows: 35, cursorBlink: true});
        terminal.open(screen);
        const ws = new WebSocket(endpoint, protocols);
        connection = ws;
        ws.binaryType = 'arraybuffer';
        const decoder = new TextDecoder();
        ws.onopen = () => { if (connection !== ws) return; status.textContent = 'Connected'; terminal.focus(); };
        ws.onmessage = event => { if (connection === ws) terminal.write(decoder.decode(event.data, {stream: true})); };
        ws.onclose = () => { if (connection !== ws) return; status.textContent = 'Disconnected. The link may have expired or the console may be busy.'; };
        ws.onerror = () => { if (connection !== ws) return; status.textContent = 'Connection failed. Check link expiry and VM state.'; };
        terminal.onData( data => { if (ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(data)); });
    } else {
        const rfb = new RFB(screen, endpoint.href, {wsProtocols: protocols, shared: true});
        connection = rfb;
        rfb.scaleViewport = true;
        rfb.addEventListener('connect', () => { if (connection !== rfb) return; status.textContent = 'Connected'; });
        rfb.addEventListener('disconnect', () => { if (connection !== rfb) return; status.textContent = 'Disconnected. Reconnect before the link expires.'; });
        rfb.addEventListener('credentialsrequired', () => { status.textContent = 'The backend requires VNC credentials; this console is unavailable.'; rfb.disconnect(); });
    }
}
document.getElementById('reconnect').addEventListener('click', connect);
connect();
