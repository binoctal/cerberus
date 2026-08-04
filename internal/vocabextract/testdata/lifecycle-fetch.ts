class UserRoom {
  async fetch(request) {
    this.broadcastToWeb({ type: 'broadcast:lifecycle' });
    return new Response('ok');
  }
  broadcastToWeb(msg) {}
}
