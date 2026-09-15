<template>
  <div class="login-wrap">
    <div class="login-panel">
      <div class="brand">
        <div class="logo">🍱</div>
        <h1>养老助餐服务平台</h1>
        <p>配送 · 餐盒回收 · 补贴核销 一体化管理</p>
      </div>
      <el-form @submit.prevent="doLogin">
        <el-form-item>
          <el-input v-model="username" size="large" placeholder="用户名" :prefix-icon="User" />
        </el-form-item>
        <el-form-item>
          <el-input v-model="password" type="password" size="large" placeholder="密码"
            :prefix-icon="Lock" show-password @keyup.enter="doLogin" />
        </el-form-item>
        <el-button type="primary" size="large" style="width:100%" :loading="loading" @click="doLogin">
          登 录
        </el-button>
      </el-form>
      <el-divider content-position="left">演示账号（点击填充）</el-divider>
      <div class="demo-accounts">
        <el-tag v-for="a in demoAccounts" :key="a.u" class="acct" @click="fill(a)">
          {{ a.label }} {{ a.u }}
        </el-tag>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'

const router = useRouter()
const username = ref('')
const password = ref('')
const loading = ref(false)

const demoAccounts = [
  { label: '家属', u: 'family01', p: 'family123' },
  { label: '老人', u: 'elder01', p: 'elder123' },
  { label: '社区', u: 'community01', p: 'community123' },
  { label: '厨房', u: 'kitchen01', p: 'kitchen123' },
  { label: '骑手', u: 'rider01', p: 'rider123' },
  { label: '志愿者', u: 'volunteer01', p: 'volunteer123' },
  { label: '财政', u: 'finance01', p: 'finance123' },
  { label: '管理员', u: 'admin', p: 'admin123' }
]

function fill(a) {
  username.value = a.u
  password.value = a.p
}

async function doLogin() {
  if (!username.value || !password.value) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    const data = await api.post('/login', { username: username.value, password: password.value })
    store.setAuth(data.token, data.user)
    ElMessage.success(`欢迎，${data.user.name}`)
    router.push('/')
  } catch (e) {
    // 拦截器已提示
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #fdf3e7 0%, #f7e4cf 50%, #f0d5b8 100%);
}
.login-panel {
  width: 420px;
  max-width: 92vw;
  background: #fff;
  border-radius: 14px;
  padding: 36px 36px 28px;
  box-shadow: 0 12px 40px rgba(180, 110, 40, 0.18);
}
.brand { text-align: center; margin-bottom: 22px; }
.brand .logo { font-size: 44px; }
.brand h1 { font-size: 22px; margin: 8px 0 4px; color: #7a4a1a; }
.brand p { color: #a0855c; font-size: 13px; margin: 0; }
.demo-accounts { display: flex; flex-wrap: wrap; gap: 8px; }
.acct { cursor: pointer; }
.acct:hover { background: #fdf0e2; }
</style>
