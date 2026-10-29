/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './*.ts', './*.go'],
  // Preserve the current CDN behavior: dark mode follows the OS preference.
  darkMode: 'media',
  theme: { extend: {} },
  plugins: [],
};
