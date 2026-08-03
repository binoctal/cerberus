// Cerberus vocabulary extractor. Anchors on broadcastToWeb / sendToBridge
// call sites, walks up to the enclosing CaseClause and role-guard `if`,
// and emits one edge per type in the switch fall-through chain.
//
// Also recognizes: notifyOrchestrator side-effects, batchOutput partial
// sinks, sendToBridge route_field/on_missing_route, and dedups equal
// (from_role,to_role,type,trigger) edges by merging their source.spans.
import { Project, SyntaxKind } from 'ts-morph';

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
for (const method of cls.getMethods()) {
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
        // exclude_sender is set when the fan-out skips the originator.
        // v1 leaves this null (no fixture discriminates it yet); the
        // field path is preserved so downstream schemas can populate it.
        exclude_sender: null,
      },
      source: { spans: [{ start: line, end: line }] },
    });
    if (cc) {
      for (const t of fallThroughTypes(cc)) {
        const e = make(t);
        if (isW2B) {
          const rf = routeFieldOf(call);
          if (rf) e.route_field = rf;
          const mr = missingRouteOf(call);
          if (mr) e.on_missing_route = mr;
        }
        edges.push(e);
      }
    } else {
      const arg = call.getArguments().find(a => a.getKind() === SyntaxKind.ObjectLiteralExpression);
      const tp = arg?.getProperties().find(p => p.getKind() === SyntaxKind.PropertyAssignment && p.getName?.() === 'type');
      const edge = { ...make(tp ? lit(tp.getInitializer()) : '(dynamic)'), best_effort: true };
      if (isW2B) {
        const rf = routeFieldOf(call);
        if (rf) edge.route_field = rf;
        const mr = missingRouteOf(call);
        if (mr) edge.on_missing_route = mr;
      }
      edges.push(edge);
    }
  }
}

// Side-effects: notifyOrchestrator calls attach {kind, when_types} to
// matching edges. Matching prefers an enclosing `if (msg.type === ...)`
// condition; otherwise falls back to the CaseClause fall-through chain.
for (const method of cls.getMethods()) {
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
for (const method of cls.getMethods()) {
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
  } else {
    merged.set(k, e);
  }
}
console.log(JSON.stringify({ edges: [...merged.values()] }));
