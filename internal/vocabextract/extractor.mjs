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
      const arg = call.getArguments().find(a => a.getKind() === SyntaxKind.ObjectLiteralExpression);
      const tp = arg?.getProperties().find(p => p.getKind() === SyntaxKind.PropertyAssignment && p.getName?.() === 'type');
      const edge = { ...make(tp ? lit(tp.getInitializer()) : '(dynamic)'), best_effort: true };
      enrichRoute(edge, call, isW2B);
      edges.push(edge);
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
    const sinkTypes = cc ? fallThroughTypes(cc) : ['(dynamic)'];
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

function addRoute(method, fullPath, mount, line) {
  const e = { method, path: fullPath, mount: mount || undefined,
              source: { spans: [{ start: line, end: line }] } };
  const k = `${e.method}|${e.path}`;
  const ex = routeMap.get(k);
  if (ex) { ex.source.spans.push(...e.source.spans); return; }
  routeMap.set(k, e);
  httpRoutes.push(e);
}

function walkFile(filePath, prefix, depth) {
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
      addRoute(name.toUpperCase(), joinPath(prefix, lit0), prefix, call.getStartLineNumber());
    } else if (name === 'route' && lit0 !== null) {
      const target = imports.get(call.getArguments()[1]?.getText().trim());
      if (target) { traversed.push(target); walkFile(target, joinPath(prefix, lit0), depth + 1); }
    } else if (name === 'on') {
      skippedOn++;
    }
  }
}
walkFile(file, '', 0);

console.log(JSON.stringify({
  edges: [...merged.values()],
  http_routes: httpRoutes,
  files: traversed.map((p) => ({ path: p })),
  skipped_on_registrations: skippedOn,
}));
