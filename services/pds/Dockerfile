FROM node:24.22.1-bookworm-slim
RUN corepack enable
WORKDIR /services/pds
COPY vendor /services/vendor
COPY pds/package.json pds/pnpm-lock.yaml ./
COPY pds/patches ./patches
RUN pnpm install --prod --frozen-lockfile
COPY pds/start.mjs pds/security-check.mjs ./
RUN node security-check.mjs && mkdir /pds && chown node:node /pds
USER node
EXPOSE 2583
ENV PDS_PORT=2583 PDS_DATA_DIRECTORY=/pds PDS_BLOBSTORE_DISK_LOCATION=/pds/blobs
CMD ["node", "start.mjs"]
