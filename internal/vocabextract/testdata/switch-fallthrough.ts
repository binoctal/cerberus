class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'encrypted':
      case 'session:created':
      case 'session:started':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      case 'workflow:task_progress':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
}
