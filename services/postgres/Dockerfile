FROM golang:1.27-bookworm AS build
# gosu 1.19, release commit 6456aaa0f3c8, authenticated by the Go checksum DB.
RUN go mod download github.com/tianon/gosu@v0.0.0-20250923190938-6456aaa0f3c8 \
 && cp -a /go/pkg/mod/github.com/tianon/gosu@v0.0.0-20250923190938-6456aaa0f3c8 /src \
 && chmod -R u+w /src
WORKDIR /src
COPY security/go.mod security/go.sum ./
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/gosu .
FROM postgres:17-alpine
COPY --from=build /out/gosu /usr/local/bin/gosu
COPY --from=build /src /usr/share/zing/gosu-source
