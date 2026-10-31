# syntax=docker/dockerfile:1
FROM node:24.13.0-bookworm-slim AS frontend
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /source
COPY ui/ ui/
COPY api/testdata/openapi.json api/testdata/openapi.json
COPY scripts/ui-assets.py scripts/ui-assets.py
RUN test "$(node --version)" = "v$(cat ui/.nvmrc)" \
    && cd ui && npm ci && npm run build

FROM golang:1.26-bookworm AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=1
RUN apt-get update \
    && apt-get install -y --no-install-recommends libvirt-dev genisoimage python3 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /source
COPY . .
COPY --from=frontend /source/ui/dist/ ui/dist/
ARG VERSION=0.0.0-unknown
RUN make UI_PREBUILT=1 VERSION="$VERSION" \
    && make UI_PREBUILT=1 test
RUN mkdir /licenses \
    && cp LICENSE /licenses/ \
    && cp ui/dist/THIRD-PARTY-LICENSES.txt /licenses/frontend.txt \
    && find vendor ui/public/vendor -type f \
       \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' -o -iname 'copyright*' \) \
       -exec cp --parents '{}' /licenses/ \;

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ca-certificates libvirt0 libvirt-clients genisoimage openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 vmango \
    && useradd --uid 10001 --gid vmango --create-home --home-dir /var/lib/vmango vmango
ARG VERSION=0.0.0-unknown
ARG REVISION=unknown
LABEL org.opencontainers.image.title="Vmango" \
      org.opencontainers.image.source="https://github.com/subuk/vmango" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION"
COPY --from=build /source/bin/vmango /usr/bin/vmango
COPY --from=build /licenses/ /usr/share/licenses/vmango/
COPY vmango.dist.conf /etc/vmango.conf
ENV HOME=/var/lib/vmango VMANGO_CONFIG=/etc/vmango.conf
USER 10001:10001
WORKDIR /var/lib/vmango
VOLUME ["/var/lib/vmango"]
EXPOSE 8080
ENTRYPOINT ["/usr/bin/vmango"]
CMD ["web"]
