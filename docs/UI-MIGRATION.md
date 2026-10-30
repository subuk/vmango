# React UI and JWT authentication migration

The application now serves an embedded React/TypeScript/Vite SPA. One Go
executable still contains the complete browser runtime, Swagger UI, noVNC and
license notices. Node/npm is a development/source-preparation dependency, not
a production runtime. Business rules remain in `compute/`, hypervisor details
in `libvirt/`, HTTP transport in `api/`, and authentication lifecycle in `auth/`.

## Upgrade configuration

Replace `web { ... }` with `api { ... }`. The legacy block fails with a migration
instruction; it is not an alias. The subsequent [cloud migration](CLOUDS.md)
replaces low-level machine creation and CPU/RAM updates with catalog commands. Remove
`static_version`, `session_secret`, `session_domain`, `session_max_age` and
`media_upload_tmp`. Listener/base URL, users/OIDC, navigation links and console
settings move into `api`. The CLI command remains `vmango web`.

The UI requires the API. An old `api_disabled=true` setting requires an explicit
operator decision; the server never silently enables it. Remote HTTP-only
installations must move to HTTPS. Behind a TLS proxy, preserve Host, Origin and
Sec-Fetch-Site, set the public `api.base_url` and enable `api.session_secure`.
Keep the backend listener inaccessible to untrusted clients. Arbitrary forwarded
headers are not trusted. HTTP is supported for loopback development.

Generate **two independent** keys with `openssl rand -hex 32`; store them in
private configuration and preserve them across restarts:

```hcl
api {
    listen = "127.0.0.1:8080"
    base_url = "https://vmango.example"
    session_secure = true
    jwt_signing_key = "REPLACE_WITH_RANDOM_SECRET"
    console_signing_key = "REPLACE_WITH_SEPARATE_RANDOM_SECRET"
    session_dir = "/var/lib/vmango/sessions"
    token_dir = "/var/lib/vmango/tokens"
    # Move your existing user, oidc and link blocks here.
}
```

The standalone defaults are `~/.vmango/sessions` and `~/.vmango/tokens`, expanded
to absolute paths. The repositories require dedicated service-owned directories
0700 and files/temporary files 0600 on Unix. On Windows, a protected DACL grants
access only to the service account. Unsafe permissions, unknown/corrupt records,
unusable storage or a second process owning either store fail startup. RPM/DEB
upgrades preserve runtime data and configuration; records are never packaged.
The store is local to one process; shared/network storage and replicas are out
of scope.

## Credential lifecycle

Basic Auth and legacy browser session cookies no longer authorize resources or
native console handshakes. Scripts/CI must migrate to UI-issued personal JWTs
sent as `Authorization: Bearer TOKEN`. Do not retain a login password in scripts.

Browser access JWTs live only in page memory for 10 minutes. Refresh credentials
are opaque, rotating HttpOnly host-only cookies scoped to `/api/v1/auth/`, with
SameSite=Lax and Secure for HTTPS. A refresh family expires 7 days after login;
rotation never extends it. Tabs coordinate refresh through Web Locks. Reload
obtains a new access JWT before loading protected resources. Neither access nor
refresh tokens are kept in localStorage/sessionStorage. The non-sensitive Light/Dark/System preference and uncertain task submission keys
(indexed by command hashes, without payloads) use browser storage.

Refresh records contain hashes and bounded rotation history. Rotation is durable
before a response is issued. Authentic reuse of an old credential revokes its
family. A lost rotation response requires signing in again; there is no grace
window or recovery secret. Temporary auth-storage failures return 503 and retain
the current UI context. Logout revokes refresh immediately, but existing web
JWTs remain valid until their short expiry.

Personal JWTs default to 90 days; the UI offers 30/90/365 days and the API accepts
1–365 days. They grant the owner's current application-wide resource permissions,
without RBAC. Token management and independent console-link issuance require an
interactive web credential. Show a personal token once, store it in a protected
client file/keychain or CI secret, replace it before expiry, then revoke the old
one. Lists return only metadata and approximate last use.

Every personal-token HTTP request and native WS handshake reads the current disk
record. No authorization registry/cache is preloaded. Deleting/revoking a record
denies subsequent checks without restart. API revocation also closes active
personal-token console streams, including concurrent-handshake races; accepted
mutations are not rolled back. Manual edits are supported only with the service
stopped and do not promise active-stream cancellation.

Local identities are namespaced configured IDs; OIDC identities are exact issuer
and validated subject pairs. Emails/display names do not own credentials and
matching emails never merge local/OIDC accounts. Email allowlists require verified
provider email. Refresh and personal-token checks apply the configured eligibility
policy. Provider-side disabling or email changes are not automatically discovered
for already issued local tokens; revoke them explicitly. Changing issuer/client
subject identity requires a new login and token issuance, not ownership transfer.

The first version limits each owner to 32 active refresh families and 64 active
personal tokens, with at most 4096 retained unexpired records per store. History
is bounded to 4096 rotations per family; reaching it requires login. Expired
records are removed at startup and hourly. Rate limiting is not implemented.

## Restart, key changes and backups

Restarting with the same keys/directories preserves login and personal tokens.
There is one persistent API signing key; no automatic regeneration or seamless
multi-key rotation. Changing it invalidates existing browser and personal JWTs.
Valid refresh sessions may issue web JWTs under the new key; personal tokens must
be reissued. Console credentials have a separate key and audience.

Deleting the session directory while stopped invalidates refresh credentials;
already issued web JWTs can survive until expiry. Restoring old session/token
backups can resurrect revoked credentials. Prefer excluding these disposable
records from backups or reset them deliberately after restoration. Never promise
that deleting files instantly terminates already accepted operations or consoles.

Signed console links are independent console-only JWTs, reusable until their
absolute expiry (default 1 hour). Logout does not revoke them. They travel in
URL fragments and WS subprotocols, never resource-token query parameters. The
public console shell works without general login; native Bearer console streams
end at access-token expiry. noVNC keeps full controls and same-origin pinning;
only surrounding controls are themed, never the guest framebuffer.

## Development

Use Node from `ui/.nvmrc`, npm and Python 3 independently of the documentation
workspace. `make ui` installs from the committed lockfile when needed, type-checks
and builds `ui/dist`. Every Go build/test/acceptance target first verifies a valid
frontend artifact. Source removal/rename is detected by the manifest, including
under parallel make. Generated files are ignored; do not edit `ui/dist`.

For HMR, start Go on loopback with `api.base_url="http://localhost:5173"` and
`api.session_secure=false`, then run `npm run dev` in `ui/`. Open
`http://localhost:5173`. Vite proxies API, OIDC and console WS to
`http://127.0.0.1:8080` by default (`VMANGO_BACKEND` can override it), preserving
Host/Origin. Keep the OIDC callback path `/login-oidc-callback/`; register the dev
origin with the provider when testing locally. Do not disable origin checks or
enable broad CORS for HMR.

`make api-types` regenerates the reviewed OpenAPI snapshot and TypeScript types.
Types are generated from `api/testdata/openapi.json`, so no running server or
frontend boot is needed. CI checks snapshot/type drift. Use `make`, `make test`,
`make ui-test` and `git diff --check`. Run browser QA for both themes, System/OS
changes, storage failure, initial paint, auth/F5, populated/empty/error states,
forms, keyboard/narrow screens and both console types. Simulations do not establish
real hypervisor behavior. Report Linux/KVM, macOS/HVF, remote hosts and completed
package builds separately.

Distribution source preparation builds UI once from the trusted exact SHA and
includes source, lockfile, licenses and dist in both archive paths. Downstream
RPM/DEB compile/install/Go-test targets use `UI_PREBUILT=1` and Python manifest
validation without Node/npm or network. Verify an extracted archive in that mode;
Debian clean must preserve its packaged dist.

## Asynchronous infrastructure operations

VM and volume mutations now require an Idempotency-Key and return 202 with a
persistent task reference. Update scripts to poll status_url and inspect final
state; a 202 response is not operation completion. There are no legacy synchronous
mutation endpoints. Configure a dedicated tasks directory; package examples use
/var/lib/vmango/tasks. The UI provides Tasks, resource history links and progress
that survives navigation and reload. See [task storage and restart semantics](TASKS.md)
for interruption, manual lock release, private payload cleanup and retention.
