/**
 * Paywall rendering for the web.
 *
 * A paywall is a document designed in the Cashier console (theme + blocks);
 * the iOS SDK draws the same document natively in SwiftUI. This renderer is
 * also what the console's editor previews with, so the web shows exactly
 * what was designed.
 *
 * It builds DOM nodes and sets text with textContent — never innerHTML with
 * document text — and re-checks colours and URLs, so a paywall cannot inject
 * markup or script into the page that shows it.
 */

import { PAYWALL_ART, type ArtShape } from './art.js'

export interface PaywallTheme {
  background: string
  background_end?: string
  surface: string
  text: string
  muted: string
  accent: string
  accent_text: string
  radius: number
  font: 'system' | 'rounded' | 'serif' | 'mono'
}

export interface PaywallItem {
  icon: string
  title: string
  text?: string
}

export type PaywallBlockType =
  | 'title' | 'text' | 'image' | 'video' | 'art' | 'icon' | 'stack' | 'footer' | 'header' | 'custom' | 'spacer'
  | 'packages' | 'cta' | 'sheet'
  | 'tabs' | 'switch' | 'button' | 'carousel'
  | 'countdown' | 'timeline' | 'social_proof' | 'testimonial' | 'features' | 'award'

/** Block types that hold children. */
export const PAYWALL_CONTAINERS: readonly PaywallBlockType[] = ['stack', 'custom', 'sheet', 'tabs', 'carousel']

export interface PaywallBlock {
  id: string
  type: PaywallBlockType
  icon?: string
  /** button: next | back | screen | close | restore | url */
  action?: 'next' | 'back' | 'screen' | 'close' | 'restore' | 'url'
  /** button with action "screen": the screen id */
  target?: string
  style?: 'primary' | 'secondary' | 'outline' | 'link'
  text?: string
  subtext?: string
  align?: 'left' | 'center' | 'right'
  size?: 's' | 'm' | 'l' | 'xl'
  url?: string
  height?: number
  items?: PaywallItem[]
  layout?: 'list' | 'cards'
  highlight?: string
  badges?: Record<string, string>
  author?: string
  rating?: number
  restore?: boolean
  terms_url?: string
  privacy_url?: string
  /** Containers: stack, custom, sheet, tabs (children are stacks; text is the label), carousel (children are pages). */
  children?: PaywallBlock[]
  axis?: 'vertical' | 'horizontal' | 'layers'
  justify?: 'start' | 'center' | 'end' | 'space_between'
  gap?: number
  padding?: number
  background?: string
  border?: string
  radius?: number
  /** carousel: seconds between pages, 0 = swipe only */
  interval?: number
  /** video: still shown before it plays */
  poster?: string
  /** art: which built-in illustration (PAYWALL_ART) */
  art?: string
  /** header: a back chevron */
  back?: boolean
  /** switch: package selected when on / off */
  package_on?: string
  package_off?: string
  /** countdown: to a moment (RFC 3339), or minutes from first view */
  until?: string
  minutes?: number
  /** social_proof: avatar URLs */
  images?: string[]
  /** One of the type's designs (PAYWALL_VARIANTS); "" or unknown is the first. */
  variant?: string
  /** icon block, option button: drawn instead of the icon */
  emoji?: string
  /** stack, custom: how children line up across the axis */
  cross?: 'start' | 'center' | 'end' | 'stretch'
  /** The block's style panel: size, spacing, fill, shape, shadow, type. */
  look?: PaywallLook
  /** stack, custom: a plan card for this package id */
  package?: string
  /** plan card: a pill over its top edge */
  badge?: string
  /** laid over look while this block's plan card is selected */
  look_selected?: PaywallLook
  /** button: recorded as the screen's answer when tapped, before its action */
  answer?: string
}

export interface PaywallLook {
  width?: 'fit' | 'fill' | 'fixed'
  width_px?: number
  height?: 'fit' | 'fill' | 'fixed'
  height_px?: number
  padding_x?: number
  padding_y?: number
  margin_x?: number
  margin_y?: number
  background?: string
  background_end?: string
  background_image?: string
  radius?: number
  /** top-left, top-right, bottom-right, bottom-left */
  corners?: number[]
  border_color?: string
  border_width?: number
  shadow_color?: string
  shadow_x?: number
  shadow_y?: number
  shadow_blur?: number
  /** 1–100; 0 or unset is opaque */
  opacity?: number
  font?: 'system' | 'rounded' | 'serif' | 'mono'
  weight?: 'regular' | 'medium' | 'semibold' | 'bold' | 'heavy'
  font_size?: number
  color?: string
  italic?: boolean
}

const WEIGHTS: Record<string, string> = { regular: '400', medium: '500', semibold: '600', bold: '700', heavy: '800' }
const num = (n: unknown, lo: number, hi: number) => (typeof n === 'number' && Number.isFinite(n) ? Math.max(lo, Math.min(hi, n)) : 0)

/** Each block type's designs, the first being the default. */
export const PAYWALL_VARIANTS: Readonly<Record<string, readonly string[]>> = {
  header: ['plain', 'large', 'pill', 'brand'],
  tabs: ['pills', 'underline', 'outline'],
  switch: ['card', 'plain', 'checkbox'],
  button: ['rounded', 'pill', 'square', 'option'],
  icon: ['tile', 'glow', 'rings', 'gradient', 'plain'],
  carousel: ['dots', 'bars', 'peek'],
  countdown: ['boxes', 'labeled', 'inline', 'banner'],
  timeline: ['line', 'cards', 'horizontal'],
  social_proof: ['avatars', 'rating', 'badge'],
  testimonial: ['card', 'quote', 'bubble'],
  features: ['list', 'checks', 'grid', 'cards'],
  award: ['laurel', 'badge', 'ribbon'],
  image: ['inline', 'hero', 'hero_fade'],
  video: ['inline', 'hero', 'hero_fade'],
  art: ['inline', 'hero', 'hero_fade'],
}

/** The design a block is drawn with: its variant if known, else the default. */
export function paywallVariant(b: Pick<PaywallBlock, 'type' | 'variant'>): string {
  const list = PAYWALL_VARIANTS[b.type]
  if (!list) return ''
  return b.variant && list.includes(b.variant) ? b.variant : list[0]
}

/** One step of the flow. */
export interface PaywallScreen {
  id: string
  name: string
  /** What this screen's answer buttons answer; defaults to the name. */
  question?: string
  background?: string
  background_end?: string
  padding_x?: number
  padding_y?: number
  spacing?: number
  justify?: 'start' | 'center' | 'end' | 'space_between'
  align?: 'left' | 'center'
  /** Animated background from the accent: glow | aurora | orbs | grid | rays */
  effect?: 'glow' | 'aurora' | 'orbs' | 'grid' | 'rays'
  /** Blocks animate in, one after another: rise | fade | zoom */
  entrance?: 'rise' | 'fade' | 'zoom'
  /** This screen's own look; missing fields come from the flow's theme. */
  theme?: Partial<PaywallTheme>
  /** A photo or muted looping video behind the screen (https), under an overlay colour. */
  background_image?: string
  background_video?: string
  background_overlay?: string
  blocks: PaywallBlock[]
}

export interface PaywallDoc {
  version: number
  theme: PaywallTheme
  /** The screen the flow opens on. */
  initial?: string
  screens?: PaywallScreen[]
  /** Version 1: a single screen's blocks. */
  blocks?: PaywallBlock[]
  /** The language the texts are written in ("" = "en"). */
  default_locale?: string
  /** Other languages the paywall is translated into. */
  locales?: string[]
  /** Translations: locale → key ("title-1.text", "features-1.items.0.title", …) → text. */
  strings?: Record<string, Record<string, string>>
}

/** An answer to a Feedback or Marketing Consent screen. */
export interface PaywallAnswer {
  screen_id: string
  question: string
  answer: string
}

const lang = (tag: string) => tag.toLowerCase().replace(/_/g, '-').split('-')[0]

/**
 * Which of the paywall's translations a device asking for `want` sees: an
 * exact match, else the first with the same language (unless that is the
 * default's language), else none — "" means the original texts.
 */
export function matchPaywallLocale(doc: Pick<PaywallDoc, 'default_locale' | 'locales'>, want: string | undefined): string {
  const w = (want ?? '').trim().toLowerCase().replace(/_/g, '-')
  if (!w) return ''
  const locales = doc.locales ?? []
  const exact = locales.find((l) => l.toLowerCase() === w)
  if (exact) return exact
  if (lang(w) === lang(doc.default_locale || 'en')) return ''
  return locales.find((l) => lang(l) === lang(w)) ?? ''
}

/** A copy of the document with the texts for `want` swapped in. */
export function localizePaywall(doc: PaywallDoc, want: string | undefined): PaywallDoc {
  const loc = matchPaywallLocale(doc, want)
  const tr = loc ? doc.strings?.[loc] : undefined
  if (!tr) return doc
  const out: PaywallDoc = JSON.parse(JSON.stringify(doc))
  const t = (key: string) => (typeof tr[key] === 'string' && tr[key] ? tr[key] : undefined)
  const walk = (list: PaywallBlock[] | undefined) => {
    for (const b of list ?? []) {
      for (const f of ['text', 'subtext', 'author', 'badge', 'answer'] as const) {
        const v = b[f] ? t(`${b.id}.${f}`) : undefined
        if (v) b[f] = v
      }
      b.items?.forEach((it, i) => {
        const title = it.title ? t(`${b.id}.items.${i}.title`) : undefined
        const text = it.text ? t(`${b.id}.items.${i}.text`) : undefined
        if (title) it.title = title
        if (text) it.text = text
      })
      for (const pkg of Object.keys(b.badges ?? {})) {
        const v = t(`${b.id}.badges.${pkg}`)
        if (v) b.badges![pkg] = v
      }
      walk(b.children)
    }
  }
  for (const s of out.screens ?? []) {
    const q = s.question ? t(`${s.id}.question`) : undefined
    if (q) s.question = q
    walk(s.blocks)
  }
  walk(out.blocks)
  return out
}

/** A package as the paywall shows it: prices already localized. */
export interface PaywallPackageView {
  id: string
  productName: string
  /** "$4.99" */
  price: string
  /** "month" | "year" | "" for a one-time purchase */
  period: string
  /** "$3.33", or "" when not meaningful */
  pricePerMonth: string
  /** "7-day", or "" */
  trial: string
  /** "19%": how much less per month than the shortest plan, or "" (see paywallSavings) */
  savings?: string
}

export interface PaywallContext {
  appName: string
  packages: PaywallPackageView[]
  onPurchase?: (pkg: PaywallPackageView) => void | Promise<void>
  onRestore?: () => void | Promise<void>
  onClose?: () => void
  /** Show a close button (default true). */
  closable?: boolean
  /** The package selected first; defaults to the packages block's highlight. */
  selected?: string
  /**
   * Draw this one screen and do not navigate (an editor drawing every
   * screen of the flow side by side). Without it the flow opens on the
   * initial screen and buttons move between screens.
   */
  screen?: string
  /** Called when a button moves the flow to another screen. */
  onNavigate?: (screenId: string) => void
  /** Tabs blocks to open on a tab other than the first: block id → tab index. */
  tabs?: Record<string, number>
  /** Play screen entrances (default true). An editor redrawing on every keystroke turns them off. */
  animate?: boolean
  /** Show the paywall in this language when it has a translation (e.g. navigator.language). */
  locale?: string
  /** A button with an answer was tapped (Feedback, Marketing Consent screens). */
  onAnswer?: (answer: PaywallAnswer) => void
}

export const PAYWALL_SCHEMA_VERSION = 2

/**
 * {{savings}} for each package, from real prices: how much less per month
 * than the plan with the shortest period of at least a month (weekly plans
 * never set the reference). Lifetime and
 * one-time purchases, the reference plan itself and anything under 1% get "".
 */
export function paywallSavings(pkgs: Array<{ id: string; amount: number; months: number }>): Record<string, string> {
  const priced = pkgs.filter((p) => p.months >= 1 && p.amount > 0)
  const ref = priced.reduce<(typeof priced)[number] | undefined>((a, p) => (!a || p.months < a.months ? p : a), undefined)
  const out: Record<string, string> = {}
  for (const p of pkgs) {
    out[p.id] = ''
    if (!ref || p === ref || p.months <= 0 || p.amount <= 0) continue
    const pct = Math.round((1 - p.amount / p.months / (ref.amount / ref.months)) * 100)
    if (pct >= 1) out[p.id] = `${pct}%`
  }
  return out
}

/** Every block of a list, children after their container. */
export function flattenBlocks(list: PaywallBlock[]): PaywallBlock[] {
  return list.flatMap((b) => [b, ...flattenBlocks(b.children ?? [])])
}

/** Milliseconds left until a countdown ends (never negative). */
export function countdownRemaining(b: Pick<PaywallBlock, 'until' | 'minutes'>, startedAt: number, now: number): number {
  const end = b.until ? Date.parse(b.until) : startedAt + (b.minutes ?? 0) * 60_000
  return Number.isFinite(end) ? Math.max(0, end - now) : 0
}

/** The package a switch selects when turned on or off. */
export function switchPackage(b: Pick<PaywallBlock, 'package_on' | 'package_off'>, on: boolean): string | undefined {
  return on ? b.package_on : b.package_off
}

/** The flow's screens, reading a version-1 document as one screen. */
export function paywallScreens(doc: PaywallDoc): PaywallScreen[] {
  if (doc.screens && doc.screens.length) return doc.screens
  return [{ id: 'paywall', name: 'Paywall', blocks: doc.blocks ?? [] }]
}

const HEX = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/
const FONTS: Record<string, string> = {
  system: '-apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif',
  rounded: 'ui-rounded, "SF Pro Rounded", "Nunito", system-ui, sans-serif',
  serif: 'ui-serif, "New York", Georgia, "Times New Roman", serif',
  mono: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
}
const TITLE_SIZES: Record<string, number> = { s: 20, m: 24, l: 30, xl: 36 }
const TEXT_SIZES: Record<string, number> = { s: 13, m: 15, l: 17, xl: 19 }
const SPACER_SIZES: Record<string, number> = { s: 8, m: 16, l: 32, xl: 48 }

/** 24×24 stroke icons (Lucide, ISC licence) for the closed icon set. */
const ICONS: Record<string, string> = {
  check: 'M20 6 9 17l-5-5',
  star: 'M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z',
  bolt: 'M13 2 3 14h9l-1 8 10-12h-9l1-8z',
  lock: 'M5 11h14v10H5zM7 11V7a5 5 0 0 1 10 0v4',
  cloud: 'M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9z',
  heart: 'M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7z',
  sparkles: 'M12 3l1.9 5.8L20 11l-6.1 2.2L12 19l-1.9-5.8L4 11l6.1-2.2L12 3zM5 3v4M3 5h4M19 17v4M17 19h4',
  infinity: 'M12 12c-2-2.67-4-4-6-4a4 4 0 1 0 0 8c2 0 4-1.33 6-4zm0 0c2 2.67 4 4 6 4a4 4 0 0 0 0-8c-2 0-4 1.33-6 4z',
  shield: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
  crown: 'M2 4l3 12h14l3-12-6 7-4-7-4 7-6-7zM5 20h14',
  gift: 'M20 12v10H4V12M2 7h20v5H2zM12 22V7M12 7H7.5a2.5 2.5 0 0 1 0-5C11 2 12 7 12 7zM12 7h4.5a2.5 2.5 0 0 0 0-5C13 2 12 7 12 7z',
  clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2',
  chart: 'M3 3v18h18M18 17V9M13 17V5M8 17v-3',
  bell: 'M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.94 1.94 0 0 0 3.4 0',
  download: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3',
  people: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
  mail: 'M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zM22 6l-10 7L2 6',
  target: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 18a6 6 0 1 0 0-12 6 6 0 0 0 0 12zM12 14a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
  trophy: 'M6 9H4.5a2.5 2.5 0 0 1 0-5H6M18 9h1.5a2.5 2.5 0 0 0 0-5H18M4 22h16M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22M18 2H6v7a6 6 0 0 0 12 0V2Z',
  play: 'M6 3l14 9-14 9V3z',
  chevronLeft: 'M15 18l-6-6 6-6',
  chevronRight: 'm9 18 6-6-6-6',
  rocket: 'M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09zM12 15l-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2zM9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5',
}

/** Fills {{variables}} from a package and the app. Unknown names become "". */
export function resolvePaywallText(text: string, appName: string, pkg?: PaywallPackageView): string {
  const vars: Record<string, string> = {
    app_name: appName,
    product_name: pkg?.productName ?? '',
    price: pkg?.price ?? '',
    period: pkg?.period ?? '',
    price_per_month: pkg?.pricePerMonth ?? '',
    trial: pkg?.trial ?? '',
    savings: pkg?.savings ?? '',
  }
  return text
    .replace(/\{\{\s*([a-z_]+)\s*\}\}/g, (_, name: string) => vars[name] ?? '')
    .replace(/[ \t]{2,}/g, ' ')
    .replace(/[ \t]+([,.:;!?])/g, '$1')
    .trim()
}

// Keyframes for effects and entrances: a fixed stylesheet, added to the page
// once. Nothing from a paywall document goes into it.
const MOTION_CSS = `
@keyframes cpw-breathe { from { opacity: .75; transform: scale(1) } to { opacity: 1; transform: scale(1.08) } }
@keyframes cpw-drift1 { to { transform: translate(10%, 6%) } }
@keyframes cpw-drift2 { to { transform: translate(-12%, 8%) } }
@keyframes cpw-drift3 { to { transform: translate(8%, -10%) } }
@keyframes cpw-float { to { transform: translateY(-10px) } }
@keyframes cpw-spin { to { transform: rotate(360deg) } }
@keyframes cpw-pulse { to { transform: scale(1.06) } }
@keyframes cpw-ring { from { transform: scale(1); opacity: .6 } to { transform: scale(1.9); opacity: 0 } }
@keyframes cpw-bob { from { transform: translateY(0) rotate(-3deg) } to { transform: translateY(-6px) rotate(3deg) } }
@keyframes cpw-twinkle { from { opacity: 1 } to { opacity: .25 } }
@keyframes cpw-drift-x { to { transform: translateX(14px) } }
@keyframes cpw-art-float { to { transform: translateY(-6px) } }
@keyframes cpw-in-rise { from { opacity: 0; transform: translateY(18px) } }
@keyframes cpw-in-fade { from { opacity: 0 } }
@keyframes cpw-in-zoom { from { opacity: 0; transform: scale(.92) } }
@media (prefers-reduced-motion: reduce) { [data-cpw-motion] { animation: none !important } }
`
function ensureMotionCSS() {
  const doc = globalThis.document
  if (!doc || doc.getElementById('cashier-paywall-motion')) return
  const style = doc.createElement('style')
  style.id = 'cashier-paywall-motion'
  style.textContent = MOTION_CSS
  ;(doc.head ?? doc.documentElement).appendChild(style)
}
const moving = <T extends HTMLElement>(n: T, animation: string): T => {
  n.style.animation = animation
  n.setAttribute('data-cpw-motion', '')
  return n
}
const ORBS: Array<[number, number, number, number, number, number]> = [
  [12, 14, 18, 0.35, 4, 0], [82, 10, 26, 0.25, 5.5, 0.6], [68, 34, 10, 0.5, 4.5, 1.2], [22, 46, 14, 0.3, 6, 0.3],
  [90, 58, 20, 0.22, 5, 0.9], [8, 72, 8, 0.45, 4.2, 1.5], [60, 82, 16, 0.28, 6.5, 0.4],
]

// ── Art ────────────────────────────────────────────────────────────────
function hexRgb(hex: string): [number, number, number, number] {
  let h = hex.slice(1)
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const n = parseInt(h.slice(0, 6), 16)
  const a = h.length === 8 ? parseInt(h.slice(6), 16) / 255 : 1
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255, a]
}
function rgbHsl(r: number, g: number, b: number): [number, number, number] {
  r /= 255; g /= 255; b /= 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b), l = (max + min) / 2
  if (max === min) return [0, 0, l]
  const d = max - min, s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  const h = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4
  return [h * 60, s, l]
}
function hslRgb(h: number, s: number, l: number): [number, number, number] {
  h = ((h % 360) + 360) % 360 / 360
  const f = (t: number) => {
    const q = l < 0.5 ? l * (1 + s) : l + s - l * s, p = 2 * l - q
    t = (t + 1) % 1
    return t < 1 / 6 ? p + (q - p) * 6 * t : t < 1 / 2 ? q : t < 2 / 3 ? p + (q - p) * (2 / 3 - t) * 6 : p
  }
  return s === 0 ? [l * 255, l * 255, l * 255] : [f(h + 1 / 3) * 255, f(h) * 255, f(h - 1 / 3) * 255]
}
/** Resolves an art colour token ("A+40*0.8@0.5") against theme colours. */
export function artColor(token: string, palette: Record<string, string>): string {
  const m = /^(AT|A|T|B|S|M|#[0-9a-fA-F]{6})([+-]\d+)?(?:\*([\d.]+))?(?:@([\d.]+))?$/.exec(token)
  if (!m) return 'transparent'
  const base = m[1].startsWith('#') ? m[1] : palette[m[1]] ?? '#000000'
  let [r, g, b, a] = hexRgb(HEX.test(base) ? base : '#000000')
  if (m[2] || m[3]) {
    const [h, s, l] = rgbHsl(r, g, b)
    ;[r, g, b] = hslRgb(h + Number(m[2] ?? 0), s, Math.max(0, Math.min(1, l * Number(m[3] ?? 1))))
  }
  if (m[4]) a *= Number(m[4])
  return `rgba(${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)}, ${Math.round(a * 1000) / 1000})`
}
let artSeq = 0
/** Draws a built-in illustration as SVG, covering its box. */
function drawArt(name: string, palette: Record<string, string>): SVGSVGElement {
  const NS = 'http://www.w3.org/2000/svg'
  const svg = document.createElementNS(NS, 'svg')
  svg.setAttribute('viewBox', '0 0 400 260')
  svg.setAttribute('preserveAspectRatio', 'xMidYMid slice')
  svg.setAttribute('aria-hidden', 'true')
  Object.assign(svg.style, { width: '100%', height: '100%', display: 'block' })
  const defs = document.createElementNS(NS, 'defs')
  svg.appendChild(defs)
  const id = `cpw-art-${++artSeq}`
  let n = 0
  const paint = (f: ArtShape['fill']): string => {
    if (!f || f === 'none') return 'none'
    if (typeof f === 'string') return artColor(f, palette)
    const g = document.createElementNS(NS, 'linearGradient')
    const gid = `${id}-g${n++}`
    g.setAttribute('id', gid)
    const rad = ((f.angle - 90) * Math.PI) / 180
    g.setAttribute('x1', String(0.5 - Math.cos(rad) / 2)); g.setAttribute('y1', String(0.5 - Math.sin(rad) / 2))
    g.setAttribute('x2', String(0.5 + Math.cos(rad) / 2)); g.setAttribute('y2', String(0.5 + Math.sin(rad) / 2))
    f.stops.forEach((c, i) => {
      const stop = document.createElementNS(NS, 'stop')
      stop.setAttribute('offset', String(i / Math.max(1, f.stops.length - 1)))
      stop.setAttribute('stop-color', artColor(c, palette))
      g.appendChild(stop)
    })
    defs.appendChild(g)
    return `url(#${gid})`
  }
  for (const sh of PAYWALL_ART[name] ?? []) {
    const e = document.createElementNS(NS, sh.t)
    const at = (k: string, v: number | string | undefined) => { if (v !== undefined) e.setAttribute(k, String(v)) }
    if (sh.t === 'rect') { at('x', sh.x); at('y', sh.y); at('width', sh.w); at('height', sh.h); at('rx', sh.r) }
    if (sh.t === 'circle') { at('cx', sh.cx); at('cy', sh.cy); at('r', sh.r) }
    if (sh.t === 'ellipse') { at('cx', sh.cx); at('cy', sh.cy); at('rx', sh.rx); at('ry', sh.ry) }
    if (sh.t === 'path') at('d', sh.d)
    e.setAttribute('fill', paint(sh.fill))
    if (sh.stroke) { e.setAttribute('stroke', artColor(sh.stroke, palette)); at('stroke-width', sh.sw ?? 1) }
    if (sh.rot) {
      const cx = sh.t === 'rect' ? (sh.x ?? 0) + (sh.w ?? 0) / 2 : sh.cx ?? 0, cy = sh.t === 'rect' ? (sh.y ?? 0) + (sh.h ?? 0) / 2 : sh.cy ?? 0
      e.setAttribute('transform', `rotate(${sh.rot} ${cx} ${cy})`)
    }
    if (sh.blur) {
      const f = document.createElementNS(NS, 'filter')
      const fid = `${id}-f${n++}`
      f.setAttribute('id', fid); f.setAttribute('x', '-50%'); f.setAttribute('y', '-50%'); f.setAttribute('width', '200%'); f.setAttribute('height', '200%')
      const b = document.createElementNS(NS, 'feGaussianBlur')
      b.setAttribute('stdDeviation', String(sh.blur / 2))
      f.appendChild(b); defs.appendChild(f)
      e.setAttribute('filter', `url(#${fid})`)
    }
    let node: SVGElement = e
    if (sh.anim) {
      // Animate a wrapper so the shape's own transform (rotation) stays put.
      const g = document.createElementNS(NS, 'g')
      g.appendChild(e)
      const st = (g as unknown as HTMLElement).style
      const delay = `${sh.delay ?? 0}s`
      st.animation = sh.anim === 'twinkle' ? `cpw-twinkle 2.2s ease-in-out ${delay} infinite alternate`
        : sh.anim === 'float' ? `cpw-art-float 3.4s ease-in-out ${delay} infinite alternate`
        : sh.anim === 'drift' ? `cpw-drift-x 9s ease-in-out ${delay} infinite alternate`
        : 'cpw-spin 70s linear infinite'
      if (sh.anim === 'spin') { st.transformBox = 'view-box'; st.transformOrigin = `${sh.cx ?? 200}px ${sh.cy ?? 130}px` }
      g.setAttribute('data-cpw-motion', '')
      node = g
    }
    svg.appendChild(node)
  }
  return svg
}

/** A hex colour at an opacity, as rgba(). */
function tint(hex: string, a: number): string {
  let h = hex.slice(1)
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const n = parseInt(h.slice(0, 6), 16)
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`
}

function color(c: string | undefined, fallback: string): string {
  return c && HEX.test(c) ? c : fallback
}

function safeURL(u: string | undefined): string | null {
  if (!u) return null
  try {
    const parsed = new URL(u)
    // Plain http only for this machine: media uploaded in development.
    const local = parsed.protocol === 'http:' && (parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1')
    return parsed.protocol === 'https:' || local ? parsed.href : null
  } catch {
    return null
  }
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, style: Partial<CSSStyleDeclaration> = {}, text?: string): HTMLElementTagNameMap[K] {
  const n = document.createElement(tag)
  Object.assign(n.style, style)
  if (text !== undefined) n.textContent = text
  return n
}

function icon(name: string, stroke: string, size = 20): SVGSVGElement {
  const NS = 'http://www.w3.org/2000/svg'
  const svg = document.createElementNS(NS, 'svg')
  svg.setAttribute('viewBox', '0 0 24 24')
  svg.setAttribute('width', String(size))
  svg.setAttribute('height', String(size))
  svg.setAttribute('fill', 'none')
  svg.setAttribute('stroke', stroke)
  svg.setAttribute('stroke-width', '2')
  svg.setAttribute('stroke-linecap', 'round')
  svg.setAttribute('stroke-linejoin', 'round')
  svg.setAttribute('aria-hidden', 'true')
  const path = document.createElementNS(NS, 'path')
  path.setAttribute('d', ICONS[name] ?? ICONS.check)
  svg.appendChild(path)
  return svg
}

/**
 * Renders a paywall into a new element. Selecting a package re-renders
 * price text in place; the purchase button calls ctx.onPurchase with the
 * selected package.
 */
export function renderPaywall(source: PaywallDoc, ctx: PaywallContext): HTMLElement {
  const doc = ctx.locale ? localizePaywall(source, ctx.locale) : source
  // The theme in use: the flow's, or a screen's own (set per screen in draw).
  let bg = '', surface = '', text = '', muted = '', accent = '', accentText = '', radius = 0, end = '', font = ''
  const useTheme = (t: Partial<PaywallTheme> | undefined) => {
    const base = doc.theme
    const pick = (k: keyof PaywallTheme, fallback: string) => color((t?.[k] as string) || undefined, color(base[k] as string, fallback))
    bg = pick('background', '#FFFFFF')
    surface = pick('surface', '#F4F4F6')
    text = pick('text', '#111114')
    muted = pick('muted', '#6B6B76')
    accent = pick('accent', '#F04E28')
    accentText = pick('accent_text', '#FFFFFF')
    radius = Math.max(0, Math.min(32, Number(t?.radius ?? base.radius) || 0))
    const e = t ? t.background_end : base.background_end
    end = e && HEX.test(e) ? e : ''
    font = FONTS[(t?.font || base.font) as string] ?? FONTS.system
  }
  useTheme(undefined)

  const root = el('div', {
    color: text,
    fontFamily: font,
    boxSizing: 'border-box',
    display: 'flex',
    flexDirection: 'column',
    minHeight: '100%',
    position: 'relative',
  })
  root.setAttribute('data-cashier-paywall', '')

  if (doc.version > PAYWALL_SCHEMA_VERSION) {
    root.appendChild(el('p', { color: muted, textAlign: 'center', padding: '28px 22px' }, 'Update the app to see this offer.'))
    return root
  }

  const screens = paywallScreens(doc)
  const screenById = new Map(screens.map((sc) => [sc.id, sc]))
  const allBlocks = screens.flatMap((sc) => flattenBlocks(sc.blocks))
  const packagesBlock = allBlocks.find((b) => b.type === 'packages')
  const byId = new Map(ctx.packages.map((p) => [p.id, p]))
  // The chosen plan survives moving between screens.
  const firstCard = allBlocks.find((b) => b.package && byId.has(b.package))?.package
  let selected = ctx.selected && byId.has(ctx.selected) ? ctx.selected
    : packagesBlock?.highlight && byId.has(packagesBlock.highlight) ? packagesBlock.highlight
    : firstCard ?? ctx.packages[0]?.id
  const current = () => (selected ? byId.get(selected) : undefined)
  let screenId = ctx.screen && screenById.has(ctx.screen) ? ctx.screen
    : doc.initial && screenById.has(doc.initial) ? doc.initial : screens[0]?.id
  const history: string[] = []
  // State that outlives a redraw: open tabs, countdown starts.
  const tabOpen = new Map<string, number>(Object.entries(ctx.tabs ?? {}))
  const countdownStart = new Map<string, number>()

  // Text that depends on the selected package, redrawn on selection.
  // Inside a plan card, text describes the card's package, not the selection.
  let cardPkg: PaywallPackageView | undefined
  const live: Array<{ node: HTMLElement; template: string; pkg?: PaywallPackageView }> = []
  // Text about savings disappears when there is nothing to save.
  const SAVINGS = /\{\{\s*savings\s*\}\}/
  const fillText = (node: HTMLElement, template: string, pkg: PaywallPackageView | undefined) => {
    node.textContent = resolvePaywallText(template, ctx.appName, pkg)
    if (SAVINGS.test(template)) node.style.display = pkg?.savings ? '' : 'none'
  }
  const bindText = (node: HTMLElement, template: string) => {
    live.push({ node, template, pkg: cardPkg })
    fillText(node, template, cardPkg ?? current())
  }
  const redraw: Array<() => void> = []
  const refresh = () => {
    for (const l of live) fillText(l.node, l.template, l.pkg ?? current())
    for (const r of redraw) r()
  }
  const select = (id: string | undefined) => {
    if (!id || !byId.has(id)) return
    selected = id
    refresh()
  }

  // Timers (carousel, countdown) stop once their node leaves the page.
  let timers: Array<ReturnType<typeof setInterval>> = []
  const every = (ms: number, node: Element, fn: () => void) => {
    let seen = false, ticks = 0
    const id = setInterval(() => {
      ticks++
      if (node.isConnected) seen = true
      else if (seen || ticks > 10) { clearInterval(id); return }
      fn()
    }, ms)
    timers.push(id)
  }

  const go = (id: string | undefined, back = false) => {
    if (!id || !screenById.has(id) || ctx.screen) return
    if (!back && screenId) history.push(screenId)
    screenId = id
    draw()
    ctx.onNavigate?.(id)
  }
  const JUSTIFY: Record<string, string> = { start: 'flex-start', center: 'center', end: 'flex-end', space_between: 'space-between' }
  const r = (n: number | undefined, fallback: number) => `${n && n > 0 ? Math.min(48, n) : fallback}px`

  function draw() {
    live.length = 0
    redraw.length = 0
    for (const id of timers) clearInterval(id)
    timers = []
    root.replaceChildren()
    const screen = screenById.get(screenId!)
    if (!screen) return
    const index = screens.indexOf(screen)
    useTheme(screen.theme)
    root.style.color = text
    root.style.fontFamily = font
    const sBg = screen.background && HEX.test(screen.background) ? screen.background : bg
    const sEnd = screen.background ? (screen.background_end && HEX.test(screen.background_end) ? screen.background_end : '') : end
    const px = screen.padding_x || 22
    const py = screen.padding_y || 28
    const pb = screen.padding_y || 20
    const closable = ctx.closable !== false && !!ctx.onClose
    const padTop = closable ? Math.max(52, py) : py
    Object.assign(root.style, {
      background: sEnd ? `linear-gradient(180deg, ${sBg}, ${sEnd})` : sBg,
      // Clear of the close button when there is one (same as iOS).
      padding: `${padTop}px ${px}px ${pb}px`,
      gap: `${screen.spacing || 14}px`,
      justifyContent: JUSTIFY[screen.justify ?? 'start'] ?? 'flex-start',
    })
    root.setAttribute('data-screen-id', screen.id)
    ensureMotionCSS()
    const media = mediaLayer(screen)
    if (media) root.appendChild(media)
    if (screen.effect) root.appendChild(effectLayer(screen.effect))

    if (closable) {
      const close = el('button', {
        position: 'absolute', top: '12px', right: '12px', width: '32px', height: '32px', borderRadius: '50%',
        border: 'none', background: surface, color: muted, fontSize: '18px', lineHeight: '32px', cursor: 'pointer', padding: '0', zIndex: '2',
      }, '×')
      close.setAttribute('aria-label', 'Close')
      close.onclick = () => ctx.onClose?.()
      root.appendChild(close)
    }

    // Each block's element names its block, so an editor can map a click on
    // the rendered paywall back to the block it came from.
    const mark = (b: PaywallBlock, node: HTMLElement) => {
      node.setAttribute('data-block-id', b.id)
      node.setAttribute('data-block-type', b.type)
      return node
    }
    // Inside a surface-coloured card or sheet, filled parts (plan rows,
    // tiles) take the screen's background so they still stand out.
    let onCard = false
    const fill = () => (onCard ? sBg : surface)
    const children = (list: PaywallBlock[] | undefined, align: string, into: HTMLElement, card = onCard) => {
      const was = onCard
      onCard = card
      for (const c of list ?? []) {
        const n = blockNode(c, align)
        // Blocks keep their height; a long screen scrolls instead of squashing.
        if (n) { n.style.flexShrink = '0'; into.appendChild(n) }
      }
      onCard = was
      return into
    }

    const press = (b: PaywallBlock) => {
      if (b.answer && screen) {
        try { ctx.onAnswer?.({ screen_id: screen.id, question: screen.question || screen.name, answer: b.answer }) } catch { /* the host's problem */ }
      }
      switch (b.action) {
        case 'next': go(screens[index + 1]?.id); break
        case 'back': go(history.pop(), true); break
        case 'screen': go(b.target); break
        case 'close': ctx.onClose?.(); break
        case 'restore': void ctx.onRestore?.(); break
        case 'url': { const u = safeURL(b.url); if (u) (globalThis as any).open?.(u, '_blank', 'noopener'); break }
      }
    }

    // The block's look, over whatever its design drew.
    // Hero media: edge to edge, flush with the top when it leads the screen,
    // and optionally fading into the page.
    const hero = (b: PaywallBlock, media: HTMLElement): HTMLElement => {
      const v = paywallVariant(b)
      if (v !== 'hero' && v !== 'hero_fade') return media
      const h = `${b.height ? Math.min(480, b.height) : 260}px`
      const wrap = el('div', { position: 'relative', height: h, marginLeft: `-${px}px`, marginRight: `-${px}px`, overflow: 'hidden', flex: '0 0 auto' })
      if (screen.blocks[0]?.id === b.id) wrap.style.marginTop = `-${padTop}px`
      Object.assign(media.style, { width: '100%', height: '100%', borderRadius: '0' })
      wrap.appendChild(media)
      if (v === 'hero_fade') wrap.appendChild(el('div', { position: 'absolute', left: '0', right: '0', bottom: '0', height: '45%', background: `linear-gradient(180deg, transparent, ${sBg})` }))
      return wrap
    }

    // The card being drawn (its package id), for look_selected inside it.
    let cardId: string | undefined
    function blockNode(b: PaywallBlock, inherited: string | undefined): HTMLElement | null {
      const card = b.package && (b.type === 'stack' || b.type === 'custom') ? b.package : undefined
      if (card && !byId.has(card)) return null // not in this offering
      const was = [cardPkg, cardId] as const
      if (card) { cardPkg = byId.get(card); cardId = card }
      const node = drawBlock(b, inherited)
      if (node) {
        const look = b.look && typeof b.look === 'object' ? b.look : undefined
        const sel = b.look_selected && typeof b.look_selected === 'object' ? b.look_selected : undefined
        const within = cardId
        if (sel && within) {
          // Remember the design's own styles, then lay the right look over them.
          const targets = [node, ...typeTargets(node, b)]
          const base = targets.map((t) => t.getAttribute('style') ?? '')
          const paint = () => {
            targets.forEach((t, i) => t.setAttribute('style', base[i]))
            const on = selected === within
            const merged = on ? { ...(look ?? {}), ...sel } : look
            if (merged) applyLook(node, { ...b, look: merged }, inherited)
          }
          paint()
          redraw.push(paint)
        } else if (look) applyLook(node, b, inherited)
        if (card) {
          node.setAttribute('role', 'radio')
          node.style.cursor = 'pointer'
          const mark2 = () => node.setAttribute('aria-checked', String(selected === card))
          mark2()
          redraw.push(mark2)
          node.addEventListener('click', () => select(card))
          if (b.badge) {
            // Over the card's top edge, without changing its size.
            if (!node.style.position) node.style.position = 'relative'
            const pill = el('span', {
              position: 'absolute', top: '0', left: '50%', transform: 'translate(-50%, -50%)', background: accent, color: accentText,
              fontSize: '11px', fontWeight: '700', padding: '3px 9px', borderRadius: '999px', whiteSpace: 'nowrap', zIndex: '1', lineHeight: '1.2',
            })
            pill.setAttribute('data-badge', '')
            bindText(pill, b.badge)
            node.appendChild(pill)
          }
        }
      }
      ;[cardPkg, cardId] = was
      return node
    }
    // The labels inside a block that its look's type settings reach.
    const typeTargets = (node: HTMLElement, b: PaywallBlock) =>
      Array.from(node.querySelectorAll<HTMLElement>(b.type === 'cta' ? ':scope > button' : b.type === 'header' ? 'strong, h1' : ':scope > strong'))
    function applyLook(node: HTMLElement, b: PaywallBlock, inherited: string | undefined) {
      const lk = b.look!, st = node.style
      const own = b.align ?? inherited
      const place = own === 'center' ? 'center' : own === 'right' ? 'flex-end' : 'flex-start'
      // Padding adds to what the design already has.
      const px = num(lk.padding_x, 0, 96), py = num(lk.padding_y, 0, 96)
      if (px || py) {
        for (const [side, add] of [['top', py], ['bottom', py], ['left', px], ['right', px]] as Array<[string, number]>) {
          const cur = st.getPropertyValue(`padding-${side}`) || '0px'
          st.setProperty(`padding-${side}`, `calc(${cur} + ${add}px)`)
        }
        st.boxSizing = 'border-box'
      }
      if (lk.width || lk.height) node.setAttribute('data-look-size', lk.width ?? '')
      if (lk.width === 'fit') Object.assign(st, { alignSelf: place, width: 'fit-content', maxWidth: '100%' })
      else if (lk.width === 'fill') Object.assign(st, { alignSelf: 'stretch', width: 'auto' })
      else if (lk.width === 'fixed' && b.type !== 'icon') Object.assign(st, { alignSelf: place, width: `${num(lk.width_px, 1, 1000)}px`, maxWidth: '100%', boxSizing: 'border-box' })
      if (lk.height === 'fill') Object.assign(st, { flexGrow: '1' })
      else if (lk.height === 'fixed') Object.assign(st, { height: `${num(lk.height_px, 1, 1000)}px`, boxSizing: 'border-box', overflow: 'hidden' })
      // Fill: colour or gradient, with an image over it.
      const c1 = lk.background && HEX.test(lk.background) ? lk.background : ''
      const c2 = lk.background_end && HEX.test(lk.background_end) ? lk.background_end : ''
      const fillCss = c1 && c2 ? `linear-gradient(180deg, ${c1}, ${c2})` : c1
      const img = safeURL(lk.background_image)
      if (img) st.background = `url("${img}") center / cover no-repeat${fillCss ? `, ${fillCss}` : ''}`
      else if (fillCss) st.background = fillCss
      if (Array.isArray(lk.corners) && lk.corners.length === 4) st.borderRadius = lk.corners.map((n) => `${num(n, 0, 200)}px`).join(' ')
      else if (lk.radius) st.borderRadius = `${num(lk.radius, 0, 200)}px`
      if (lk.border_color && HEX.test(lk.border_color) && num(lk.border_width, 0, 12) > 0) {
        st.border = `${num(lk.border_width, 0, 12)}px solid ${lk.border_color}`
        st.boxSizing = 'border-box'
      }
      if (lk.shadow_color && HEX.test(lk.shadow_color)) st.boxShadow = `${num(lk.shadow_x, -50, 50)}px ${num(lk.shadow_y, -50, 50)}px ${num(lk.shadow_blur, 0, 100)}px ${lk.shadow_color}`
      if (lk.opacity) st.opacity = String(num(lk.opacity, 1, 100) / 100)
      const mx = num(lk.margin_x, -48, 96), my = num(lk.margin_y, -48, 96)
      if (mx || my) st.margin = `${my}px ${mx}px`
      // Type: on the node, and on the label inside blocks that draw one.
      const targets = [node, ...typeTargets(node, b)]
      for (const t of targets) {
        if (lk.font && FONTS[lk.font]) t.style.fontFamily = FONTS[lk.font]
        if (lk.weight && WEIGHTS[lk.weight]) t.style.fontWeight = WEIGHTS[lk.weight]
        if (lk.font_size) t.style.fontSize = `${num(lk.font_size, 8, 96)}px`
        if (lk.italic) t.style.fontStyle = 'italic'
        if (lk.color && HEX.test(lk.color) && b.type !== 'icon') t.style.color = lk.color
      }
      if (own === 'right' && (b.type === 'title' || b.type === 'text')) st.textAlign = 'right'
    }

    function drawBlock(b: PaywallBlock, inherited: string | undefined): HTMLElement | null {
      // Countdowns and social proof centre unless told otherwise (as on iOS).
      const own = b.type === 'countdown' || b.type === 'social_proof' ? (b.align ?? 'center') : (b.align ?? inherited)
      const align = own === 'center' || own === 'right' ? own : 'left'
      const self = align === 'center' ? 'center' : align === 'right' ? 'flex-end' : 'flex-start'
      switch (b.type) {
        case 'icon': {
          const variant = paywallVariant(b)
          const size = b.look?.width === 'fixed' && b.look.width_px ? num(b.look.width_px, 8, 400)
            : ({ s: 40, m: 56, l: 72, xl: 96 } as Record<string, number>)[b.size ?? 'l'] ?? 72
          const tintGlyph = b.look?.color && HEX.test(b.look.color) ? b.look.color : ''
          const glyph = (color: string, scale: number) => b.emoji
            ? el('span', { fontSize: `${Math.round(size * (variant === 'plain' ? 0.8 : 0.55))}px`, lineHeight: '1' }, b.emoji)
            : icon(b.icon ?? 'star', tintGlyph || color, Math.round(size * scale))
          const gradient = `linear-gradient(135deg, ${accent}, ${accent})`
          const box = el('div', { position: 'relative', width: `${size}px`, height: `${size}px`, display: 'grid', placeItems: 'center', alignSelf: self, flex: '0 0 auto' })
          switch (variant) {
            case 'plain':
              box.appendChild(glyph(accent, 0.6))
              break
            case 'glow': {
              const orb = moving(el('div', {
                width: '100%', height: '100%', borderRadius: '50%', display: 'grid', placeItems: 'center',
                background: gradient, boxShadow: `0 10px 40px ${tint(accent, 0.55)}`,
              }), 'cpw-pulse 2.4s ease-in-out infinite alternate')
              // The second stop, hue-shifted, over the first.
              const sheen = el('div', { position: 'absolute', inset: '0', borderRadius: '50%', background: `linear-gradient(135deg, transparent, ${accent})`, filter: 'hue-rotate(30deg)' })
              orb.style.position = 'relative'
              const g = glyph(accentText, 0.5) as HTMLElement
              g.style.position = 'relative'
              orb.append(sheen, g)
              box.appendChild(orb)
              break
            }
            case 'rings': {
              for (const delay of [0, 1.2]) {
                box.appendChild(moving(el('span', { position: 'absolute', inset: '0', borderRadius: '50%', border: `2px solid ${tint(accent, 0.5)}` }),
                  `cpw-ring 2.4s ease-out ${delay}s infinite`))
              }
              const core = el('div', { position: 'relative', width: '100%', height: '100%', borderRadius: '50%', background: fill(), display: 'grid', placeItems: 'center' })
              core.appendChild(glyph(accent, 0.5))
              box.appendChild(core)
              break
            }
            case 'gradient': {
              const tile = moving(el('div', {
                position: 'relative', width: '100%', height: '100%', borderRadius: '28%', display: 'grid', placeItems: 'center', overflow: 'hidden',
                background: accent, boxShadow: `0 8px 18px ${tint(accent, 0.35)}`,
              }), 'cpw-bob 3.2s ease-in-out infinite alternate')
              const sheen = el('div', { position: 'absolute', inset: '0', background: `linear-gradient(135deg, transparent, ${accent})`, filter: 'hue-rotate(30deg)' })
              const g = glyph(accentText, 0.5) as HTMLElement
              g.style.position = 'relative'
              tile.append(sheen, g)
              box.appendChild(tile)
              break
            }
            default:
              Object.assign(box.style, { borderRadius: '50%', background: fill() })
              box.appendChild(glyph(accent, 0.5))
          }
          return mark(b, box)
        }
        case 'button': {
          const style = b.style ?? 'primary'
          const shape = paywallVariant(b)
          if (shape === 'option') {
            const opt = el('button', {
              display: 'flex', alignItems: 'center', gap: '14px', width: '100%', textAlign: 'left', cursor: 'pointer', font: 'inherit', color: text,
              background: fill(), borderRadius: `${radius}px`, padding: '16px', border: '1.5px solid transparent', transition: 'border-color .15s, transform .15s',
            })
            if (b.emoji) opt.appendChild(el('span', { fontSize: '24px', lineHeight: '1', flex: '0 0 auto' }, b.emoji))
            else if (b.icon) {
              const t = el('span', { width: '36px', height: '36px', borderRadius: '10px', background: tint(accent, 0.12), display: 'grid', placeItems: 'center', flex: '0 0 auto' })
              t.appendChild(icon(b.icon, accent, 18))
              opt.appendChild(t)
            }
            const label = el('span', { flex: '1', fontSize: '16px', fontWeight: '600', minWidth: '0' })
            bindText(label, b.text ?? '')
            const chev = icon('chevronRight', muted, 16)
            chev.style.flex = '0 0 auto'
            opt.append(label, chev)
            opt.onmouseenter = () => { opt.style.borderColor = accent }
            opt.onmouseleave = () => { opt.style.borderColor = 'transparent' }
            opt.onclick = () => press(b)
            return mark(b, opt)
          }
          const corner = shape === 'pill' ? '999px' : shape === 'square' ? '4px' : `${Math.max(radius, 10)}px`
          const btn = el('button', style === 'link'
            ? { background: 'none', border: 'none', color: muted, textDecoration: 'underline', cursor: 'pointer', font: 'inherit', fontSize: '14px', padding: '6px', alignSelf: 'center' }
            : {
                background: style === 'primary' ? accent : style === 'outline' ? 'transparent' : fill(),
                color: style === 'primary' ? accentText : style === 'outline' ? accent : text,
                border: style === 'outline' ? `1.5px solid ${accent}` : 'none',
                borderRadius: corner, padding: '15px', fontSize: '16px', cursor: 'pointer', font: 'inherit',
              })
          if (style !== 'link') btn.style.fontWeight = '700'
          bindText(btn, b.text ?? 'Continue')
          btn.onclick = () => press(b)
          return mark(b, btn)
        }
        case 'spacer':
          return mark(b, el('div', { height: `${SPACER_SIZES[b.size ?? 'm'] ?? 16}px`, flex: '0 0 auto' }))
        case 'image': {
          const src = safeURL(b.url)
          if (!src) return null
          const img = el('img', {
            width: '100%', height: b.height ? `${Math.min(480, b.height)}px` : 'auto', objectFit: 'cover',
            borderRadius: `${radius}px`, display: 'block',
          })
          img.src = src
          img.alt = ''
          return mark(b, hero(b, img))
        }
        case 'art': {
          const box = el('div', { position: 'relative', width: '100%', height: `${b.height ? Math.min(480, b.height) : 220}px`, borderRadius: `${radius}px`, overflow: 'hidden', flex: '0 0 auto' })
          box.appendChild(drawArt(b.art ?? '', { A: accent, AT: accentText, T: text, B: sBg, S: surface, M: muted }))
          return mark(b, hero(b, box))
        }
        case 'video': {
          const h = `${b.height ? Math.min(480, b.height) : 200}px`
          const src = safeURL(b.url)
          const box = el('div', { position: 'relative', width: '100%', height: h, borderRadius: `${radius}px`, overflow: 'hidden', background: fill(), flex: '0 0 auto' })
          if (src) {
            const v = el('video', { width: '100%', height: '100%', objectFit: 'cover', display: 'block' })
            v.muted = true; v.loop = true; v.autoplay = true; v.playsInline = true
            v.setAttribute('muted', ''); v.setAttribute('playsinline', '')
            const poster = safeURL(b.poster)
            if (poster) v.poster = poster
            v.src = src
            box.appendChild(v)
          } else {
            const ph = el('div', { position: 'absolute', inset: '0', display: 'grid', placeItems: 'center' })
            ph.appendChild(icon('play', muted, 36))
            box.appendChild(ph)
          }
          return mark(b, hero(b, box))
        }
        case 'title': {
          const h = el('h2', {
            margin: '0', fontSize: `${TITLE_SIZES[b.size ?? 'l'] ?? 30}px`, lineHeight: '1.15', fontWeight: '700',
            letterSpacing: '-0.02em', textAlign: align,
          })
          bindText(h, b.text ?? '')
          return mark(b, h)
        }
        case 'text': {
          const p = el('p', { margin: '0', fontSize: `${TEXT_SIZES[b.size ?? 'm'] ?? 15}px`, lineHeight: '1.5', color: muted, textAlign: align })
          bindText(p, b.text ?? '')
          return mark(b, p)
        }
        case 'header': {
          const variant = paywallVariant(b)
          // Level with the close button.
          const level = { marginTop: closable ? `${12 - padTop}px` : '0', flex: '0 0 auto' }
          const backBtn = () => {
            const back = el('button', { width: '32px', height: '32px', borderRadius: '50%', border: 'none', background: fill(), display: 'grid', placeItems: 'center', cursor: 'pointer', padding: '0', flex: '0 0 auto' })
            back.setAttribute('aria-label', 'Back')
            back.appendChild(icon('chevronLeft', text, 18))
            back.onclick = () => go(history.pop(), true)
            return back
          }
          if (variant === 'large') {
            const box = el('div', { display: 'flex', flexDirection: 'column', gap: '10px', ...level })
            if (b.back || closable) box.appendChild(b.back ? backBtn() : el('span', { height: '32px' }))
            const title = el('h1', { margin: '0', fontSize: '28px', fontWeight: '700', letterSpacing: '-0.02em', lineHeight: '1.15', textAlign: 'left' })
            bindText(title, b.text ?? '')
            box.appendChild(title)
            return mark(b, box)
          }
          if (variant === 'brand') {
            const row = el('div', { display: 'flex', alignItems: 'center', gap: '10px', minHeight: '32px', marginRight: closable ? '36px' : '0', ...level })
            if (b.back) row.appendChild(backBtn())
            const tile = el('span', { width: '30px', height: '30px', borderRadius: '9px', background: accent, display: 'grid', placeItems: 'center', flex: '0 0 auto' })
            tile.appendChild(icon(b.icon || 'sparkles', accentText, 16))
            const title = el('strong', { fontSize: '17px', fontWeight: '700', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' })
            bindText(title, b.text ?? '')
            row.append(tile, title)
            return mark(b, row)
          }
          const bar = el('div', { display: 'grid', gridTemplateColumns: '32px 1fr 32px', alignItems: 'center', minHeight: '32px', marginRight: closable ? '36px' : '0', ...level })
          bar.appendChild(b.back ? backBtn() : el('span'))
          const title = el('strong', { textAlign: 'center', fontSize: variant === 'pill' ? '14px' : '16px', fontWeight: '600', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' })
          bindText(title, b.text ?? '')
          if (variant === 'pill' && b.text) {
            Object.assign(title.style, { background: fill(), borderRadius: '999px', padding: '6px 14px', justifySelf: 'center', maxWidth: '100%', boxSizing: 'border-box' })
          }
          bar.append(title, el('span'))
          return mark(b, bar)
        }
        case 'stack':
        case 'custom': {
          const row = b.axis === 'horizontal', layers = b.axis === 'layers'
          const CROSS: Record<string, string> = { start: 'flex-start', center: 'center', end: 'flex-end', stretch: 'stretch' }
          const box = el('div', layers
            ? { display: 'grid', justifyItems: b.cross === 'stretch' ? 'stretch' : ({ start: 'start', center: 'center', end: 'end' } as Record<string, string>)[b.cross ?? 'center'] ?? 'center',
                alignItems: ({ start: 'start', center: 'center', end: 'end', space_between: 'center' } as Record<string, string>)[b.justify ?? 'center'] ?? 'center', minWidth: '0' }
            : {
                display: 'flex', flexDirection: row ? 'row' : 'column', gap: `${b.gap && b.gap > 0 ? b.gap : 12}px`,
                justifyContent: JUSTIFY[b.justify ?? 'start'] ?? 'flex-start', alignItems: CROSS[b.cross ?? ''] ?? (row ? 'center' : 'stretch'), minWidth: '0',
              })
          if (b.type === 'custom') {
            Object.assign(box.style, {
              background: color(b.background, surface), borderRadius: r(b.radius, radius), padding: `${b.padding && b.padding > 0 ? b.padding : 16}px`,
              border: b.border && HEX.test(b.border) ? `1px solid ${b.border}` : 'none',
            })
          }
          children(b.children, align, box, b.type === 'custom' ? color(b.background, surface) === surface : onCard)
          if (row) for (const c of Array.from(box.children) as HTMLElement[]) { c.style.minWidth = '0'; const w = c.getAttribute('data-look-size'); if (w === 'fill') c.style.flex = '1 1 0'; else if (w === null) c.style.flex = '0 1 auto' }
          if (layers) for (const c of Array.from(box.children) as HTMLElement[]) c.style.gridArea = '1 / 1'
          return mark(b, box)
        }
        case 'sheet': {
          const sheet = el('div', {
            display: 'flex', flexDirection: 'column', gap: `${b.gap && b.gap > 0 ? b.gap : 12}px`,
            background: color(b.background, surface), borderRadius: `${r(b.radius, 28)} ${r(b.radius, 28)} 0 0`,
            padding: `26px ${b.padding && b.padding > 0 ? b.padding : 22}px ${Math.max(pb, 22)}px`,
            // Pinned to the bottom, edge to edge.
            marginTop: 'auto', marginLeft: `-${px}px`, marginRight: `-${px}px`, marginBottom: `-${pb}px`,
            position: 'relative', boxShadow: '0 -12px 32px -20px rgba(0,0,0,.35)',
          })
          sheet.appendChild(el('span', { position: 'absolute', top: '9px', left: '50%', transform: 'translateX(-50%)', width: '36px', height: '5px', borderRadius: '3px', background: muted, opacity: '0.35' }))
          children(b.children, align, sheet, color(b.background, surface) === surface)
          return mark(b, sheet)
        }
        case 'tabs': {
          const variant = paywallVariant(b)
          const tabs = b.children ?? []
          const box = el('div', { display: 'flex', flexDirection: 'column', gap: '14px' })
          const bar = el('div', variant === 'underline'
            ? { display: 'flex', borderBottom: `1px solid ${tint(muted, 0.25)}` }
            : variant === 'outline' ? { display: 'flex', gap: '8px' }
            : { display: 'flex', gap: '4px', padding: '4px', background: fill(), borderRadius: '999px' })
          bar.setAttribute('role', 'tablist')
          const panel = el('div', { display: 'flex', flexDirection: 'column' })
          const show = () => {
            const open = Math.min(tabOpen.get(b.id) ?? 0, tabs.length - 1)
            Array.from(bar.children).forEach((btn, i) => {
              const on = i === open, st = (btn as HTMLElement).style
              if (variant === 'underline') Object.assign(st, { color: on ? text : muted, boxShadow: on ? `inset 0 -2.5px 0 ${accent}` : 'none' })
              else if (variant === 'outline') Object.assign(st, { color: on ? accent : muted, border: `1.5px solid ${on ? accent : tint(muted, 0.35)}`, background: on ? tint(accent, 0.1) : 'transparent' })
              else Object.assign(st, { background: on ? accent : 'transparent', color: on ? accentText : muted })
              btn.setAttribute('aria-selected', String(on))
            })
            panel.replaceChildren()
            const n = tabs[open] && blockNode(tabs[open], align)
            if (n) panel.appendChild(n)
          }
          tabs.forEach((tab, i) => {
            const btn = el('button', {
              flex: '1', border: 'none', background: 'transparent', font: 'inherit', fontSize: variant === 'underline' ? '15px' : '14px', fontWeight: '600', cursor: 'pointer',
              borderRadius: variant === 'underline' ? '0' : '999px', padding: variant === 'underline' ? '10px 6px 12px' : '9px 10px',
            })
            btn.setAttribute('role', 'tab')
            bindText(btn, tab.text ?? `Tab ${i + 1}`)
            btn.onclick = () => {
              tabOpen.set(b.id, i)
              show()
              // Tiers: opening a tab picks its first plan if the selection is elsewhere.
              const cards = flattenBlocks([tab]).map((c) => c.package).filter((id): id is string => !!id && byId.has(id))
              if (cards.length && !cards.includes(selected ?? '')) select(cards[0])
            }
            bar.appendChild(btn)
          })
          show()
          box.append(bar, panel)
          return mark(b, box)
        }
        case 'switch': {
          const variant = paywallVariant(b)
          const row = el('div', variant === 'plain'
            ? { display: 'flex', alignItems: 'center', gap: '12px', padding: '4px 0 12px', borderBottom: `1px solid ${tint(muted, 0.2)}`, cursor: 'pointer' }
            : { display: 'flex', alignItems: 'center', gap: '12px', background: fill(), borderRadius: `${radius}px`, padding: '14px 16px', cursor: 'pointer' })
          const label = el('div', { flex: '1', display: 'flex', flexDirection: 'column', gap: '2px', minWidth: '0' })
          const t1 = el('strong', { fontSize: '15px', fontWeight: '600' })
          bindText(t1, b.text ?? '')
          label.appendChild(t1)
          if (b.subtext) { const t2 = el('span', { fontSize: '13px', color: muted }); bindText(t2, b.subtext); label.appendChild(t2) }
          let paint: () => void
          if (variant === 'checkbox') {
            const box = el('button', { width: '24px', height: '24px', borderRadius: '7px', padding: '0', cursor: 'pointer', display: 'grid', placeItems: 'center', flex: '0 0 auto' })
            box.setAttribute('role', 'switch')
            const mark2 = icon('check', '#FFFFFF', 16)
            box.appendChild(mark2)
            paint = () => {
              const on = selected === b.package_on
              Object.assign(box.style, { background: on ? accent : 'transparent', border: on ? `2px solid ${accent}` : `2px solid ${tint(muted, 0.45)}` })
              mark2.style.visibility = on ? 'visible' : 'hidden'
              box.setAttribute('aria-checked', String(on))
            }
            row.append(box, label)
          } else {
            const track = el('button', { width: '50px', height: '30px', borderRadius: '999px', border: 'none', position: 'relative', flex: '0 0 auto', cursor: 'pointer', padding: '0', transition: 'background .2s' })
            track.setAttribute('role', 'switch')
            const knob = el('span', { position: 'absolute', top: '3px', width: '24px', height: '24px', borderRadius: '50%', background: '#FFFFFF', boxShadow: '0 1px 3px rgba(0,0,0,.3)', transition: 'left .2s' })
            track.appendChild(knob)
            paint = () => {
              const on = selected === b.package_on
              track.style.background = on ? accent : muted
              track.style.opacity = on ? '1' : '0.45'
              knob.style.left = on ? '23px' : '3px'
              track.setAttribute('aria-checked', String(on))
            }
            row.append(label, track)
          }
          paint()
          redraw.push(paint)
          row.onclick = () => select(switchPackage(b, selected !== b.package_on))
          return mark(b, row)
        }
        case 'carousel': {
          const variant = paywallVariant(b)
          const pages = b.children ?? []
          const peek = variant === 'peek'
          const box = el('div', { display: 'flex', flexDirection: 'column', gap: '10px', flex: '0 0 auto' })
          const track = el('div', {
            display: 'flex', overflowX: 'auto', scrollSnapType: 'x mandatory', height: `${b.height ? Math.min(480, b.height) : 260}px`,
            scrollbarWidth: 'none', borderRadius: peek ? '0' : `${radius}px`, gap: peek ? '12px' : '0',
          } as Partial<CSSStyleDeclaration>)
          const slots: HTMLElement[] = []
          for (const pg of pages) {
            const slot = el('div', peek
              ? { flex: '0 0 84%', scrollSnapAlign: 'start', display: 'flex', flexDirection: 'column', justifyContent: 'center', boxSizing: 'border-box', padding: '16px', background: fill(), borderRadius: `${radius}px` }
              : { flex: '0 0 100%', scrollSnapAlign: 'start', display: 'flex', flexDirection: 'column', justifyContent: 'center', boxSizing: 'border-box', padding: '0 2px' })
            const n = blockNode(pg, align)
            if (n) slot.appendChild(n)
            track.appendChild(slot)
            slots.push(slot)
          }
          const step = () => (slots[1] ? slots[1].offsetLeft - slots[0].offsetLeft : track.clientWidth) || track.clientWidth || 1
          const page = () => Math.min(pages.length - 1, Math.round(track.scrollLeft / step()))
          const goTo = (i: number) => track.scrollTo({ left: i * step(), behavior: 'smooth' })
          const marks = el('div', variant === 'bars' ? { display: 'flex', gap: '4px' } : { display: 'flex', justifyContent: 'center', gap: '6px' })
          const paintMarks = () => Array.from(marks.children).forEach((d, i) => {
            const on = i === page()
            if (variant === 'bars') Object.assign((d as HTMLElement).style, { background: on ? accent : tint(muted, 0.3) })
            else Object.assign((d as HTMLElement).style, { background: on ? accent : muted, opacity: on ? '1' : '0.35', width: on ? '18px' : '7px' })
          })
          pages.forEach((_, i) => {
            const d = el('button', variant === 'bars'
              ? { flex: '1', height: '3px', borderRadius: '999px', border: 'none', padding: '0', cursor: 'pointer' }
              : { height: '7px', borderRadius: '999px', border: 'none', padding: '0', cursor: 'pointer', transition: 'width .2s' })
            d.setAttribute('aria-label', `Page ${i + 1}`)
            d.onclick = () => goTo(i)
            marks.appendChild(d)
          })
          track.addEventListener('scroll', paintMarks, { passive: true })
          paintMarks()
          if (b.interval && b.interval > 0 && pages.length > 1) every(b.interval * 1000, track, () => goTo((page() + 1) % pages.length))
          if (variant === 'bars') box.append(marks, track)
          else box.append(track, marks)
          return mark(b, box)
        }
        case 'countdown': {
          const variant = paywallVariant(b)
          if (!countdownStart.has(b.id)) countdownStart.set(b.id, Date.now())
          const parts = () => {
            const s = Math.floor(countdownRemaining(b, countdownStart.get(b.id)!, Date.now()) / 1000)
            return [Math.floor(s / 3600), Math.floor(s / 60) % 60, s % 60].map((n) => String(n).padStart(2, '0'))
          }
          const labelNode = (style: Partial<CSSStyleDeclaration>) => { const l = el('span', style); bindText(l, b.text ?? ''); return l }
          if (variant === 'inline' || variant === 'banner') {
            const banner = variant === 'banner'
            const row = el('div', banner
              ? { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', background: accent, color: accentText, borderRadius: `${radius}px`, padding: '12px 16px' }
              : { display: 'flex', alignItems: 'center', gap: '8px', background: fill(), borderRadius: '999px', padding: '8px 14px', alignSelf: 'center' })
            if (!banner) row.appendChild(icon('clock', accent, 16))
            if (b.text) row.appendChild(labelNode(banner ? { fontSize: '13px', fontWeight: '600' } : { fontSize: '13px', color: muted }))
            const time = el('strong', { fontVariantNumeric: 'tabular-nums', fontSize: banner ? '20px' : '15px', fontWeight: '700', fontFamily: banner ? 'inherit' : 'inherit' })
            const tick = () => { time.textContent = parts().join(':') }
            tick(); every(1000, time, tick)
            row.appendChild(time)
            return mark(b, row)
          }
          const box = el('div', { display: 'flex', flexDirection: 'column', alignItems: self, gap: '8px' })
          if (b.text) box.appendChild(labelNode({ fontSize: '12px', fontWeight: '600', color: muted, letterSpacing: '.08em', textTransform: 'uppercase' }))
          const digits = el('div', { display: 'flex', alignItems: 'center', gap: '6px', fontVariantNumeric: 'tabular-nums' })
          const units = ['hours', 'min', 'sec']
          const cells = units.map((u) => {
            const cell = el('span', { background: fill(), borderRadius: `${Math.min(radius, 12)}px`, padding: variant === 'labeled' ? '8px 10px 6px' : '8px 10px', minWidth: '44px', textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center' })
            const n = el('span', { fontSize: '22px', fontWeight: '700', lineHeight: '1.2' })
            cell.appendChild(n)
            if (variant === 'labeled') cell.appendChild(el('span', { fontSize: '10px', color: muted, marginTop: '2px' }, u))
            return { cell, n }
          })
          cells.forEach((c, i) => { if (i) digits.appendChild(el('span', { fontWeight: '700', color: muted }, ':')); digits.appendChild(c.cell) })
          const tick = () => { const p = parts(); cells.forEach((c, i) => { c.n.textContent = p[i] }) }
          tick()
          every(1000, digits, tick)
          box.appendChild(digits)
          return mark(b, box)
        }
        case 'timeline': {
          const variant = paywallVariant(b)
          const items = b.items ?? []
          const texts = (it: PaywallItem, center = false, small = false) => {
            const body = el('div', { display: 'flex', flexDirection: 'column', gap: '2px', minWidth: '0', textAlign: center ? 'center' : 'left', alignItems: center ? 'center' : 'flex-start' })
            const t1 = el('strong', { fontSize: small ? '13px' : '15px', fontWeight: '600' }); bindText(t1, it.title); body.appendChild(t1)
            if (it.text) { const t2 = el('span', { fontSize: small ? '11px' : '13px', color: muted, lineHeight: '1.4' }); bindText(t2, it.text); body.appendChild(t2) }
            return body
          }
          const circle = (it: PaywallItem, bgc: string) => {
            const dot = el('span', { width: '32px', height: '32px', borderRadius: '50%', background: bgc, display: 'grid', placeItems: 'center', flex: '0 0 auto', position: 'relative', zIndex: '1' })
            dot.appendChild(icon(it.icon, accent, 16))
            return dot
          }
          if (variant === 'cards') {
            const list = el('div', { display: 'flex', flexDirection: 'column', gap: '8px' })
            for (const it of items) {
              const card = el('div', { display: 'flex', gap: '12px', alignItems: 'center', background: fill(), borderRadius: `${radius}px`, padding: '14px' })
              card.append(circle(it, tint(accent, 0.15)), texts(it))
              list.appendChild(card)
            }
            return mark(b, list)
          }
          if (variant === 'horizontal') {
            const grid = el('div', { display: 'grid', gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))`, position: 'relative' })
            // The line runs through the circle centres, first to last.
            const half = 100 / items.length / 2
            grid.appendChild(el('span', { position: 'absolute', top: '15px', left: `${half}%`, right: `${half}%`, height: '2px', background: tint(muted, 0.3) }))
            for (const it of items) {
              const col = el('div', { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '8px', padding: '0 4px' })
              col.append(circle(it, fill()), texts(it, true, true))
              grid.appendChild(col)
            }
            return mark(b, grid)
          }
          const list = el('div', { display: 'flex', flexDirection: 'column' })
          items.forEach((it, i) => {
            const row = el('div', { display: 'flex', gap: '14px', alignItems: 'stretch' })
            const rail = el('div', { display: 'flex', flexDirection: 'column', alignItems: 'center', flex: '0 0 32px' })
            rail.appendChild(circle(it, fill()))
            if (i < items.length - 1) rail.appendChild(el('span', { width: '2px', flex: '1', minHeight: '14px', background: muted, opacity: '0.3', margin: '4px 0' }))
            const body = texts(it)
            Object.assign(body.style, { paddingTop: '5px', paddingBottom: i < items.length - 1 ? '14px' : '0' })
            row.append(rail, body)
            list.appendChild(row)
          })
          return mark(b, list)
        }
        case 'social_proof': {
          const variant = paywallVariant(b)
          const imgs = (b.images ?? []).map(safeURL).filter((u): u is string => !!u).slice(0, 5)
          const faces = (size: number) => {
            const row = el('div', { display: 'flex', flex: '0 0 auto' })
            const count = imgs.length || 4
            for (let i = 0; i < count; i++) {
              const f = el('span', { width: `${size}px`, height: `${size}px`, borderRadius: '50%', border: `2px solid ${variant === 'badge' ? fill() : sBg}`, marginLeft: i ? `-${Math.round(size * 0.3)}px` : '0', overflow: 'hidden', display: 'grid', placeItems: 'center', background: fill(), boxSizing: 'border-box' })
              if (imgs[i]) {
                const img = el('img', { width: '100%', height: '100%', objectFit: 'cover' })
                img.src = imgs[i]; img.alt = ''
                f.appendChild(img)
              } else {
                f.style.background = accent
                f.style.opacity = String(1 - i * 0.18)
                f.appendChild(icon('people', accentText, Math.round(size * 0.47)))
              }
              row.appendChild(f)
            }
            return row
          }
          if (variant === 'badge') {
            const pill = el('div', { display: 'flex', alignItems: 'center', gap: '8px', background: fill(), borderRadius: '999px', padding: '6px 14px 6px 6px', alignSelf: self })
            const t1 = el('strong', { fontSize: '13px', fontWeight: '600' }); bindText(t1, b.text ?? '')
            pill.append(faces(24), t1)
            return mark(b, pill)
          }
          const box = el('div', { display: 'flex', flexDirection: 'column', alignItems: self, gap: '6px', textAlign: align })
          if (variant !== 'rating') box.appendChild(faces(34))
          if (b.rating) box.appendChild(el('div', { color: accent, letterSpacing: '2px', fontSize: variant === 'rating' ? '22px' : '15px' }, '★'.repeat(Math.min(5, b.rating))))
          const t1 = el('strong', { fontSize: variant === 'rating' ? '17px' : '15px', fontWeight: variant === 'rating' ? '700' : '600' }); bindText(t1, b.text ?? ''); box.appendChild(t1)
          if (b.subtext) { const t2 = el('span', { fontSize: '13px', color: muted }); bindText(t2, b.subtext); box.appendChild(t2) }
          return mark(b, box)
        }
        case 'award': {
          const variant = paywallVariant(b)
          if (variant === 'ribbon') {
            const pill = el('div', { display: 'flex', alignItems: 'center', gap: '8px', background: accent, color: accentText, borderRadius: '999px', padding: '8px 16px', alignSelf: 'center' })
            pill.appendChild(icon('star', accentText, 16))
            const t1 = el('strong', { fontSize: '14px', fontWeight: '700' }); bindText(t1, b.text ?? ''); pill.appendChild(t1)
            if (b.subtext) { const t2 = el('span', { fontSize: '13px', opacity: '0.8' }); t2.textContent = '· '; const sub = el('span'); bindText(sub, b.subtext); t2.appendChild(sub); pill.appendChild(t2) }
            return mark(b, pill)
          }
          if (variant === 'badge') {
            const col = el('div', { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '6px', textAlign: 'center', alignSelf: 'center' })
            const medal = el('span', { width: '56px', height: '56px', borderRadius: '50%', background: tint(accent, 0.15), display: 'grid', placeItems: 'center' })
            medal.appendChild(icon('trophy', accent, 28))
            const t1 = el('strong', { fontSize: '17px', fontWeight: '700' }); bindText(t1, b.text ?? '')
            col.append(medal, t1)
            if (b.subtext) { const t2 = el('span', { fontSize: '12px', color: muted }); bindText(t2, b.subtext); col.appendChild(t2) }
            return mark(b, col)
          }
          const box = el('div', { display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '10px', alignSelf: 'center' })
          const body = el('div', { display: 'flex', flexDirection: 'column', alignItems: 'center', textAlign: 'center', gap: '2px' })
          const t1 = el('strong', { fontSize: '17px', fontWeight: '700', lineHeight: '1.2' }); bindText(t1, b.text ?? ''); body.appendChild(t1)
          if (b.subtext) { const t2 = el('span', { fontSize: '12px', color: muted, letterSpacing: '.04em' }); bindText(t2, b.subtext); body.appendChild(t2) }
          box.append(laurel(text, false), body, laurel(text, true))
          return mark(b, box)
        }
        case 'features': {
          const variant = paywallVariant(b)
          const items = b.items ?? []
          const texts = (it: PaywallItem, titleSize: string, textSize: string) => {
            const body = el('div', { display: 'flex', flexDirection: 'column', gap: '2px', minWidth: '0' })
            const title = el('strong', { fontSize: titleSize, fontWeight: '600' }); bindText(title, it.title); body.appendChild(title)
            if (it.text) { const sub = el('span', { fontSize: textSize, color: muted, lineHeight: '1.4' }); bindText(sub, it.text); body.appendChild(sub) }
            return body
          }
          if (variant === 'grid') {
            const grid = el('div', { display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '10px', margin: '4px 0' })
            for (const it of items) {
              const tile = el('div', { display: 'flex', flexDirection: 'column', gap: '8px', background: fill(), borderRadius: `${radius}px`, padding: '14px' })
              tile.append(icon(it.icon, accent, 22), texts(it, '14px', '12px'))
              grid.appendChild(tile)
            }
            return mark(b, grid)
          }
          const list = el('div', { display: 'flex', flexDirection: 'column', gap: variant === 'checks' ? '8px' : variant === 'cards' ? '8px' : '12px', margin: '4px 0' })
          for (const it of items) {
            if (variant === 'checks') {
              const row = el('div', { display: 'flex', gap: '10px', alignItems: 'flex-start' })
              const c = icon('check', accent, 18); c.style.flex = '0 0 auto'; c.style.marginTop = '1px'
              row.append(c, texts(it, '15px', '13px'))
              list.appendChild(row)
              continue
            }
            const row = el('div', variant === 'cards'
              ? { display: 'flex', gap: '12px', alignItems: 'center', background: fill(), borderRadius: `${radius}px`, padding: '12px 14px' }
              : { display: 'flex', gap: '12px', alignItems: 'flex-start' })
            const badge = el('span', {
              flex: '0 0 auto', width: '34px', height: '34px', borderRadius: `${Math.min(radius, 12)}px`, background: variant === 'cards' ? tint(accent, 0.12) : fill(),
              display: 'grid', placeItems: 'center',
            })
            badge.appendChild(icon(it.icon, accent, 18))
            const body = texts(it, '15px', '13px')
            if (variant !== 'cards') body.style.paddingTop = '2px'
            row.append(badge, body)
            list.appendChild(row)
          }
          return mark(b, list)
        }
        case 'packages': {
          const cards = b.layout === 'cards'
          const wrap = el('div', {
            display: cards ? 'grid' : 'flex', flexDirection: 'column', gap: '10px',
            gridTemplateColumns: cards ? `repeat(${Math.min(3, Math.max(1, ctx.packages.length))}, minmax(0, 1fr))` : '',
            margin: '6px 0',
          })
          wrap.setAttribute('role', 'radiogroup')
          if (ctx.packages.length === 0) {
            wrap.appendChild(el('p', { color: muted, fontSize: '13px', margin: '0' }, 'No plans are available right now.'))
          }
          for (const pkg of ctx.packages) {
            const opt = el('button', {
              position: 'relative', textAlign: cards ? 'center' : 'left', cursor: 'pointer', font: 'inherit', color: text,
              background: fill(), borderRadius: `${radius}px`, padding: cards ? '18px 10px 14px' : '14px 16px',
              display: 'flex', flexDirection: cards ? 'column' : 'row', alignItems: 'center',
              justifyContent: 'space-between', gap: '4px',
            })
            opt.setAttribute('role', 'radio')
            const name = el('span', { fontWeight: '600', fontSize: '15px' }, pkg.productName)
            const price = el('span', { fontSize: '14px', color: muted },
              pkg.period ? `${pkg.price} / ${pkg.period}` : pkg.price)
            opt.append(name, price)
            const badgeText = b.badges?.[pkg.id]
            if (badgeText) {
              opt.appendChild(el('span', {
                position: 'absolute', top: '-9px', right: cards ? '50%' : '12px', transform: cards ? 'translateX(50%)' : '',
                background: accent, color: accentText, fontSize: '11px', fontWeight: '700', padding: '2px 8px',
                borderRadius: '999px', whiteSpace: 'nowrap',
              }, badgeText))
            }
            const paint = () => {
              const on = pkg.id === selected
              opt.style.border = `2px solid ${on ? accent : 'transparent'}`
              opt.setAttribute('aria-checked', String(on))
            }
            paint()
            redraw.push(paint)
            opt.onclick = () => select(pkg.id)
            wrap.appendChild(opt)
          }
          return mark(b, wrap)
        }
        case 'cta': {
          const box = el('div', { display: 'flex', flexDirection: 'column', gap: '8px', alignItems: 'stretch', marginTop: '4px' })
          const btn = el('button', {
            background: accent, color: accentText, border: 'none', borderRadius: `${Math.max(radius, 10)}px`,
            padding: '16px', fontSize: '17px', cursor: 'pointer', font: 'inherit',
          })
          btn.style.fontWeight = '700'
          bindText(btn, b.text ?? 'Continue')
          btn.onclick = async () => {
            const pkg = current()
            if (!pkg || !ctx.onPurchase) return
            btn.disabled = true
            btn.style.opacity = '0.7'
            try {
              await ctx.onPurchase(pkg)
            } finally {
              btn.disabled = false
              btn.style.opacity = '1'
            }
          }
          box.appendChild(btn)
          if (b.subtext) {
            const fine = el('p', { margin: '0', fontSize: '12px', color: muted, textAlign: 'center', lineHeight: '1.4' })
            bindText(fine, b.subtext)
            box.appendChild(fine)
          }
          return mark(b, box)
        }
        case 'testimonial': {
          const variant = paywallVariant(b)
          const stars = (center: boolean) => el('div', { color: accent, letterSpacing: '2px', marginBottom: '6px', textAlign: center ? 'center' : 'left' }, '★'.repeat(Math.min(5, b.rating ?? 0)))
          if (variant === 'quote') {
            const fig = el('figure', { margin: '0', display: 'flex', flexDirection: 'column', alignItems: 'center', textAlign: 'center' })
            if (b.rating) fig.appendChild(stars(true))
            fig.appendChild(el('span', { fontFamily: FONTS.serif, fontSize: '48px', lineHeight: '0.9', color: accent, height: '30px' }, '“'))
            const q = el('blockquote', { margin: '0', fontSize: '18px', lineHeight: '1.45' }); bindText(q, b.text ?? ''); fig.appendChild(q)
            if (b.author) fig.appendChild(el('figcaption', { marginTop: '10px', fontSize: '13px', color: muted }, `— ${b.author}`))
            return mark(b, fig)
          }
          if (variant === 'bubble') {
            const fig = el('figure', { margin: '0', display: 'flex', flexDirection: 'column', gap: '10px' })
            const bubble = el('div', { background: fill(), borderRadius: `${radius}px ${radius}px ${radius}px 4px`, padding: '16px' })
            if (b.rating) bubble.appendChild(stars(false))
            const q = el('blockquote', { margin: '0', fontSize: '15px', lineHeight: '1.5' }); bindText(q, b.text ?? ''); bubble.appendChild(q)
            fig.appendChild(bubble)
            if (b.author) {
              const who = el('figcaption', { display: 'flex', alignItems: 'center', gap: '8px', fontSize: '13px', color: muted })
              who.append(el('span', { width: '28px', height: '28px', borderRadius: '50%', background: accent, color: accentText, display: 'grid', placeItems: 'center', fontWeight: '700', fontSize: '13px' }, [...b.author.trim()][0]?.toUpperCase() ?? ''), el('span', {}, b.author))
              fig.appendChild(who)
            }
            return mark(b, fig)
          }
          const card = el('figure', { margin: '0', background: fill(), borderRadius: `${radius}px`, padding: '16px' })
          if (b.rating) card.appendChild(stars(false))
          const q = el('blockquote', { margin: '0', fontSize: '15px', lineHeight: '1.5' })
          bindText(q, b.text ?? '')
          card.appendChild(q)
          if (b.author) card.appendChild(el('figcaption', { marginTop: '8px', fontSize: '13px', color: muted }, `— ${b.author}`))
          return mark(b, card)
        }
        case 'footer': {
          const row = el('div', { display: 'flex', justifyContent: 'center', gap: '16px', flexWrap: 'wrap', fontSize: '12px', marginTop: '4px' })
          const link = (label: string, onClick?: () => void, href?: string | null) => {
            if (href) {
              const a = el('a', { color: muted, textDecoration: 'underline' }, label)
              a.href = href
              a.target = '_blank'
              a.rel = 'noopener noreferrer'
              row.appendChild(a)
            } else if (onClick) {
              const btn = el('button', { background: 'none', border: 'none', color: muted, textDecoration: 'underline', cursor: 'pointer', font: 'inherit', padding: '0' }, label)
              btn.onclick = onClick
              row.appendChild(btn)
            }
          }
          if (b.restore) link('Restore purchases', () => void ctx.onRestore?.())
          link('Terms', undefined, safeURL(b.terms_url))
          link('Privacy', undefined, safeURL(b.privacy_url))
          return mark(b, row)
        }
      }
      return null
    }

    // A bottom sheet hugs the foot of the screen, after everything else.
    const blocks = [...screen.blocks.filter((b) => b.type !== 'sheet'), ...screen.blocks.filter((b) => b.type === 'sheet')]
    const first = root.childNodes.length
    children(blocks, screen.align ?? 'left', root)
    // Above the effect layer; in one after another when the screen opens.
    const entrance = ctx.animate === false ? '' : screen.entrance
    Array.from(root.childNodes).slice(first).forEach((n, i) => {
      const node = n as HTMLElement
      if (node.style.position !== 'absolute') { if (!node.style.position) node.style.position = 'relative'; node.style.zIndex = '1' }
      if (entrance === 'rise' || entrance === 'fade' || entrance === 'zoom') {
        node.style.animation = `cpw-in-${entrance} .55s cubic-bezier(.2,.8,.2,1) ${(i * 0.07).toFixed(2)}s both`
        node.setAttribute('data-cpw-motion', '')
      }
    })
  }

  // A photo or looping video behind the screen, under its overlay colour.
  function mediaLayer(screen: PaywallScreen): HTMLElement | null {
    const img = safeURL(screen.background_image), vid = safeURL(screen.background_video)
    const overlay = screen.background_overlay && HEX.test(screen.background_overlay) ? screen.background_overlay : ''
    if (!img && !vid && !overlay) return null
    const layer = el('div', { position: 'absolute', inset: '0', overflow: 'hidden', pointerEvents: 'none', zIndex: '0' })
    layer.setAttribute('aria-hidden', 'true')
    layer.setAttribute('data-media', '')
    if (vid) {
      const v = el('video', { position: 'absolute', inset: '0', width: '100%', height: '100%', objectFit: 'cover' })
      v.muted = true; v.loop = true; v.autoplay = true; v.playsInline = true
      v.setAttribute('muted', ''); v.setAttribute('playsinline', '')
      if (img) v.poster = img
      v.src = vid
      layer.appendChild(v)
    } else if (img) {
      Object.assign(layer.style, { backgroundImage: `url("${img}")`, backgroundSize: 'cover', backgroundPosition: 'center' })
    }
    if (overlay) layer.appendChild(el('div', { position: 'absolute', inset: '0', background: overlay }))
    return layer
  }

  // The screen's animated background, drawn from the accent.
  function effectLayer(effect: string): HTMLElement {
    const layer = el('div', { position: 'absolute', inset: '0', overflow: 'hidden', pointerEvents: 'none', zIndex: '0' })
    layer.setAttribute('aria-hidden', 'true')
    layer.setAttribute('data-effect', effect)
    switch (effect) {
      case 'glow':
        layer.appendChild(moving(el('div', {
          position: 'absolute', left: '-35%', right: '-35%', top: '-60%', height: '120%',
          background: `radial-gradient(closest-side, ${tint(accent, 0.38)}, transparent)`,
        }), 'cpw-breathe 6s ease-in-out infinite alternate'))
        break
      case 'aurora':
        ;([[15, 8, 0, 'cpw-drift1 14s'], [85, 22, 40, 'cpw-drift2 17s'], [35, 48, -35, 'cpw-drift3 12s']] as Array<[number, number, number, string]>).forEach(([x, y, hue, anim]) => {
          // Centred on (x, y); the inner blob drifts.
          const spot = el('div', { position: 'absolute', left: `${x}%`, top: `${y}%`, width: '75%', aspectRatio: '1', transform: 'translate(-50%, -50%)' } as Partial<CSSStyleDeclaration>)
          spot.appendChild(moving(el('div', {
            width: '100%', height: '100%', borderRadius: '50%', background: tint(accent, 0.4), filter: `blur(60px) hue-rotate(${hue}deg)`,
          }), `${anim} ease-in-out infinite alternate`))
          layer.appendChild(spot)
        })
        break
      case 'orbs':
        for (const [x, y, d, o, dur, delay] of ORBS) {
          layer.appendChild(moving(el('span', {
            position: 'absolute', left: `${x}%`, top: `${y}%`, width: `${d}px`, height: `${d}px`, borderRadius: '50%', background: accent, opacity: String(o),
          }), `cpw-float ${dur}s ease-in-out ${delay}s infinite alternate`))
        }
        break
      case 'grid':
        Object.assign(layer.style, {
          backgroundImage: `linear-gradient(${tint(text, 0.07)} 1px, transparent 1px), linear-gradient(90deg, ${tint(text, 0.07)} 1px, transparent 1px)`,
          backgroundSize: '24px 24px',
          maskImage: 'linear-gradient(180deg, #000, transparent 60%)', webkitMaskImage: 'linear-gradient(180deg, #000, transparent 60%)',
        } as Partial<CSSStyleDeclaration>)
        break
      case 'rays': {
        const wrap = el('div', {
          position: 'absolute', left: '50%', top: '-10%', width: '220%', aspectRatio: '1', transform: 'translate(-50%, -50%)',
          maskImage: 'radial-gradient(closest-side, #000, transparent)', webkitMaskImage: 'radial-gradient(closest-side, #000, transparent)',
        } as Partial<CSSStyleDeclaration>)
        wrap.appendChild(moving(el('div', {
          position: 'absolute', inset: '0',
          background: `repeating-conic-gradient(${tint(accent, 0.12)} 0deg 11.25deg, transparent 11.25deg 22.5deg)`,
        }), 'cpw-spin 60s linear infinite'))
        layer.appendChild(wrap)
        break
      }
    }
    return layer
  }

  draw()
  return root
}

/** A laurel branch: leaves along an arc, mirrored for the right side. */
function laurel(stroke: string, right: boolean): SVGSVGElement {
  const NS = 'http://www.w3.org/2000/svg'
  const svg = document.createElementNS(NS, 'svg')
  svg.setAttribute('viewBox', '0 0 24 48')
  svg.setAttribute('width', '20')
  svg.setAttribute('height', '40')
  svg.setAttribute('aria-hidden', 'true')
  svg.style.opacity = '0.75'
  if (right) svg.style.transform = 'scaleX(-1)'
  const stem = document.createElementNS(NS, 'path')
  stem.setAttribute('d', 'M18 45C6 38 4 18 12 3')
  stem.setAttribute('fill', 'none')
  stem.setAttribute('stroke', stroke)
  stem.setAttribute('stroke-width', '1.6')
  stem.setAttribute('stroke-linecap', 'round')
  svg.appendChild(stem)
  const leaves: Array<[number, number, number]> = [[16, 42, 60], [10, 34, 40], [7, 25, 20], [8, 16, 0], [11, 8, -20]]
  for (const [x, y, a] of leaves) {
    for (const side of [-1, 1]) {
      const leaf = document.createElementNS(NS, 'ellipse')
      leaf.setAttribute('cx', String(x + side * 3.2))
      leaf.setAttribute('cy', String(y))
      leaf.setAttribute('rx', '3.4')
      leaf.setAttribute('ry', '1.6')
      leaf.setAttribute('fill', stroke)
      leaf.setAttribute('transform', `rotate(${a + side * 35} ${x + side * 3.2} ${y})`)
      svg.appendChild(leaf)
    }
  }
  return svg
}

/** Formats a smallest-unit amount for display ("$4.99"), or "" when unknown. */
export function formatAmount(amount: number, currency: string, locale?: string): string {
  if (!currency) return ''
  try {
    const f = new Intl.NumberFormat(locale, { style: 'currency', currency })
    const digits = f.resolvedOptions().maximumFractionDigits ?? 2
    return f.format(amount / 10 ** digits)
  } catch {
    return `${(amount / 100).toFixed(2)} ${currency}`
  }
}

/** Words for a billing period in months. */
export function periodWord(months: number): string {
  switch (months) {
    case 0: return ''
    case 1: return 'month'
    case 12: return 'year'
    case 3: return '3 months'
    case 6: return '6 months'
    default: return `${months} months`
  }
}
