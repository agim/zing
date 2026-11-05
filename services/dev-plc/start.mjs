// Maintained upstream PLC source, with real PostgreSQL persistence.
import { PlcServer, PgDatabase, SequencerLeader } from '@did-plc/server';
if (!process.env.ZING_PLC_DATABASE_URL) throw new Error('Local PLC database is required');
const db = PgDatabase.create({ url: process.env.ZING_PLC_DATABASE_URL });
await db.migrateToLatestOrThrow();
const leader = new SequencerLeader(db);
void leader.run().catch(() => { console.error('PLC sequencing failed'); process.exit(1); });
const server = PlcServer.create({ db, port: Number(process.env.ZING_PLC_PORT || 2582) });
const listen = server.app.listen.bind(server.app);
server.app.listen = (port) => listen(port, '127.0.0.1');
await server.start();
for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, async () => {
  await server.destroy();
  await leader.destroy();
  await db.close();
  process.exit(0);
});
