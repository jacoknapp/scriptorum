/** @type {import('tailwindcss').Config} */
module.exports = {
  // Scan all Go HTML templates so Tailwind includes every utility class used in the UI.
  content: [
    './internal/httpapi/web/templates/**/*.html',
    './internal/httpapi/web/setup/**/*.html',
  ],
  theme: {
    extend: {
      colors: {
        // Custom dark background palette used throughout the app.
        night: {
          700: 'rgb(23 23 37)',
          750: 'rgb(20 20 33)',
          800: 'rgb(17 17 26)',
          900: 'rgb(11 11 19)',
        },
        // App primary/accent color (violet).
        royal: {
          200: 'rgb(221 214 254)',
          300: 'rgb(196 181 253)',
          500: 'rgb(139 92 246)',
          600: 'rgb(124 58 237)',
          700: 'rgb(109 40 217)',
        },
      },
    },
  },
  plugins: [],
}
