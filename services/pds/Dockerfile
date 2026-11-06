FROM gcr.io/distroless/cc-debian13:nonroot AS cpp-runtime
FROM node:24.22.1-bookworm-slim AS build
# Only Node's linked C++/GCC libraries are needed; no unused system zlib/OpenSSL.
COPY --from=cpp-runtime /usr/lib /tmp/cc-libs
COPY --from=cpp-runtime /var/lib/dpkg/status.d /tmp/cc-status
COPY --from=cpp-runtime /usr/share/doc /tmp/cc-doc
RUN mkdir -p /runtime/usr/lib /runtime/var/lib/dpkg/status.d /runtime/usr/share/doc \
 && for zing_arch in /tmp/cc-libs/*-linux-gnu; do \
      zing_name=$(basename "$zing_arch"); mkdir -p "/runtime/usr/lib/$zing_name"; \
      cp -a "$zing_arch"/libstdc++.so.6* "$zing_arch"/libgcc_s.so.1 "/runtime/usr/lib/$zing_name/"; \
    done \
 && for zing_pkg in libstdc++6 libgcc-s1 gcc-14-base; do \
      cp /tmp/cc-status/"$zing_pkg"* /runtime/var/lib/dpkg/status.d/; \
      cp -a /tmp/cc-doc/"$zing_pkg" /runtime/usr/share/doc/; \
    done
RUN corepack enable
WORKDIR /services/pds
COPY vendor /services/vendor
COPY security /services/security
COPY pds/package.json pds/pnpm-lock.yaml ./
COPY pds/patches ./patches
RUN NODE_USE_ENV_PROXY=1 pnpm install --prod --frozen-lockfile
COPY pds/start.mjs pds/security-check.mjs ./
RUN node security-check.mjs && mkdir /pds && chown node:node /pds
FROM gcr.io/distroless/nodejs24-debian13:nonroot AS node-runtime
FROM gcr.io/distroless/base-nossl-debian13:nonroot
COPY --from=build /runtime /
COPY --from=node-runtime /nodejs/bin/node /nodejs/bin/node
ENTRYPOINT ["/nodejs/bin/node"]
WORKDIR /services/pds
COPY --from=build /services /services
COPY --from=build --chown=65532:65532 /pds /pds
USER 65532:65532
EXPOSE 2583
ENV PDS_PORT=2583 PDS_DATA_DIRECTORY=/pds PDS_BLOBSTORE_DISK_LOCATION=/pds/blobs
CMD ["start.mjs"]
