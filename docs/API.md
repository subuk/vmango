# VM automation API v1

The JSON API powers the embedded React UI. Existing users retain application-wide
resource permissions; RBAC and background jobs are outside v1. The UI requires
the API; the legacy `web` block and `api_disabled=true` fail with a migration
instruction. See [upgrade and authentication migration](UI-MIGRATION.md).

## Authentication and transport

Protected resource requests and native console handshakes require
`Authorization: Bearer ACCESS_JWT`. Basic Auth, legacy UI session cookies,
refresh cookies and console-only JWTs are rejected at resource endpoints.
Missing/invalid/expired/revoked credentials return JSON 401 with a Bearer
challenge. Authentication failures never redirect API clients to HTML login.

Browser sessions use a 10-minute in-memory web JWT and a rotating HttpOnly
refresh cookie with a 7-day absolute session lifetime. CLI/CI clients use named
personal API JWTs issued in the account menu (90-day default, 365-day maximum).
They need neither a login password nor a refresh-cookie jar. Store credentials
in a protected client file/keychain or CI secret; never put them in URLs or logs.

| Endpoint | Credential and behavior |
| --- | --- |
| `GET /api/v1/auth/config` | Public minimal password/OIDC capabilities |
| `POST /api/v1/auth/login` | Local username/password JSON; refresh cookie and web JWT |
| `POST /api/v1/auth/refresh` | Rotate refresh cookie; issue a new web JWT |
| `POST /api/v1/auth/logout` | Revoke refresh and clear cookie; repeatable after access expiry |
| `GET /api/v1/me` | Bearer; stable identity/version/navigation |
| `GET /api/v1/tokens` | Interactive web JWT; metadata owned by the current user |
| `POST /api/v1/tokens` | Interactive web JWT; name and days (1–365, default 90); return JWT once |
| `DELETE /api/v1/tokens/{id}` | Interactive web JWT; idempotent owner-isolated revocation |

Resource authentication selects the verified signed `token_kind=web|personal`
profile. Web JWTs are verified without session/token-store reads. Every personal
JWT request/native handshake reads its current disk record; missing or revoked
records deny access immediately. No application authorization cache is loaded.
Personal credentials cannot mint/manage tokens, refresh browser sessions or
issue independent console links. Logout does not revoke personal tokens.
API revocation closes active personal-token console streams, but does not undo
already accepted mutations. Web JWTs survive logout until their short expiry.

Refresh rotation persists before returning credentials. Authentic rotated-token
reuse revokes its family. Losing a successful rotation response requires login
again; there is no reuse grace window. Storage corruption/I/O failure yields a
503 service error, not a claim that the credential was revoked. The UI retains
its context on a temporary refresh failure. See the migration guide for identity,
key rotation, storage permissions, locking, cleanup and backup semantics.

Remote clients must use HTTPS/WSS. HTTP/WS is allowed for loopback development.
For TLS termination, set `api.base_url` to the public HTTPS origin and
`api.session_secure=true`, preserve Host/Origin/Sec-Fetch-Site, and keep the
unencrypted backend inaccessible to untrusted clients. Arbitrary forwarded
headers are not trusted. Cross-origin resource requests and WS upgrades are
rejected. The OIDC callback is a state/PKCE-validated cross-site GET.

Interactive Swagger UI is at `/api/docs`; its static shell can load publicly,
then uses browser login before fetching `/api/openapi.json` or protocol data
with Bearer authentication. Assets/licenses are bundled locally, with no CDN or
external validator. Tokens are not persisted in Swagger storage. The reviewed
snapshot is `api/testdata/openapi.json`; `make test` checks it. After intentional
contract changes run `make api-types`, review the snapshot/type diff and run
`make ui-test`. CI validates OpenAPI 3.1 and generated TypeScript drift.

## Resource contract

All node resources live under `/api/v1/nodes/{node}`. VM names are scoped to the
node. Volume IDs are opaque URL-safe identifiers returned by volume discovery;
clients should not construct or interpret them. SSH keys are application-wide
and identified by their returned fingerprint. Sizes are integer **bytes**.
Enums are strings. Unknown fields and invalid types are rejected.

| Resource | Operations |
| --- | --- |
| `/nodes` | GET list |
| `/vms`, `/volumes`, `/networks` | GET across all nodes; optional repeated `node` filters and volume `pool` filter |
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
| `/nodes/{node}/volumes` | GET list (optional `pool`, `usage=attachable`), POST create |
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
curl --config ~/.config/vmango/curl.conf https://vmango.example/api/v1/nodes/local/images
curl --config ~/.config/vmango/curl.conf --json @vm.json https://vmango.example/api/v1/nodes/local/vms
```

The protected curl configuration (mode 0600) contains
`header = "Authorization: Bearer YOUR_PERSONAL_JWT"`. Do not commit it.

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
in OpenAPI. Native clients send Bearer JWTs on the WebSocket handshake:

`/api/v1/nodes/{node}/vms/{vm}/consoles/{serial|vnc}/ws`

Serial bytes and VNC/RFB bytes use **binary** WebSocket messages. Message boundaries
are not byte-stream boundaries. Maximum message size is 1 MiB; text messages close
with code 1003. Backend EOF or either peer disconnecting closes both transports.
A stalled WebSocket writer has a 15-second deadline. Validate authentication,
resource state and backend stream availability before upgrade where possible.

Serial refuses an occupied console with 409. `?force=true` explicitly displaces
an existing client. Browser serial takeover requires an explicit confirmation
and a scoped link carrying force permission. VNC browser
clients request shared mode; actual sharing is controlled by the backend.

Run the native example with a protected file containing your personal JWT:

```sh
go run -mod=vendor ./examples/console -token-file ~/.config/vmango/token \
  -url wss://vmango.example/api/v1/nodes/local/vms/demo/consoles/serial/ws

go run -mod=vendor ./examples/console -token-file ~/.config/vmango/token -listen 127.0.0.1:5901 \
  -url wss://vmango.example/api/v1/nodes/local/vms/demo/consoles/vnc/ws
```

For VNC, connect a native viewer to the local forwarding port. Backend graphics
ports and hypervisor credentials remain behind Vmango. The client forwards one
viewer connection and exits when it disconnects.

### Reusable browser links

```sh
curl --token-file ~/.config/vmango/token --json '{}' \
  https://vmango.example/api/v1/nodes/local/vms/demo/consoles/vnc/links
```

This operation requires a short-lived interactive web JWT; a personal JWT
cannot issue an independent sharing credential. The protected curl configuration
in this example supplies that interactive Bearer header. The browser Console
page provides the same issuance/sharing flow.

The response contains `url` and `expires_at`. Serial issuance accepts
`{"force":true}` to authorize explicit takeover for connections using that link.
Links use HS256 JWTs with a distinct `vmango-console` audience,
binding purpose, issuing user, node, VM, console type, force permission and
absolute expiry. The payload is not encrypted and contains no account password.
A link grants only console access; the public page is an empty client shell.

The token is in the URL fragment. JavaScript sends it only in the
`Sec-WebSocket-Protocol` header as `vmango.token.TOKEN`, alongside `binary`.
The server selects only `binary`. It is never a general API JWT or a query
parameter. The public shell does not require resource login. noVNC is pinned to
a same-origin console endpoint regardless of URL parameters or saved settings.

`api.console_link_lifetime_seconds` defaults to 3600 (allowed 1..2592000).
Links may be reused until their absolute expiration; existing streams end then
(code 1008 when deliverable). Logout does not revoke independent sharing links.
Native access-JWT streams end at access expiry; reconnect with a valid token.
API revocation additionally closes personal-token streams. Manual file removal
only guarantees rejection at the next authentication check.

Set `api.console_signing_key` to a separate persistent random secret of at least
32 bytes (`openssl rand -hex 32`). It must differ from `api.jwt_signing_key`.
There is no legacy session-secret fallback. Restarting with the same key
preserves links; changing it rejects subsequent handshakes. Already active
sharing streams retain their original expiry. Individual link revocation and
multi-process/shared storage are outside this version.

### Reverse proxy logging

All authenticated API and link responses use `Cache-Control: no-store`; console pages also use
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


Authentication Problem Details include stable `code` values:
`access_token_expired`, `invalid_credential`, `credential_forbidden`,
`auth_unavailable`, `credential_limit`, `origin_rejected` and transport/OIDC
specific codes. Resource-guard 401 responses include
`rejected_before_execution=true`. Only `access_token_expired` with that proof
permits one coordinated refresh and mutation replay. Never refresh automatically
on 403, replay a timeout/5xx/uncertain mutation, or treat a 503 auth-store error
as revoked credentials. Refresh/bootstrap 401 requires login.
