/*
 * Nimbus Cloud documents — server-side client.
 *
 * This half holds the API key and therefore must never run in a browser.
 * The flow it exists to support is: your server uploads a document, gets a
 * short-lived signed URL back, and hands only that URL to the page. The key
 * stays where you put it.
 */

import {
  ClientOptions,
  LinkMode,
  LinkOptions,
  NimbusDocsError,
  NimbusDocument,
  Product,
  Quota,
  SignedLink,
  UploadOptions,
} from './types.js';

/** Where the API lives. Overridable only for local testing. */
const DEFAULT_BASE_URL = 'https://nimbusgo.space';
const DEFAULT_TIMEOUT_MS = 30_000;

export class NimbusDocs {
  readonly product: Product;
  readonly #apiKey: string;
  readonly #baseUrl: string;
  readonly #timeoutMs: number;
  readonly #fetch: typeof globalThis.fetch;

  constructor(options: ClientOptions) {
    if (!options?.apiKey) {
      throw new NimbusDocsError('invalid_request', 'An apiKey is required.');
    }
    if (options.product !== 'sigma' && options.product !== 'carbon') {
      throw new NimbusDocsError('invalid_request', 'product must be "sigma" or "carbon".');
    }
    // A key in the browser is a key in everyone's browser. The check is a
    // guard rail, not security — but it catches the mistake at the moment
    // it is made rather than after the key is public.
    if (typeof window !== 'undefined' && typeof document !== 'undefined') {
      throw new NimbusDocsError(
        'invalid_request',
        'NimbusDocs holds your API key and must run on a server. In the browser, use @codesyncr/nimbus-docs/embed with a signed URL your server minted.',
      );
    }

    this.product = options.product;
    this.#apiKey = options.apiKey;
    this.#baseUrl = (options.baseUrl ?? DEFAULT_BASE_URL).replace(/\/+$/, '');
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.#fetch = options.fetch ?? globalThis.fetch;

    if (typeof this.#fetch !== 'function') {
      throw new NimbusDocsError('invalid_request', 'No fetch implementation. Use Node 18+ or pass one in options.fetch.');
    }
  }

  /** Upload a document and get it back with a signed URL. */
  async upload(options: UploadOptions): Promise<NimbusDocument> {
    if (!options?.content) {
      throw new NimbusDocsError('invalid_request', 'content is required.');
    }

    const query = new URLSearchParams();
    if (options.ttlSeconds) query.set('ttl', String(options.ttlSeconds));
    if (options.mode) query.set('mode', options.mode);
    if (options.retainDays) query.set('retain_days', String(options.retainDays));

    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (options.idempotencyKey) headers['Idempotency-Key'] = options.idempotencyKey;

    return this.#request<NimbusDocument>('POST', `/documents${qs(query)}`, {
      headers,
      body: JSON.stringify({
        title: options.title ?? options.filename,
        filename: options.filename,
        mime: options.mime ?? '',
        content: toText(options.content),
      }),
    });
  }

  /** List documents, newest first. */
  async list(limit = 50): Promise<NimbusDocument[]> {
    const out = await this.#request<{ documents: NimbusDocument[] }>(
      'GET',
      `/documents?limit=${encodeURIComponent(String(limit))}`,
    );
    return out.documents ?? [];
  }

  /** Fetch one document's metadata. */
  get(id: number): Promise<NimbusDocument> {
    return this.#request<NimbusDocument>('GET', `/documents/${id}`);
  }

  /**
   * Mint a fresh signed URL for a document that already exists. This is the
   * answer to an expired link: the document never went anywhere.
   */
  link(id: number, options: LinkOptions = {}): Promise<SignedLink> {
    const query = new URLSearchParams();
    if (options.ttlSeconds) query.set('ttl', String(options.ttlSeconds));
    if (options.mode) query.set('mode', options.mode);
    return this.#request<SignedLink>('POST', `/documents/${id}/link${qs(query)}`);
  }

  /** Delete a document and its content. */
  async delete(id: number): Promise<void> {
    await this.#request<{ deleted: number }>('DELETE', `/documents/${id}`);
  }

  /**
   * Invalidate every outstanding signed link for this account. Individual
   * links cannot be recalled once issued; this rotates the signing version
   * so all of them stop at once.
   */
  async revokeAllLinks(): Promise<void> {
    await this.#request<{ revoked: string }>('POST', '/links/revoke');
  }

  /** This month's allowance and how much of it is spent. */
  usage(): Promise<Quota> {
    return this.#request<Quota>('GET', '/usage');
  }

  // ── transport ───────────────────────────────────────────────────────────

  async #request<T>(method: string, path: string, init: RequestInit = {}): Promise<T> {
    const url = `${this.#baseUrl}/api/v1/${this.product}${path}`;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.#timeoutMs);

    let response: Response;
    try {
      response = await this.#fetch(url, {
        ...init,
        method,
        signal: controller.signal,
        headers: {
          Authorization: `Bearer ${this.#apiKey}`,
          Accept: 'application/json',
          ...(init.headers as Record<string, string> | undefined),
        },
      });
    } catch (cause) {
      const aborted = (cause as Error)?.name === 'AbortError';
      throw new NimbusDocsError(
        aborted ? 'timeout' : 'network_error',
        aborted
          ? `The request to Nimbus Cloud timed out after ${this.#timeoutMs}ms.`
          : `Could not reach Nimbus Cloud: ${(cause as Error)?.message ?? 'unknown error'}`,
      );
    } finally {
      clearTimeout(timer);
    }

    const body = await readJson(response);
    if (!response.ok) {
      throw new NimbusDocsError(
        codeFor(response.status),
        typeof body?.error === 'string' ? body.error : `Nimbus Cloud returned ${response.status}.`,
        response.status,
        body?.quota as Quota | undefined,
      );
    }
    return body as T;
  }
}

function qs(params: URLSearchParams): string {
  const s = params.toString();
  return s ? `?${s}` : '';
}

function toText(content: string | Uint8Array | ArrayBuffer): string {
  if (typeof content === 'string') return content;
  const bytes = content instanceof ArrayBuffer ? new Uint8Array(content) : content;
  return new TextDecoder().decode(bytes);
}

async function readJson(response: Response): Promise<Record<string, unknown>> {
  try {
    return (await response.json()) as Record<string, unknown>;
  } catch {
    return {};
  }
}

function codeFor(status: number) {
  switch (status) {
    case 401:
    case 403:
      return 'unauthorized' as const;
    case 402:
      return 'quota_exceeded' as const;
    case 404:
      return 'not_found' as const;
    case 413:
      return 'too_large' as const;
    case 400:
      return 'invalid_request' as const;
    case 429:
      return 'rate_limited' as const;
    default:
      return status >= 500 ? ('server_error' as const) : ('invalid_request' as const);
  }
}
