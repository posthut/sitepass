import { createRoot } from 'react-dom/client'
import '@fontsource-variable/onest/wght.css'
import '@fontsource-variable/jetbrains-mono/wght.css'
import App from './App.jsx'
import './tokens.css'

createRoot(document.getElementById('root')).render(<App />)
