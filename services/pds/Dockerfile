FROM node:24.22.1-bookworm-slim
RUN corepack enable
WORKDIR /service
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --prod --frozen-lockfile
COPY start.mjs ./
RUN mkdir /pds && chown node:node /pds
USER node
EXPOSE 2583
ENV PDS_PORT=2583 PDS_DATA_DIRECTORY=/pds PDS_BLOBSTORE_DISK_LOCATION=/pds/blobs
CMD ["node", "start.mjs"]
