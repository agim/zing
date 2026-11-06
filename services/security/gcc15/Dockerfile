# Experimental toolchain proof. It does not replace either validated service image.
FROM node:24.22.2-alpine3.24 AS headers
RUN apk add --no-cache g++ python3 make patch linux-headers
COPY security/gcc15 /review
RUN python3 /review/check.py baseline
RUN python3 /review/prepare.py && python3 /review/check.py patched

# Full consumer rebuild is required: template fixes cannot repair an old Node binary.
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
RUN cp /opt/patched-node/bin/node /usr/local/bin/node \
 && cp -a /opt/patched-node/include/node/. /usr/local/include/node/
