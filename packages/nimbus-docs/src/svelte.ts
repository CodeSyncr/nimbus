/*
 * Svelte action for the Nimbus Cloud document embed. Works in Svelte 4
 * and 5 without a compiler step, because an action is a plain function.
 *
 *   <script>
 *     import { nimbusDocument } from '@codesyncr/nimbus-docs/svelte';
 *     let params = { url: signedUrl, height: 'auto' };
 *   </script>
 *   <div use:nimbusDocument={params}></div>
 */
import { embedDocument, type EmbedHandle, type EmbedOptions } from './embed.js';

export type NimbusDocumentParams = Omit<EmbedOptions, 'target'>;

export function nimbusDocument(node: HTMLElement, params: NimbusDocumentParams) {
  let handle: EmbedHandle = embedDocument({ ...params, target: node });
  let current = params;
  return {
    update(next: NimbusDocumentParams) {
      if (next.url !== current.url) handle.reload(next.url);
      if (next.theme && next.theme !== current.theme) handle.setTheme(next.theme);
      current = next;
    },
    destroy() {
      handle.destroy();
    },
  };
}
