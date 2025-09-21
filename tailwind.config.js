module.exports = {
  content: [
    './internal/httpapi/web/templates/**/*.html',
    './**/*.go',
    './static/**/*.html'
  ],
  theme: {
    extend: {
      colors: {
        royal: {
          50: '#f6f3ff',
          100: '#ede9fe',
          200: '#ddd6fe',
          300: '#c4b5fd',
          400: '#a78bfa',
          500: '#8b5cf6',
          600: '#7c3aed',
          700: '#6d28d9',
          800: '#5b21b6',
          900: '#4c1d95'
        },
        surface: {
          50: '#0b0b13',
          100: '#11111a'
        },
        night: {
          700: '#171725',
          750: '#141421',
          800: '#11111a',
          900: '#0b0b13'
        }
      },
      borderRadius: {
        xl2: '1.25rem'
      },
      boxShadow: {
        card: '0 8px 24px rgba(20,20,35,.45)',
        glow: '0 0 0 4px rgba(124,58,237,.25)'
      }
    }
  },
  plugins: []
};
