class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'session:send':
        if (meta.type === 'web') {
          const payload = msg.payload;
          if (!payload.deviceId) {
            this.sendError(ws, 'MISSING_DEVICE_ID', 'Cannot route message without deviceId');
            break;
          }
          this.sendToBridge(payload.deviceId, msg);
          this.broadcastToWeb(msg, ws);
        }
        break;
      default:
    }
  }
  sendToBridge(deviceId, msg) {}
  broadcastToWeb(msg, ws) {}
  sendError(ws, code, reason) {}
}
