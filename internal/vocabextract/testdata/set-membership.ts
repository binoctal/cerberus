// No-switch relay style: module-level Set whitelists gate the relay calls via
// NAME.has(msg.type), and per-type ifs guard special cases.
const BRIDGE_TO_WEB_TYPES = new Set([
  'stub:b2w-one',
  'stub:b2w-two',
]);

const WEB_TO_BRIDGE_TYPES = new Set([
  'stub:w2b-one',
]);

class UserRoom {
  handleMessage(ws, meta, msg) {
    if (msg.type === 'stub:batched') {
      if (meta.type === 'bridge') { this.batchOutput(msg); }
      return;
    }

    if (msg.type === 'stub:special') {
      if (meta.type === 'web') {
        if (!payload.deviceId) { this.sendError(ws, 'MISSING_DEVICE_ID', 'x'); return; }
        this.sendToBridge(payload.deviceId, msg);
      }
      return;
    }

    if (meta.type === 'bridge' && BRIDGE_TO_WEB_TYPES.has(msg.type)) {
      this.broadcastToWeb(msg);
      return;
    }

    if (meta.type === 'web' && WEB_TO_BRIDGE_TYPES.has(msg.type)) {
      const payload = msg.payload;
      if (payload.deviceId) {
        this.sendToBridge(payload.deviceId, msg);
      } else {
        this.sendError(ws, 'MISSING_DEVICE_ID', 'Cannot route');
      }
      return;
    }
  }
  broadcastToWeb(msg) {}
  sendToBridge(id, msg) {}
  sendError(ws, code, text) {}
  batchOutput(msg) {}
}
