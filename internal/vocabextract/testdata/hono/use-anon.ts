import { Hono } from 'hono';

// Mirrors the real open-agents worker shape: an ANONYMOUS inline auth gate
// registered under a glob prefix (no named authMiddleware exists there), plus
// global named middlewares under a bare '*'.
const app = new Hono();
const requestLogger = async (c: any, next: any) => { await next(); };

app.use('/api/*', async (c: any, next: any) => { await next(); });
// A second anonymous /api/* use must not collide with the first.
app.use('/api/*', async (c: any, next: any) => { await next(); });
app.use('*', requestLogger);

app.get('/health', (c) => c.json({ ok: true }));
app.post('/api/things', (c) => c.json({}));
app.get('/api/things/:id', (c) => c.json({}));
export default app;
