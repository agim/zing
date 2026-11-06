# GoToSocial security rebuild

The companion server remains GoToSocial 0.22.3 (AGPL-3.0). Zing does not implement
ActivityPub itself. The Dockerfile builds the upstream release source with the
compatible dependency versions pinned in this directory's `go.mod` and `go.sum`.
Protocol/application source is unchanged; the executable identifies itself as
`0.22.3-zing-security.1`. The matching upstream container supplies its web assets.

Corresponding source:
- https://codeberg.org/superseriousbusiness/gotosocial/archive/v0.22.3.tar.gz
- Release commit: `5a6c8e26c1d028a04b9b50c1a84f55e63429a1a4`
- Archive SHA-256: `c7096b06f6a70136414926a659da57b4b3c75edcbd79d052d2880898e473e6bb`

To reproduce: extract the archive, replace its go.mod/go.sum with these files,
and run the Go build command recorded in `../Dockerfile`. `-mod=readonly` uses
the reviewed module graph instead of the release's older vendor tree. The Go
module proxy cannot package this upstream tag because its repository contains a
literal `*` filename; the archive checksum is checked before extraction instead.
The upstream license is retained at `/usr/share/licenses/gotosocial/LICENSE` in
the image. These dependency files and this source offer are also shipped there
under `/usr/share/zing/gotosocial-security`.

Review motivation: whole-image scanning found affected versions of go-jose,
compress, quic-go, OpenTelemetry, x/crypto, x/image, x/net, x/text and gRPC in the
unmodified upstream image. Updating these dependencies preserves the stable
server interface; native account, publication, media, moderation and recovery
acceptance must pass against the rebuilt image before a release.

For a native development profile, stop the native supervisor and run
`python3 scripts/build-activitypub.py` with a patched Go 1.27.3+ toolchain. It checks
the same source archive hash, uses these dependency files and build flags, and
atomically replaces only the executable. `social-local.py --install` invokes it
when the security rebuild is missing. Web assets retain their release checksum.
