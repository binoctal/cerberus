class UserRoom {
  async fetch(request) {
    const msg = await request.json();
    this.broadcastToWeb(msg);
    return new Response('ok');
  }
  broadcastToWeb(msg) {}
}
