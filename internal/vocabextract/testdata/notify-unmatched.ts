class UserRoom {
  handleMessage(ws, meta, msg) {
    if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
    this.notifyOrchestrator(msg);
  }
  broadcastToWeb(msg) {}
  notifyOrchestrator(msg) {}
}
