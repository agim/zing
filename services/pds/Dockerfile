# Rebuild Node and native consumers with the reviewed GCC header backports.
ARG ZING_NODE_BUILD_IMAGE=node-build
FROM node:24.22.2-alpine3.24 AS headers
RUN apk add --no-cache g++ python3 make patch linux-headers
COPY security/gcc15 /review
RUN python3 /review/check.py baseline
RUN python3 /review/prepare.py && python3 /review/check.py patched

FROM headers AS node-build
ARG NODE_SOURCE_SHA256=8fe2ec1723bb9f8d0b683907c82708debbeda64af21cb2aa214c4241eed0a061
RUN python3 -c 'import urllib.request; urllib.request.urlretrieve("https://nodejs.org/dist/v24.22.2/node-v24.22.2.tar.xz", "/node.tar.xz")' \
 && echo "$NODE_SOURCE_SHA256  /node.tar.xz" | sha256sum -c - \
 && tar -xJf /node.tar.xz -C /tmp && rm /node.tar.xz
WORKDIR /tmp/node-v24.22.2
RUN ./configure --prefix=/opt/patched-node --without-npm --without-corepack \
 && make -j1 && make install \
 && /opt/patched-node/bin/node -e 'if (process.version !== "v24.22.2") process.exit(1)'

ENV ZING_NODE_SOURCE_SHA256=$NODE_SOURCE_SHA256

FROM ${ZING_NODE_BUILD_IMAGE} AS build
RUN test ! -e /services \
 && cp /opt/patched-node/bin/node /usr/local/bin/node \
 && cp -a /opt/patched-node/include/node/. /usr/local/include/node/
RUN corepack enable
WORKDIR /services/pds
COPY vendor /services/vendor
COPY pds/package.json pds/pnpm-lock.yaml ./
COPY pds/patches ./patches
ENV npm_config_nodedir=/usr/local npm_config_build_from_source=true
RUN zing_native_store="$(mktemp -d /tmp/zing-pnpm.XXXXXX)" \
 && NODE_USE_ENV_PROXY=1 pnpm install --prod --frozen-lockfile --store-dir "$zing_native_store"
COPY security /services/security
COPY pds/start.mjs pds/security-check.mjs ./
RUN node security-check.mjs && mkdir /pds && chown 65532:65532 /pds
RUN g++ -std=c++17 -O2 /services/security/aligned-allocation-check.cpp -o /tmp/aligned-allocation-check \
 && /tmp/aligned-allocation-check
RUN sh /services/security/prepare-node-runtime.sh \
 && cmp /usr/lib/libstdc++.so.6 /runtime/usr/lib/libstdc++.so.6
RUN python3 /services/security/node-backport-proof.py --pds
FROM scratch
COPY --from=build /runtime /
WORKDIR /services/pds
COPY --from=build /services /services
COPY --from=build --chown=65532:65532 /pds /pds
USER 65532:65532
ENTRYPOINT ["/nodejs/bin/node"]
EXPOSE 2583
ENV PDS_PORT=2583 PDS_DATA_DIRECTORY=/pds PDS_BLOBSTORE_DISK_LOCATION=/pds/blobs
CMD ["start.mjs"]
