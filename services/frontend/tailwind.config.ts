import type { Config } from 'tailwindcss';

const config: Config = {
  darkMode: [
    'variant',
    [
      '&:where(.dark, .dark *)',
      '@media (prefers-color-scheme: dark) { &:where(:root:not(.light), :root:not(.light) *) }',
    ],
  ],
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
        brand: {
          DEFAULT: 'var(--ov-brand)',
          foreground: 'var(--ov-on-brand)',
          hover: 'var(--ov-brand-hover)',
          soft: 'var(--ov-brand-soft)',
        },
        control: 'var(--ov-control)',
        overlay: 'var(--ov-overlay)',
        // Deprecated compatibility alias for generated primitives.
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
        display: ['var(--font-sans)', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['var(--font-mono)', 'ui-monospace', 'monospace'],
      },
      fontSize: {
        hero: ['2.25rem', { lineHeight: '2.5rem', letterSpacing: '-0.025em', fontWeight: '600' }],
        'hero-lg': [
          '3.5rem',
          { lineHeight: '3.75rem', letterSpacing: '-0.025em', fontWeight: '600' },
        ],
        section: [
          '1.75rem',
          { lineHeight: '2.125rem', letterSpacing: '-0.015em', fontWeight: '600' },
        ],
        'section-lg': [
          '2.25rem',
          { lineHeight: '2.625rem', letterSpacing: '-0.015em', fontWeight: '600' },
        ],
        'page-title': ['1.5rem', { lineHeight: '1.875rem', fontWeight: '600' }],
        'document-title': ['1.25rem', { lineHeight: '1.75rem', fontWeight: '600' }],
        reading: ['1rem', { lineHeight: '1.5625rem' }],
        action: ['1rem', { lineHeight: '1.375rem', fontWeight: '500' }],
        meta: ['.875rem', { lineHeight: '1.25rem' }],
        technical: ['.8125rem', { lineHeight: '1.125rem' }],
        price: ['2rem', { lineHeight: '2.375rem', fontWeight: '500' }],
      },
      borderRadius: {
        sm: 'var(--ov-radius-sm)',
        md: 'var(--ov-radius-md)',
        lg: 'var(--ov-radius-lg)',
        xl: 'var(--ov-radius-xl)',
      },
      boxShadow: {
        overlay: 'var(--ov-shadow-overlay)',
        'ov-1': 'var(--ov-shadow-1)',
        'ov-2': 'var(--ov-shadow-2)',
        'ov-3': 'var(--ov-shadow-3)',
      },
    },
  },
  plugins: [require('tailwindcss-animate'), require('@tailwindcss/typography')],
};
export default config;
