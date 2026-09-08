/*
 * React binding for the Nimbus Cloud document embed.
 *
 *   <NimbusDocument url={signedUrl} height="auto" onReady={...} />
 *
 * The component owns one iframe for its lifetime. A new `url` reloads the
 * frame in place (the answer to a re-minted link); a new `theme` is pushed
 * to the frame; unmounting destroys it.
 */
import { createElement, useEffect, useRef, type CSSProperties } from 'react';
import { embedDocument, type EmbedDocumentInfo, type EmbedHandle, type EmbedTheme } from './embed.js';

export interface NimbusDocumentProps {
  /** A signed URL minted on your server. Never an API key. */
  url: string;
  height?: number | 'auto';
  theme?: EmbedTheme;
  title?: string;
  className?: string;
  style?: CSSProperties;
  onReady?: (info: EmbedDocumentInfo) => void;
  onResize?: (height: number) => void;
  onError?: (error: { message: string }) => void;
}

export function NimbusDocument(props: NimbusDocumentProps) {
  const { url, height, theme, title, className, style } = props;
  const host = useRef<HTMLDivElement | null>(null);
  const handle = useRef<EmbedHandle | null>(null);
  const mountedUrl = useRef<string>('');

  // Callbacks live in a ref so re-renders never remount the frame.
  const callbacks = useRef({ onReady: props.onReady, onResize: props.onResize, onError: props.onError });
  callbacks.current = { onReady: props.onReady, onResize: props.onResize, onError: props.onError };

  useEffect(() => {
    if (!host.current) return;
    handle.current = embedDocument({
      url,
      target: host.current,
      height,
      theme,
      title,
      className,
      onReady: (info) => callbacks.current.onReady?.(info),
      onResize: (h) => callbacks.current.onResize?.(h),
      onError: (e) => callbacks.current.onError?.(e),
    });
    mountedUrl.current = url;
    return () => {
      handle.current?.destroy();
      handle.current = null;
    };
    // Structural props remount; url and theme are handled below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [height, title, className]);

  useEffect(() => {
    if (handle.current && url !== mountedUrl.current) {
      handle.current.reload(url);
      mountedUrl.current = url;
    }
  }, [url]);

  useEffect(() => {
    if (theme) handle.current?.setTheme(theme);
  }, [theme]);

  return createElement('div', { ref: host, style });
}
