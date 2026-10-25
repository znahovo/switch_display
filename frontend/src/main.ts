import { createApp } from "vue"
import { createPinia } from "pinia"
import App from "./App.vue"
import { i18n } from "./locales"
import { watchTheme } from "./lib/theme"
import "./style.css"

// 先定主题再挂载，避免首帧闪白。
watchTheme()

createApp(App).use(createPinia()).use(i18n).mount("#app")
