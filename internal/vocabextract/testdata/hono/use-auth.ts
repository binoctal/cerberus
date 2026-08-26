import { Hono } from 'hono';
import { thingRoutes } from './routes/things';

const app = new Hono();
const authMiddleware = async (c: any, next: any) => { await next(); };
const rateLimiter = async (c: any, next: any) => { await next(); };

app.get('/health', (c) => c.json({ ok: true }));
app.use('/api/things', authMiddleware);
app.get('/limited', rateLimiter, (c) => c.json({}));
app.get('/api/things', (c) => c.json([]));
app.route('/api/things', thingRoutes);
export default app;
