class UserRoom {
  webSocketClose(ws) {
    const meta = ws.deserializeAttachment();
    if (meta.type === 'bridge') {
      this.broadcastToWeb({ type: 'device:offline' });
    }
  }
  broadcastToWeb(msg) {}
}
