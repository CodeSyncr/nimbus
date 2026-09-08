/** Which Nimbus Cloud product a key and its documents belong to. */
export type Product = 'sigma' | 'carbon';

/** What a signed link permits. */
export type LinkMode = 'view' | 'edit';

/** A document stored in Nimbus Cloud. */
export interface NimbusDocument {
  id: number;
  product: Product;
  title: string;
  filename: string;
  mime: string;
  size: number;
  checksum: string;
  created_at: string;
  expires_at?: string;
  /** Present on the response that created or re-minted a link. */
  url?: string;
  url_expires_at?: string;
  mode?: LinkMode;
}

/** A freshly minted signed link. */
export interface SignedLink {
  url: string;
  mode: LinkMode;
  expires_at: string;
}

/** This month's allowance for one product. */
export interface Quota {
  period: string;
  limit: number;
  used: number;
  remaining: number;
  overage: number;
  allowed: boolean;
  warn: boolean;
  paid: boolean;
}

export interface ClientOptions {
  /** A per-product key: sgm_live_… or cbn_live_…. Server-side only. */
  apiKey: string;
  product: Product;
  /** Milliseconds before a request is abandoned. Default 30000. */
  timeoutMs?: number;
  /**
   * Override the API origin. Intended for testing against a local
   * deployment; production callers should leave it alone.
   */
  baseUrl?: string;
  fetch?: typeof globalThis.fetch;
}

export interface UploadOptions {
  /** File bytes, or the text of a document. */
  content: string | Uint8Array | ArrayBuffer;
  filename: string;
  title?: string;
  mime?: string;
  /** Link options for the URL returned alongside the document. */
  ttlSeconds?: number;
  mode?: LinkMode;
  /** Days to keep the document. Omit to keep it until deleted. */
  retainDays?: number;
  /**
   * Makes a retried upload return the original document instead of
   * creating — and charging for — a second one.
   */
  idempotencyKey?: string;
}

export interface LinkOptions {
  ttlSeconds?: number;
  mode?: LinkMode;
}

/** Stable codes, so callers can branch without matching on prose. */
export type NimbusErrorCode =
  | 'unauthorized'
  | 'quota_exceeded'
  | 'not_found'
  | 'too_large'
  | 'invalid_request'
  | 'rate_limited'
  | 'server_error'
  | 'network_error'
  | 'timeout';

/** Every failure this SDK raises. */
export class NimbusDocsError extends Error {
  readonly code: NimbusErrorCode;
  readonly status: number;
  /** The quota state, when the request was refused for being over it. */
  readonly quota?: Quota;

  constructor(code: NimbusErrorCode, message: string, status = 0, quota?: Quota) {
    super(message);
    this.name = 'NimbusDocsError';
    this.code = code;
    this.status = status;
    this.quota = quota;
  }
}
