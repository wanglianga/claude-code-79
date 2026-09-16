import { createRouter, createWebHistory } from 'vue-router'
import { store } from './store'

const routes = [
  { path: '/login', component: () => import('./views/Login.vue'), meta: { public: true } },
  {
    path: '/',
    component: () => import('./views/Layout.vue'),
    children: [
      { path: '', component: () => import('./views/Dashboard.vue'), meta: { title: '工作台' } },
      { path: 'order/new', component: () => import('./views/OrderCreate.vue'), meta: { title: '订餐下单', roles: ['family', 'elder', 'community', 'admin'] } },
      { path: 'orders', component: () => import('./views/Orders.vue'), meta: { title: '餐单管理' } },
      { path: 'orders/:id', component: () => import('./views/OrderDetail.vue'), meta: { title: '餐单详情' } },
      { path: 'elders', component: () => import('./views/Elders.vue'), meta: { title: '老人档案', roles: ['community', 'admin', 'finance'] } },
      { path: 'elders/:id', component: () => import('./views/ElderDetail.vue'), meta: { title: '老人档案详情', roles: ['community', 'admin', 'finance'] } },
      { path: 'kitchen', component: () => import('./views/Kitchen.vue'), meta: { title: '厨房工作台', roles: ['kitchen', 'admin'] } },
      { path: 'delivery', component: () => import('./views/Delivery.vue'), meta: { title: '配送任务', roles: ['rider', 'volunteer'] } },
      { path: 'anomalies', component: () => import('./views/Anomalies.vue'), meta: { title: '异常工单' } },
      { path: 'boxes', component: () => import('./views/Boxes.vue'), meta: { title: '餐盒回收', roles: ['community', 'admin', 'finance'] } },
      { path: 'volunteers-manage', component: () => import('./views/VolunteersManage.vue'), meta: { title: '志愿者资质考核', roles: ['community', 'admin'] } },
      { path: 'finance', component: () => import('./views/Finance.vue'), meta: { title: '财政核销', roles: ['finance', 'admin'] } },
      { path: 'finance/:id', component: () => import('./views/ReconciliationDetail.vue'), meta: { title: '核销详情', roles: ['finance', 'admin'] } },
      { path: 'subsidy-changes', component: () => import('./views/SubsidyChanges.vue'), meta: { title: '补贴变更记录', roles: ['finance', 'admin', 'community'] } },
      { path: 'status-changes', component: () => import('./views/StatusChanges.vue'), meta: { title: '状态变更与四段清算' } },
      { path: 'status-changes/:id', component: () => import('./views/StatusChangeDetail.vue'), meta: { title: '四段清算详情' } },
      { path: 'dishes', component: () => import('./views/Dishes.vue'), meta: { title: '菜品管理', roles: ['kitchen', 'admin'] } },
      { path: 'users', component: () => import('./views/Users.vue'), meta: { title: '用户管理', roles: ['admin'] } }
    ]
  }
]

const router = createRouter({ history: createWebHistory(), routes })

router.beforeEach((to) => {
  if (to.meta.public) return true
  if (!store.isLoggedIn) return '/login'
  if (to.meta.roles && !to.meta.roles.includes(store.role)) return '/'
  return true
})

export default router
