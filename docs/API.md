# VM automation API v1

The JSON API powers the embedded React UI. Authenticated operators retain their
existing application-wide resource authority, including Administration, until
namespace/RBAC support is introduced separately. Existing credential-kind,
transport and origin checks still apply. Infrastructure mutations use durable
asynchronous tasks. The UI requires
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
(the libvirt domain UUID); `name` is a separate display field. Volume URLs use
one compound identifier without a separate host segment. Copy IDs from returned
resources; they identify existing addresses rather than select placement. Cloud
membership is checked before reads, mutations and console connections.

Machine and volume lists return `{ "items": [...], "unavailable_hosts": [...] }`
for the requested cloud, with arrays even when empty. Successful inventory is
fresh; failed hosts are omitted and explicitly named. A partial or completely
unavailable scan returns 200 with the successful items and failed host IDs. Only
empty items with no unavailable hosts confirm an empty collection. The machine
routing index stores only IDs and locations, not machine state. Machine point
reads use the known host, or discover it after a miss. An unknown machine ID
returns 503 if the search is incomplete and 404 after all hosts confirm absence.

For libvirt, a volume's public ID is `base64url(node) + "." + base64url(path)`,
with both components unpadded. This reversible address encoding distinguishes
identical paths on different hosts; it does not replace backend identity or
promise immutability after replacement at the same address. Exactly two nonempty
canonical components are required, with at most 4096 decoded path bytes.
Malformed IDs, including the former path-only IDs, return 422. A decoded host
outside the requested cloud returns 404 before storage access; a missing volume
returns 404 and an unavailable backend returns 503.

SSH keys remain application-wide and identified by their fingerprint. Sizes are
integer **bytes**. Enums are strings. Unknown fields and invalid types are
rejected. No namespace selection or workspace headers are introduced here.
Paths below are relative to `/api/v1`.

| Resource | Operations |
| --- | --- |
| `/clouds`, `/clouds/{cloud}` | GET catalogs, compatible offers and placement capabilities |
| `/clouds/{cloud}/machines` | GET managed/unmanaged inventory, POST scheduled creation |
| `/clouds/{cloud}/machines/{vm}` | GET, DELETE |
| `/clouds/{cloud}/machines/{vm}/actions` | POST `start`, `reboot`, `shutdown`, `poweroff` |
| `/clouds/{cloud}/machines/{vm}/instance-type` | POST explicit stopped, same-host type change |
| `/clouds/{cloud}/machines/{vm}/adopt` | POST explicit adoption |
| `/clouds/{cloud}/machines/{vm}/disks` | POST create and attach a disk using storage class and size |
| `/clouds/{cloud}/machines/{vm}/disks/attach` | POST attach an existing local volume |
| `/clouds/{cloud}/machines/{vm}/disks/{volume}` | DELETE attachment without deleting the disk |
| `/clouds/{cloud}/machines/{vm}/interfaces` | POST attach a configured logical network |
| `/clouds/{cloud}/machines/{vm}/interfaces/{mac}` | DELETE attachment |
| `/clouds/{cloud}/volumes` | GET visible volumes, POST scheduled creation |
| `/clouds/{cloud}/volumes/{volume}` | GET, DELETE |
| `/clouds/{cloud}/volumes/{volume}/clone` | POST host-local clone to a storage class |
| `/clouds/{cloud}/volumes/{volume}/resize` | POST explicit growth |
| `/keys` | GET list, POST add a public key |
| `/keys/{fingerprint}` | GET, DELETE |

`GET /clouds/{cloud}/volumes` accepts `host` to filter by observed locality and
`usage=attachable` to exclude attached disks and image sources. Neither changes
placement. Standalone disk creation uses `name`, `zone`, `storage_class` and
`size_bytes`; it has no host or raw-pool input.

Interface attachment accepts `network` (a logical network ID) and optional `mac`.
NIC model and VLAN are selected by the administrator's host network binding;
`model`, `vlan` and other unsupported request fields are rejected. Observed
interface responses retain `model` and `vlan` for diagnostics.

All former `/hosts...` API routes, host-segment volume routes and legacy
`/nodes/...`, `/vms`, `/volumes` and `/networks` endpoints are removed. Authenticated
calls receive 404, with no redirect or compatibility alias. Dedicated host
configuration and diagnostics now belong to cloud-centered Administration.
Clients that intentionally aggregate multiple clouds can query each cloud's
collection; the bundled Volumes page requests only its selected cloud.

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

## Resource detail catalogs and disk references

Machine detail includes `catalog: {networks: [{id, name}], storage_classes:
[{id, name}]}` for the machine's current placement. Volume detail includes
`catalog: {storage_classes: [{id, name}]}` for local clone destinations, including
source constraints. Cloud and zone remain fields of the resource. Lists do not
discover a catalog for each row. If a resource is readable but supplemental
choice discovery fails, its detail still returns 200 with `catalog: null`.
Empty arrays mean discovery succeeded with no choices. Refresh the same detail
to retry; there is no separate workload catalog endpoint. Command acceptance and
execution still validate current conditions independently.

The same compound ID appears in volume `id`, VM `disks[].volume_id`, attachment
bodies and detachment URLs. Existing-disk creation accepts only the public ID:

```json
{
  "existing_disks": [
    { "volume_id": "aG9zdC1h.L3Bvb2wvZGF0YS5xY293Mg" }
  ]
}
```

This fragment refers to `host-a` and `/pool/data.qcow2`; include the other machine
creation fields in a complete request. The former per-disk `host` input is
removed. Existing disks must share a host in the selected cloud/zone. VM
attach/detach resolves the VM independently and rejects a disk on another host
with 422, even if the same path exists there. Public ID changes do not rewrite
accepted task payloads, historical resource references, ownership or lock keys.

## Read-only Administration

Administration is available to users authenticated through the existing
local/OIDC/token flows, subject to the same credential-kind, transport and origin
restrictions. It adds no temporary administrator flag or role system. Configuration
and diagnostics do not send or use workspace namespace/context headers for
filtering or authority. Namespace scope and RBAC are separate later changes.

| GET path | Result |
| --- | --- |
| `/administration/configuration` | Accepted snapshot metadata and safe installation settings |
| `/administration/clouds` | Configured cloud summaries |
| `/administration/clouds/{cloud}` | Common cloud overview and typed backend references |
| `/administration/clouds/{cloud}/configuration` | Effective zones, instance types, logical networks, storage classes and image references |
| `/administration/clouds/{cloud}/configuration/hosts` | Configured host summaries, when supported |
| `/administration/clouds/{cloud}/configuration/hosts/{node}` | Effective host settings and physical network/storage bindings |
| `/administration/clouds/{cloud}/images` | This cloud's configured images, independent of live availability |
| `/administration/clouds/{cloud}/images/{image}` | Safe full technical definition |
| `/administration/clouds/{cloud}/images/{image}/availability` | Separately observed backend availability, when supported |
| `/administration/clouds/{cloud}/hosts` | Live host inventory and explicit failures |
| `/administration/clouds/{cloud}/hosts/{node}` | Live diagnostics, CPU topology, NUMA memory and pins |
| `/administration/clouds/{cloud}/hosts/{node}/networks` | Observed backend networks |
| `/administration/clouds/{cloud}/hosts/{node}/pools` | Observed backend storage pools |
| `/administration/clouds/{cloud}/hosts/{node}/capabilities` | Live launch capabilities, optionally filtered by architecture |
| `/administration/clouds/{cloud}/hosts/{node}/catalog` | Backend-validated logical networks and storage classes |

Each common cloud descriptor has `id`, effective `name`, `backend_kind` and
`capabilities: {host_inventory, image_availability}`. Backend capabilities describe
supported views independently of permissions and runtime health. Libvirt-specific
host references/counts and bindings appear in typed `libvirt` detail. Unsupported
host or image-availability views return 404. An offline libvirt host remains a
supported host; its individual live endpoints return 503. Unknown cloud/host or
wrong-parent pairs return 404 before backend access.

Configuration comes from an immutable deep copy accepted after startup
validation/defaulting, with no backend reads. Its process-local `snapshot_id`, UTC
`loaded_at`, application `version` and `activation: "restart"` identify what is
running. Editing the file does not update this snapshot; restart to activate
changes. Explicit false/zero values and unresolved target-dependent inheritance
remain distinguishable. Configuration/diagnostic endpoints are GET-only and never
prepare/download images, submit tasks, apply/reload settings or expose raw HCL.

Projections exclude credentials, complete connection URIs, secrets, authentication
policy and private storage locations. Administrative local image paths and
configured emulator/pool references remain readable technical information.
Remote image sources expose only `{scheme, host, port, redacted: true}`; path,
userinfo, query and fragment are omitted, including signed tokens in paths.
Errors preserve the same secret boundary. Ordinary resource IDs and non-secret
diagnostics remain usable.

The live host list returns `{items: [...], unavailable_hosts: [{id, code}]}`.
Partial and all-unavailable inventories return 200; successful hosts remain visible.
Image availability returns `{cloud_id, image_id, libvirt: {hosts: [...]}}` for
libvirt, with an entry per configured host. `observation`, `compatibility` and
`source_state` are separate states; unavailable observations use unknown values
rather than asserting readiness. These reads have no preparation side effects.

Image list `arch` accepts `x86_64` or `aarch64`; omission lists all configured
architectures. Invalid values return 422. Host capabilities use the same allowed
values and default to the host's architecture. Images are owned by cloud; a
missing image in one cloud returns 404 even if its ID exists in another. See
[configuration and image migration](CLOUDS.md) for nested definitions and removal
of global image references. Public cloud catalogs expose user-facing OS,
requirements and guest compatibility, with administrative sources/download/cache
settings omitted by the server.

Tasks remain at `/tasks...`, with their existing API methods, scope, references,
polling and cancel/release safety checks. Their placement in Administration
navigation does not change the task contract. The removed `/hosts/{node}/cloud`
reverse lookup has no replacement endpoint.

## Create a VM

Administrators first prepare the hypervisors and configure the cloud catalog,
image requirements and private host bindings; see [configured clouds](CLOUDS.md)
and the [image guide](content/en/docs/images.md). Users discover a cloud's catalog
with `GET /api/v1/clouds` or `GET /api/v1/clouds/{cloud}` and select compatible
image, instance type, zone, logical network and storage class IDs. The scheduler
chooses the host. Creation uses the public cloud catalog and offers; it does not
require administrative host or image requests.

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

Signing keys are loaded from `api.console_signing_key_file` and
`api.jwt_signing_key_file`, defaulting to `~/.vmango/console_signing_key` and
`~/.vmango/jwt_signing_key`. Missing files are generated privately on startup;
existing files must contain distinct random secrets of at least 32 bytes.
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
