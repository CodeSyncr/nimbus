import type {
  Registry,
  HiveConfig,
  CallOptions,
  HiveRequest,
  HiveError,
  HiveHTTPError,
  HiveNetworkError,
  SafeResult,
  ValidationError,
  UrlForResult,
  RouteResponse,
  RouteErrors,
  RouteParams,
  ApiProxy,
} from './types.js';

// Helper to determine if a value is an object
const isObject = (val: unknown): val is Record<string, unknown> =>
  typeof val === 'object' && val !== null;

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// Substitute path parameters, supporting both Nimbus syntaxes: "/posts/:id"
// and "/posts/{id}". Values are URL-encoded. The negative lookahead stops
// ":id" from matching the ":idx" prefix in "/posts/:idx".
function substituteParams(path: string, params?: Record<string, unknown>): string {
  if (!params) return path;
  let out = path;
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    const encoded = encodeURIComponent(String(value));
    out = out
      .replace(new RegExp(`\\{${escapeRe(key)}\\}`, 'g'), encoded)
      .replace(new RegExp(`:${escapeRe(key)}(?![a-zA-Z0-9_])`, 'g'), encoded);
  }
  return out;
}

// Append query values to a URL, serializing arrays as repeated params
// (e.g. { tag: ['a', 'b'] } -> ?tag=a&tag=b) and skipping null/undefined.
function appendQuery(url: URL, query: Record<string, unknown>): void {
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item !== undefined && item !== null) url.searchParams.append(key, String(item));
      }
    } else {
      url.searchParams.append(key, String(value));
    }
  }
}

const sleep = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms));

// Exponential backoff with full jitter, capped at limit.
function computeBackoff(attempt: number, limit: number): number {
  const base = Math.min(limit, 300 * 2 ** (attempt - 1));
  return Math.round(Math.random() * base);
}

// Parse a Retry-After header (seconds or HTTP-date) into ms, capped at limit.
function parseRetryAfter(response: Response, limit: number): number | undefined {
  const header = response.headers.get('retry-after');
  if (!header) return undefined;
  let ms: number;
  if (/^\d+$/.test(header.trim())) {
    ms = Number(header) * 1000;
  } else {
    const date = Date.parse(header);
    if (Number.isNaN(date)) return undefined;
    ms = date - Date.now();
  }
  if (ms < 0) ms = 0;
  return Math.min(ms, limit);
}

// Combine caller and timeout signals so either can abort the request. Falls
// back to a manual bridge on runtimes without AbortSignal.any.
function combineSignals(signals: (AbortSignal | undefined)[]): AbortSignal {
  const active = signals.filter((s): s is AbortSignal => !!s);
  if (active.length === 1) return active[0];
  if (typeof (AbortSignal as any).any === 'function') {
    return (AbortSignal as any).any(active);
  }
  const controller = new AbortController();
  for (const s of active) {
    if (s.aborted) {
      controller.abort((s as any).reason);
      break;
    }
    s.addEventListener('abort', () => controller.abort((s as any).reason), { once: true });
  }
  return controller.signal;
}

const DEFAULT_RETRY_METHODS = ['get', 'put', 'head', 'delete', 'options', 'trace'];
const DEFAULT_RETRY_STATUS = [408, 413, 429, 500, 502, 503, 504];

// Parse response helper based on type
async function parseResponse(response: Response, type?: string) {
  if (type === 'blob') return response.blob();
  if (type === 'arrayBuffer') return response.arrayBuffer();
  if (type === 'text') return response.text();

  const contentType = response.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return response.json();
  }
  return response.text();
}

// Build standard error helpers
class HTTPErrorImpl<TErrors = Record<number, unknown>> implements HiveHTTPError<TErrors> {
  readonly kind = 'http' as const;
  constructor(
    readonly status: number,
    readonly response: any,
    readonly message: string
  ) {}

  isStatus<S extends keyof TErrors>(status: S): this is HiveHTTPError<TErrors> & { status: S; response: TErrors[S] };
  isStatus(status: number): boolean;
  isStatus(status: any): any {
    return this.status === status;
  }

  isValidationError(): this is HiveHTTPError<TErrors> & { response: { errors: ValidationError[] } } {
    return this.status === 422;
  }
}

class NetworkErrorImpl implements HiveNetworkError {
  readonly kind = 'network' as const;
  readonly status = undefined;
  readonly response = undefined;
  constructor(readonly message: string) {}

  isStatus(_status: number): false {
    return false;
  }

  isValidationError(): false {
    return false;
  }
}


// Execute the HTTP fetch call
function makeRequest<R extends Registry, Name extends keyof R>(
  config: HiveConfig<R>,
  routeName: string,
  options: CallOptions<R, Name>
): HiveRequest<any, any> {
  const route = config.registry[routeName];
  if (!route) {
    throw new Error(`Route "${routeName}" not found in registry.`);
  }

  const opts = (options || {}) as any;

  // Substitute path parameters (":id" and "{id}" are both supported)
  const urlPath = substituteParams(route.path, opts.params);

  // Construct URL with query parameters
  const url = new URL(urlPath, config.baseUrl);
  if (opts.query) {
    appendQuery(url, opts.query);
  }

  // Prepare fetch init options
  const fetchInit: RequestInit = {
    method: route.method,
    headers: {
      ...config.headers,
      ...opts.fetchInit?.headers,
    },
    ...opts.fetchInit,
  };

  // Handle body
  if (opts.body) {
    if (opts.body instanceof FormData || opts.body instanceof URLSearchParams || opts.body instanceof Blob) {
      fetchInit.body = opts.body;
    } else if (isObject(opts.body) && Object.values(opts.body).some(v => v instanceof File || (Array.isArray(v) && v[0] instanceof File))) {
      // Automatic FormData conversion if a File is present (Tuyau style)
      const formData = new FormData();
      for (const [key, value] of Object.entries(opts.body)) {
        if (value instanceof File) {
          formData.append(key, value);
        } else if (Array.isArray(value)) {
          value.forEach(item => {
            if (item instanceof File) {
              formData.append(key, item);
            } else {
              formData.append(key, String(item));
            }
          });
        } else if (value !== undefined && value !== null) {
          formData.append(key, String(value));
        }
      }
      fetchInit.body = formData;
    } else {
      fetchInit.body = JSON.stringify(opts.body);
      if (!fetchInit.headers) fetchInit.headers = {};
      (fetchInit.headers as Record<string, string>)['Content-Type'] = 'application/json';
    }
  }

  if (config.credentials) {
    fetchInit.credentials = config.credentials;
  }

  // Resolve retry policy (per-request overrides merged over global config).
  const retry = { ...config.retry, ...opts.retry };
  const maxRetries = retry.limit ?? 0;
  const retryMethods = retry.methods ?? DEFAULT_RETRY_METHODS;
  const retryStatuses = retry.statusCodes ?? DEFAULT_RETRY_STATUS;
  const backoffLimit = retry.backoffLimit ?? 30000;
  const methodRetryable =
    maxRetries > 0 && retryMethods.includes((route.method || 'GET').toLowerCase());

  const timeout = opts.timeout ?? config.timeout ?? 30000;

  // Run beforeError hooks and return the (possibly transformed) error.
  const finalizeError = async (error: HiveError): Promise<HiveError> => {
    if (config.hooks?.beforeError) {
      for (const hook of config.hooks.beforeError) {
        error = await hook(error);
      }
    }
    return error;
  };

  // A single attempt; returns { data } on success, or { retryable, error, delay }.
  const attempt = async (
    n: number
  ): Promise<{ data: any } | { retryable: boolean; error: HiveError; delay: number }> => {
    // Fresh timeout controller per attempt (a signal is single-use).
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);
    fetchInit.signal = combineSignals([controller.signal, opts.signal]);

    // Run beforeRequest hooks against a real Request (mutable headers).
    if (config.hooks?.beforeRequest) {
      const reqObj = new Request(url.toString(), { method: fetchInit.method || 'GET' });
      for (const hook of config.hooks.beforeRequest) {
        await hook(reqObj);
      }
      const merged = new Headers(fetchInit.headers as HeadersInit);
      reqObj.headers.forEach((v, k) => merged.set(k, v));
      fetchInit.headers = merged;
    }

    try {
      const response = await fetch(url.toString(), fetchInit);
      clearTimeout(timeoutId);

      if (config.hooks?.afterResponse) {
        const reqObj = new Request(url.toString(), { method: fetchInit.method || 'GET' });
        for (const hook of config.hooks.afterResponse) {
          await hook(reqObj, response);
        }
      }

      if (!response.ok) {
        const errPayload = await parseResponse(response, opts.responseType);
        const error: HiveError = new HTTPErrorImpl(
          response.status,
          errPayload,
          `Request failed with status ${response.status}`
        );
        const retryable =
          methodRetryable && n < maxRetries && retryStatuses.includes(response.status);
        const delay = parseRetryAfter(response, backoffLimit) ?? computeBackoff(n + 1, backoffLimit);
        return { retryable, error, delay };
      }

      return { data: await parseResponse(response, opts.responseType) };
    } catch (err: any) {
      clearTimeout(timeoutId);
      const error: HiveError = new NetworkErrorImpl(err?.message || 'Network error');
      return { retryable: methodRetryable && n < maxRetries, error, delay: computeBackoff(n + 1, backoffLimit) };
    }
  };

  const performFetch = async () => {
    for (let n = 0; ; n++) {
      const result = await attempt(n);
      if ('data' in result) return result.data;
      if (!result.retryable) throw await finalizeError(result.error);
      if (retry.onRetry) {
        await retry.onRetry({ attempt: n + 1, error: result.error, delay: result.delay });
      }
      await sleep(result.delay);
    }
  };

  const promise = performFetch();

  // Attach .safe() method onto the promise to match Tuyau interface
  const hiveRequest = promise as unknown as HiveRequest<any, any>;
  hiveRequest.safe = async (): Promise<SafeResult<any, any>> => {
    try {
      const data = await promise;
      return [data, null];
    } catch (err: any) {
      return [null, err as HiveError];
    }
  };

  return hiveRequest;
}

// Deep proxy generator for the client.api chaining
function createApiProxy<R extends Registry>(config: HiveConfig<R>, path: string[] = []): any {
  return new Proxy(() => {}, {
    get(_target, prop) {
      if (typeof prop === 'string') {
        return createApiProxy(config, [...path, prop]);
      }
      return undefined;
    },
    apply(_target, _thisArg, args) {
      // Reconstruct route name from path segments: e.g. ["posts", "store"] -> "posts.store"
      const routeName = path.join('.');
      return makeRequest(config, routeName, args[0]);
    },
  });
}

/**
 * The initialized Hive client type.
 */
export interface HiveClient<R extends Registry = Registry> {
  /** The fluent type-safe API client interface. */
  api: ApiProxy<R>

  /** Alternative string-based route call. */
  $<Name extends keyof R>(
    name: Name,
    options: CallOptions<R, Name>
  ): HiveRequest<RouteResponse<R, Name>, RouteErrors<R, Name>>

  /** Safe URL generator helper. */
  url<Name extends keyof R>(
    name: Name,
    params: RouteParams<R, Name>,
    query?: Record<string, unknown>
  ): string

  /** Check if a route exists in the registry. */
  has(name: string): boolean

  /** Check if the current browser route matches a given pattern (browser-only). */
  current(name?: string, options?: { params?: Record<string, unknown>; query?: Record<string, unknown> }): string | boolean | undefined
}

export function createHive<R extends Registry>(config: HiveConfig<R>): HiveClient<R> {
  const apiProxy = createApiProxy<R>(config);

  return {
    api: apiProxy,

    $(name, options) {
      return makeRequest(config, name as string, options);
    },

    url(name, params, query) {
      const route = config.registry[name as string];
      if (!route) {
        throw new Error(`Route "${String(name)}" not found in registry.`);
      }
      const path = substituteParams(route.path, params as Record<string, unknown>);
      const url = new URL(path, config.baseUrl);
      if (query) {
        appendQuery(url, query);
      }
      return url.toString();
    },

    has(name) {
      return !!config.registry[name];
    },

    current(name, options) {
      if (typeof window === 'undefined') return undefined;
      const pathname = window.location.pathname;

      // Find if any registered route matches the current path
      let matchedRouteName: string | undefined;
      for (const [routeName, route] of Object.entries(config.registry)) {
        // Convert route path pattern into a regex. Both param syntaxes are
        // supported: /posts/:id and /posts/{id} -> ^/posts/[^/]+$
        const regexStr = '^' + route.path
          .replace(/\{[a-zA-Z_][a-zA-Z0-9_]*\}/g, '[^/]+')
          .replace(/:[a-zA-Z_][a-zA-Z0-9_]*/g, '[^/]+')
          .replace(/\*/g, '.*') + '$';
        const regex = new RegExp(regexStr);
        if (regex.test(pathname)) {
          matchedRouteName = routeName;
          break;
        }
      }

      if (!name) {
        return matchedRouteName;
      }

      if (!matchedRouteName) {
        return false;
      }

      // Wildcard check
      if (name.includes('*')) {
        const pattern = '^' + name.replace(/\*/g, '.*') + '$';
        return new RegExp(pattern).test(matchedRouteName);
      }

      return matchedRouteName === name;
    },
  };
}
