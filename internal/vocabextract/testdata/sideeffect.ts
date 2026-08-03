class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'workflow:task_progress':
      case 'workflow:task_result':
        if (meta.type === 'bridge') {
          this.broadcastToWeb(msg);
          if (msg.type === 'workflow:task_progress' || msg.type === 'workflow:task_result') {
            this.notifyOrchestrator(msg);
          }
        }
        break;
      default:
    }
  }
  broadcastToWeb(msg) {}
  notifyOrchestrator(msg) {}
}
