# Swagger UI distribution

Runtime files from the official `swagger-ui-dist` npm package, pinned to 5.34.0.
Source: https://github.com/swagger-api/swagger-ui
Package SHA-1: 5be3205fdaa7f3e7ec9fbb2ab6717c10e34e876b

Only the CSS, browser bundle and required license/notice files are shipped.
Vmango initializes the bundle at `/api/docs` with a same-origin OpenAPI URL,
disables the external validator, and does not persist authorization in storage.
