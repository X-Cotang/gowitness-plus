import { createRouter, createWebHistory } from 'vue-router'

const routes = [
    { path: '/', name: 'home', component: () => import('../views/Gallery.vue') },
    { path: '/gallery', name: 'gallery', component: () => import('../views/Gallery.vue') },
    { path: '/table', name: 'table', component: () => import('../views/Table.vue') },
    { path: '/submit', name: 'submit', component: () => import('../views/Submit.vue') },
    { path: '/log', name: 'log', component: () => import('../views/Log.vue') },
    { path: '/config', name: 'config', component: () => import('../views/Config.vue') },
    { path: '/detail/:id', name: 'detail', component: () => import('../views/Detail.vue') },
]

const router = createRouter({
    routes,
    history: createWebHistory(),
})

export default router
