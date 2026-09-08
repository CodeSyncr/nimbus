/*
 * Vue 3 binding for the Nimbus Cloud document embed.
 *
 *   <NimbusDocument :url="signedUrl" height="auto" @ready="onReady" />
 */
import { defineComponent, h, onBeforeUnmount, onMounted, ref, watch, type PropType } from 'vue';
import { embedDocument, type EmbedDocumentInfo, type EmbedHandle, type EmbedTheme } from './embed.js';

export const NimbusDocument = defineComponent({
  name: 'NimbusDocument',
  props: {
    /** A signed URL minted on your server. Never an API key. */
    url: { type: String, required: true },
    height: { type: [Number, String] as PropType<number | 'auto'>, default: 'auto' },
    theme: { type: String as PropType<EmbedTheme>, default: undefined },
    title: { type: String, default: undefined },
  },
  emits: {
    ready: (_info: EmbedDocumentInfo) => true,
    resize: (_height: number) => true,
    error: (_error: { message: string }) => true,
  },
  setup(props, { emit }) {
    const host = ref<HTMLElement | null>(null);
    let handle: EmbedHandle | null = null;

    onMounted(() => {
      if (!host.value) return;
      handle = embedDocument({
        url: props.url,
        target: host.value,
        height: props.height,
        theme: props.theme,
        title: props.title,
        onReady: (info) => emit('ready', info),
        onResize: (height) => emit('resize', height),
        onError: (error) => emit('error', error),
      });
    });
    watch(() => props.url, (url) => handle?.reload(url));
    watch(() => props.theme, (theme) => { if (theme) handle?.setTheme(theme); });
    onBeforeUnmount(() => {
      handle?.destroy();
      handle = null;
    });

    return () => h('div', { ref: host });
  },
});
