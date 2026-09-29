import assert from 'node:assert/strict'
import test from 'node:test'
import { Window } from 'happy-dom'

const win = new Window()
globalThis.document = win.document
const { renderPaywall, localizePaywall, matchPaywallLocale } = await import('../dist/index.js')
const { readFileSync } = await import('node:fs')
const fixtures = JSON.parse(readFileSync(new URL('./paywalls.json', import.meta.url), 'utf8'))
const doc = fixtures.localized

const block = (d, id) => {
  let found
  const walk = (list) => { for (const b of list ?? []) { if (b.id === id) found = b; walk(b.children) } }
  for (const s of d.screens) walk(s.blocks)
  return found
}

test('matchPaywallLocale: exact, language fallback, default language, none', () => {
  const cases = { es: 'es', 'es-MX': 'es', ES_es: 'es', 'pt-BR': 'pt-BR', 'pt-PT': 'pt-BR', pt: 'pt-BR', 'en-GB': '', fr: '', '': '' }
  for (const [want, got] of Object.entries(cases)) assert.equal(matchPaywallLocale(doc, want), got, want)
  assert.equal(matchPaywallLocale({ default_locale: 'es', locales: ['en'] }, 'en-US'), 'en')
  assert.equal(matchPaywallLocale({ default_locale: 'es', locales: ['es-MX'] }, 'es-AR'), '')
})

test('localizePaywall swaps texts, keeps missing keys, never touches the original', () => {
  const es = localizePaywall(doc, 'es-MX')
  assert.equal(block(es, 't2').text, 'Hazte Pro')
  assert.equal(block(es, 'o1').text, 'Un amigo')
  assert.equal(block(es, 'o2').text, 'Social media')
  assert.equal(block(es, 'f1').items[0].title, 'Sin anuncios')
  assert.equal(block(es, 'f1').items[0].text, 'Ever.')
  assert.equal(block(es, 'p1').badges.annual, 'Mejor precio')
  assert.equal(block(es, 'c1').subtext, 'Cancel anytime')
  assert.equal(es.screens[0].question, '¿Cómo nos conociste?')
  assert.equal(block(doc, 't2').text, 'Go Pro')
  assert.equal(localizePaywall(doc, 'en-US'), doc)
  assert.equal(block(localizePaywall(doc, 'pt'), 't2').text, 'Seja Pro')
})

test('renderPaywall localizes, fills variables in translations, and reports answers', () => {
  const answers = []
  const node = renderPaywall(doc, {
    appName: 'Acme', locale: 'es', closable: false, animate: false,
    packages: [{ id: 'annual', productName: 'Anual', price: '$39.99', period: 'year', pricePerMonth: '$3.33', trial: '' }],
    onAnswer: (a) => answers.push(a),
  })
  document.body.replaceChildren(node)
  assert.match(node.textContent, /¿Cómo nos conociste\?/)
  const btn = [...node.querySelectorAll('button')].find((b) => b.textContent.includes('Un amigo'))
  assert.ok(btn, 'translated option button')
  btn.click()
  assert.deepEqual(answers, [{ screen_id: 'ask', question: '¿Cómo nos conociste?', answer: 'friend' }])
  assert.match(node.textContent, /Hazte Pro/)
  assert.match(node.textContent, /Continuar por \$39\.99/)
})
