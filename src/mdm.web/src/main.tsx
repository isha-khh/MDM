import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { registerSW } from 'virtual:pwa-register'
import './i18n'
import 'driver.js/dist/driver.css'
import './index.css'
import App from './App.tsx'

// Only the /m/* mobile self-service pages need this — registering
// unconditionally is harmless for the desktop admin app (the service worker
// just sits idle for routes it has no cached data for) and means the
// offline queue's auto-flush triggers work regardless of which route
// happens to be open when connectivity returns.
if ('serviceWorker' in navigator) {
  registerSW({ immediate: true })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
