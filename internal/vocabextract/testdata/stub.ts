class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'stub:type':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
}
