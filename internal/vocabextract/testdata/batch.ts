class UserRoom {
  handleMessage(ws, meta, msg) {
    if (meta.type === 'bridge') { this.batchOutput(msg); }
  }
  batchOutput(msg) {}
  flushBatch(sessionId) { this.broadcastToWeb({ type: 'session:output-batch' }); }
  broadcastToWeb(msg) {}
}
