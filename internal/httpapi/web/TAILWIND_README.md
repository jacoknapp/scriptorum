# Tailwind CSS build

This project should include a production-built Tailwind CSS file at `internal/httpapi/web/static/css/tailwind.css`.

## Quick steps (Tailwind CLI):

1. Install the Tailwind CLI (you can use npm):
   ```bash
   npm install -D tailwindcss
   ```

2. Create a minimal `tailwind.config.js` at the repo root if you need to customize the theme.

3. Build the CSS (PowerShell):
   ```bash
   npm run build:css
   ```
   
   Or manually:
   ```bash
   npx tailwindcss -i ./assets/tailwind.input.css -o ./internal/httpapi/web/static/css/tailwind.css --minify
   ```

4. **Important**: After building CSS, you must rebuild the Go application to embed the new CSS:
   ```bash
   go build -o scriptorum.exe ./cmd/scriptorum
   ```

## Notes:
- The CSS file must be at `internal/httpapi/web/static/css/tailwind.css` because the server uses Go's embedded filesystem (`//go:embed web/static/*`)
- `assets/tailwind.input.css` should include the Tailwind directives:
  ```css
  @tailwind base;
  @tailwind components;  
  @tailwind utilities;
  ```
- Alternatively, integrate Tailwind as a PostCSS plugin in your existing build pipeline
- During development you may keep using the CDN, but avoid it in production
