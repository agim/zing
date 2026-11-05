import { PDS } from '@atproto/pds';
// Upstream PDS owns repository signing, auth, blobs, and synchronization.
const server = await PDS.fromEnv();
if (process.env.ZING_BIND_LOOPBACK === 'true') {
  const listen = server.app.listen.bind(server.app);
  server.app.listen = (port) => listen(port, '127.0.0.1');
}
await server.start();
for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, async () => {
  await server.destroy();
  process.exit(0);
});
