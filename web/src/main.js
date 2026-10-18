import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router.js'
import { startIdentityWatch } from './auth.js'
import './style.css'
startIdentityWatch()
createApp(App).use(router).mount('#app')
