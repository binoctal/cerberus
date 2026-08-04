class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'session:start':
        if (meta.type === 'web') {
          const payload = msg.payload;
          if (payload.deviceId) { this.sendToBridge(payload.deviceId, msg); }
        }
        break;
      default:
    }
  }
  sendToBridge(deviceId, msg) {}
}
