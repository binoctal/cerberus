import { Hono } from 'hono';

const app = new Hono();

app.put('/secret', (c) => c.json({}));

export const helper = app;
