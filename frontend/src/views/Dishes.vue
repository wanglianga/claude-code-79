<template>
  <div class="page-card">
    <div class="flex-row" style="justify-content: space-between">
      <h3 class="page-title" style="margin:0">菜品管理</h3>
      <el-button type="primary" @click="openForm()">新增菜品</el-button>
    </div>
    <el-table :data="list" v-loading="loading" class="mt-12">
      <el-table-column prop="name" label="菜品" width="140" />
      <el-table-column label="价格" width="90">
        <template #default="{ row }">{{ fmtMoney(row.price) }}</template>
      </el-table-column>
      <el-table-column label="营养/适口" min-width="200">
        <template #default="{ row }">
          <el-tag v-if="row.low_salt" size="small" type="success" effect="plain">低盐</el-tag>
          <el-tag v-if="row.low_sugar" size="small" type="success" effect="plain" style="margin-left:4px">低糖</el-tag>
          <el-tag size="small" effect="plain" style="margin-left:4px">
            {{ { normal: '普通', soft: '软', mushy: '软烂' }[row.softness] }}
          </el-tag>
          <el-tag v-if="row.holiday_only" size="small" type="warning" effect="plain" style="margin-left:4px">节日特供</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="nutrition" label="营养说明" min-width="180" show-overflow-tooltip />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.available ? 'success' : 'info'" size="small">{{ row.available ? '在售' : '停售' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="90">
        <template #default="{ row }">
          <el-button size="small" @click="openForm(row)">编辑</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="formVisible" :title="form.id ? '编辑菜品' : '新增菜品'" width="520px">
      <el-form label-width="100px">
        <el-form-item label="菜品名称" required>
          <el-input v-model="form.name" :disabled="!!form.id" />
        </el-form-item>
        <el-form-item label="价格(元)" required>
          <el-input-number v-model="form.price" :min="0" :max="200" :precision="1" />
        </el-form-item>
        <el-form-item label="适口性">
          <el-checkbox v-model="form.low_salt">低盐</el-checkbox>
          <el-checkbox v-model="form.low_sugar">低糖</el-checkbox>
          <el-checkbox v-model="form.holiday_only">节日特供</el-checkbox>
        </el-form-item>
        <el-form-item label="软烂程度">
          <el-radio-group v-model="form.softness">
            <el-radio-button value="normal">普通</el-radio-button>
            <el-radio-button value="soft">软</el-radio-button>
            <el-radio-button value="mushy">软烂</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="营养说明">
          <el-input v-model="form.nutrition" />
        </el-form-item>
        <el-form-item label="过敏原">
          <el-input v-model="form.allergens" />
        </el-form-item>
        <el-form-item label="是否在售" v-if="form.id">
          <el-switch v-model="form.available" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
import { fmtMoney } from '../utils'

const list = ref([])
const loading = ref(false)
const acting = ref(false)
const formVisible = ref(false)
const emptyForm = {
  id: null, name: '', price: 5, low_salt: false, low_sugar: false, softness: 'normal',
  nutrition: '', allergens: '', holiday_only: false, available: true
}
const form = reactive({ ...emptyForm })

async function load() {
  loading.value = true
  try {
    list.value = await api.get('/dishes')
  } finally {
    loading.value = false
  }
}

function openForm(row) {
  Object.assign(form, emptyForm, row || {})
  formVisible.value = true
}

async function save() {
  if (!form.name) {
    ElMessage.warning('请填写菜品名称')
    return
  }
  acting.value = true
  try {
    if (form.id) {
      await api.put(`/dishes/${form.id}`, form)
    } else {
      await api.post('/dishes', form)
    }
    ElMessage.success('已保存')
    formVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>
