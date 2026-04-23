package commands

// nimbusLandingPageCSS is shared by layoutViewTmpl (server-rendered apps) and
// inertiaLayoutNimbus (Inertia root HTML) for the default welcome / marketing home.
const nimbusLandingPageCSS = `
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

  :root {
    --bg:        #FAFAF7;
    --bg-card:   #FFFFFF;
    --bg-muted:  #F3F2EE;
    --border:    #E4E2DC;
    --border-md: #D0CEC6;
    --text:      #1A1916;
    --text-2:    #6B6860;
    --text-3:    #A09D97;
    --accent:    #1B6B4A;
    --accent-bg: #EAF5EE;
    --accent-2:  #E07B2C;
    --amber:     #B97218;
    --amber-bg:  #FEF5E7;
    --cyan:      #1565A8;
    --cyan-bg:   #EBF3FC;
    --rose:      #A0283C;
    --rose-bg:   #FBE9EC;
    --violet:    #6039B0;
    --violet-bg: #F3EFFC;
    --green:     #2E6A30;
    --green-bg:  #EDF6EE;
    --sky:       #0E5F8A;
    --sky-bg:    #E8F4FA;
    --code-bg:   #1C1B19;
    --code-text: #E8E6DF;
    --radius:    12px;
    --radius-sm: 8px;
  }

  html { scroll-behavior: smooth; }

  body {
    font-family: 'DM Sans', sans-serif;
    background: var(--bg);
    color: var(--text);
    font-size: 15px;
    line-height: 1.65;
    -webkit-font-smoothing: antialiased;
  }

  nav {
    position: sticky;
    top: 0;
    z-index: 100;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 2rem;
    height: 60px;
    background: rgba(250, 250, 247, 0.88);
    backdrop-filter: blur(16px);
    border-bottom: 1px solid var(--border);
  }

  .nav-logo {
    display: flex;
    align-items: center;
    gap: 9px;
    text-decoration: none;
    color: var(--text);
    font-weight: 500;
    font-size: 15px;
    letter-spacing: -0.01em;
  }

  .nav-logo svg { color: var(--accent); flex-shrink: 0; }

  .nav-badge {
    font-size: 11px;
    font-weight: 500;
    padding: 2px 7px;
    border-radius: 20px;
    background: var(--accent-bg);
    color: var(--accent);
    letter-spacing: 0.02em;
  }

  .nav-links {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .nav-link {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 13px;
    border-radius: var(--radius-sm);
    text-decoration: none;
    color: var(--text-2);
    font-size: 14px;
    font-weight: 400;
    transition: background 0.15s, color 0.15s;
  }
  .nav-link:hover { background: var(--bg-muted); color: var(--text); }

  .nav-link.primary {
    background: var(--text);
    color: var(--bg);
    font-weight: 500;
  }
  .nav-link.primary:hover { background: #333; color: #fff; }

  .wrapper {
    max-width: 1080px;
    margin: 0 auto;
    padding: 0 2rem;
  }

  .hero {
    padding: 80px 0 64px;
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 56px;
    align-items: center;
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 5px 12px;
    border-radius: 20px;
    border: 1px solid var(--border);
    background: var(--bg-card);
    font-size: 12.5px;
    color: var(--text-2);
    font-weight: 400;
    margin-bottom: 22px;
  }

  .pulse-dot {
    position: relative;
    width: 8px;
    height: 8px;
    flex-shrink: 0;
  }
  .pulse-dot span {
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: #22c55e;
  }
  .pulse-dot span:first-child {
    animation: pulse-ring 2s ease-out infinite;
    background: rgba(34, 197, 94, 0.35);
  }
  @keyframes pulse-ring {
    0%   { transform: scale(1);   opacity: 1; }
    100% { transform: scale(2.6); opacity: 0; }
  }

  .hero-title {
    font-family: 'Fraunces', serif;
    font-size: clamp(38px, 5vw, 56px);
    font-weight: 300;
    line-height: 1.08;
    letter-spacing: -0.02em;
    color: var(--text);
    margin-bottom: 20px;
  }

  .hero-title .accent {
    font-style: italic;
    color: var(--accent);
  }

  .hero-sub {
    font-size: 16px;
    color: var(--text-2);
    line-height: 1.7;
    max-width: 420px;
    margin-bottom: 32px;
  }

  .hero-ctas {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    margin-bottom: 28px;
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 9px 18px;
    border-radius: var(--radius-sm);
    font-size: 14px;
    font-weight: 500;
    text-decoration: none;
    transition: all 0.15s;
    cursor: pointer;
    border: none;
    font-family: 'DM Sans', sans-serif;
  }

  .btn-primary {
    background: var(--text);
    color: #fff;
  }
  .btn-primary:hover { background: #333; transform: translateY(-1px); }

  .btn-ghost {
    background: var(--bg-card);
    color: var(--text-2);
    border: 1px solid var(--border);
  }
  .btn-ghost:hover { background: var(--bg-muted); color: var(--text); transform: translateY(-1px); }

  .cmd-block {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-family: 'JetBrains Mono', monospace;
    font-size: 13px;
    color: var(--text);
    max-width: 100%;
  }

  .prompt { color: var(--accent); font-weight: 500; }

  .cmd-copy {
    margin-left: auto;
    background: none;
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 3px 8px;
    font-size: 13px;
    cursor: pointer;
    color: var(--text-3);
    transition: all 0.15s;
  }
  .cmd-copy:hover { background: var(--bg-muted); color: var(--text); }
  .cmd-copy.copied { background: var(--accent-bg); color: var(--accent); border-color: #b2d8c4; }

  .hero-code-window {
    background: var(--code-bg);
    border-radius: var(--radius);
    border: 1px solid #2E2D2A;
    overflow: hidden;
    box-shadow: 0 24px 64px rgba(0,0,0,0.15), 0 8px 24px rgba(0,0,0,0.1);
  }

  .hero-code-titlebar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 11px 14px;
    border-bottom: 1px solid #2E2D2A;
    background: #161512;
  }

  .code-dot {
    width: 10px; height: 10px;
    border-radius: 50%;
    background: #3A3936;
  }
  .hero-code-titlebar .code-dot:nth-child(1) { background: #FF5F57; }
  .hero-code-titlebar .code-dot:nth-child(2) { background: #FEBC2E; }
  .hero-code-titlebar .code-dot:nth-child(3) { background: #28C840; }

  .hero-code-window .code-filename {
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    color: #6B6860;
    margin-left: 6px;
  }

  .hero-code-window pre {
    font-family: 'JetBrains Mono', monospace;
    font-size: 12.5px;
    line-height: 1.75;
    color: var(--code-text);
    padding: 20px 20px 22px;
    overflow-x: auto;
  }

  .kw  { color: #C792EA; }
  .str { color: #C3E88D; }
  .cmt { color: #546E7A; }
  .fn  { color: #82AAFF; }
  .type{ color: #FFCB6B; }
  .op  { color: #89DDFF; }
  .num { color: #F78C6C; }

  .stats-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 1px;
    background: var(--border);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    margin-bottom: 72px;
  }

  .stat {
    background: var(--bg-card);
    padding: 28px 28px;
    text-align: center;
  }

  .stat-val {
    font-family: 'Fraunces', serif;
    font-size: 30px;
    font-weight: 400;
    color: var(--text);
    line-height: 1.1;
    margin-bottom: 6px;
    letter-spacing: -0.02em;
  }

  .stat-label {
    font-size: 13px;
    color: var(--text-3);
    font-weight: 400;
  }

  .section-label {
    font-size: 12px;
    font-weight: 500;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-3);
    margin-bottom: 20px;
  }

  .features-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 16px;
    margin-bottom: 72px;
  }

  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 24px;
    transition: transform 0.2s, box-shadow 0.2s, border-color 0.2s;
  }

  .card:hover {
    transform: translateY(-3px);
    box-shadow: 0 12px 40px rgba(0,0,0,0.07);
    border-color: var(--border-md);
  }

  .card h3 {
    font-size: 15px;
    font-weight: 500;
    margin-bottom: 8px;
    color: var(--text);
    letter-spacing: -0.01em;
  }

  .card p {
    font-size: 13.5px;
    color: var(--text-2);
    line-height: 1.65;
  }

  .card-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    border-radius: var(--radius-sm);
    margin-bottom: 16px;
    flex-shrink: 0;
  }

  .card-icon.amber  { background: var(--amber-bg);  color: var(--amber);  }
  .card-icon.cyan   { background: var(--cyan-bg);   color: var(--cyan);   }
  .card-icon.rose   { background: var(--rose-bg);   color: var(--rose);   }
  .card-icon.violet { background: var(--violet-bg); color: var(--violet); }
  .card-icon.green  { background: var(--green-bg);  color: var(--green);  }
  .card-icon.sky    { background: var(--sky-bg);    color: var(--sky);    }

  .code-section {
    margin-bottom: 72px;
  }

  .code-window {
    background: var(--code-bg);
    border-radius: var(--radius);
    border: 1px solid #2E2D2A;
    overflow: hidden;
    box-shadow: 0 16px 48px rgba(0,0,0,0.12);
  }

  .code-titlebar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 11px 16px;
    border-bottom: 1px solid #2A2926;
    background: #161512;
  }

  .code-dots { display: flex; gap: 6px; }

  .code-dots .code-dot:nth-child(1) { background: #FF5F57; }
  .code-dots .code-dot:nth-child(2) { background: #FEBC2E; }
  .code-dots .code-dot:nth-child(3) { background: #28C840; }

  .code-titlebar .code-filename {
    font-family: 'JetBrains Mono', monospace;
    font-size: 11.5px;
    color: #5A5854;
    margin-left: 8px;
  }

  .code-window pre {
    font-family: 'JetBrains Mono', monospace;
    font-size: 13px;
    line-height: 1.8;
    color: var(--code-text);
    padding: 24px 28px 28px;
    overflow-x: auto;
  }

  footer {
    border-top: 1px solid var(--border);
    padding: 28px 0;
    margin-top: 0;
  }

  .footer-inner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
  }

  .footer-text {
    font-size: 13.5px;
    color: var(--text-3);
  }

  .footer-text a {
    color: var(--text-2);
    text-decoration: none;
    font-weight: 500;
    transition: color 0.15s;
  }
  .footer-text a:hover { color: var(--accent); }

  .footer-links {
    display: flex;
    gap: 4px;
  }

  .footer-links a {
    padding: 5px 11px;
    font-size: 13.5px;
    color: var(--text-3);
    text-decoration: none;
    border-radius: var(--radius-sm);
    transition: background 0.15s, color 0.15s;
  }
  .footer-links a:hover { background: var(--bg-muted); color: var(--text); }

  @keyframes fadeUp {
    from { opacity: 0; transform: translateY(20px); }
    to   { opacity: 1; transform: translateY(0); }
  }

  .hero-left > * { opacity: 0; animation: fadeUp 0.55s ease forwards; }
  .hero-left > *:nth-child(1) { animation-delay: 0.05s; }
  .hero-left > *:nth-child(2) { animation-delay: 0.15s; }
  .hero-left > *:nth-child(3) { animation-delay: 0.25s; }
  .hero-left > *:nth-child(4) { animation-delay: 0.32s; }
  .hero-left > *:nth-child(5) { animation-delay: 0.38s; }

  .hero-right { opacity: 0; animation: fadeUp 0.65s 0.2s ease forwards; }

  .stats-row { opacity: 0; animation: fadeUp 0.5s 0.45s ease forwards; }

  .fade-up {
    opacity: 0;
    transform: translateY(14px);
    transition: opacity 0.5s ease, transform 0.5s ease;
  }
  .fade-up.visible { opacity: 1; transform: none; }

  .section-divider {
    display: flex;
    align-items: center;
    gap: 14px;
    margin-bottom: 20px;
  }
  .section-divider .section-label { margin-bottom: 0; }
  .section-divider hr {
    flex: 1;
    border: none;
    border-top: 1px solid var(--border);
  }

  @media (max-width: 820px) {
    .hero {
      grid-template-columns: 1fr;
      gap: 40px;
      padding: 56px 0 48px;
    }
    .hero-right { display: none; }
    .features-grid { grid-template-columns: 1fr 1fr; }
    .stats-row { grid-template-columns: 1fr; }
  }
  @media (max-width: 560px) {
    nav { padding: 0 1.25rem; }
    .wrapper { padding: 0 1.25rem; }
    .features-grid { grid-template-columns: 1fr; }
    .hero-title { font-size: 36px; }
    .hero-ctas { flex-direction: column; }
    .hero-ctas .btn { justify-content: center; }
    .footer-inner { flex-direction: column; align-items: flex-start; }
  }
`
