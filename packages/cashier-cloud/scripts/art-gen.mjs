// Generates the paywall art scenes (400×260, drawn to cover) as plain data.
// Colours are theme tokens: A accent, AT accent text, T text, B background,
// S surface, M muted, or #RRGGBB; then optional modifiers in order:
// +deg / -deg hue shift, *k lightness multiplier, @a alpha.
let seed = 7
const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647)
const r1 = (n) => Math.round(n * 10) / 10
const lin = (from, to, angle = 180) => ({ type: 'linear', angle, stops: [from, to] })
const W = 400, H = 260

function wave(y, amp, len, phase) {
  let d = `M0 ${H}L0 ${y}`
  for (let x = 0; x <= W; x += 10) d += `L${x} ${r1(y + Math.sin((x / len) * Math.PI * 2 + phase) * amp)}`
  return d + `L${W} ${H}Z`
}
function ridge(base, peaks) {
  let d = `M0 ${H}L0 ${base}`
  for (const [x, y] of peaks) d += `L${x} ${y}`
  return d + `L${W} ${base}L${W} ${H}Z`
}
function pine(x, base, h) {
  const w = h * 0.42
  return `M${x} ${base - h}L${r1(x + w * 0.5)} ${r1(base - h * 0.55)}L${r1(x + w * 0.3)} ${r1(base - h * 0.55)}L${r1(x + w * 0.7)} ${r1(base - h * 0.2)}L${r1(x + w * 0.35)} ${r1(base - h * 0.2)}L${r1(x + w * 0.5)} ${base}L${r1(x - w * 0.5)} ${base}L${r1(x - w * 0.35)} ${r1(base - h * 0.2)}L${r1(x - w * 0.7)} ${r1(base - h * 0.2)}L${r1(x - w * 0.3)} ${r1(base - h * 0.55)}L${r1(x - w * 0.5)} ${r1(base - h * 0.55)}Z`
}
function stars(n, maxY, color = '#FFFFFF') {
  const out = []
  for (let i = 0; i < n; i++) {
    const r = rnd() < 0.15 ? 1.6 : rnd() < 0.5 ? 1 : 0.6
    out.push({ t: 'circle', cx: r1(rnd() * W), cy: r1(rnd() * maxY), r, fill: color + '@' + (0.5 + rnd() * 0.5).toFixed(2), anim: i % 4 === 0 ? 'twinkle' : undefined, delay: r1(rnd() * 3) })
  }
  return out
}

const scenes = {
  stars: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('#070B1E', 'A-20*0.35') },
    { t: 'ellipse', cx: 260, cy: 150, rx: 260, ry: 90, fill: 'A@0.35', blur: 40, anim: 'drift' },
    ...stars(90, 190),
    { t: 'path', d: [pine(20, 262, 90), pine(62, 262, 120), pine(100, 262, 80), pine(140, 262, 105), pine(300, 262, 95), pine(338, 262, 130), pine(380, 262, 85)].join(''), fill: '#04060F' },
    { t: 'rect', x: 0, y: 236, w: W, h: 24, fill: '#04060F' },
  ],
  mountains: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('A+35*1.25', 'A-15') },
    { t: 'circle', cx: 290, cy: 118, r: 40, fill: '#FFF6E0@0.95', anim: 'float' },
    { t: 'path', d: ridge(175, [[40, 150], [90, 128], [150, 158], [210, 112], [262, 150], [330, 138], [400, 150]]), fill: 'A-15*0.85' },
    { t: 'path', d: ridge(200, [[60, 160], [120, 185], [180, 150], [250, 190], [310, 160], [370, 185]]), fill: 'A-30*0.6' },
    { t: 'path', d: ridge(235, [[30, 215], [100, 230], [170, 205], [240, 228], [320, 210], [400, 225]]), fill: 'A-40*0.25' },
  ],
  waves: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('A+20*1.35', 'A*1.1') },
    { t: 'circle', cx: 90, cy: 70, r: 30, fill: '#FFFFFF@0.8', anim: 'float' },
    { t: 'path', d: wave(150, 10, 220, 0), fill: 'A-10@0.45', anim: 'drift' },
    { t: 'path', d: wave(175, 12, 180, 1.5), fill: 'A-20*0.8@0.7', anim: 'drift', delay: 1.2 },
    { t: 'path', d: wave(205, 9, 150, 3), fill: 'A-30*0.55', anim: 'drift', delay: 2.4 },
  ],
  blobs: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: 'B' },
    { t: 'circle', cx: 110, cy: 90, r: 110, fill: 'A@0.75', blur: 34, anim: 'drift' },
    { t: 'circle', cx: 300, cy: 70, r: 95, fill: 'A+60@0.65', blur: 34, anim: 'drift', delay: 2 },
    { t: 'circle', cx: 230, cy: 210, r: 120, fill: 'A-50@0.6', blur: 38, anim: 'drift', delay: 4 },
  ],
  planet: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('#05060F', '#0E1030') },
    ...stars(60, 260),
    { t: 'circle', cx: 200, cy: 135, r: 70, fill: 'A@0.35', blur: 30 },
    { t: 'circle', cx: 200, cy: 135, r: 56, fill: lin('A+30*1.3', 'A-30*0.5', 160), anim: 'float' },
    { t: 'ellipse', cx: 200, cy: 135, rx: 104, ry: 20, rot: -14, fill: 'none', stroke: 'A+40*1.4@0.9', sw: 5, anim: 'float' },
  ],
  sunburst: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('A*1.15', 'A-20*0.8') },
    { t: 'path', d: Array.from({ length: 16 }, (_, i) => { const a1 = (i / 16) * Math.PI * 2, a2 = a1 + Math.PI / 16, R = 420; return `M200 150L${r1(200 + Math.cos(a1) * R)} ${r1(150 + Math.sin(a1) * R)}L${r1(200 + Math.cos(a2) * R)} ${r1(150 + Math.sin(a2) * R)}Z` }).join(''), fill: '#FFFFFF@0.12', anim: 'spin', cx: 200, cy: 150 },
    { t: 'circle', cx: 200, cy: 150, r: 64, fill: '#FFFFFF@0.22' },
    { t: 'circle', cx: 200, cy: 150, r: 44, fill: '#FFFFFF@0.9', anim: 'float' },
  ],
  gift: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('A-30*0.5', '#0B0B12') },
    ...Array.from({ length: 26 }, (_, i) => ({ t: 'rect', x: r1(rnd() * W), y: r1(rnd() * 200), w: 6, h: 11, r: 1.5, rot: Math.round(rnd() * 180), fill: ['A', 'A+60', 'A-60', '#FFFFFF'][i % 4] + '@0.9', anim: 'float', delay: r1(rnd() * 3) })),
    { t: 'rect', x: 140, y: 120, w: 120, h: 100, r: 8, fill: lin('A*1.2', 'A*0.8'), anim: 'float' },
    { t: 'rect', x: 130, y: 100, w: 140, h: 30, r: 7, fill: 'A*1.35', anim: 'float' },
    { t: 'rect', x: 191, y: 100, w: 18, h: 120, fill: '#FFFFFF@0.95', anim: 'float' },
    { t: 'path', d: 'M200 100C180 70 150 76 160 94C168 106 190 102 200 100ZM200 100C220 70 250 76 240 94C232 106 210 102 200 100Z', fill: '#FFFFFF@0.95', anim: 'float' },
  ],
  synthwave: [
    { t: 'rect', x: 0, y: 0, w: W, h: 150, fill: lin('#12002B', 'A+30*0.9') },
    { t: 'circle', cx: 200, cy: 130, r: 64, fill: lin('#FFE66D', 'A+50'), anim: 'float' },
    ...[108, 120, 131, 141, 150].map((y, i) => ({ t: 'rect', x: 130, y, w: 140, h: 3 + i, fill: 'A+30*0.9' })),
    { t: 'rect', x: 0, y: 150, w: W, h: 110, fill: '#0A0014' },
    ...Array.from({ length: 13 }, (_, i) => ({ t: 'path', d: `M200 150L${r1(-400 + i * 100)} 260`, fill: 'none', stroke: 'A@0.8', sw: 1.2 })),
    ...[156, 166, 180, 198, 222, 252].map((y) => ({ t: 'rect', x: 0, y, w: W, h: 1.2, fill: 'A@0.8' })),
  ],
  hills: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: lin('#9FD8FF', '#E9F7FF') },
    { t: 'circle', cx: 320, cy: 70, r: 34, fill: '#FFD76A', anim: 'float' },
    { t: 'ellipse', cx: 90, cy: 60, rx: 44, ry: 14, fill: '#FFFFFF@0.85', anim: 'drift' },
    { t: 'ellipse', cx: 220, cy: 40, rx: 34, ry: 10, fill: '#FFFFFF@0.7', anim: 'drift', delay: 3 },
    { t: 'path', d: 'M0 180C80 140 160 150 240 175C300 195 360 170 400 160L400 260L0 260Z', fill: 'A*0.9' },
    { t: 'path', d: 'M0 215C90 180 170 200 250 215C320 228 370 205 400 200L400 260L0 260Z', fill: 'A-15*0.65' },
  ],
  confetti: [
    { t: 'rect', x: 0, y: 0, w: W, h: H, fill: 'B' },
    ...Array.from({ length: 46 }, (_, i) => ({ t: i % 3 === 0 ? 'circle' : 'rect', cx: r1(rnd() * W), cy: r1(rnd() * H), r: 3.5, x: r1(rnd() * W), y: r1(rnd() * H), w: 5, h: 12, rot: Math.round(rnd() * 180), fill: ['A', 'A+70', 'A-70', 'A+150'][i % 4] + '@0.85', anim: 'float', delay: r1(rnd() * 3) })),
  ],
}
// Drop undefined fields so the data stays small and clean.
const clean = JSON.parse(JSON.stringify(scenes))
process.stdout.write(JSON.stringify(clean))
