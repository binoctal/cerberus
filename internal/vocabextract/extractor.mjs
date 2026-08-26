// Cerberus vocabulary extractor. Anchors on broadcastToWeb / sendToBridge
// call sites, walks up to the enclosing CaseClause and role-guard `if`,
// and emits one edge per type in the switch fall-through chain.
//
// Also recognizes: notifyOrchestrator side-effects, batchOutput partial
// sinks, sendToBridge route_field/on_missing_route, and dedups equal
// (from_role,to_role,type,trigger) edges by merging their source.spans.
import { Project, SyntaxKind } from 'ts-morph';
import path from 'node:path';
import fs from 'node:fs';

const file = process.argv[2];
if (!file) { console.error('usage: node extractor.mjs <source.ts>'); process.exit(2); }
const project = new Project();
const sf = project.addSourceFileAtPath(file);

const lit = (n) => n?.getText().replace(/^['"`]|['"`]$/g, '');

// Collect a CaseClause's fall-through chain: preceding empty-body cases.
function fallThroughTypes(cc) {
  const block = cc.getParent();
  if (block.getKind() !== SyntaxKind.CaseBlock) return [lit(cc.getExpression())];
  const clauses = block.getClauses();
  const idx = clauses.indexOf(cc);
  const types = [lit(cc.getExpression())];
  for (let i = idx - 1; i >= 0; i--) {
    const c = clauses[i];
    if (c.getKind() !== SyntaxKind.CaseClause) break;
    if (c.getStatements().length > 0) break; // non-empty body stops the chain
    const e = c.getExpression();
    if (e) types.unshift(lit(e));
  }
  return types;
}

// Nearest `if (meta.type === 'web'|'bridge')` enclosing node.
function roleGuard(node) {
  for (let n = node; n; n = n.getParent()) {
    if (n.getKind() === SyntaxKind.IfStatement) {
      const cond = n.getExpression().getText();
      const m = cond.match(/meta\.type\s*===?\s*['"](web|bridge)['"]/);
      if (m) return { from_role: m[1], guard: cond };
    }
    if (n.getKind() === SyntaxKind.MethodDeclaration) break;
  }
  return { from_role: null, guard: null };
}

// excludeSenderOf: broadcastToWeb(msg, ws?) excludes the originator when a
// second argument is present (the DO's private broadcastToWeb(msg, excludeWs)).
function excludeSenderOf(call, isB2W) {
  if (!isB2W) return false;
  return call.getArguments().length > 1;
}

// sendToBridge route_field: first arg shape like `payload.deviceId`.
function routeFieldOf(call) {
  const args = call.getArguments();
  if (args.length < 1) return null;
  const t = args[0].getText();
  const m = t.match(/(payload\.\w+)/);
  return m ? m[1] : null;
}

// on_missing_route: sibling `else { sendError(ws, 'CODE', ...) }`.
function missingRouteOf(call) {
  const iff = call.getFirstAncestorByKind(SyntaxKind.IfStatement);
  if (!iff) return null;
  if (!iff.getElseStatement()) return null;
  const errs = iff.getElseStatement().getDescendantsOfKind(SyntaxKind.CallExpression)
    .filter(c => c.getExpression().getText().endsWith('sendError'));
  if (errs.length === 0) return null;
  const codeArg = errs[0].getArguments()[1]?.getText().replace(/^['"`]|['"`]$/g, '');
  return codeArg ? { kind: 'send_error', code: codeArg } : null;
}

// preconditionRouteOf: detect a preceding-sibling guard of the form
// `if (!payload.<field>) { ...sendError(ws,'<CODE>',...)...; break }` in the
// same block as the emit call. This is session:send's deviceId gate: the
// route/broadcast only fires when payload.<field> is present, else sendError.
// Unlike missingRouteOf (if/else around the call), the guard here is a prior
// statement in the enclosing block. Returns {route_field, on_missing_route}.
function preconditionRouteOf(call) {
  const block = call.getFirstAncestorByKind(SyntaxKind.Block);
  if (!block) return null;
  const stmts = block.getStatements();
  const ownStmt = call.getFirstAncestorByKind(SyntaxKind.ExpressionStatement);
  let idx = -1;
  for (let i = 0; i < stmts.length; i++) {
    if (stmts[i] === ownStmt) { idx = i; break; }
  }
  for (let i = idx - 1; i >= 0; i--) {
    const s = stmts[i];
    if (s.getKind() !== SyntaxKind.IfStatement) continue;
    const m = s.getExpression().getText().match(/!\s*payload\.(\w+)/);
    if (!m) continue;
    const thenStmt = s.getThenStatement();
    if (!thenStmt) continue;
    const errs = thenStmt.getDescendantsOfKind(SyntaxKind.CallExpression)
      .filter(c => c.getExpression().getText().endsWith('sendError'));
    if (errs.length === 0) continue;
    const code = errs[0].getArguments()[1]?.getText().replace(/^['"`]|['"`]$/g, '') || '';
    return { route_field: 'payload.' + m[1], on_missing_route: { kind: 'send_error', code } };
  }
  return null;
}

// enrichRoute attaches route_field / on_missing_route to an edge from the
// call site. sendToBridge carries route_field in its first arg; either shape
// may additionally sit behind a !payload.<field> precondition guard.
function enrichRoute(e, call, isW2B) {
  if (isW2B) {
    const rf = routeFieldOf(call);
    if (rf) e.route_field = rf;
    const mr = missingRouteOf(call);
    if (mr) e.on_missing_route = mr;
  }
  const pre = preconditionRouteOf(call);
  if (pre) {
    if (!e.route_field) e.route_field = pre.route_field;
    if (!e.on_missing_route) e.on_missing_route = pre.on_missing_route;
  }
}

// Extract `msg.type === 'X'` literals from a binary/OR condition tree.
function msgTypeLiterals(node) {
  const out = [];
  const walk = (n) => {
    if (!n) return;
    if (n.getKind() === SyntaxKind.BinaryExpression && n.getOperatorToken().getKind() === SyntaxKind.BarBarToken) {
      walk(n.getLeft());
      walk(n.getRight());
      return;
    }
    if (n.getKind() === SyntaxKind.BinaryExpression && (n.getOperatorToken().getKind() === SyntaxKind.EqualsEqualsEqualsToken || n.getOperatorToken().getKind() === SyntaxKind.EqualsEqualsToken)) {
      const t = n.getText();
      const m = t.match(/msg\.type\s*===?\s*['"`]([\w:-]+)['"`]/);
      if (m) out.push(m[1]);
    }
  };
  walk(node);
  return out;
}

// Module-level `const NAME = new Set(['a', 'b'])` whitelist constants. The
// no-switch relay style gates calls on NAME.has(msg.type) instead of a case
// fall-through chain; members resolve statically just like case labels.
const setMembers = new Map();
for (const stmt of sf.getStatements()) {
  if (stmt.getKind() !== SyntaxKind.VariableStatement) continue;
  for (const d of stmt.getDeclarations()) {
    const init = d.getInitializer();
    if (!init || init.getKind() !== SyntaxKind.NewExpression) continue;
    if (init.getExpression().getText() !== 'Set') continue;
    const arr = init.getArguments()[0];
    if (!arr || arr.getKind() !== SyntaxKind.ArrayLiteralExpression) continue;
    const strs = arr.getElements()
      .filter(e => e.getKind() === SyntaxKind.StringLiteral)
      .map(e => lit(e));
    if (strs.length > 0) setMembers.set(d.getName(), strs);
  }
}

// Types gating a relay call when there is no enclosing CaseClause, walking
// the ancestor ifs: a NAME.has(msg.type) whitelist membership resolves to
// the Set's members; an if (msg.type === 'X') literal resolves to X.
// Returns null when the type is genuinely dynamic.
function guardTypesOf(node) {
  for (let n = node; n; n = n.getParent()) {
    if (n.getKind() === SyntaxKind.MethodDeclaration) break;
    if (n.getKind() !== SyntaxKind.IfStatement) continue;
    const cond = n.getExpression();
    const sm = cond.getText().match(/([A-Za-z_$][\w$]*)\s*\.\s*has\s*\(\s*msg\.type\s*\)/);
    if (sm && setMembers.has(sm[1])) return setMembers.get(sm[1]);
    const lits = msgTypeLiterals(cond);
    if (lits.length > 0) return lits;
  }
  return null;
}

const edges = [];
const cls = sf.getClasses()[0];
// The WS pass assumes a DO room class; a classless entry (e.g. a Hono
// worker.ts) only runs the HTTP pass below.
if (cls) for (const method of cls.getMethods()) {
  const mname = method.getName();
  for (const call of method.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    const expr = call.getExpression();
    if (expr.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    const name = expr.getName();
    const isB2W = name === 'broadcastToWeb';
    const isW2B = name === 'sendToBridge';
    if (!isB2W && !isW2B) continue;

    const cc = call.getFirstAncestorByKind(SyntaxKind.CaseClause);
    const { from_role, guard } = roleGuard(call);
    const trigger = mname === 'handleMessage' ? 'message_handled'
                  : mname === 'fetch' ? 'fetch_branch'
                  : mname === 'webSocketClose' ? 'disconnect_bridge' : mname;
    const line = call.getStartLineNumber();
    const make = (type) => ({
      from_role: from_role ?? null,
      to_role: isB2W ? 'web' : 'bridge',
      type, trigger, guard,
      delivery: {
        mode: isB2W ? 'broadcast_web' : 'send_bridge_by_device',
        exclude_sender: excludeSenderOf(call, isB2W),
      },
      source: { spans: [{ start: line, end: line }] },
    });
    if (cc) {
      for (const t of fallThroughTypes(cc)) {
        const e = make(t);
        enrichRoute(e, call, isW2B);
        edges.push(e);
      }
    } else {
      const guarded = guardTypesOf(call);
      if (guarded) {
        for (const t of guarded) {
          const e = make(t);
          enrichRoute(e, call, isW2B);
          edges.push(e);
        }
      } else {
        const arg = call.getArguments().find(a => a.getKind() === SyntaxKind.ObjectLiteralExpression);
        const tp = arg?.getProperties().find(p => p.getKind() === SyntaxKind.PropertyAssignment && p.getName?.() === 'type');
        const edge = { ...make(tp ? lit(tp.getInitializer()) : '(dynamic)'), best_effort: true };
        enrichRoute(edge, call, isW2B);
        edges.push(edge);
      }
    }
  }
}

// Side-effects: notifyOrchestrator calls attach {kind, when_types} to
// matching edges. Matching prefers an enclosing `if (msg.type === ...)`
// condition; otherwise falls back to the CaseClause fall-through chain.
if (cls) for (const method of cls.getMethods()) {
  for (const call of method.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    const expr = call.getExpression();
    if (expr.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    if (expr.getName() !== 'notifyOrchestrator') continue;

    let when_types = [];
    const iff = call.getFirstAncestorByKind(SyntaxKind.IfStatement);
    if (iff) when_types = msgTypeLiterals(iff.getExpression());

    let pool = [];
    if (when_types.length === 0) {
      const cc = call.getFirstAncestorByKind(SyntaxKind.CaseClause);
      if (cc) {
        when_types = fallThroughTypes(cc);
      }
    }
    if (when_types.length > 0) {
      pool = edges.filter(e => when_types.includes(e.type));
    }
    const se = { kind: 'notify_orchestrator', when_types };
    for (const e of pool) {
      e.side_effects = e.side_effects || [];
      e.side_effects.push(se);
    }
    // If no matching edge was found, recognize-but-cannot-resolve.
    if (pool.length === 0) {
      edges.push({
        from_role: null, to_role: null,
        type: '(unsupported:notify_orchestrator)',
        trigger: method.getName() || null,
        guard: null,
        unsupported: true,
        source: { spans: [{ start: call.getStartLineNumber(), end: call.getStartLineNumber() }] },
      });
    }
  }
}

// Batch detector: a `this.batchOutput(...)` sink marks the edge as
// `partial: true` because the final batched type is produced by a flush
// loop we cannot statically resolve in v1. window/key are best-effort
// literals (spec §6 Step 7); correctness of the batched type is OOS.
const BATCH = { window_ms: 50, key: 'payload.sessionId' };
if (cls) for (const method of cls.getMethods()) {
  for (const call of method.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    const expr = call.getExpression();
    if (expr.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    if (expr.getName() !== 'batchOutput') continue;

    const cc = call.getFirstAncestorByKind(SyntaxKind.CaseClause);
    const { from_role, guard } = roleGuard(call);
    const mname = method.getName();
    const trigger = mname === 'handleMessage' ? 'message_handled' : mname;
    const line = call.getStartLineNumber();
    const sinkTypes = cc ? fallThroughTypes(cc) : (guardTypesOf(call) ?? ['(dynamic)']);
    for (const t of sinkTypes) {
      edges.push({
        from_role: from_role ?? null,
        to_role: 'web',
        type: t, trigger, guard,
        delivery: { mode: 'broadcast_web', exclude_sender: null },
        partial: true,
        batch: { ...BATCH },
        best_effort: true,
        source: { spans: [{ start: line, end: line }] },
      });
    }
  }
}

// Dedup: merge equal (from_role,to_role,type,trigger) edges by joining spans.
// Carries forward side_effects/route_field/on_missing_route/batch/partial
// from duplicates into the surviving edge (union, not overwrite).
const key = (e) => `${e.from_role}|${e.to_role}|${e.type}|${e.trigger}`;
const merged = new Map();
for (const e of edges) {
  const k = key(e);
  const ex = merged.get(k);
  if (ex) {
    ex.source.spans.push(...e.source.spans);
    if (e.route_field && !ex.route_field) ex.route_field = e.route_field;
    if (e.on_missing_route && !ex.on_missing_route) ex.on_missing_route = e.on_missing_route;
    if (e.partial) ex.partial = true;
    if (e.batch && !ex.batch) ex.batch = e.batch;
    if (e.best_effort) ex.best_effort = true;
    if (e.unsupported) ex.unsupported = true;
    if (Array.isArray(e.side_effects) && e.side_effects.length) {
      ex.side_effects = (ex.side_effects || []).concat(e.side_effects);
    }
    if (e.delivery && e.delivery.exclude_sender && !ex.delivery.exclude_sender) {
      ex.delivery.exclude_sender = true;
    }
  } else {
    merged.set(k, e);
  }
}
// ── HTTP route extraction (Hono) ────────────────────────────────
// Walks app.<method>('path') and app.route('/prefix', router) at module
// level, following relative imports with Node resolution semantics
// (exact file beats directory: base, base.ts, base/index.ts). Only mounted
// routers are traversed; an imported-but-unmounted router never appears.
const HTTP_METHODS = ['get', 'post', 'put', 'delete', 'patch', 'options', 'head', 'all'];
const httpRoutes = [];
const routeMap = new Map();
const traversed = [file];
const visitedFiles = new Set();
let skippedOn = 0;

function joinPath(prefix, p) {
  const a = prefix.replace(/\/+$/, '');
  const b = p.replace(/^\/+/, '');
  if (!b) return a || '/';
  if (!a) return '/' + b;
  return a + '/' + b;
}

function resolveSpecifier(fromFile, spec) {
  if (!spec.startsWith('.')) return null;
  const base = path.resolve(path.dirname(fromFile), spec);
  for (const cand of [base, base + '.ts', path.join(base, 'index.ts')]) {
    if (fs.existsSync(cand) && fs.statSync(cand).isFile()) return cand;
  }
  return null;
}

// importName → resolved source path for relative imports (default, named, ns).
function importMap(sf) {
  const m = new Map();
  for (const imp of sf.getImportDeclarations()) {
    const src = resolveSpecifier(sf.getFilePath(), imp.getModuleSpecifierValue());
    if (!src) continue;
    const def = imp.getDefaultImport();
    if (def) m.set(def.getText(), src);
    for (const n of imp.getNamedImports()) m.set(n.getName(), src);
    const ns = imp.getNamespaceImport();
    if (ns) m.set(ns.getText(), src);
  }
  return m;
}

// routeHasPrefix: an app.use(prefix) middleware applies to the prefix itself
// and every path under it. A path-less app.use(mw) records prefix '/', which
// applies to every route. A prefix ending in '/*' (e.g. Hono's '/api/*') is a
// glob: it matches the stripped prefix and everything under it; a bare '*'
// matches every path (it survives joinPath as '/*' at the root, '<mount>/*'
// inside a mounted router).
function routeHasPrefix(p, pre) {
  if (pre === '/' || pre === '*') return true;
  if (pre.endsWith('/*')) {
    const base = pre.slice(0, -2);
    return p === base || p.startsWith(base + '/');
  }
  return p === pre || p.startsWith(pre + '/');
}

function addRoute(method, fullPath, mount, line, middlewares, minBody) {
  const e = { method, path: fullPath, mount: mount || undefined,
              middlewares: middlewares.length ? middlewares : undefined,
              min_body: minBody || undefined,
              source: { spans: [{ start: line, end: line }] } };
  const k = `${e.method}|${e.path}`;
  const ex = routeMap.get(k);
  if (ex) { ex.source.spans.push(...e.source.spans); return; }
  routeMap.set(k, e);
  httpRoutes.push(e);
}

// walkFile collects routes declared in one file. useMws carries the
// app.use(prefix) middlewares declared by parent files (entries {prefix, name});
// they keep applying to every route registered under their prefix, including
// routes inside routers mounted later via app.route.
// ── zod minimal request bodies ──────────────────────────────────
// zValidator('json', schema) lets the extractor derive the minimal legal
// request body from literal zod primitives. Only plain z.string()/
// z.number()/z.boolean() properties are extractable; anything richer
// (.refine, nested z.object, .optional() chains, spreads) marks the WHOLE
// schema unextractable — min_body is omitted, never guessed.

// z.object literal mapper: returns {field: minimal value} or null when any
// property is not a plain PropertyAssignment of a zero-arg z.<primitive>().
const ZOD_PRIMITIVES = { string: 'x', number: 0, boolean: false };
function zodBodyOf(node) {
  if (!node || node.getKind() !== SyntaxKind.CallExpression) return null;
  const callee = node.getExpression();
  if (callee.getKind() !== SyntaxKind.PropertyAccessExpression) return null;
  if (callee.getName() !== 'object' || callee.getExpression().getText() !== 'z') return null;
  const arg = node.getArguments()[0];
  if (!arg || arg.getKind() !== SyntaxKind.ObjectLiteralExpression) return null;
  const body = {};
  for (const p of arg.getProperties()) {
    if (p.getKind() !== SyntaxKind.PropertyAssignment) return null; // spread etc.
    const init = p.getInitializer();
    if (!init || init.getKind() !== SyntaxKind.CallExpression) return null;
    const c = init.getExpression();
    if (c.getKind() !== SyntaxKind.PropertyAccessExpression) return null;
    if (c.getExpression().getText() !== 'z') return null;
    const prim = ZOD_PRIMITIVES[c.getName()];
    if (prim === undefined || init.getArguments().length > 0) return null;
    body[p.getName()] = prim;
  }
  return body;
}

// Module-level `const X = z.object({...})` declarations. Value is the mapped
// body, or explicit null when the schema exists but is unextractable.
function schemaMapOf(sf) {
  const m = new Map();
  for (const stmt of sf.getStatements()) {
    if (stmt.getKind() !== SyntaxKind.VariableStatement) continue;
    for (const d of stmt.getDeclarations()) {
      const body = zodBodyOf(d.getInitializer());
      if (body !== null) m.set(d.getName(), body);
    }
  }
  return m;
}

// zValidator('json', <arg2>) among route args: arg2 is an Identifier resolved
// through schemaMapOf or an inline z.object literal. The validator may be a
// bare imported identifier or a namespace member (validators.zValidator).
// Unresolvable or unextractable schemas return undefined (omit min_body).
function isZValidator(callee) {
  if (callee.getKind() === SyntaxKind.Identifier) return callee.getText() === 'zValidator';
  if (callee.getKind() === SyntaxKind.PropertyAccessExpression) return callee.getName() === 'zValidator';
  return false;
}
function zValidatorBodyOf(args, schemas) {
  for (const a of args) {
    if (a.getKind() !== SyntaxKind.CallExpression) continue;
    if (!isZValidator(a.getExpression())) continue;
    const target = a.getArguments()[0];
    if (!target || target.getText().replace(/^['"`]|['"`]$/g, '') !== 'json') continue;
    const schema = a.getArguments()[1];
    if (!schema) return undefined;
    if (schema.getKind() === SyntaxKind.Identifier) return schemas.get(schema.getText());
    return zodBodyOf(schema) ?? undefined;
  }
  return undefined;
}

function walkFile(filePath, prefix, depth, useMws) {
  const abs = path.resolve(filePath);
  if (depth > 8 || visitedFiles.has(abs)) return;
  visitedFiles.add(abs);
  let sf2;
  try { sf2 = project.addSourceFileAtPath(abs); } catch { return; }
  const honoVars = new Set();
  for (const d of sf2.getVariableDeclarations()) {
    const init = d.getInitializer();
    if (init?.getKind() === SyntaxKind.NewExpression && init.getExpression().getText() === 'Hono') {
      honoVars.add(d.getName());
    }
  }
  const imports = importMap(sf2);
  const schemas = schemaMapOf(sf2);
  const mws = [...useMws];
  // Per-file counter that disambiguates repeated synthesized anonymous-use
  // names (scoped to this file; parent-file names are already fixed).
  const anonUseNames = new Map();
  for (const stmt of sf2.getStatements()) {
    if (stmt.getKind() !== SyntaxKind.ExpressionStatement) continue;
    const call = stmt.getExpression();
    if (call.getKind() !== SyntaxKind.CallExpression) continue;
    const prop = call.getExpression();
    if (prop.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    if (!honoVars.has(prop.getExpression().getText())) continue;
    const name = prop.getName();
    const arg0 = call.getArguments()[0];
    const lit0 = arg0 && arg0.getKind() === SyntaxKind.StringLiteral ? lit(arg0) : null;
    if (HTTP_METHODS.includes(name) && lit0 !== null) {
      // Inline middleware: bare identifier args between the path and the
      // handler (arrow/function args are the handler itself).
      const inlineMw = call.getArguments().slice(1)
        .filter(a => a.getKind() === SyntaxKind.Identifier)
        .map(a => a.getText());
      const mwsForRoute = [...inlineMw, ...mws.filter(u => routeHasPrefix(joinPath(prefix, lit0), u.prefix)).map(u => u.name)];
      const minBody = zValidatorBodyOf(call.getArguments(), schemas);
      addRoute(name.toUpperCase(), joinPath(prefix, lit0), prefix, call.getStartLineNumber(), mwsForRoute, minBody);
    } else if (name === 'use') {
      // app.use('/p', mw, ...) registers each identifier middleware under
      // the prefix; a path-less app.use(mw) covers everything ('/'). An
      // inline arrow/function middleware (the anonymous auth gate pattern)
      // gets a stable synthesized name 'use:<prefix>' so it still rides the
      // middleware chain; a repeated name within one file gets '#2', '#3',
      // ... The name is pattern-derived, not SUT-specific.
      const args = call.getArguments();
      const mwPrefix = lit0 !== null ? joinPath(prefix, lit0) : '/';
      const mwArgs = lit0 !== null ? args.slice(1) : args;
      for (const a of mwArgs) {
        if (a.getKind() === SyntaxKind.Identifier) {
          mws.push({ prefix: mwPrefix, name: a.getText() });
        } else if (a.getKind() === SyntaxKind.ArrowFunction || a.getKind() === SyntaxKind.FunctionExpression) {
          const base = 'use:' + mwPrefix;
          const n = (anonUseNames.get(base) || 0) + 1;
          anonUseNames.set(base, n);
          mws.push({ prefix: mwPrefix, name: n === 1 ? base : `${base}#${n}` });
        }
      }
    } else if (name === 'route' && lit0 !== null) {
      const target = imports.get(call.getArguments()[1]?.getText().trim());
      if (target) { traversed.push(target); walkFile(target, joinPath(prefix, lit0), depth + 1, mws); }
    } else if (name === 'on') {
      skippedOn++;
    }
  }
}
walkFile(file, '', 0, []);

console.log(JSON.stringify({
  edges: [...merged.values()],
  http_routes: httpRoutes,
  files: traversed.map((p) => ({ path: p })),
  skipped_on_registrations: skippedOn,
}));
