---
name: theme-factory
description: Toolkit for styling web apps, UI, landing pages, and components with distinctive, intentional themes. Includes 10 curated pre-set themes with complete color palettes, hex codes, typography pairings, and CSS custom property token mappings.
license: Complete terms in LICENSE.txt
---

# Theme Factory Skill

This skill provides a curated collection of professional font and color themes, each with carefully selected color palettes, hex codes, typography pairings, and CSS custom property token systems.

## The 10 Curated Themes & Specifications

### 1. Tech Innovation (High-Contrast Cyber/Tech Dark Mode)
- **Primary Accent / Electric Blue**: `#0066FF`
- **Highlight / Neon Cyan**: `#00FFFF`
- **Deep Background / Dark Gray**: `#0D1117` / `#161B22`
- **Surface Card**: `#1E1E2E`
- **Text Primary / Crisp White**: `#FFFFFF`
- **Text Secondary / Slate**: `#94A3B8`
- **Typography**: Space Grotesk / Inter / Outfit
- **CSS Tokens**:
  ```css
  :root {
    --bg-primary: #0d1117;
    --bg-surface: #161b22;
    --bg-card: #1e1e2e;
    --border-color: rgba(255, 255, 255, 0.1);
    --accent: #0066ff;
    --accent-glow: rgba(0, 102, 255, 0.25);
    --accent-neon: #00ffff;
    --text-primary: #ffffff;
    --text-secondary: #94a3b8;
  }
  ```

### 2. Midnight Galaxy (Dramatic Cosmic Violet & Deep Indigo)
- **Primary Base / Deep Purple**: `#130D24`
- **Surface / Cosmic Indigo**: `#1E1638`
- **Card Background**: `#2B1E3E`
- **Mid-Tone Accent / Cosmic Blue**: `#6366F1`
- **Highlight / Lavender**: `#A490C2`
- **Light Contrast**: `#E6E6FA`
- **Typography**: Plus Jakarta Sans / Inter
- **CSS Tokens**:
  ```css
  :root {
    --bg-primary: #130d24;
    --bg-surface: #1e1638;
    --bg-card: #2b1e3e;
    --border-color: rgba(164, 144, 194, 0.2);
    --accent: #6366f1;
    --accent-glow: rgba(99, 102, 241, 0.25);
    --accent-soft: #a490c2;
    --text-primary: #e6e6fa;
    --text-secondary: #9ca3af;
  }
  ```

### 3. Ocean Depths (Calming Maritime Slate & Deep Navy)
- **Primary Background / Deep Navy**: `#0F172A`
- **Surface Background**: `#1E293B`
- **Card Background**: `#1A2332`
- **Primary Accent / Teal**: `#2DD4BF`
- **Secondary Accent / Seafoam**: `#A8DADC`
- **Text Primary / Cream White**: `#F8FAFC`
- **Text Secondary / Muted Ice**: `#94A3B8`
- **Typography**: Inter / Outfit

### 4. Sunset Boulevard (Warm Amber, Coral & Terracotta)
- **Primary Accent / Burnt Orange**: `#E76F51`
- **Secondary Warm / Coral**: `#F4A261`
- **Highlight / Warm Sand**: `#E9C46A`
- **Dark Base / Deep Slate**: `#1A1D24`
- **Surface Card**: `#242831`
- **Text Primary**: `#F8F9FA`
- **Typography**: Outfit / Inter

### 5. Modern Minimalist (Monochrome Luxury & Clean Grayscale)
- **Primary Background / Near Black**: `#121212`
- **Surface Card / Charcoal**: `#1E1E1E`
- **Border / Subdued Slate**: `#2E2E2E`
- **Primary Accent / Clean White**: `#FFFFFF`
- **Secondary Accent / Cool Slate**: `#9E9E9E`
- **Typography**: Inter / Roboto

### 6. Arctic Frost (Cool Ice Blue & Steel Accent)
- **Primary Background**: `#0F141C`
- **Surface Card**: `#182230`
- **Primary Accent / Steel Blue**: `#4A6FA5`
- **Highlight / Ice Cyan**: `#7DD3FC`
- **Text Primary**: `#F8FAFC`
- **Typography**: Inter / Plus Jakarta Sans

### 7. Golden Hour (Rich Autumnal Ochre & Warm Espresso)
- **Primary Background**: `#181512`
- **Surface Card**: `#26201A`
- **Primary Accent / Amber Gold**: `#F59E0B`
- **Secondary Accent / Warm Ochre**: `#D97706`
- **Text Primary**: `#FEF3C7`
- **Typography**: Outfit / Plus Jakarta Sans

### 8. Botanical Garden (Organic Sage, Emerald & Forest)
- **Primary Background**: `#0E1612`
- **Surface Card**: `#16241D`
- **Primary Accent / Emerald**: `#10B981`
- **Secondary Accent / Mint**: `#6EE7B7`
- **Text Primary**: `#ECFDF5`
- **Typography**: Inter / Plus Jakarta Sans

### 9. Desert Rose (Dusty Mauve, Rosewood & Warm Stone)
- **Primary Background**: `#191416`
- **Surface Card**: `#271F23`
- **Primary Accent / Dusty Rose**: `#FB7185`
- **Secondary Accent / Warm Terracotta**: `#FDA4AF`
- **Text Primary**: `#FFF1F2`
- **Typography**: Outfit / Inter

### 10. Forest Canopy (Deep Pine, Moss & Earth Tones)
- **Primary Background**: `#0B130E`
- **Surface Card**: `#132118`
- **Primary Accent / Pine Green**: `#22C55E`
- **Secondary Accent / Moss**: `#86EFAC`
- **Text Primary**: `#F0FDF4`
- **Typography**: Inter / Space Grotesk

## Application Rules for Frontend & UI Code:

When building web interfaces (HTML, CSS, JavaScript, components):
1. **Choose an intentional theme**: Pick one of the distinctive palettes above (or create a harmonized palette derived from these principles).
2. **Define full CSS variables in `:root`**:
   - `--bg-primary`, `--bg-surface`, `--bg-card`
   - `--border-color`, `--border-hover`
   - `--accent`, `--accent-glow`, `--accent-hover`
   - `--text-primary`, `--text-secondary`, `--text-muted`
3. **Typography**: Load Google Fonts (e.g. `<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Outfit:wght@500;600;700&display=swap" rel="stylesheet">`).
4. **Rich Micro-Interactions**:
   - Smooth transitions (`transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);`).
   - Subtle glowing focus rings (`box-shadow: 0 0 0 3px var(--accent-glow);`).
   - Glassmorphic card styling (`backdrop-filter: blur(12px); background: rgba(..., 0.7);`).
   - Animated checkmarks, strike-through transitions, and badge filters.
5. **Complete Interactive Features**:
   - Filter tabs (All, Active, Completed) with item counters.
   - LocalStorage persistence.
   - Inline edit, drag-or-delete, keyboard shortcuts (`Enter` to add, `Esc` to cancel).
