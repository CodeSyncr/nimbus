package commands

const inertiaPageHomeVue = `<template>
<nav>
  <a href="/" class="nav-logo">
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
    </svg>
    {{ appName }}
    <span class="nav-badge">v{{ version }}</span>
  </a>
  <div class="nav-links">
    <a href="/health" class="nav-link">Health</a>
    <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" class="nav-link">Docs</a>
    <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="nav-link primary">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
      GitHub
    </a>
  </div>
</nav>
<div class="wrapper">
  <section class="hero">
    <div class="hero-left">
      <div class="status-pill">
        <span class="pulse-dot"><span></span><span></span></span>
        Live · {{ env }} environment
      </div>
      <h1 class="hero-title">Build fast.<br>Deploy with<br><span class="accent">{{ appName }}.</span></h1>
      <p class="hero-sub">{{ tagline || 'A Laravel-inspired web framework for Go — expressive, elegant, and blazing fast. From scaffolding to production in minutes.' }}</p>
      <div class="hero-ctas">
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="btn btn-primary">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
          View on GitHub
        </a>
        <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" class="btn btn-ghost">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14,2 14,8 20,8"/></svg>
          Docs
        </a>
        <a href="/health" class="btn btn-ghost">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22,12 18,12 15,21 9,3 6,12 2,12"/></svg>
          Health
        </a>
      </div>
      <div class="cmd-block">
        <span class="prompt">$</span>
        <span class="cmd-text">nimbus new my-app --kit=vue</span>
        <button type="button" class="cmd-copy" title="Copy command" @click="copyCommand">⎘</button>
      </div>
    </div>
    <div class="hero-right">
      <div class="hero-code-window">
        <div class="hero-code-titlebar">
          <div class="code-dot"></div><div class="code-dot"></div><div class="code-dot"></div>
          <span class="code-filename">users_controller.go</span>
        </div>
        <pre v-html="heroCodeHtml"></pre>
      </div>
    </div>
  </section>
  <div class="stats-row">
    <div class="stat"><div class="stat-val">~0ms</div><div class="stat-label">Cold start overhead</div></div>
    <div class="stat"><div class="stat-val">Go</div><div class="stat-label">Powered by Go runtime</div></div>
    <div class="stat"><div class="stat-val">Laravel</div><div class="stat-label">Inspired by</div></div>
  </div>
  <div class="section-divider"><p class="section-label">What's included</p><hr /></div>
  <div class="features-grid">
    <div class="card fade-up"><div class="card-icon amber"><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg></div><h3>MVC Architecture</h3><p>Clean separation of concerns with Models, Views, and Controllers — batteries included, opinionated by default.</p></div>
    <div class="card fade-up"><div class="card-icon cyan"><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg></div><h3>Blazing Fast Router</h3><p>Radix-tree based HTTP router with named params, groups, middleware, and resource routing out of the box.</p></div>
    <div class="card fade-up"><div class="card-icon rose"><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg></div><h3>ORM &amp; Migrations</h3><p>Expressive query builder with relationship support. Version-controlled schema migrations that just work.</p></div>
    <div class="card fade-up"><div class="card-icon violet"><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></div><h3>Auth Scaffolding</h3><p>Session, token, and OAuth-based auth with guards and middleware — generated in seconds with the CLI.</p></div>
    <div class="card fade-up"><div class="card-icon green"><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg></div><h3>Nimbus Templates</h3><p>Server-side HTML templating with layouts, partials, and live hot-reload. Edit and see changes instantly.</p></div>
    <div class="card fade-up"><div class="card-icon sky"><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg></div><h3>Event System</h3><p>Typed event emitter and listener system. Decouple your business logic with first-class async event handling.</p></div>
  </div>
  <div class="code-section">
    <div class="section-divider" style="margin-bottom:20px"><p class="section-label">Looks like this</p><hr /></div>
    <div class="code-window">
      <div class="code-titlebar">
        <div class="code-dots"><div class="code-dot"></div><div class="code-dot"></div><div class="code-dot"></div></div>
        <span class="code-filename">app/controllers/users_controller.go</span>
      </div>
      <pre v-html="bottomCodeHtml"></pre>
    </div>
  </div>
  <footer>
    <div class="footer-inner">
      <p class="footer-text">Built with <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">Nimbus</a> — Laravel-inspired framework for Go</p>
      <div class="footer-links">
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">GitHub</a>
        <a href="/health">Health</a>
        <a href="https://github.com/CodeSyncr/nimbus/issues" target="_blank" rel="noopener">Issues</a>
      </div>
    </div>
  </footer>
</div>
</template>

<script setup>
import { onMounted } from 'vue'
defineProps({ appName: String, version: String, env: String, tagline: String })
const heroCodeHtml = ` + "`" + inertiaHomeHeroCodeHTML + "`" + `
const bottomCodeHtml = ` + "`" + inertiaHomeBottomCodeHTML + "`" + `
function copyCommand(e) {
  navigator.clipboard.writeText('nimbus new my-app --kit=vue')
  const btn = e.currentTarget
  btn.classList.add('copied')
  btn.innerHTML = '✓'
  setTimeout(() => { btn.classList.remove('copied'); btn.innerHTML = '⎘' }, 1800)
}
onMounted(() => {
  const observer = new IntersectionObserver((entries) => {
    entries.forEach((e, i) => {
      if (e.isIntersecting) {
        setTimeout(() => e.target.classList.add('visible'), i * 80)
        observer.unobserve(e.target)
      }
    })
  }, { threshold: 0.12 })
  document.querySelectorAll('.fade-up').forEach((el) => observer.observe(el))
})
</script>
`
