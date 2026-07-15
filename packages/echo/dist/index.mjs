// src/index.ts
var Channel = class {
  constructor(echo, name) {
    this.callbacks = /* @__PURE__ */ new Map();
    this.echo = echo;
    this.name = name;
  }
  /** Listen for a specific event on this channel */
  listen(event, callback) {
    if (!this.callbacks.has(event)) {
      this.callbacks.set(event, /* @__PURE__ */ new Set());
    }
    this.callbacks.get(event).add(callback);
    this.echo._registerListener(this.name, event, callback);
    return this;
  }
  /** Listen for all events on this channel */
  listenAll(callback) {
    return this.listen("*", callback);
  }
  /** Remove a listener. Omit `callback` to remove every listener for the event. */
  stopListening(event, callback) {
    if (callback) {
      this.callbacks.get(event)?.delete(callback);
    } else {
      this.callbacks.delete(event);
    }
    this.echo._unregisterListener(this.name, event, callback);
    return this;
  }
  /** Unsubscribe from this channel entirely */
  unsubscribe() {
    this.echo._unsubscribe(this.name);
    this.callbacks.clear();
  }
};
var PrivateChannel = class extends Channel {
  constructor(echo, name) {
    super(echo, `private-${name}`);
  }
};
var PresenceChannel = class extends Channel {
  constructor(echo, name) {
    super(echo, `presence-${name}`);
  }
  /** Called with the list of currently present users */
  here(callback) {
    this.hereCb = callback;
    this.echo._registerListener(this.name, "__presence:here", (data) => {
      callback(data.users || []);
    });
    return this;
  }
  /** Called when a new user joins */
  joining(callback) {
    this.joiningCb = callback;
    this.echo._registerListener(this.name, "__presence:joining", (data) => {
      callback(data.user);
    });
    return this;
  }
  /** Called when a user leaves */
  leaving(callback) {
    this.leavingCb = callback;
    this.echo._registerListener(this.name, "__presence:leaving", (data) => {
      callback(data.user);
    });
    return this;
  }
};
var Echo = class {
  constructor(config) {
    this.eventSource = null;
    this.uid = "";
    this.subscriptions = /* @__PURE__ */ new Map();
    this.listeners = /* @__PURE__ */ new Map();
    this.reconnectAttempts = 0;
    this.connected = false;
    this.connecting = false;
    this.config = {
      baseURL: config.baseURL.replace(/\/$/, ""),
      path: config.path || "__transmit",
      bearerToken: config.bearerToken || "",
      csrfToken: config.csrfToken || "",
      autoReconnect: config.autoReconnect ?? true,
      reconnectDelay: config.reconnectDelay || 1e3,
      maxReconnectAttempts: config.maxReconnectAttempts || Infinity,
      headers: config.headers || {}
    };
  }
  // ── Public API ──────────────────────────────────────────────
  /** Subscribe to a public channel */
  channel(name) {
    const ch = new Channel(this, name);
    this._subscribe(name);
    return ch;
  }
  /** Subscribe to a private (authenticated) channel */
  private_(name) {
    const ch = new PrivateChannel(this, name);
    this._subscribe(`private-${name}`);
    return ch;
  }
  /** Alias for private_ */
  private(name) {
    return this.private_(name);
  }
  /** Join a presence channel */
  join(name) {
    const ch = new PresenceChannel(this, name);
    this._subscribe(`presence-${name}`);
    return ch;
  }
  /** Leave a channel */
  leave(name) {
    this._unsubscribe(name);
    this._unsubscribe(`private-${name}`);
    this._unsubscribe(`presence-${name}`);
  }
  /** Set bearer token for authenticated channels */
  setBearerToken(token) {
    this.config.bearerToken = token;
    return this;
  }
  /** Set CSRF token */
  setCsrfToken(token) {
    this.config.csrfToken = token;
    return this;
  }
  /** Register connection event listener */
  onConnect(cb) {
    this.onConnectCb = cb;
    return this;
  }
  /** Register disconnection event listener */
  onDisconnect(cb) {
    this.onDisconnectCb = cb;
    return this;
  }
  /** Register error event listener */
  onError(cb) {
    this.onErrorCb = cb;
    return this;
  }
  /** Get the connection UID */
  getUid() {
    return this.uid;
  }
  /** Check if connected */
  isConnected() {
    return this.connected;
  }
  /** Disconnect from the SSE stream */
  disconnect() {
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
    this.connected = false;
    this.connecting = false;
    this.uid = "";
    this.subscriptions.clear();
    this.listeners.clear();
    this.onDisconnectCb?.();
  }
  // ── Internal Methods ────────────────────────────────────────
  /** @internal Connect to SSE if not already connected */
  connect() {
    if (this.connected || this.connecting) return;
    this.connecting = true;
    const url = `${this.config.baseURL}/${this.config.path}/events`;
    this.eventSource = new EventSource(url);
    this.eventSource.onopen = () => {
      this.connected = true;
      this.connecting = false;
      this.reconnectAttempts = 0;
      this.onConnectCb?.();
    };
    this.eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.uid) {
          this.uid = data.uid;
          for (const [channel] of this.subscriptions) {
            this.sendSubscribe(channel);
          }
          return;
        }
        if (data.channel) {
          this.dispatchEvent(data.channel, data.event || "*", data.payload || data);
          return;
        }
        if (data.type === "ping") return;
      } catch {
      }
    };
    this.eventSource.onerror = (error) => {
      this.connected = false;
      this.connecting = false;
      this.onErrorCb?.(error);
      this.onDisconnectCb?.();
      if (this.config.autoReconnect && this.reconnectAttempts < this.config.maxReconnectAttempts) {
        this.reconnectAttempts++;
        const delay = this.config.reconnectDelay * Math.min(this.reconnectAttempts, 10);
        setTimeout(() => this.connect(), delay);
      }
    };
  }
  /** @internal Subscribe to a channel on the server */
  _subscribe(channel) {
    if (this.subscriptions.has(channel)) return;
    this.subscriptions.set(channel, {
      channel,
      callbacks: /* @__PURE__ */ new Map()
    });
    if (!this.connected && !this.connecting) {
      this.connect();
    }
    if (this.uid) {
      this.sendSubscribe(channel);
    }
  }
  /** @internal Unsubscribe from a channel */
  _unsubscribe(channel) {
    if (!this.subscriptions.has(channel)) return;
    this.subscriptions.delete(channel);
    this.listeners.delete(channel);
    if (this.uid) {
      this.sendUnsubscribe(channel);
    }
    if (this.subscriptions.size === 0) {
      this.disconnect();
    }
  }
  /** @internal Register a listener for a channel event */
  _registerListener(channel, event, callback) {
    if (!this.listeners.has(channel)) {
      this.listeners.set(channel, /* @__PURE__ */ new Map());
    }
    const channelListeners = this.listeners.get(channel);
    if (!channelListeners.has(event)) {
      channelListeners.set(event, /* @__PURE__ */ new Set());
    }
    channelListeners.get(event).add(callback);
  }
  /**
   * @internal Remove a listener for a channel event. When `callback` is
   * omitted, every listener for that event is removed.
   */
  _unregisterListener(channel, event, callback) {
    const channelListeners = this.listeners.get(channel);
    if (!channelListeners) return;
    if (callback) {
      const eventListeners = channelListeners.get(event);
      eventListeners?.delete(callback);
      if (eventListeners && eventListeners.size === 0) {
        channelListeners.delete(event);
      }
    } else {
      channelListeners.delete(event);
    }
    if (channelListeners.size === 0) {
      this.listeners.delete(channel);
    }
  }
  /** @internal Dispatch an event to registered listeners */
  dispatchEvent(channel, event, data) {
    const channelListeners = this.listeners.get(channel);
    if (!channelListeners) return;
    const eventListeners = channelListeners.get(event);
    if (eventListeners) {
      for (const cb of eventListeners) {
        try {
          cb(data);
        } catch (e) {
          console.error("[Echo] Listener error:", e);
        }
      }
    }
    const wildcardListeners = channelListeners.get("*");
    if (wildcardListeners) {
      for (const cb of wildcardListeners) {
        try {
          cb({ event, data });
        } catch (e) {
          console.error("[Echo] Listener error:", e);
        }
      }
    }
  }
  /** @internal Send subscribe request to server */
  async sendSubscribe(channel) {
    try {
      const url = `${this.config.baseURL}/${this.config.path}/subscribe`;
      const headers = {
        "Content-Type": "application/json",
        ...this.config.headers
      };
      if (this.config.bearerToken) {
        headers["Authorization"] = `Bearer ${this.config.bearerToken}`;
      }
      const body = { uid: this.uid, channel };
      if (this.config.csrfToken) {
        body.csrf_token = this.config.csrfToken;
      }
      await fetch(url, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
        credentials: "include"
      });
    } catch (e) {
      console.error(`[Echo] Failed to subscribe to ${channel}:`, e);
    }
  }
  /** @internal Send unsubscribe request to server */
  async sendUnsubscribe(channel) {
    try {
      const url = `${this.config.baseURL}/${this.config.path}/unsubscribe`;
      const headers = {
        "Content-Type": "application/json",
        ...this.config.headers
      };
      const body = { uid: this.uid, channel };
      if (this.config.csrfToken) {
        body.csrf_token = this.config.csrfToken;
      }
      await fetch(url, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
        credentials: "include"
      });
    } catch (e) {
    }
  }
};
var index_default = Echo;
export {
  Channel,
  Echo,
  PresenceChannel,
  PrivateChannel,
  index_default as default
};
