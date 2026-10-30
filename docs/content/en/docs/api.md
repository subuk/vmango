---
title: "Automation API"
description: "Explore Swagger UI, authenticate API clients and automate virtual machine operations."
weight: 4
---

Vmango provides a versioned JSON API under `/api/v1`. It powers the web
interface and is also available to scripts, CLI tools and CI jobs.

## What you can automate

- Discover configured clouds, images, instance types, availability zones,
  logical networks and storage classes.
- List, create and delete virtual machines; start, reboot or power them off;
  change instance types and manage attached disks and network interfaces.
- Create, clone, grow and delete volumes, and manage SSH public keys.
- Inspect host capabilities, networks and storage pools through read-only
  administrative endpoints.
- Track asynchronous tasks and connect to serial or VNC consoles over WebSocket.

Machine and volume operations are scoped to a cloud. Creation requests select
catalog entries and an availability zone; the scheduler chooses the host.
See [configured clouds and scheduling](/docs/clouds/) for the resource model.

## Open Swagger UI

Open `/api/docs` on **your Vmango server**, for example
`https://vmango.example/api/docs`. You can also choose **API documentation**
from the account menu in the web interface.

Sign in with your usual Vmango account, using a local password or your configured
OIDC provider. Swagger UI uses that browser session to load the specification
and authenticate requests automatically. Expand an operation to inspect its
parameters, request body, responses and schemas. **Try it out** sends real
requests to your installation, including any selected create or delete operation.

The server also exposes its OpenAPI 3.1 specification at `/api/openapi.json`.
Fetching it requires Bearer authentication, just like protected resource
endpoints. Use the specification from your running server for its supported
operations and exact request formats.

## Authenticate scripts and CI jobs

1. Sign in to the web interface and open the account menu.
2. Choose **API tokens**, then **Create token**.
3. Give the token a name and choose its lifetime. The default is 90 days;
   the maximum is 365 days.
4. Copy the token when it is shown. It is displayed only once.

Send the token in the `Authorization: Bearer TOKEN` header over HTTPS. For
example, save the following in `~/.config/vmango/curl.conf`, replacing the
placeholder with your token, and restrict the file permissions to `0600`:

```text
header = "Authorization: Bearer YOUR_PERSONAL_JWT"
```

Then list the clouds available on your server:

```sh
chmod 600 ~/.config/vmango/curl.conf
curl --config ~/.config/vmango/curl.conf \
  https://vmango.example/api/v1/clouds
```

The same configuration can fetch `/api/openapi.json`. Store tokens in a
protected client file, keychain or CI secret; keep them out of URLs, source
control and logs. HTTP is supported only for loopback development.

Personal tokens grant your account's application-wide resource permissions.
They cannot manage API tokens or issue console sharing links. Logging out of
the browser does not revoke them: use **Revoke** on the **API tokens** page.
Replace tokens before they expire. Missing, expired or revoked credentials
return `401 Unauthorized`; clients must use Bearer tokens rather than Basic
Auth or browser cookies for protected resource requests.

## Follow operation progress

Infrastructure changes run as durable asynchronous tasks. Send a unique
`Idempotency-Key` header with each new command. A successful submission returns
`202 Accepted`, a task and a `status_url`; poll that URL with the same Bearer
authentication until the task finishes. Acceptance does not mean the operation
has completed successfully.

If a submission response is lost, retry the same command with the same key
to retrieve its original task. Inspect a failed or interrupted task before
submitting another command. Task history survives server restarts and is
available under `/api/v1/tasks`.

For detailed examples and contracts, see the repository's
[API reference](https://github.com/subuk/vmango/blob/master/docs/API.md) and
[task guide](https://github.com/subuk/vmango/blob/master/docs/TASKS.md).
The console WebSocket protocol is included in OpenAPI and is also available
through the **Console protocol** link in Swagger UI. Binary release archives
include `API.md` and `TASKS.md` for offline reference.
