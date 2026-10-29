# VM automation API v1

The JSON API is enabled alongside the UI. Set `web.api_disabled = true` to
turn off API, documentation and signed console routes. Existing users retain
application-wide permissions. There is no RBAC or background job API.

## Authentication and transport

Use HTTP Basic with a configured local username and password. The existing
bcrypt hashes are shared with form login. UI cookies, OIDC credentials and
console tokens do not authorize the API. OIDC-only accounts need a configured
local password before using Basic Auth. Missing or invalid credentials return
401 with a Basic challenge; API failures never redirect to login.

Remote clients must use HTTPS/WSS. HTTP/WS is allowed only when both the peer
and requested host are loopback (or the host is `localhost`). For TLS termination,
set `web.base_url` to the external HTTPS origin and prevent clients from reaching
the unencrypted backend directly. Vmango does not trust arbitrary forwarded
headers. Cross-origin browser requests, including authenticated GET and WebSocket
upgrades, are rejected. Console pages cannot be framed.

Interactive Swagger UI: `/api/docs`. OpenAPI 3.1: `/api/openapi.json`.
Both require Basic Auth. Swagger UI and its licenses are bundled locally; no CDN
or external specification validator is used. All REST schemas come from the same
Go types that validate requests. The reviewed snapshot is
`web/testdata/openapi.json`; `make test` checks it. After intentional contract
changes, inspect `UPDATE_API_SPEC=1 make test` and the resulting diff. CI also
validates the snapshot against OpenAPI 3.1 using openapi-spec-validator 0.7.2.

## Resource contract

All node resources live under `/api/v1/nodes/{node}`. VM names are scoped to the
node. Volume IDs are opaque URL-safe identifiers returned by volume discovery;
clients should not construct or interpret them. SSH keys are application-wide
and identified by their returned fingerprint. Sizes are integer **bytes**.
Enums are strings. Unknown fields and invalid types are rejected.

| Resource | Operations |
| --- | --- |
| `/nodes` | GET list |
| `/nodes/{node}` | GET node, CPU topology, NUMA memory and pins |
| `/nodes/{node}/networks` | GET list |
| `/nodes/{node}/pools` | GET list |
| `/nodes/{node}/images` | GET available images, optional `arch` |
| `/nodes/{node}/capabilities` | GET supported settings, optional `arch` |
| `/nodes/{node}/vms` | GET list, POST create |
| `/nodes/{node}/vms/{vm}` | GET, PATCH configuration, DELETE |
| `/nodes/{node}/vms/{vm}/actions` | POST `start`, `reboot`, `poweroff` |
| `/nodes/{node}/vms/{vm}/disks` | POST attach existing volume |
| `/nodes/{node}/vms/{vm}/disks/{volume}` | DELETE attachment |
| `/nodes/{node}/vms/{vm}/interfaces` | POST attach network interface |
| `/nodes/{node}/vms/{vm}/interfaces/{mac}` | DELETE attachment |
| `/nodes/{node}/volumes` | GET list (optional `pool`), POST create |
| `/nodes/{node}/volumes/{volume}` | GET, DELETE |
| `/nodes/{node}/volumes/{volume}/clone` | POST clone |
| `/nodes/{node}/volumes/{volume}/resize` | POST grow |
| `/keys` | GET list, POST add public key |
| `/keys/{fingerprint}` | GET, DELETE |

Creation returns 201 and the resource. Reads and PATCH return 200. Actions and
deletes return 204. `DELETE` is explicit destructive intent; deleting a VM keeps
its disks unless `?delete_volumes=true` is supplied. Protected or attached volumes
cannot be deleted, protected volumes cannot be resized, and resizing cannot
shrink a disk. `poweroff` calls libvirt Destroy: it is a **hard power-off**, not a
graceful shutdown. Configuration changes affect persistent domain settings and
may require a restart, as with the UI.

PATCH preserves omitted fields; optional null fields are also treated as omitted.
False and zero are distinct from omission (zero vCPUs/memory are rejected).
Devices are changed through dedicated attachment routes. Repeating creation of
an existing VM or volume returns 409. There is no `Idempotency-Key` support in v1.

Operations run synchronously until the domain/backend operation returns. The
request body has a 2 MiB limit and Huma's body-read timeout; there is no added
operation timeout that would imply cancellation of a blocking libvirt call.
Client/proxy timeouts and disconnects do **not** imply cancellation or rollback.
Do not retry a mutation automatically after an ambiguous timeout; discover the
current state first. Configure proxy read timeouts for long disk cloning and
cloud-init provisioning. Concurrent updates have no revision preconditions.

Errors use `application/problem+json`: 400 malformed input, 401 authentication,
403 origin/transport, 404 missing resource, 409 conflict, 422 validation,
503 unavailable backend, and 500 other failures. A failed VM provisioning request
returns 500 with `provisioning.stage`, `node`, `vm`, `vm_created`, `volumes` and
`outcome_uncertain`. The volume list contains opaque volume IDs. These describe confirmed successful steps; the failed backend
call may also have changed state. No automatic rollback is performed, including
on event-hook or start failure. Inspect the VM and volumes before retrying or
removing resources. Error bodies do not include cloud-init user-data or credentials.

## Create a VM

Discover the node's images, pools, networks and capabilities first. Image defaults,
minimum RAM/disk sizes, config-drive creation and event hooks use the same domain
manager as the UI. `defaults` overrides image settings explicitly. Set
`defaults.initialization` to `none` to disable cloud-init. User-data is literal
text; key fingerprints refer to `/api/v1/keys`.

```sh
curl --user admin https://vmango.example/api/v1/nodes/local/images
curl --user admin --json @vm.json https://vmango.example/api/v1/nodes/local/vms
```

Example `vm.json` (replace the image, pool and network with discovered values):

```json
{
  "id": "demo",
  "vcpus": 2,
  "memory_bytes": 2147483648,
  "start": true,
  "graphics": "vnc",
  "clone_disks": [{"image_id": "ubuntu", "name": "demo-root", "pool": "default"}],
  "interfaces": [{"network": "default"}],
  "cloud_init": {"user_data": "#cloud-config\npackages: [curl]\n"}
}
```

## Consoles

The full stream protocol is also served at `/api/console-protocol` and included
in OpenAPI. Native clients connect with Basic Auth on the WebSocket handshake:

`/api/v1/nodes/{node}/vms/{vm}/consoles/{serial|vnc}/ws`

Serial bytes and VNC/RFB bytes use **binary** WebSocket messages. Message boundaries
are not byte-stream boundaries. Maximum message size is 1 MiB; text messages close
with code 1003. Backend EOF or either peer disconnecting closes both transports.
A stalled WebSocket writer has a 15-second deadline. Validate authentication,
resource state and backend stream availability before upgrade where possible.

Serial refuses an occupied console with 409. `?force=true` explicitly displaces
an existing client. UI serial connections use the same default; append
`?force=true` to the UI WebSocket route only when takeover is intended. VNC browser
clients request shared mode; actual sharing is controlled by the backend.

Run the included native client from the source checkout (password is prompted,
or read from `VMANGO_PASSWORD`):

```sh
go run -mod=vendor ./examples/console -user admin \
  -url wss://vmango.example/api/v1/nodes/local/vms/demo/consoles/serial/ws

go run -mod=vendor ./examples/console -user admin -listen 127.0.0.1:5901 \
  -url wss://vmango.example/api/v1/nodes/local/vms/demo/consoles/vnc/ws
```

For VNC, connect a native viewer to the local forwarding port. Backend graphics
ports and hypervisor credentials remain behind Vmango. The client forwards one
viewer connection and exits when it disconnects.

### Reusable browser links

```sh
curl --user admin --json '{}' \
  https://vmango.example/api/v1/nodes/local/vms/demo/consoles/vnc/links
```

The response contains `url` and `expires_at`. Serial issuance accepts
`{"force":true}` to authorize explicit takeover for connections using that link.
Links use a versioned base64url JSON payload authenticated with HMAC-SHA256,
binding purpose, issuing user, node, VM, console type, force permission and
absolute expiry. The payload is not encrypted and contains no account password.
A link grants only console access; the public page is an empty client shell.

The token is in the URL fragment and is removed from the address bar after page
load. JavaScript retains it in page memory and sends it in the
`Sec-WebSocket-Protocol` header as `vmango.token.TOKEN`, alongside `binary`.
The server selects only `binary`. The browser's Reconnect button retains the
credential; reloading the cleaned URL requires reopening the original link.

`web.console_link_lifetime_seconds` defaults to 3600 (allowed 1..2592000).
Links may be reused until their original expiration. Existing streams terminate
at expiry, with close code 1008 when deliverable. Reconnecting never renews the
expiry. Individual link revocation is outside v1.

Set `web.console_signing_key` to a persistent random secret of at least 32 bytes
(e.g. the output of `openssl rand -hex 32`). Keep it outside source control.
Without a separate key, Vmango derives a purpose-separated HMAC key from
`session_secret`, which must then be at least 32 bytes; otherwise link issuance
returns 503. Do not use the example `changeme` session secret. Restarting with the
same key preserves links. Rotating the key rejects subsequent connections; active
streams retain their original expiration. Run all serving instances with the
same persistent signing key if requests can be distributed across them.

### Reverse proxy logging

All API and link responses use `Cache-Control: no-store`; console pages also use
`Referrer-Policy: no-referrer`. Keep `Authorization`, `Cookie`,
`Sec-WebSocket-Protocol`, request/response bodies and URL fragments out of proxy,
tracing and application logs. Do not enable request-header debugging on console
routes. A minimal nginx access format logs only the path:

```nginx
log_format vmango '$remote_addr $request_method $uri $status';

location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $http_host;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
    access_log /var/log/nginx/vmango.log vmango;
}
```

Terminate TLS on this server, configure Vmango's external HTTPS `base_url`, and
restrict backend network access. Do not add credential headers to the log format.
