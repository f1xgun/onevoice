import type { Config } from 'tailwindcss';

// OneVoice — Linen design system.
// Tokens are OKLCH (defined in app/globals.css). Tailwind reads them via
// bare var(--token) — wrapping in hsl(var(...)) silently fails against OKLCH.
const config: Config = {
  darkMode: ['class'],
  content: [
    './pages/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
    './app/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      colors: {
        // shadcn aliases — keep existing components working
        background: 'var(--background)',
        foreground: 'var(--foreground)',
        card: {
          DEFAULT: 'var(--card)',
          foreground: 'var(--card-foreground)',
        },
        popover: {
          DEFAULT: 'var(--popover)',
          foreground: 'var(--popover-foreground)',
        },
        primary: {
          DEFAULT: 'var(--primary)',
          foreground: 'var(--primary-foreground)',
        },
        secondary: {
          DEFAULT: 'var(--secondary)',
          foreground: 'var(--secondary-foreground)',
        },
        muted: {
          DEFAULT: 'var(--muted)',
          foreground: 'var(--muted-foreground)',
        },
        accent: {
          DEFAULT: 'var(--accent)',
          foreground: 'var(--accent-foreground)',
        },
        destructive: {
          DEFAULT: 'var(--destructive)',
          foreground: 'var(--destructive-foreground)',
        },
        border: 'var(--border)',
        input: 'var(--input)',
        ring: 'var(--ring)',
        chart: {
          '1': 'var(--chart-1)',
          '2': 'var(--chart-2)',
          '3': 'var(--chart-3)',
          '4': 'var(--chart-4)',
          '5': 'var(--chart-5)',
        },

        // Direct OneVoice tokens — use where shadcn aliases don't fit
        paper: {
          DEFAULT: 'var(--ov-paper)',
          raised: 'var(--ov-paper-raised)',
          sunken: 'var(--ov-paper-sunken)',
          well: 'var(--ov-paper-well)',
        },
        ink: {
          DEFAULT: 'var(--ov-ink)',
          mid: 'var(--ov-ink-mid)',
          soft: 'var(--ov-ink-soft)',
          faint: 'var(--ov-ink-faint)',
        },
        line: {
          DEFAULT: 'var(--ov-line)',
          soft: 'var(--ov-line-soft)',
        },
        ochre: {
          DEFAULT: 'var(--ov-accent)',
          deep: 'var(--ov-accent-deep)',
          soft: 'var(--ov-accent-soft)',
          ink: 'var(--ov-accent-ink)',
        },
        success: {
          DEFAULT: 'var(--ov-success)',
          soft: 'var(--ov-success-soft)',
        },
        warning: {
          DEFAULT: 'var(--ov-warning)',
          soft: 'var(--ov-warning-soft)',
          ink: 'var(--ov-warning-ink)',
        },
        danger: {
          DEFAULT: 'var(--ov-danger)',
          soft: 'var(--ov-danger-soft)',
        },
        info: {
          DEFAULT: 'var(--ov-info)',
          soft: 'var(--ov-info-soft)',
        },
      },
      fontFamily: {
        sans: ['var(--font-sans)', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['var(--font-mono)', 'ui-monospace', 'monospace'],
      },
      borderRadius: {
        sm: 'var(--ov-radius-sm)',
        md: 'var(--ov-radius-md)',
        lg: 'var(--ov-radius-lg)',
        xl: 'var(--ov-radius-xl)',
      },
      boxShadow: {
        'ov-1': 'var(--ov-shadow-1)',
        'ov-2': 'var(--ov-shadow-2)',
        'ov-3': 'var(--ov-shadow-3)',
      },
    },
  },
  plugins: [require('tailwindcss-animate'), require('@tailwindcss/typography')],
};
export default config;
