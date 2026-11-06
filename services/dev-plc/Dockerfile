FROM node:24.22.2-alpine3.24 AS build
RUN corepack enable
WORKDIR /services/dev-plc
COPY vendor /services/vendor
COPY dev-plc/package.json dev-plc/pnpm-lock.yaml ./
COPY dev-plc/patches ./patches
ENV npm_config_nodedir=/usr/local npm_config_build_from_source=true
RUN NODE_USE_ENV_PROXY=1 pnpm install --prod --frozen-lockfile
COPY security /services/security
COPY dev-plc/start.mjs dev-plc/security-check.mjs ./
RUN node security-check.mjs
RUN sh /services/security/prepare-node-runtime.sh
FROM scratch
COPY --from=build /runtime /
WORKDIR /services/dev-plc
COPY --from=build /services /services
USER 65532:65532
ENTRYPOINT ["/nodejs/bin/node"]
CMD ["start.mjs"]
