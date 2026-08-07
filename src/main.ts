import { createApp } from 'vue'
import App from './App.vue'
import { initServiceWorker } from './utils/serviceWorker'
import './styles/main.css'

createApp(App).mount('#app')

void initServiceWorker()
