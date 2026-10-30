# noVNC integration

Runtime assets are vendored from the official noVNC v1.7.0 release:
https://github.com/novnc/noVNC/releases/tag/v1.7.0

Vmango serves the native ES modules directly; it does not run noVNC's npm build
or test toolchain. The package manifest retains upstream metadata and version,
but omits development dependencies and scripts and marks this copy private.
To develop noVNC itself, use the full upstream source tree.

Local changes:

- `app/ui.js` resolves translations and package metadata relative to its module
  URL, since the console page lives under `/machines/`.
- `../novnc.html` preserves the upstream full-console controls. `../vnc-client.js`
  receives cloud/UUID-scoped credentials from its same-origin parent through postMessage.
  It validates the message source and pins `UI.start`'s mandatory host, port,
  path, encryption and shared mode. URL parameters and saved settings cannot
  redirect the connection. The credential is held only in memory and is sent
  through the `vmango.token.JWT` WebSocket subprotocol, alongside `binary`.
- `app/ui.js` passes transient `UI.vmangoProtocols` to the RFB constructor.
- `../console-theme.css` themes the control chrome; the framebuffer is never
  filtered or recolored.

The old IE11/module-loader fallback and its Rollup bundles are no longer shipped.
Licenses and attribution are retained in `LICENSE.txt`, `AUTHORS`, and `docs/`.
