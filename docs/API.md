# VM automation API v1

The JSON API powers the embedded React UI. Existing users retain application-wide
resource permissions; RBAC is outside v1; infrastructure mutations use durable asynchronous tasks. The UI requires
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

All machine and volume operations live under `/api/v1/clouds/{cloud}`.
Create commands select catalog entries and an availability zone; the scheduler
selects the host. Machine URLs contain the stable `id` returned by discovery
(the libvirt domain UUID); `name` is a separate display field. Volume URLs retain
`{node}` because disks are host-local. Copy identities from discovery; they are
not placement inputs. Cloud membership is checked before reads, mutations and
console connections.

Machine lists return `{ "items": [...], "unavailable_hosts": [...] }`. Every list
request reads fresh backend state; failed hosts are omitted and explicitly named.
The routing index stores only IDs and locations, not machine state. Point reads
use the known host, or discover it after a miss. An unknown ID returns 503 if the
search is incomplete and 404 after all hosts confirm absence.

Volume IDs are opaque URL-safe identifiers returned by discovery; clients should
not construct or interpret them. SSH keys are application-wide and identified by
their fingerprint. Sizes are integer **bytes**. Enums are strings. Unknown fields
and invalid types are rejected. Paths below are relative to `/api/v1`.

| Resource | Operations |
| --- | --- |
| `/clouds`, `/clouds/{cloud}` | GET catalogs, compatible offers and placement capabilities |
| `/clouds/{cloud}/machines` | GET managed/unmanaged inventory, POST scheduled creation |
| `/clouds/{cloud}/machines/{vm}` | GET, DELETE |
| `/clouds/{cloud}/machines/{vm}/actions` | POST `start`, `reboot`, `poweroff` |
| `/clouds/{cloud}/machines/{vm}/instance-type` | POST explicit stopped, same-host type change |
| `/clouds/{cloud}/machines/{vm}/adopt` | POST explicit adoption |
| `/clouds/{cloud}/machines/{vm}/disks` | POST create and attach a disk using storage class and size |
| `/clouds/{cloud}/machines/{vm}/disks/attach` | POST attach an existing local volume |
| `/clouds/{cloud}/machines/{vm}/disks/{volume}` | DELETE attachment without deleting the disk |
| `/clouds/{cloud}/machines/{vm}/interfaces` | POST attach a configured logical network |
| `/clouds/{cloud}/machines/{vm}/interfaces/{mac}` | DELETE attachment |
| `/clouds/{cloud}/volumes` | GET visible volumes, POST scheduled creation |
| `/clouds/{cloud}/volumes/{node}/{volume}` | GET, DELETE |
| `/clouds/{cloud}/volumes/{node}/{volume}/clone` | POST host-local clone to a storage class |
| `/clouds/{cloud}/volumes/{node}/{volume}/resize` | POST explicit growth |
| `/hosts`, `/hosts/{node}` | GET administrative diagnostics, topology, NUMA memory and pins |
| `/hosts/{node}/networks`, `/hosts/{node}/pools` | GET backend diagnostics |
| `/hosts/{node}/images`, `/hosts/{node}/capabilities` | GET backend diagnostics, optional `arch` |
| `/hosts/{node}/catalog` | GET logical networks/storage classes supported on an observed host, plus its cloud and AZ |
| `/hosts/{node}/cloud` | GET configured cloud membership without a backend read |
| `/keys` | GET list, POST add a public key |
| `/keys/{fingerprint}` | GET, DELETE |

`GET /hosts/{node}/images` and `GET /hosts/{node}/capabilities` are intentional
API-only diagnostics. The bundled UI does not call them; the acceptance suite
exercises both routes. `GET /clouds/{cloud}` also remains available to API
clients and acceptance tests, while the UI fetches the full cloud catalog.

`GET /clouds/{cloud}/volumes` accepts `host` to filter by observed locality and
`usage=attachable` to exclude attached disks and image sources. Neither changes
placement. Standalone disk creation uses `name`, `zone`, `storage_class` and
`size_bytes`; it has no host or raw-pool input.

Interface attachment accepts `network` (a logical network ID) and optional `mac`.
NIC model and VLAN are selected by the administrator's host network binding;
`model`, `vlan` and other unsupported request fields are rejected. Observed
interface responses retain `model` and `vlan` for diagnostics.

The separate `/hosts` API is **read-only**. It cannot create, update, adopt or
delete infrastructure. Legacy `/nodes/...`, `/vms`, `/volumes` and `/networks`
endpoints are removed; there are no compatibility aliases. Clients aggregating
multiple clouds enumerate `/clouds` and query each cloud's collections.

Infrastructure mutations return **202 Accepted** with a task and `status_url`,
and require an `Idempotency-Key` header. Reads return 200. Key creation and
console link issuance remain synchronous (201); key deletion returns 204.
Use authenticated `GET /api/v1/tasks/{id}` to observe execution and
`GET /api/v1/tasks` for persistent paginated history. See [task contracts,
completion, restart, authorization and retention](TASKS.md).

`DELETE` is explicit destructive intent; deleting a VM keeps its disks unless
`?delete_volumes=true` is supplied. Protected or attached volumes cannot be
deleted, protected volumes cannot be resized, and resizing cannot shrink a disk.
`poweroff` is a **hard power-off**, not graceful shutdown. Configuration changes
are persistent and may require stopping and starting a VM.

Machine policy has no PATCH endpoint. Images own `guest_agent`, `graphics` and
`video_model`; instance types own `autostart`, CPU, RAM and hugepages. The private
cloud host owns `graphics_listen`. Creation accepts catalog references,
not per-machine policy overrides. The explicit instance-type command applies its
autostart policy without starting or stopping the VM and preserves applied guest
devices, graphics listener and disk sizes. `start` on creation is a separate
one-time action. Attachment changes have dedicated routes. Unknown fields and invalid requests are rejected
before acceptance with field errors where available. Dynamic backend conditions
are rechecked during execution where appropriate and can fail an accepted task.

Errors before acceptance use `application/problem+json`: 400 malformed input,
401 authentication, 403 origin/transport, 404 missing resource, 409 conflict,
422 validation, 503 backend/storage unavailable, and 500 other failures.
Conflicting mutations return 409 and the blocking `task_id`/`task_url`.
Reuse of an idempotency key with a different command returns 409. Reuse with the
same command returns the original task even after failure or interruption.

After acceptance, inspect the task's stages, confirmed effects and sanitized
backend error, including libvirt message/code/domain. Browser disconnection does
not cancel execution. Do not automatically resubmit failed/interrupted work;
inspect its outcome first. No rollback is performed on hook or start failure.

## Create a VM

Administrators first prepare the hypervisors and configure the cloud catalog,
image requirements and private host bindings; see [configured clouds](CLOUDS.md)
and the [image guide](content/en/docs/images.md). Users discover a cloud's catalog
with `GET /api/v1/clouds` or `GET /api/v1/clouds/{cloud}` and select compatible
image, instance type, zone, logical network and storage class IDs. The scheduler
chooses the host; `/hosts/...` discovery is administrative diagnostics, not the
source of creation inputs.

Creation uses the same domain manager as the UI for image requirements, resource
admission, image preparation, config drives and event hooks. There is no `defaults`
input or `defaults.initialization` override. To offer a machine without cloud-init,
an administrator defines an image entry with `initialization = "none"`; the user
selects that image ID. User-data is literal text for images with guest initialization;
optional `keys` contains fingerprints returned by `/api/v1/keys`.

```sh
curl --config ~/.config/vmango/curl.conf https://vmango.example/api/v1/clouds/local
curl --config ~/.config/vmango/curl.conf -H "Idempotency-Key: demo-create-001" --json @vm.json https://vmango.example/api/v1/clouds/local/machines
```

The protected curl configuration (mode 0600) contains
`header = "Authorization: Bearer YOUR_PERSONAL_JWT"`. Do not commit it.

Example `vm.json` (replace the image, instance type, zone, storage class and network with discovered values):

```json
{
  "name": "demo",
  "image": "ubuntu-24.04-x86_64-bios",
  "instance_type": "small",
  "zone": "local",
  "storage_class": "standard",
  "network": "lan",
  "start": true,
  "user_data": "#cloud-config\npackages: [curl]\n"
}
```

The response is 202 Accepted with a task and `status_url`. Poll that authenticated
URL to confirm completion; acceptance alone does not mean the VM exists or has
started. `start` requests a one-time start after provisioning, independently of
the instance type's `autostart` policy.

See [configured clouds](CLOUDS.md) for placement, local disk ownership, adoption
and API migration rules. Binary archives include this reference as `API.md`,
alongside `CLOUDS.md`, `IMAGES.md`, `TASKS.md` and `MIGRATION.md`; in an extracted
archive use `IMAGES.md` for the image guide.

## Consoles

The full stream protocol is also served at `/api/console-protocol` and included
in OpenAPI. Native clients send Bearer JWTs on the WebSocket handshake:

`/api/v1/clouds/{cloud}/machines/{vm}/consoles/{serial|vnc}/ws`

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
  -url wss://vmango.example/api/v1/clouds/local/machines/550e8400-e29b-41d4-a716-446655440000/consoles/serial/ws

go run -mod=vendor ./examples/console -token-file ~/.config/vmango/token -listen 127.0.0.1:5901 \
  -url wss://vmango.example/api/v1/clouds/local/machines/550e8400-e29b-41d4-a716-446655440000/consoles/vnc/ws
```

For VNC, connect a native viewer to the local forwarding port. Backend graphics
ports and hypervisor credentials remain behind Vmango. The client forwards one
viewer connection and exits when it disconnects.

### Reusable browser links

```sh
curl --config ~/.config/vmango/web-curl.conf --json '{}' \
  https://vmango.example/api/v1/clouds/local/machines/550e8400-e29b-41d4-a716-446655440000/consoles/vnc/links
```

This operation requires a short-lived interactive web JWT; a personal JWT
cannot issue an independent sharing credential. The protected curl configuration
in this example must contain that interactive Bearer header and have mode 0600. The browser Console
page provides the same issuance/sharing flow.

The response contains `url` and `expires_at`. Serial issuance accepts
`{"force":true}` to authorize explicit takeover for connections using that link.
Links use HS256 JWTs with a distinct `vmango-console` audience,
binding purpose, issuing user, cloud, machine UUID, console type, force permission and
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
on 403, replay an uncertain mutation with a new idempotency key, or treat a 503 auth-store error
as revoked credentials. Refresh/bootstrap 401 requires login.
