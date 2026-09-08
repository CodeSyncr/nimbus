/*
 * Nimbus Cloud documents — browser embed.
 *
 * This half never sees your API key. It takes a signed URL your server
 * minted and puts it on the page, then translates the frame's messages into
 * callbacks.
 *
 * A note on how the frame is trusted. The viewer is served with a CSP
 * sandbox and no allow-same-origin, so its origin is the literal string
 * "null" — there is no meaningful origin to compare against, and checking
 * for one would either reject every message or accept any sender claiming
 * "null". So messages are verified by identity instead: the event's source
 * window must be this iframe's own contentWindow, which a third party
 * cannot forge.
 */

export type EmbedTheme = 'light' | 'dark';

export interface EmbedDocumentInfo {
  id: number;
  product: 'sigma' | 'carbon';
  title: string;
  filename: string;
  size: number;
  mode: 'view' | 'edit';
}

export interface EmbedOptions {
  /** A signed URL from your server. Never an API key. */
  url: string;
  /** Element or selector to mount into. */
  target: string | HTMLElement;
  /** Fixed height, or 'auto' to follow the document. Default 'auto'. */
  height?: number | 'auto';
  /** Starting theme; can be changed later with setTheme(). */
  theme?: EmbedTheme;
  /** Accessible title for the frame. */
  title?: string;
  className?: string;

  onReady?: (info: EmbedDocumentInfo) => void;
  onResize?: (height: number) => void;
  onError?: (error: { message: string }) => void;
}

export interface EmbedHandle {
  /** The iframe, if you need to style it further. */
  readonly frame: HTMLIFrameElement;
  setTheme(theme: EmbedTheme): void;
  /** Reload the document — useful after re-minting an expired link. */
  reload(url?: string): void;
  destroy(): void;
}

const HOST_SOURCE = 'nimbus-docs-host';
const FRAME_SOURCE = 'nimbus-docs';

/** Mount a Nimbus document into the page. */
export function embedDocument(options: EmbedOptions): EmbedHandle {
  const target =
    typeof options.target === 'string'
      ? document.querySelector<HTMLElement>(options.target)
      : options.target;

  if (!target) {
    throw new Error(`nimbus-docs: no element matched ${String(options.target)}`);
  }
  if (!options.url) {
    throw new Error('nimbus-docs: a signed url is required');
  }
  if (/[?&](api_?key|token)=/i.test(options.url)) {
    // A key in a URL is a key in the browser history, the referrer header
    // and every analytics tool on the page.
    throw new Error('nimbus-docs: that looks like an API key in a URL. Embed a signed link minted on your server.');
  }

  const frame = document.createElement('iframe');
  frame.src = options.url;
  frame.title = options.title ?? 'Nimbus document';
  frame.setAttribute('loading', 'lazy');
  // The viewer is already sandboxed by its CSP; this is the same policy
  // applied from the host side, so isolation does not depend on the server
  // alone — and it still holds if a non-Nimbus URL is embedded by mistake.
  frame.setAttribute('sandbox', 'allow-scripts allow-forms allow-popups');
  frame.style.width = '100%';
  frame.style.border = '0';
  frame.style.display = 'block';
  frame.style.height = options.height === 'auto' || options.height == null ? '480px' : `${options.height}px`;
  if (options.className) frame.className = options.className;

  const autoHeight = options.height === 'auto' || options.height == null;
  let destroyed = false;

  const post = (message: Record<string, unknown>) => {
    // "*" is correct here: the frame has an opaque origin, so no specific
    // target origin can match. Nothing sent is secret.
    frame.contentWindow?.postMessage({ source: HOST_SOURCE, ...message }, '*');
  };

  const onMessage = (event: MessageEvent) => {
    if (destroyed) return;
    // Identity, not origin — see the note at the top of this file.
    if (event.source !== frame.contentWindow) return;
    const data = event.data as Record<string, unknown> | null;
    if (!data || data.source !== FRAME_SOURCE) return;

    switch (data.type) {
      case 'ready':
        if (options.theme) post({ type: 'theme', theme: options.theme });
        options.onReady?.(data.document as EmbedDocumentInfo);
        break;
      case 'resize': {
        const height = Number(data.height);
        if (!Number.isFinite(height) || height <= 0) return;
        if (autoHeight) frame.style.height = `${height}px`;
        options.onResize?.(height);
        break;
      }
      case 'error':
        options.onError?.({ message: String(data.message ?? 'The document could not be displayed.') });
        break;
    }
  };

  window.addEventListener('message', onMessage);
  frame.addEventListener('error', () => {
    options.onError?.({ message: 'The document failed to load. The link may have expired.' });
  });
  target.appendChild(frame);

  return {
    frame,
    setTheme(theme: EmbedTheme) {
      post({ type: 'theme', theme });
    },
    reload(url?: string) {
      if (url) frame.src = url;
      else frame.contentWindow?.location.reload();
    },
    destroy() {
      destroyed = true;
      window.removeEventListener('message', onMessage);
      frame.remove();
    },
  };
}
