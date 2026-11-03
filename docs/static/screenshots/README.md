# UI screenshots

These screenshots show the current React UI using synthetic demonstration data,
not a production installation. They contain no user credentials or real host data.

- `machines.webp`: machine inventory, resource totals and state filters.
- `create-machine.webp`: machine creation and the selected instance type's CPU,
  memory and boot disk allocation.

Capture the application page without browser chrome in Chrome DevTools, using a
1440-pixel-wide viewport, 100% page zoom, device pixel ratio 2 and the light theme.
Use a 980-pixel viewport height for the inventory and a full-page capture for the
creation form. Export PNG, then encode lossless WebP to keep text sharp. Preserve
these filenames when refreshing the screenshots and review their captions and alt
text in `docs/layouts/home.html` and `docs/content/en/docs/_index.md`.

`cloud-init-template.jpg` shows the saved-template selector and preview in an
isolated local application, with a demonstration Nginx configuration. It was
captured as a full-page JPEG in the in-app browser at its default viewport and
system dark theme; it contains no credentials or private infrastructure details.
