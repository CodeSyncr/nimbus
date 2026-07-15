/**
 * @codesyncr/hive — Type system
 *
 * Provides all type utilities for Nimbus Hive's end-to-end type safety.
 */

// ── Registry types ────────────────────────────────────────────────────────────

/** Shape of a single route entry in the generated registry. */
export type RouteDefinition = {
  readonly method: string
  readonly path: string
  readonly params: Record<string, 'string' | 'number'>
  readonly types?: {
    params?: Record<string, any>
    query?: Record<string, any>
    body?: Record<string, any>
    response?: any
    errors?: Record<number, any>
  }
}

/** The generated registry type (from .nimbus-client/registry.ts). */
export type Registry = Record<string, RouteDefinition>

// ── Request/Response extraction helpers ───────────────────────────────────────

/** Extract the params type for a given route name. */
export type RouteParams<
  R extends Registry,
  Name extends keyof R,
> = R[Name]['types'] extends { params: infer P } ? P : Record<string, never>

/** Extract the query type for a given route name. */
export type RouteQuery<
  R extends Registry,
  Name extends keyof R,
> = R[Name]['types'] extends { query: infer Q } ? Q : Record<string, never>

/** Extract the body type for a given route name. */
export type RouteBody<
  R extends Registry,
  Name extends keyof R,
> = R[Name]['types'] extends { body: infer B } ? B : Record<string, never>

/** Extract the response type for a given route name. */
export type RouteResponse<
  R extends Registry,
  Name extends keyof R,
> = R[Name]['types'] extends { response: infer Res } ? Res : any

/** Extract typed errors for a given route name. */
export type RouteErrors<
  R extends Registry,
  Name extends keyof R,
> = R[Name]['types'] extends { errors: infer E } ? E : Record<number, unknown>

// ── Request options ───────────────────────────────────────────────────────────

/** Inferred call options for a specific route. */
export type CallOptions<
  R extends Registry,
  Name extends keyof R,
> = ([RouteParams<R, Name>] extends [Record<string, never>] ? object : { params: RouteParams<R, Name> }) &
  ([RouteQuery<R, Name>] extends [Record<string, never>] ? object : { query?: RouteQuery<R, Name> }) &
  ([RouteBody<R, Name>] extends [Record<string, never>] ? object : { body: RouteBody<R, Name> }) & {
    /** Override response parsing: 'json' | 'text' | 'arrayBuffer' | 'blob' */
    responseType?: 'json' | 'text' | 'arrayBuffer' | 'blob'
    /** Per-request timeout in ms. Overrides HiveConfig.timeout for this call. */
    timeout?: number
    /**
     * Caller-supplied AbortSignal. Combined with the internal timeout signal,
     * so aborting here cancels the request without clobbering the timeout.
     */
    signal?: AbortSignal
    /** Per-request retry override. Merges over HiveConfig.retry. */
    retry?: Partial<RetryConfig>
    /** Additional fetch init options for this specific request. */
    fetchInit?: RequestInit
  }

// ── Error types ───────────────────────────────────────────────────────────────

/** Discriminated union of HTTP and network errors. */
export type HiveError<TErrors = Record<number, unknown>> =
  | HiveHTTPError<TErrors>
  | HiveNetworkError

export interface HiveHTTPError<TErrors = Record<number, unknown>> {
  readonly kind: 'http'
  readonly status: number
  readonly response: unknown
  readonly message: string
  /** Narrow this error to a specific HTTP status code. */
  isStatus<S extends keyof TErrors>(status: S): this is HiveHTTPError<TErrors> & { status: S; response: TErrors[S] }
  isStatus(status: number): boolean
  /** Returns true when status is 422 (validation error). */
  isValidationError(): this is HiveHTTPError<TErrors> & { response: { errors: ValidationError[] } }
}

export interface HiveNetworkError {
  readonly kind: 'network'
  readonly status: undefined
  readonly response: undefined
  readonly message: string
  isStatus(_status: number): false
  isValidationError(): false
}

/** A single VineJS-style validation error. */
export interface ValidationError {
  field: string
  message: string
  rule?: string
}

// ── Safe result tuple ─────────────────────────────────────────────────────────

export type SafeResult<T, TErrors = Record<number, unknown>> =
  | [data: T, error: null]
  | [data: null, error: HiveError<TErrors>]

// ── Callable result (supports .safe()) ───────────────────────────────────────

export interface HiveRequest<T, TErrors = Record<number, unknown>> extends Promise<T> {
  /**
   * Returns a [data, error] tuple instead of throwing.
   *
   * @example
   * const [post, error] = await client.posts.show({ params: { id: '1' } }).safe()
   * if (error?.isStatus(404)) { ... }
   */
  safe(): Promise<SafeResult<T, TErrors>>
}

// ── URL generation ────────────────────────────────────────────────────────────

export type UrlForResult = { method: string; url: string }

// ── Type helpers (Route<> and Path<>) ─────────────────────────────────────────

/**
 * Extract type information by route name.
 *
 * @example
 * type Body    = Route.Body<Registry, 'posts.store'>
 * type Response = Route.Response<Registry, 'posts.show'>
 * type Error   = Route.Error<Registry, 'posts.show'>
 */
export namespace Route {
  export type Request<R extends Registry, Name extends keyof R> = CallOptions<R, Name>
  export type Response<R extends Registry, Name extends keyof R> = RouteResponse<R, Name>
  export type Error<R extends Registry, Name extends keyof R> = HiveError<RouteErrors<R, Name>>
  export type Params<R extends Registry, Name extends keyof R> = RouteParams<R, Name>
  export type Body<R extends Registry, Name extends keyof R> = RouteBody<R, Name>
  export type Query<R extends Registry, Name extends keyof R> = RouteQuery<R, Name>
}

// ── Hive configuration ────────────────────────────────────────────────────────

export interface HiveConfig<R extends Registry = Registry> {
  /** Base URL of the Nimbus API server. */
  baseUrl: string
  /** The generated route registry from .nimbus-client/registry.ts */
  registry: R
  /** Default headers sent with every request. */
  headers?: Record<string, string>
  /**
   * Include credentials (cookies) with cross-origin requests.
   * Set to 'include' for session-based auth.
   */
  credentials?: RequestCredentials
  /** Request timeout in milliseconds. Default: 30000 (30s). */
  timeout?: number
  /** Automatic retry configuration. Disabled unless `limit` > 0. */
  retry?: RetryConfig
  /** Request/response lifecycle hooks. */
  hooks?: {
    beforeRequest?: Array<(request: Request) => void | Promise<void>>
    afterResponse?: Array<(request: Request, response: Response) => void | Promise<void>>
    beforeError?: Array<(error: HiveError) => HiveError | Promise<HiveError>>
  }
}

/**
 * Retry policy. A request is retried when it fails with a network error, or
 * responds with a status in `statusCodes`, up to `limit` times — but only for
 * idempotent `methods`. Delay uses exponential backoff with jitter, capped at
 * `backoffLimit`, and honors a `Retry-After` response header when present.
 */
export interface RetryConfig {
  /** Maximum number of retries (0 disables retrying). */
  limit: number
  /** HTTP methods eligible for retry. Default: GET, PUT, HEAD, DELETE, OPTIONS, TRACE. */
  methods?: string[]
  /** Response status codes that trigger a retry. Default: 408, 413, 429, 500, 502, 503, 504. */
  statusCodes?: number[]
  /** Maximum backoff delay in ms. Default: 30000. */
  backoffLimit?: number
  /** Called before each retry sleep. */
  onRetry?: (info: RetryInfo) => void | Promise<void>
}

/** Context passed to RetryConfig.onRetry. */
export interface RetryInfo {
  /** 1-based index of the retry about to happen. */
  attempt: number
  /** The error (or synthetic HTTP error) that triggered the retry. */
  error: HiveError
  /** Milliseconds Hive will wait before the retry. */
  delay: number
}

// ── Proxy Client Type Generation Helpers ─────────────────────────────────────

type Split<S extends string, D extends string> =
  S extends `${infer T}${D}${infer U}` ? [T, ...Split<U, D>] : [S]

type NestRoute<Paths extends string[], R extends Registry, FullName extends keyof R> =
  Paths extends [infer Head, ...infer Tail]
    ? Head extends string
      ? Tail extends string[]
        ? {
            [K in Head]: Tail['length'] extends 0
              ? (options: CallOptions<R, FullName>) => HiveRequest<RouteResponse<R, FullName>, RouteErrors<R, FullName>>
              : NestRoute<Tail, R, FullName>
          }
        : never
      : never
    : never

type UnionToIntersection<U> =
  (U extends any ? (k: U) => void : never) extends ((k: infer I) => void) ? I : never

export type ApiProxy<R extends Registry> = UnionToIntersection<
  {
    [K in keyof R & string]: NestRoute<Split<K, '.'>, R, K>
  }[keyof R & string]
>

