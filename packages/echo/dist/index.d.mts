/**
 * @codesyncr/echo — Real-time client SDK for Nimbus Transmit SSE
 *
 * Usage:
 *   import { Echo } from '@codesyncr/echo'
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
interface EchoConfig {
    /** Base URL of the Nimbus server (e.g. "http://localhost:3333") */
    baseURL: string;
    /** Path prefix for Transmit routes (default: "__transmit") */
    path?: string;
    /** Bearer token for authenticated channels */
    bearerToken?: string;
    /** CSRF token for POST requests */
    csrfToken?: string;
    /** Auto-reconnect on disconnect (default: true) */
    autoReconnect?: boolean;
    /** Reconnect delay in ms (default: 1000) */
    reconnectDelay?: number;
    /** Max reconnect attempts (default: Infinity) */
    maxReconnectAttempts?: number;
    /** Custom headers for SSE connection */
    headers?: Record<string, string>;
}
type EventCallback = (data: any) => void;
type PresenceCallback = (users: any[]) => void;
type UserCallback = (user: any) => void;
declare class Channel {
    protected echo: Echo;
    protected name: string;
    protected callbacks: Map<string, Set<EventCallback>>;
    constructor(echo: Echo, name: string);
    /** Listen for a specific event on this channel */
    listen(event: string, callback: EventCallback): this;
    /** Listen for all events on this channel */
    listenAll(callback: EventCallback): this;
    /** Remove a listener. Omit `callback` to remove every listener for the event. */
    stopListening(event: string, callback?: EventCallback): this;
    /** Unsubscribe from this channel entirely */
    unsubscribe(): void;
}
declare class PrivateChannel extends Channel {
    constructor(echo: Echo, name: string);
}
declare class PresenceChannel extends Channel {
    private hereCb?;
    private joiningCb?;
    private leavingCb?;
    constructor(echo: Echo, name: string);
    /** Called with the list of currently present users */
    here(callback: PresenceCallback): this;
    /** Called when a new user joins */
    joining(callback: UserCallback): this;
    /** Called when a user leaves */
    leaving(callback: UserCallback): this;
}
declare class Echo {
    private config;
    private eventSource;
    private uid;
    private subscriptions;
    private listeners;
    private reconnectAttempts;
    private connected;
    private connecting;
    private onConnectCb?;
    private onDisconnectCb?;
    private onErrorCb?;
    constructor(config: EchoConfig);
    /** Subscribe to a public channel */
    channel(name: string): Channel;
    /** Subscribe to a private (authenticated) channel */
    private_(name: string): PrivateChannel;
    /** Alias for private_ */
    private(name: string): PrivateChannel;
    /** Join a presence channel */
    join(name: string): PresenceChannel;
    /** Leave a channel */
    leave(name: string): void;
    /** Set bearer token for authenticated channels */
    setBearerToken(token: string): this;
    /** Set CSRF token */
    setCsrfToken(token: string): this;
    /** Register connection event listener */
    onConnect(cb: () => void): this;
    /** Register disconnection event listener */
    onDisconnect(cb: () => void): this;
    /** Register error event listener */
    onError(cb: (error: any) => void): this;
    /** Get the connection UID */
    getUid(): string;
    /** Check if connected */
    isConnected(): boolean;
    /** Disconnect from the SSE stream */
    disconnect(): void;
    /** @internal Connect to SSE if not already connected */
    private connect;
    /** @internal Subscribe to a channel on the server */
    _subscribe(channel: string): void;
    /** @internal Unsubscribe from a channel */
    _unsubscribe(channel: string): void;
    /** @internal Register a listener for a channel event */
    _registerListener(channel: string, event: string, callback: EventCallback): void;
    /**
     * @internal Remove a listener for a channel event. When `callback` is
     * omitted, every listener for that event is removed.
     */
    _unregisterListener(channel: string, event: string, callback?: EventCallback): void;
    /** @internal Dispatch an event to registered listeners */
    private dispatchEvent;
    /** @internal Send subscribe request to server */
    private sendSubscribe;
    /** @internal Send unsubscribe request to server */
    private sendUnsubscribe;
}

export { Channel, Echo, type EchoConfig, type EventCallback, type PresenceCallback, PresenceChannel, PrivateChannel, type UserCallback, Echo as default };
