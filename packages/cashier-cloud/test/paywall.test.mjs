import assert from 'node:assert/strict'
import test from 'node:test'
import { Window } from 'happy-dom'

const win = new Window()
globalThis.document = win.document
const { renderPaywall, resolvePaywallText, formatAmount, periodWord, paywallScreens, flattenBlocks, countdownRemaining, switchPackage, paywallVariant, paywallSavings, PAYWALL_VARIANTS } = await import('../dist/index.js')
const { readFileSync } = await import('node:fs')
const fixtures = JSON.parse(readFileSync(new URL('./paywalls.json', import.meta.url), 'utf8'))

const packages = [
  { id: '$monthly', productName: 'Pro Monthly', price: '$4.99', period: 'month', pricePerMonth: '', trial: '' },
  { id: '$annual', productName: 'Pro Annual', price: '$39.99', period: 'year', pricePerMonth: '$3.33', trial: '7-day' },
]

const doc = {
  version: 1,
  theme: { background: '#FFFFFF', surface: '#F4F4F6', text: '#111114', muted: '#6B6B76', accent: '#F04E28', accent_text: '#FFFFFF', radius: 16, font: 'system' },
  blocks: [
    { id: 'title', type: 'title', text: 'Unlock {{app_name}} Pro' },
    { id: 'packages', type: 'packages', layout: 'list', highlight: '$annual', badges: { $annual: 'Best value' } },
    { id: 'cta', type: 'cta', text: 'Start my {{trial}} trial', subtext: 'then {{price}}/{{period}}' },
    { id: 'footer', type: 'footer', restore: true, terms_url: 'https://example.com/terms' },
  ],
}

test('variables resolve from the selected package', () => {
  assert.equal(resolvePaywallText('{{price}} / {{period}}', 'App', packages[1]), '$39.99 / year')
  assert.equal(resolvePaywallText('Start my {{trial}} trial', 'App', packages[0]), 'Start my trial')
  assert.equal(resolvePaywallText('{{nope}}x', 'App'), 'x')
  assert.equal(resolvePaywallText('After {{trial}}: billed {{price}}.', 'App', packages[0]), 'After: billed $4.99.')
})

test('renders, preselects the highlight, and follows the selection', async () => {
  let bought = null
  const node = renderPaywall(doc, { appName: 'Acme', packages, onPurchase: (p) => { bought = p.id } })
  assert.equal(node.querySelector('h2').textContent, 'Unlock Acme Pro')
  const cta = [...node.querySelectorAll('button')].find((b) => b.textContent.startsWith('Start'))
  assert.equal(cta.textContent, 'Start my 7-day trial')
  assert.ok(node.textContent.includes('then $39.99/year'))
  const radios = node.querySelectorAll('[role=radio]')
  assert.equal(radios[1].getAttribute('aria-checked'), 'true')
  radios[0].click()
  assert.equal(radios[0].getAttribute('aria-checked'), 'true')
  assert.ok(node.textContent.includes('then $4.99/month'), 'price text follows the selection')
  await cta.onclick()
  assert.equal(bought, '$monthly')
  assert.ok(node.textContent.includes('Best value'))
  assert.ok(node.textContent.includes('Restore purchases'))
  assert.deepEqual([...node.querySelectorAll('[data-block-id]')].map((n) => n.getAttribute('data-block-id')), ['title', 'packages', 'cta', 'footer'])
})

test('document text and URLs cannot inject markup', () => {
  const evil = structuredClone(doc)
  evil.blocks[0].text = '<img src=x onerror=alert(1)>'
  evil.blocks.push({ id: 'img', type: 'image', url: 'javascript:alert(1)' })
  evil.blocks[3].terms_url = 'javascript:alert(1)'
  evil.theme.accent = 'red;background:url(//evil)'
  const node = renderPaywall(evil, { appName: 'A', packages })
  assert.equal(node.querySelector('img'), null, 'no image from a non-https URL, no parsed markup')
  assert.equal(node.querySelector('h2').textContent, '<img src=x onerror=alert(1)>')
  assert.equal(node.querySelector('a'), null, 'no javascript: link')
  assert.ok(!node.outerHTML.includes('evil'), 'a bad colour falls back')
})

test('a newer schema version is not guessed at', () => {
  const node = renderPaywall({ ...doc, version: 3 }, { appName: 'A', packages })
  assert.match(node.textContent, /Update the app/)
})

test('money and periods', () => {
  assert.equal(formatAmount(499, 'USD', 'en-US'), '$4.99')
  assert.equal(formatAmount(49900, 'INR', 'en-IN'), '₹499.00')
  assert.equal(formatAmount(500, 'JPY', 'en-US'), '¥500')
  assert.equal(periodWord(12), 'year')
  assert.equal(periodWord(0), '')
})

const flow = {
  version: 2,
  theme: doc.theme,
  initial: 'welcome',
  screens: [
    { id: 'welcome', name: 'Welcome', justify: 'center', align: 'center', blocks: [
      { id: 'hi', type: 'icon', icon: 'sparkles', size: 'xl' },
      { id: 'w-title', type: 'title', text: 'Welcome to {{app_name}}' },
      { id: 'go', type: 'button', text: 'Next', action: 'next' },
      { id: 'skip', type: 'button', text: 'Skip to plans', action: 'screen', target: 'plans', style: 'link' },
    ] },
    { id: 'consent', name: 'Consent', background: '#101820', blocks: [
      { id: 'c-title', type: 'title', text: 'Tips by email?' },
      { id: 'yes', type: 'button', text: 'Yes', action: 'next' },
      { id: 'evil', type: 'button', text: 'Evil', action: 'url', url: 'javascript:alert(1)', style: 'secondary' },
    ] },
    { id: 'plans', name: 'Paywall', blocks: doc.blocks },
  ],
}

test('a version-1 document reads as one screen', () => {
  assert.deepEqual(paywallScreens(doc).map((s) => s.id), ['paywall'])
  assert.deepEqual(paywallScreens(flow).map((s) => s.id), ['welcome', 'consent', 'plans'])
})

test('a flow opens on its initial screen and buttons move through it', () => {
  const seen = []
  const node = renderPaywall(flow, { appName: 'Acme', packages, onNavigate: (id) => seen.push(id) })
  assert.equal(node.getAttribute('data-screen-id'), 'welcome')
  assert.equal(node.querySelector('h2').textContent, 'Welcome to Acme')
  assert.equal(node.style.justifyContent, 'center')
  assert.ok(node.querySelector('[data-block-type=icon] svg'), 'the hero icon is drawn')

  node.querySelector('[data-block-id=go]').click()
  assert.equal(node.getAttribute('data-screen-id'), 'consent')
  assert.match(node.style.background, /16, 24, 32|#101820/i, 'the screen background overrides the theme')

  // A link button with a script URL goes nowhere.
  let opened = null
  globalThis.open = (u) => { opened = u }
  node.querySelector('[data-block-id=evil]').click()
  assert.equal(opened, null)
  assert.equal(node.getAttribute('data-screen-id'), 'consent')

  node.querySelector('[data-block-id=yes]').click()
  assert.equal(node.getAttribute('data-screen-id'), 'plans')
  assert.ok(node.querySelector('[data-block-id=packages]'))
  assert.deepEqual(seen, ['consent', 'plans'])
})

test('the chosen plan survives moving between screens', () => {
  const back = structuredClone(flow)
  back.screens[2].blocks = [...flow.screens[2].blocks, { id: 'back', type: 'button', text: 'Back', action: 'screen', target: 'welcome', style: 'link' }]
  const node = renderPaywall(back, { appName: 'A', packages })
  node.querySelector('[data-block-id=skip]').click()
  node.querySelectorAll('[role=radio]')[0].click()
  node.querySelector('[data-block-id=back]').click()
  node.querySelector('[data-block-id=skip]').click()
  assert.equal(node.querySelectorAll('[role=radio]')[0].getAttribute('aria-checked'), 'true')
})

test('an editor can draw one screen without navigating', () => {
  const node = renderPaywall(flow, { appName: 'A', packages, screen: 'consent' })
  assert.equal(node.getAttribute('data-screen-id'), 'consent')
  node.querySelector('[data-block-id=yes]').click()
  assert.equal(node.getAttribute('data-screen-id'), 'consent', 'a static screen does not navigate')
})

// ── The server's own documents (test/paywalls.json, written by the Go
// server's TestWriteRendererFixtures) ─────────────────────────────────────

test('every screen of every server document renders every block', () => {
  // Every package the templates' plan cards use.
  const all = ['$weekly', '$monthly', '$six_month', '$annual', '$lifetime', '$premium_monthly', '$premium_annual']
    .map((id) => ({ id, productName: id, price: '$1', period: 'month', pricePerMonth: '', trial: '', savings: '10%' }))
  for (const [name, d] of Object.entries(fixtures)) {
    for (const sc of paywallScreens(d)) {
      const node = renderPaywall(d, { appName: 'Acme', packages: all, screen: sc.id })
      for (const b of flattenBlocks(sc.blocks)) {
        // A tab that is not open and pages are drawn lazily/elsewhere; everything else is on screen.
        const hiddenTab = flattenBlocks(sc.blocks).some((t) => t.type === 'tabs' && t.children.slice(1).some((c) => flattenBlocks([c]).includes(b)))
        if (hiddenTab) continue
        assert.ok(node.querySelector(`[data-block-id="${b.id}"]`), `${name}/${sc.id}: ${b.id} (${b.type}) not drawn`)
      }
    }
  }
})

test('the showcase uses every block type', () => {
  const types = new Set(paywallScreens(fixtures.showcase).flatMap((s) => flattenBlocks(s.blocks)).map((b) => b.type))
  for (const t of ['title', 'text', 'image', 'video', 'icon', 'stack', 'footer', 'header', 'custom', 'spacer', 'packages', 'cta', 'sheet',
    'tabs', 'switch', 'button', 'carousel', 'countdown', 'timeline', 'social_proof', 'testimonial', 'features', 'award']) {
    assert.ok(types.has(t), t)
  }
})

test('tabs switch their panel', () => {
  const node = renderPaywall(fixtures.showcase, { appName: 'Acme', packages, screen: 'paywall' })
  const tabs = node.querySelectorAll('[role=tab]')
  assert.deepEqual([...tabs].map((t) => t.textContent), ['Features', 'How it works'])
  assert.ok(node.querySelector('[data-block-type=features]'))
  assert.ok(!node.querySelector('[data-block-type=timeline]'))
  tabs[1].click()
  assert.ok(node.querySelector('[data-block-type=timeline]'))
  assert.equal(node.querySelectorAll('[role=tab]')[1].getAttribute('aria-selected'), 'true')
})

test('a switch picks between its two packages and follows the plan list', () => {
  assert.equal(switchPackage({ package_on: '$annual', package_off: '$monthly' }, true), '$annual')
  assert.equal(switchPackage({ package_on: '$annual', package_off: '$monthly' }, false), '$monthly')
  const node = renderPaywall(fixtures.showcase, { appName: 'Acme', packages, screen: 'paywall', selected: '$monthly' })
  const sw = node.querySelector('[role=switch]')
  const radios = () => [...node.querySelectorAll('[role=radio]')].map((r) => r.getAttribute('aria-checked'))
  assert.equal(sw.getAttribute('aria-checked'), 'false')
  sw.click()
  assert.equal(sw.getAttribute('aria-checked'), 'true')
  assert.deepEqual(radios(), ['false', 'true'])
  node.querySelectorAll('[role=radio]')[0].click()
  assert.equal(sw.getAttribute('aria-checked'), 'false')
})

test('countdowns count down to a moment or for minutes, never below zero', () => {
  const t0 = Date.parse('2026-01-01T00:00:00Z')
  assert.equal(countdownRemaining({ minutes: 30 }, t0, t0 + 60_000), 29 * 60_000)
  assert.equal(countdownRemaining({ until: '2026-01-01T01:00:00Z' }, 0, t0), 3_600_000)
  assert.equal(countdownRemaining({ until: '2025-01-01T00:00:00Z' }, 0, t0), 0)
  assert.equal(countdownRemaining({ until: 'soon' }, 0, t0), 0)
  const node = renderPaywall(fixtures.showcase, { appName: 'Acme', packages, screen: 'paywall' })
  const digits = node.querySelector('[data-block-type=countdown]').textContent
  assert.match(digits, /00:(29|30):\d\d/)
})

test('the bottom sheet holds the plans, sits last and bleeds to the edges', () => {
  const node = renderPaywall(fixtures.showcase, { appName: 'Acme', packages, screen: 'paywall' })
  const sheet = node.querySelector('[data-block-type=sheet]')
  assert.equal(node.lastElementChild, sheet)
  assert.ok(sheet.querySelector('[data-block-type=packages]') && sheet.querySelector('[data-block-type=cta]'))
  assert.equal(sheet.style.marginTop, 'auto')
  assert.equal(sheet.style.marginLeft, '-22px')
})

test('back goes to the previous screen', () => {
  const node = renderPaywall(fixtures.showcase, { appName: 'Acme', packages })
  assert.equal(node.getAttribute('data-screen-id'), 'welcome')
  ;[...node.querySelectorAll('button')].find((b) => b.textContent === 'Continue').click()
  assert.equal(node.getAttribute('data-screen-id'), 'paywall')
  node.querySelector('[aria-label=Back]').click()
  assert.equal(node.getAttribute('data-screen-id'), 'welcome')
})

test('media and avatars only load from https', () => {
  const d = structuredClone(fixtures.showcase)
  const welcome = d.screens[0].blocks
  welcome.find((b) => b.type === 'video').url = 'javascript:alert(1)'
  welcome.find((b) => b.type === 'social_proof').images = ['http://x/a.png', 'https://x/b.png']
  const node = renderPaywall(d, { appName: 'Acme', packages, screen: 'welcome' })
  assert.equal(node.querySelector('video'), null)
  const faces = [...node.querySelectorAll('[data-block-type=social_proof] img')].map((i) => i.src)
  assert.deepEqual(faces, ['https://x/b.png'])
})

// ── Design variants ──────────────────────────────────────────────────

test('the variants document has every design of every block, and each draws', () => {
  const d = fixtures.variants
  const blocks = paywallScreens(d).flatMap((s) => flattenBlocks(s.blocks))
  for (const [type, list] of Object.entries(PAYWALL_VARIANTS)) {
    for (const v of list) assert.ok(blocks.some((b) => b.type === type && b.variant === v), `${type}/${v}`)
  }
  // Designs look different: no two variants of a type draw the same markup.
  for (const sc of paywallScreens(d).slice(2)) {
    const node = renderPaywall(d, { appName: 'Acme', packages, screen: sc.id })
    const seen = new Map()
    for (const b of sc.blocks) {
      const html = node.querySelector(`[data-block-id="${b.id}"]`).outerHTML.replace(/data-block-id="[^"]+"/g, '').replace(/\d\d:\d\d:\d\d|\b\d\d\b/g, '')
      assert.ok(!seen.has(b.type + html), `${b.type}/${b.variant} draws like ${seen.get(b.type + html)}`)
      seen.set(b.type + html, b.variant)
    }
  }
})

test('an unknown or missing variant draws the default design', () => {
  assert.equal(paywallVariant({ type: 'tabs', variant: 'sparkly' }), 'pills')
  assert.equal(paywallVariant({ type: 'tabs' }), 'pills')
  assert.equal(paywallVariant({ type: 'features', variant: 'grid' }), 'grid')
  assert.equal(paywallVariant({ type: 'spacer', variant: 'grid' }), '')
  const d = structuredClone(fixtures.showcase)
  const tabs = d.screens[1].blocks.find((b) => b.type === 'tabs')
  const plain = renderPaywall(d, { appName: 'A', packages, screen: 'paywall' }).querySelector('[data-block-type=tabs]').outerHTML
  tabs.variant = 'sparkly'
  assert.equal(renderPaywall(d, { appName: 'A', packages, screen: 'paywall' }).querySelector('[data-block-type=tabs]').outerHTML, plain)
})

test('interactive variants still work', () => {
  const d = structuredClone(fixtures.showcase)
  const blocks = d.screens[1].blocks
  blocks.find((b) => b.type === 'tabs').variant = 'underline'
  blocks.find((b) => b.type === 'sheet').children.find((b) => b.type === 'switch').variant = 'checkbox'
  const node = renderPaywall(d, { appName: 'A', packages, screen: 'paywall', selected: '$monthly' })
  node.querySelectorAll('[role=tab]')[1].click()
  assert.ok(node.querySelector('[data-block-type=timeline]'))
  const sw = node.querySelector('[role=switch]')
  sw.click()
  assert.equal(sw.getAttribute('aria-checked'), 'true')
  assert.equal(node.querySelectorAll('[role=radio]')[1].getAttribute('aria-checked'), 'true')
})

test('story bars sit above the carousel pages; dots below', () => {
  const d = structuredClone(fixtures.showcase)
  const car = d.screens[0].blocks.find((b) => b.type === 'carousel')
  const first = (v) => { car.variant = v; return renderPaywall(d, { appName: 'A', packages, screen: 'welcome' }).querySelector('[data-block-type=carousel]').firstElementChild }
  assert.ok(first('bars').querySelector('[aria-label="Page 1"]'))
  assert.ok(!first('dots').querySelector('[aria-label="Page 1"]'))
})

// ── Motion and richer visuals ────────────────────────────────────────

test('screens draw their effect behind the blocks and animate blocks in', () => {
  const d = fixtures.variants
  const effects = new Set(), entrances = new Set()
  for (const sc of paywallScreens(d).slice(2)) {
    effects.add(sc.effect); entrances.add(sc.entrance)
    const node = renderPaywall(d, { appName: 'A', packages, screen: sc.id })
    const layer = node.querySelector('[data-effect]')
    assert.equal(layer.getAttribute('data-effect'), sc.effect)
    assert.equal(layer.style.pointerEvents, 'none')
    const blocks = [...node.children].filter((n) => n.hasAttribute('data-block-id'))
    assert.ok(blocks.every((n) => n.style.zIndex === '1'), 'blocks sit above the effect')
    assert.match(blocks[0].style.animation, new RegExp(`cpw-in-${sc.entrance}`))
    assert.match(blocks[1].style.animation, /0\.07s/)
    // An editor turns entrances off.
    const still = renderPaywall(d, { appName: 'A', packages, screen: sc.id, animate: false })
    assert.ok(![...still.children].some((n) => /cpw-in-/.test(n.style.animation)))
  }
  assert.deepEqual([...effects].sort(), ['aurora', 'glow', 'grid', 'orbs', 'rays'])
  assert.deepEqual([...entrances].sort(), ['fade', 'rise', 'zoom'])
  assert.ok(document.getElementById('cashier-paywall-motion'), 'motion stylesheet added once')
  assert.equal(document.querySelectorAll('#cashier-paywall-motion').length, 1)
})

test('emoji icons and option buttons', () => {
  const d = structuredClone(fixtures.showcase)
  d.screens[0].blocks.unshift(
    { id: 'wave', type: 'icon', emoji: '👋', variant: 'gradient' },
    { id: 'opt', type: 'button', variant: 'option', emoji: '🚀', text: 'Grow faster', action: 'next' },
  )
  const node = renderPaywall(d, { appName: 'A', packages })
  assert.equal(node.querySelector('[data-block-id=wave]').textContent, '👋')
  const opt = node.querySelector('[data-block-id=opt]')
  assert.ok(opt.textContent.includes('🚀') && opt.textContent.includes('Grow faster'))
  opt.click()
  assert.equal(node.getAttribute('data-screen-id'), 'paywall')
})

test('a screen with its own theme is drawn in it; the others use the flow theme', () => {
  const d = fixtures.variants
  const own = renderPaywall(d, { appName: 'A', packages, screen: 'variants-1' })
  assert.match(own.style.background, /#0B0B1F|rgb\(11, 11, 31\)/i)
  assert.match(own.style.color, /#FFFFFF|rgb\(255, 255, 255\)/i)
  const flow = renderPaywall(d, { appName: 'A', packages, screen: 'variants-2' })
  assert.notEqual(flow.style.background, own.style.background)
  // Moving through a flow switches looks screen by screen.
  const steps = { ...fixtures.steps_1, initial: fixtures.steps_1.screens[0].id }
  const node = renderPaywall(steps, { appName: 'A', packages })
  const first = node.style.color
  ;[...node.querySelectorAll('button')].find((b) => b.getAttribute('data-block-type') === 'button').click()
  assert.notEqual(node.getAttribute('data-screen-id'), steps.initial)
  assert.notEqual(node.style.color, first)
})

test('a block look styles its box and type', () => {
  const node = renderPaywall(fixtures.variants, { appName: 'A', packages, screen: 'looks' })
  const [title, pill, card, layers, fill, fit] = [...node.children].filter((n) => n.hasAttribute('data-block-id'))
  assert.equal(title.style.fontStyle, 'italic')
  assert.equal(title.style.fontWeight, '800')
  assert.equal(title.style.fontSize, '34px')
  assert.equal(title.style.textAlign, 'right')
  assert.equal(pill.style.width, 'fit-content')
  assert.equal(card.style.width, '300px')
  assert.equal(card.style.borderRadius, '0px 0px 12px 12px')
  assert.match(card.style.border, /2px solid/)
  assert.match(card.style.boxShadow, /8px 18px/)
  assert.match(card.style.background, /linear-gradient/)
  const row = card.querySelector('[data-block-type=stack]')
  assert.equal(row.style.alignItems, 'center')
  assert.equal(layers.style.display, 'grid')
  assert.equal(layers.style.height, '120px')
  assert.ok([...layers.children].every((c) => c.style.gridArea.startsWith('1')))
  assert.equal(layers.querySelector('[data-block-type=icon]').style.opacity, '0.3')
  assert.equal(fill.style.alignSelf, 'stretch')
  assert.equal(fit.style.width, 'fit-content')
  // A fixed icon size and glyph colour.
  const star = card.querySelector('[data-block-type=icon]')
  assert.equal(star.style.width, '20px')
  assert.equal(star.querySelector('svg').getAttribute('stroke'), '#FFA901')
})

test('a look cannot smuggle in an unsafe image or colour', () => {
  const d = structuredClone(fixtures.showcase)
  d.screens[0].blocks[3].look = { background_image: 'javascript:alert(1)', background: 'red;x:y', color: 'url(x)', corners: [1, 2] }
  const node = renderPaywall(d, { appName: 'A', packages, screen: 'welcome' })
  const n = node.querySelector(`[data-block-id="${d.screens[0].blocks[3].id}"]`)
  assert.ok(!/javascript|red|url\(x\)/.test(n.getAttribute('style') ?? ''))
})

// ── Plan cards, selected looks, media, savings ───────────────────────

const tiered = [
  { id: '$monthly', productName: 'Monthly', price: '$9.99', period: 'month', pricePerMonth: '', trial: '', savings: '' },
  { id: '$annual', productName: 'Yearly', price: '$69.99', period: 'year', pricePerMonth: '$5.83', trial: '', savings: '42%' },
  { id: '$premium_monthly', productName: 'Premium monthly', price: '$19.99', period: 'month', pricePerMonth: '', trial: '', savings: '' },
  { id: '$premium_annual', productName: 'Premium yearly', price: '$149.99', period: 'year', pricePerMonth: '$12.50', trial: '', savings: '37%' },
]

test('savings come from real prices', () => {
  const s = paywallSavings([
    { id: 'w', amount: 399, months: 0.25 }, { id: 'm', amount: 999, months: 1 }, { id: 'y', amount: 6999, months: 12 },
    { id: 'h', amount: 5994, months: 6 }, { id: 'life', amount: 11999, months: 0 }, { id: 'pm', amount: 1999, months: 1 },
  ])
  // Monthly is the reference (weekly never is); pricier plans and lifetime save nothing.
  assert.deepEqual(s, { w: '', m: '', y: '42%', h: '', life: '', pm: '' }) // 6 months at the monthly rate saves nothing
  assert.equal(resolvePaywallText('Save {{savings}}', 'A', tiered[1]), 'Save 42%')
})

test('plan cards select their package, describe it, and wear their selected look', () => {
  const node = renderPaywall(fixtures.plans, { appName: 'A', packages: tiered })
  const cards = [...node.querySelectorAll('[role=radio]')]
  assert.equal(cards.length, 2, 'only the open tab’s cards are drawn')
  assert.equal(cards[0].getAttribute('aria-checked'), 'true', 'the first card starts selected')
  assert.ok(cards[1].textContent.includes('$69.99 / year'), 'a card describes its own package')
  const border0 = cards[0].style.border
  assert.match(border0, /2px solid/)
  cards[1].click()
  assert.equal(cards[1].getAttribute('aria-checked'), 'true')
  assert.match(cards[1].style.border, /2px solid/)
  assert.ok(!/2px solid/.test(cards[0].style.border), 'the selected look comes off')
  const price = cards[1].querySelector('[data-block-type=text]')
  assert.equal(price.style.fontWeight, '700', 'look_selected reaches inside the card')
  assert.ok(node.textContent.includes('Save 42%'), 'outside cards, text follows the selection')
})

test('tiers: opening a tab selects its first plan; missing packages hide their card', () => {
  const node = renderPaywall(fixtures.plans, { appName: 'A', packages: tiered })
  node.querySelectorAll('[role=tab]')[1].click()
  const cards = [...node.querySelectorAll('[role=radio]')]
  assert.ok(cards[0].textContent.includes('$19.99'))
  assert.equal(cards[0].getAttribute('aria-checked'), 'true')
  const few = renderPaywall(fixtures.plans, { appName: 'A', packages: tiered.slice(0, 1) })
  assert.equal(few.querySelectorAll('[role=radio]').length, 1)
})

test('screen media and hero images', () => {
  const node = renderPaywall(fixtures.plans, { appName: 'A', packages: tiered, onClose() {} })
  const media = node.querySelector('[data-media]')
  assert.match(media.style.backgroundImage, /example\.com\/bg\.jpg/)
  assert.equal(media.lastChild.style.background.replace(/\s/g, '').toLowerCase().includes('#00000066') || /rgba\(0,0,0,0\.4/.test(media.lastChild.style.background.replace(/\s/g, '')), true)
  const hero = node.querySelector('[data-block-type=image]')
  assert.equal(hero.style.marginLeft, '-22px')
  assert.equal(hero.style.marginTop, '-52px', 'a leading hero sits flush with the top')
  assert.match(hero.lastChild.style.background, /linear-gradient/)
})

test('plan card badges, and savings text that hides when there is nothing to save', () => {
  const d = structuredClone(fixtures.plans)
  const row = d.screens[0].blocks.find((b) => b.type === 'tabs').children[0]
  row.children[0].badge = 'Save {{savings}}'
  row.children[1].badge = 'Save {{savings}}'
  const node = renderPaywall(d, { appName: 'A', packages: tiered })
  const badges = [...node.querySelectorAll('[data-badge]')]
  assert.equal(badges[0].style.display, 'none', 'monthly saves nothing')
  assert.equal(badges[1].textContent, 'Save 42%')
  assert.equal(badges[1].style.position, 'absolute')
  const line = [...node.querySelectorAll('[data-block-type=text]')].find((n) => n.textContent.startsWith('Save') || n.style.display === 'none' && /savings/.test(''))
  const outside = [...node.children].find((n) => n.getAttribute('data-block-type') === 'text')
  assert.equal(outside.style.display, 'none', 'follows the selection: monthly, nothing to save')
  node.querySelectorAll('[role=radio]')[1].click()
  assert.equal(outside.style.display, '')
  assert.equal(outside.textContent, 'Save 42% with a year')
})
