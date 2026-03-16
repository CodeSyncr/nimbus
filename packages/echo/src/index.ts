/**
 * @nimbus/echo — Real-time client SDK for Nimbus Transmit SSE
 *
 * Usage:
 *   import { Echo } from '@nimbus/echo'
 *
 *   const echo = new Echo({ baseURL: 'http://localhost:3333' })
 *
 *   echo.channel('notifications')
 *     .listen('NewMessage', (data) => console.log(data))
 *
 *   echo.private('projects.1')
 *     .listen('RenderComplete', (data) => console.log(data))
 *
 *   echo.join('room.1')
 *     .here((users) => console.log('Online:', users))
 *     .joining((user) => console.log('Joined:', user))
 *     .leaving((user) => console.log('Left:', user))
 */

// ── Types ───────────────────────────────────────────────────────

export interface EchoConfig {
  /** Base URL of the Nimbus server (e.g. "http://localhost:3333") */
  baseURL: string
  /** Path prefix for Transmit routes (default: "__transmit") */
  path?: string
  /** Bearer token for authenticated channels */
  bearerToken?: string
  /** CSRF token for POST requests */
  csrfToken?: string
  /** Auto-reconnect on disconnect (default: true) */
  autoReconnect?: boolean
  /** Reconnect delay in ms (default: 1000) */
  reconnectDelay?: number
  /** Max reconnect attempts (default: Infinity) */
  maxReconnectAttempts?: number
  /** Custom headers for SSE connection */
  headers?: Record<string, string>
}

export type EventCallback = (data: any) => void
export type PresenceCallback = (users: any[]) => void
export type UserCallback = (user: any) => void

interface Subscription {
  channel: string
  callbacks: Map<string, Set<EventCallback>>
}

// ── Channel Classes ─────────────────────────────────────────────

export class Channel {
  protected echo: Echo
  protected name: string
  protected callbacks = new Map<string, Set<EventCallback>>()

  constructor(echo: Echo, name: string) {
    this.echo = echo
    this.name = name
  }

  /** Listen for a specific event on this channel */
  listen(event: string, callback: EventCallback): this {
    if (!this.callbacks.has(event)) {
      this.callbacks.set(event, new Set())
    }
    this.callbacks.get(event)!.add(callback)
    this.echo._registerListener(this.name, event, callback)
    return this
  }

  /** Listen for all events on this channel */
  listenAll(callback: EventCallback): this {
    return this.listen('*', callback)
  }

  /** Remove a listener */
  stopListening(event: string, callback?: EventCallback): this {
    if (callback) {
      this.callbacks.get(event)?.delete(callback)
    } else {
      this.callbacks.delete(event)
    }
    return this
  }

  /** Unsubscribe from this channel entirely */
  unsubscribe(): void {
    this.echo._unsubscribe(this.name)
    this.callbacks.clear()
  }
}

export class PrivateChannel extends Channel {
  constructor(echo: Echo, name: string) {
    super(echo, `private-${name}`)
  }
}

export class PresenceChannel extends Channel {
  private hereCb?: PresenceCallback
  private joiningCb?: UserCallback
  private leavingCb?: UserCallback

  constructor(echo: Echo, name: string) {
    super(echo, `presence-${name}`)
  }

  /** Called with the list of currently present users */
  here(callback: PresenceCallback): this {
    this.hereCb = callback
    this.echo._registerListener(this.name, '__presence:here', (data) => {
      callback(data.users || [])
    })
    return this
  }

  /** Called when a new user joins */
  joining(callback: UserCallback): this {
    this.joiningCb = callback
    this.echo._registerListener(this.name, '__presence:joining', (data) => {
      callback(data.user)
    })
    return this
  }

  /** Called when a user leaves */
  leaving(callback: UserCallback): this {
    this.leavingCb = callback
    this.echo._registerListener(this.name, '__presence:leaving', (data) => {
      callback(data.user)
    })
    return this
  }
}

// ── Echo Client ─────────────────────────────────────────────────

export class Echo {
  private config: Required<EchoConfig>
  private eventSource: EventSource | null = null
  private uid: string = ''
  private subscriptions = new Map<string, Subscription>()
  private listeners = new Map<string, Map<string, Set<EventCallback>>>()
  private reconnectAttempts = 0
  private connected = false
  private connecting = false

  // Connection event callbacks
  private onConnectCb?: () => void
  private onDisconnectCb?: () => void
  private onErrorCb?: (error: any) => void

  constructor(config: EchoConfig) {
    this.config = {
      baseURL: config.baseURL.replace(/\/$/, ''),
      path: config.path || '__transmit',
      bearerToken: config.bearerToken || '',
      csrfToken: config.csrfToken || '',
      autoReconnect: config.autoReconnect ?? true,
      reconnectDelay: config.reconnectDelay || 1000,
      maxReconnectAttempts: config.maxReconnectAttempts || Infinity,
      headers: config.headers || {},
    }
  }

  // ── Public API ──────────────────────────────────────────────

  /** Subscribe to a public channel */
  channel(name: string): Channel {
    const ch = new Channel(this, name)
    this._subscribe(name)
    return ch
  }

  /** Subscribe to a private (authenticated) channel */
  private_(name: string): PrivateChannel {
    const ch = new PrivateChannel(this, name)
    this._subscribe(`private-${name}`)
    return ch
  }

  /** Alias for private_ */
  private(name: string): PrivateChannel {
    return this.private_(name)
  }

  /** Join a presence channel */
  join(name: string): PresenceChannel {
    const ch = new PresenceChannel(this, name)
    this._subscribe(`presence-${name}`)
    return ch
  }

  /** Leave a channel */
  leave(name: string): void {
    this._unsubscribe(name)
    this._unsubscribe(`private-${name}`)
    this._unsubscribe(`presence-${name}`)
  }

  /** Set bearer token for authenticated channels */
  setBearerToken(token: string): this {
    this.config.bearerToken = token
    return this
  }

  /** Set CSRF token */
  setCsrfToken(token: string): this {
    this.config.csrfToken = token
    return this
  }

  /** Register connection event listener */
  onConnect(cb: () => void): this {
    this.onConnectCb = cb
    return this
  }

  /** Register disconnection event listener */
  onDisconnect(cb: () => void): this {
    this.onDisconnectCb = cb
    return this
  }

  /** Register error event listener */
  onError(cb: (error: any) => void): this {
    this.onErrorCb = cb
    return this
  }

  /** Get the connection UID */
  getUid(): string {
    return this.uid
  }

  /** Check if connected */
  isConnected(): boolean {
    return this.connected
  }

  /** Disconnect from the SSE stream */
  disconnect(): void {
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }
    this.connected = false
    this.connecting = false
    this.uid = ''
    this.subscriptions.clear()
    this.listeners.clear()
    this.onDisconnectCb?.()
  }

  // ── Internal Methods ────────────────────────────────────────

  /** @internal Connect to SSE if not already connected */
  private connect(): void {
    if (this.connected || this.connecting) return
    this.connecting = true

    const url = `${this.config.baseURL}/${this.config.path}/events`
    this.eventSource = new EventSource(url)

    this.eventSource.onopen = () => {
      this.connected = true
      this.connecting = false
      this.reconnectAttempts = 0
      this.onConnectCb?.()
    }

    this.eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)

        // Handle initial UID message
        if (data.uid) {
          this.uid = data.uid
          // Re-subscribe to channels that were waiting for connection
          for (const [channel] of this.subscriptions) {
            this.sendSubscribe(channel)
          }
          return
        }

        // Handle channel event
        if (data.channel) {
          this.dispatchEvent(data.channel, data.event || '*', data.payload || data)
          return
        }

        // Handle ping
        if (data.type === 'ping') return

      } catch {
        // Non-JSON message, ignore
      }
    }

    this.eventSource.onerror = (error) => {
      this.connected = false
      this.connecting = false
      this.onErrorCb?.(error)
      this.onDisconnectCb?.()

      if (this.config.autoReconnect && this.reconnectAttempts < this.config.maxReconnectAttempts) {
        this.reconnectAttempts++
        const delay = this.config.reconnectDelay * Math.min(this.reconnectAttempts, 10)
        setTimeout(() => this.connect(), delay)
      }
    }
  }

  /** @internal Subscribe to a channel on the server */
  _subscribe(channel: string): void {
    if (this.subscriptions.has(channel)) return

    this.subscriptions.set(channel, {
      channel,
      callbacks: new Map(),
    })

    if (!this.connected && !this.connecting) {
      this.connect()
    }

    if (this.uid) {
      this.sendSubscribe(channel)
    }
  }

  /** @internal Unsubscribe from a channel */
  _unsubscribe(channel: string): void {
    if (!this.subscriptions.has(channel)) return
    this.subscriptions.delete(channel)
    this.listeners.delete(channel)

    if (this.uid) {
      this.sendUnsubscribe(channel)
    }

    // If no subscriptions left, disconnect
    if (this.subscriptions.size === 0) {
      this.disconnect()
    }
  }

  /** @internal Register a listener for a channel event */
  _registerListener(channel: string, event: string, callback: EventCallback): void {
    if (!this.listeners.has(channel)) {
      this.listeners.set(channel, new Map())
    }
    const channelListeners = this.listeners.get(channel)!
    if (!channelListeners.has(event)) {
      channelListeners.set(event, new Set())
    }
    channelListeners.get(event)!.add(callback)
  }

  /** @internal Dispatch an event to registered listeners */
  private dispatchEvent(channel: string, event: string, data: any): void {
    const channelListeners = this.listeners.get(channel)
    if (!channelListeners) return

    // Dispatch to specific event listeners
    const eventListeners = channelListeners.get(event)
    if (eventListeners) {
      for (const cb of eventListeners) {
        try { cb(data) } catch (e) { console.error('[Echo] Listener error:', e) }
      }
    }

    // Dispatch to wildcard listeners
    const wildcardListeners = channelListeners.get('*')
    if (wildcardListeners) {
      for (const cb of wildcardListeners) {
        try { cb({ event, data }) } catch (e) { console.error('[Echo] Listener error:', e) }
      }
    }
  }

  /** @internal Send subscribe request to server */
  private async sendSubscribe(channel: string): Promise<void> {
    try {
      const url = `${this.config.baseURL}/${this.config.path}/subscribe`
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        ...this.config.headers,
      }
      if (this.config.bearerToken) {
        headers['Authorization'] = `Bearer ${this.config.bearerToken}`
      }

      const body: any = { uid: this.uid, channel }
      if (this.config.csrfToken) {
        body.csrf_token = this.config.csrfToken
      }

      await fetch(url, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        credentials: 'include',
      })
    } catch (e) {
      console.error(`[Echo] Failed to subscribe to ${channel}:`, e)
    }
  }

  /** @internal Send unsubscribe request to server */
  private async sendUnsubscribe(channel: string): Promise<void> {
    try {
      const url = `${this.config.baseURL}/${this.config.path}/unsubscribe`
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        ...this.config.headers,
      }

      const body: any = { uid: this.uid, channel }
      if (this.config.csrfToken) {
        body.csrf_token = this.config.csrfToken
      }

      await fetch(url, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        credentials: 'include',
      })
    } catch (e) {
      // Ignore unsubscribe errors (best effort)
    }
  }
}

// ── Default export ──────────────────────────────────────────────

export default Echo
