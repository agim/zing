FROM superseriousbusiness/gotosocial:0.22.3 AS upstream
FROM golang:1.27-bookworm AS build
WORKDIR /src
# Upstream release source, pinned independently of its container tag.
ADD https://codeberg.org/superseriousbusiness/gotosocial/archive/v0.22.3.tar.gz /tmp/upstream.tar.gz
RUN echo "c7096b06f6a70136414926a659da57b4b3c75edcbd79d052d2880898e473e6bb  /tmp/upstream.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/upstream.tar.gz --strip-components=1 -C /src && rm /tmp/upstream.tar.gz
COPY security/go.mod security/go.sum ./
ENV GOFLAGS=-mod=readonly GOMAXPROCS=2
RUN CGO_ENABLED=0 go build -p 2 -trimpath -tags "netgo osusergo static_build timetzdata nomsgpack stdlibjson" \
 -ldflags="-s -w -X main.Version=0.22.3-zing-security.1" -o /out/gotosocial ./cmd/gotosocial
FROM python:3.12-alpine
RUN apk upgrade --no-cache && rm -rf /usr/local/lib/python3.12/site-packages/pip* /usr/local/bin/pip* \
 && addgroup -S -g 10001 zing && adduser -S -u 10001 -G zing zing \
 && mkdir /data && chown zing:zing /data
COPY --from=upstream /gotosocial/web /gotosocial/web
COPY --from=build /out/gotosocial /gotosocial/gotosocial
COPY --from=build /src/LICENSE /usr/share/licenses/gotosocial/LICENSE
COPY security /usr/share/zing/gotosocial-security
COPY agent.py run.py /service/
USER 10001:10001
EXPOSE 8080 8091
CMD ["python3", "/service/run.py"]
