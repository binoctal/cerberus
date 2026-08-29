import { Hono } from 'hono';
// Aliased named import: app.route references the LOCAL name, so the resolver
// must follow the alias (real open-agents: import { app as authRoutes }).
import zodRoutes from './zod-body';
import { thingRoutes as stuffRoutes } from './routes/things';
import { helper } from './routes/unmounted';

const app = new Hono();

app.get('/health', (c) => c.json({ ok: true }));
app.post('/api/dev/setup', strictRateLimit, (c) => c.json({}));
app.post('/api/dev/setup', (c) => c.json({}));
app.post('/api/auth/delete-account', (c) => c.json({ deleted: true }));
app.route('/zod', zodRoutes);
app.route('/zod', zodRoutes);
app.route('/api/things', stuffRoutes);

export default app;
