<template>
  <div class="page-card">
    <div class="flex-row" style="justify-content: space-between">
      <h3 class="page-title" style="margin:0">用户管理</h3>
      <el-button type="primary" @click="openForm()">新增用户</el-button>
    </div>
    <el-table :data="list" v-loading="loading" class="mt-12">
      <el-table-column prop="username" label="用户名" width="130" />
      <el-table-column prop="name" label="姓名" width="110" />
      <el-table-column label="角色" width="130">
        <template #default="{ row }">
          <el-tag size="small">{{ roleNames[row.role] }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="phone" label="电话" width="140" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'" size="small">{{ row.active ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" min-width="200">
        <template #default="{ row }">
          <el-button size="small" :type="row.active ? 'danger' : 'success'" plain @click="toggle(row)">
            {{ row.active ? '停用' : '启用' }}
          </el-button>
          <el-button size="small" @click="resetPw(row)">重置密码</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="formVisible" title="新增用户" width="440px">
      <el-form label-width="90px">
        <el-form-item label="用户名" required><el-input v-model="form.username" /></el-form-item>
        <el-form-item label="姓名" required><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="密码" required><el-input v-model="form.password" placeholder="至少 6 位" /></el-form-item>
        <el-form-item label="电话"><el-input v-model="form.phone" /></el-form-item>
        <el-form-item label="角色" required>
          <el-select v-model="form.role" style="width:100%">
            <el-option v-for="(v, k) in roleNames" :key="k" :label="v" :value="k" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="save">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { roleNames } from '../store'

const list = ref([])
const loading = ref(false)
const acting = ref(false)
const formVisible = ref(false)
const form = reactive({ username: '', name: '', password: '', phone: '', role: 'family' })

async function load() {
  loading.value = true
  try {
    list.value = await api.get('/admin/users')
  } finally {
    loading.value = false
  }
}

function openForm() {
  Object.assign(form, { username: '', name: '', password: '', phone: '', role: 'family' })
  formVisible.value = true
}

async function save() {
  acting.value = true
  try {
    await api.post('/admin/users', form)
    ElMessage.success('用户已创建')
    formVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

async function toggle(row) {
  await api.put(`/admin/users/${row.id}`, { active: !row.active })
  ElMessage.success('已更新')
  await load()
}

async function resetPw(row) {
  const { value } = await ElMessageBox.prompt(`为 ${row.name} 设置新密码`, '重置密码', {
    inputPattern: /^.{6,}$/,
    inputErrorMessage: '密码至少 6 位'
  })
  await api.put(`/admin/users/${row.id}`, { password: value })
  ElMessage.success('密码已重置')
}

onMounted(load)
</script>
