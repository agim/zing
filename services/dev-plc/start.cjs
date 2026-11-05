// Real upstream PLC validation + PostgreSQL persistence, isolated for local use.
const { PlcServer, Database } = require('@did-plc/server');
(async () => {
  if (!process.env.ZING_PLC_DATABASE_URL) throw new Error('Local PLC database is required');
  const db = Database.postgres({ url: process.env.ZING_PLC_DATABASE_URL });
  await db.migrateToLatestOrThrow();
  const server = PlcServer.create({ db, port: 2582 });
  const listen = server.app.listen.bind(server.app);
  server.app.listen = (port) => listen(port, '127.0.0.1');
  await server.start();
  for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, async () => {
    await server.destroy(); await db.close(); process.exit(0);
  });
})().catch(() => { console.error('Local PLC could not start; check its database configuration.'); process.exit(1); });
