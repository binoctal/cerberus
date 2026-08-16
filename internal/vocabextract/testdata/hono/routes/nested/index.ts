import { Hono } from 'hono';

const app = new Hono();

app.delete('/jobs/*', (c) => c.json({}));

export default app;
