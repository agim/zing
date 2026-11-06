# PostgreSQL helper security rebuild

PostgreSQL remains the official PostgreSQL 17 Alpine distribution. Its entrypoint
uses gosu to drop privileges. The bundled gosu 1.19 executable was built with Go
1.24.6 and old moby/sys/user and x/sys dependencies; whole-image scanning flagged
it even though it has no network listener. Rebuild the same upstream helper with
Go 1.27 and this pinned, compatible dependency graph. The PostgreSQL executable,
extensions and official entrypoint remain unchanged.

Source: https://github.com/tianon/gosu/tree/6456aaa0f3c8
Go module: `github.com/tianon/gosu@v0.0.0-20250923190938-6456aaa0f3c8`
Checksum: `h1:HIpXk5mGBQGfOqcaBbRT4Vnss8NPICnMGlD5xTlPBdQ=`

The full helper source, updated dependency files and Apache-2.0 license are
included at `/usr/share/zing/gosu-source` in the image. Native database startup,
logical backup and fresh-database restore acceptance must pass with this image.
