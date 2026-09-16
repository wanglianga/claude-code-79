<template>
  <el-container class="layout">
    <el-aside width="230px" class="aside">
      <div class="logo">
        <span class="logo-icon">🍱</span>
        <div>
          <div class="logo-title">养老助餐平台</div>
          <div class="logo-sub">配送·回收·核销一体化</div>
        </div>
      </div>
      <el-menu :default-active="$route.path" router background-color="#3d2c1e" text-color="#d8c8b8"
        active-text-color="#ffb26b">
        <el-menu-item v-for="m in menus" :key="m.path" :index="m.path">
          <el-icon><component :is="m.icon" /></el-icon>
          <span>{{ m.title }}</span>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <div class="crumb">{{ $route.meta.title || '工作台' }}</div>
        <div class="flex-row">
          <el-popover placement="bottom-end" :width="380" trigger="click">
            <template #reference>
              <el-badge :value="unread" :hidden="unread === 0" :max="99">
                <el-icon :size="20" style="cursor:pointer"><Bell /></el-icon>
              </el-badge>
            </template>
            <div class="flex-row" style="justify-content:space-between">
              <b>站内通知</b>
              <el-button link type="primary" size="small" @click="readAll">全部已读</el-button>
            </div>
            <el-divider style="margin:8px 0" />
            <div v-if="notifications.length === 0" class="muted">暂无通知</div>
            <div v-for="n in notifications" :key="n.id" class="notice" :class="{ unread: !n.read }"
              @click="goOrder(n)">
              <div class="flex-row" style="justify-content:space-between">
                <b>{{ n.title }}</b>
                <span class="muted">{{ fmtTime(n.created_at).slice(5, 16) }}</span>
              </div>
              <div class="muted">{{ n.content }}</div>
            </div>
          </el-popover>
          <el-tag type="warning" effect="plain">{{ roleNames[store.role] }}</el-tag>
          <el-dropdown @command="onCmd">
            <span style="cursor:pointer">{{ store.user?.name }} <el-icon><ArrowDown /></el-icon></span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="logout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>
      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { store, roleNames } from '../store'
import { fmtTime } from '../utils'
import api from '../api'

const router = useRouter()
const unread = ref(0)
const notifications = ref([])

const allMenus = [
  { path: '/', title: '工作台', icon: 'Monitor', roles: null },
  { path: '/order/new', title: '订餐下单', icon: 'ShoppingCart', roles: ['family', 'elder', 'community', 'admin'] },
  { path: '/orders', title: '餐单管理', icon: 'Tickets', roles: null },
  { path: '/elders', title: '老人档案', icon: 'UserFilled', roles: ['community', 'admin', 'finance'] },
  { path: '/kitchen', title: '厨房工作台', icon: 'Food', roles: ['kitchen', 'admin'] },
  { path: '/delivery', title: '配送任务', icon: 'Van', roles: ['rider', 'volunteer'] },
  { path: '/anomalies', title: '异常工单', icon: 'WarningFilled', roles: null },
  { path: '/status-changes', title: '状态变更清算', icon: 'Switch', roles: null },
  { path: '/boxes', title: '餐盒回收', icon: 'Refresh', roles: ['community', 'admin', 'finance'] },
  { path: '/volunteers-manage', title: '志愿者资质考核', icon: 'Avatar', roles: ['community', 'admin'] },
  { path: '/finance', title: '财政核销', icon: 'Money', roles: ['finance', 'admin'] },
  { path: '/subsidy-changes', title: '补贴变更记录', icon: 'Document', roles: ['finance', 'admin', 'community'] },
  { path: '/dishes', title: '菜品管理', icon: 'Dish', roles: ['kitchen', 'admin'] },
  { path: '/users', title: '用户管理', icon: 'Setting', roles: ['admin'] }
]

const menus = computed(() => allMenus.filter((m) => !m.roles || m.roles.includes(store.role)))

let timer = null
async function refreshUnread() {
  try {
    const d = await api.get('/notifications/unread-count')
    unread.value = d.count
  } catch (e) { /* 忽略 */ }
}
async function loadNotifications() {
  try {
    notifications.value = await api.get('/notifications')
  } catch (e) { /* 忽略 */ }
}
async function readAll() {
  await api.post('/notifications/read-all')
  await refreshUnread()
  await loadNotifications()
}
function goOrder(n) {
  if (n.order_id) router.push(`/orders/${n.order_id}`)
}
function onCmd(cmd) {
  if (cmd === 'logout') {
    store.logout()
    router.push('/login')
  }
}

onMounted(() => {
  refreshUnread()
  loadNotifications()
  timer = setInterval(() => {
    refreshUnread()
    loadNotifications()
  }, 15000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.layout { min-height: 100vh; }
.aside { background: #3d2c1e; }
.logo {
  display: flex; align-items: center; gap: 10px;
  padding: 18px 16px; color: #ffe8d1;
}
.logo-icon { font-size: 30px; }
.logo-title { font-weight: 700; font-size: 16px; }
.logo-sub { font-size: 11px; color: #c9a87f; }
.header {
  background: #fff; display: flex; align-items: center; justify-content: space-between;
  box-shadow: 0 1px 4px rgba(0,0,0,0.06); z-index: 2;
}
.crumb { font-weight: 600; font-size: 16px; }
.main { background: #f5f3ef; padding: 18px; }
.notice { padding: 8px 4px; border-bottom: 1px solid #f0e8de; cursor: pointer; }
.notice.unread b { color: #d9702b; }
.notice:hover { background: #fdf6ee; }
</style>
