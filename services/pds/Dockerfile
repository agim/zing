FROM node:24.22.2-alpine3.24 AS build
RUN apk add --no-cache python3 make g++
RUN corepack enable
WORKDIR /services/pds
COPY vendor /services/vendor
COPY pds/package.json pds/pnpm-lock.yaml ./
COPY pds/patches ./patches
ENV npm_config_nodedir=/usr/local npm_config_build_from_source=true
RUN NODE_USE_ENV_PROXY=1 pnpm install --prod --frozen-lockfile
COPY security /services/security
COPY pds/start.mjs pds/security-check.mjs ./
RUN node security-check.mjs && mkdir /pds && chown 65532:65532 /pds
RUN g++ -std=c++17 -O2 /services/security/aligned-allocation-check.cpp -o /tmp/aligned-allocation-check \
 && /tmp/aligned-allocation-check
RUN sh /services/security/prepare-node-runtime.sh \
 && cmp /usr/lib/libstdc++.so.6 /runtime/usr/lib/libstdc++.so.6
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
