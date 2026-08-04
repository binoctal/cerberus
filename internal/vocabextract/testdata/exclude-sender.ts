class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'echo-all':
        if (meta.type === 'web') { this.broadcastToWeb(msg, ws); }
        break;
      case 'echo-everyone':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg, excludeWs) {}
}
