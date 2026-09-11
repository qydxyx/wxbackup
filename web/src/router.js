import { createRouter, createWebHistory } from 'vue-router'
import HomeView from './views/HomeView.vue'
import ChatView from './views/ChatView.vue'

export default createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    {
      path: '/chat/:accountId/:talkerId?',
      name: 'chat',
      component: ChatView,
      props: true,
    },
  ],
})
