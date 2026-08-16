import { Hono } from 'hono';
import { thingRoutes } from './routes/things';
import { helper } from './routes/unmounted';

const app = new Hono();

app.get('/health', (c) => c.json({ ok: true }));
app.post('/api/dev/setup', strictRateLimit, (c) => c.json({}));
app.post('/api/dev/setup', (c) => c.json({}));
app.route('/api/things', thingRoutes);

export default app;
