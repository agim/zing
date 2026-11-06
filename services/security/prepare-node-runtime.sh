#!/bin/sh
# A musl Node root without a shell, package manager, compiler or glibc.
set -eu
mkdir -p /runtime/etc/apk /runtime/nodejs/bin /runtime/tmp
cp -a /etc/apk/keys /runtime/etc/apk/keys
cp /etc/apk/repositories /runtime/etc/apk/repositories
# These packages need no post-install programs; the CA bundle is pre-generated.
apk --root /runtime --initdb --no-cache --no-scripts add \
  musl libgcc libstdc++ libatomic ca-certificates-bundle tzdata
cp /etc/alpine-release /runtime/etc/alpine-release
cp /etc/os-release /runtime/etc/os-release
cp /usr/local/bin/node /runtime/nodejs/bin/node
mkdir -p /runtime/usr/share/licenses/node
cp /usr/local/LICENSE /runtime/usr/share/licenses/node/LICENSE
mkdir -p /runtime/usr/share/licenses/gcc
cp /services/security/licenses/COPYING* /runtime/usr/share/licenses/gcc/
printf 'nonroot:x:65532:65532:Zing:/nonexistent:/sbin/nologin\n' > /runtime/etc/passwd
printf 'nonroot:x:65532:\n' > /runtime/etc/group
chmod 1777 /runtime/tmp
# Refuse to publish a compatibility shim or GNU libc by accident.
test ! -e /runtime/lib/libc.so.6
test ! -e /runtime/lib64/ld-linux-x86-64.so.2
test ! -e /runtime/bin/sh
