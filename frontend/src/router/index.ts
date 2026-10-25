// 极简路由：三个页面，全部走 hash。
//
// 用 hash 而不是 history：Wails 用自定义 scheme 提供资源，history 模式下
// 刷新会打到 asset server 上拿不到 index.html（ADR-006）。
import { createRouter, createWebHashHistory } from "vue-router"
import SwitcherView from "@/views/SwitcherView.vue"

export const routes = [
  { path: "/", name: "switcher", component: SwitcherView },
  {
    path: "/sources",
    name: "sources",
    component: () => import("@/views/SourcesView.vue"),
  },
  {
    path: "/settings",
    name: "settings",
    component: () => import("@/views/SettingsView.vue"),
  },
  { path: "/:pathMatch(.*)*", redirect: "/" },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
