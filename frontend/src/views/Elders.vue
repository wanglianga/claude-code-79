<template>
  <div class="page-card">
    <div class="flex-row" style="justify-content: space-between">
      <h3 class="page-title" style="margin:0">老人档案</h3>
      <div class="flex-row">
        <el-input v-model="q" placeholder="姓名 / 身份证 / 地址" style="width: 220px" clearable @change="load" />
        <el-button @click="load" :icon="Search">查询</el-button>
        <el-button v-if="canEdit" type="primary" @click="openForm()">新建档案</el-button>
      </div>
    </div>
    <el-table :data="list" v-loading="loading" class="mt-12">
      <el-table-column prop="name" label="姓名" width="90">
        <template #default="{ row }">
          <el-link type="primary" @click="$router.push(`/elders/${row.id}`)">{{ row.name }}</el-link>
        </template>
      </el-table-column>
      <el-table-column prop="gender" label="性别" width="60" />
      <el-table-column prop="address" label="送餐地址" min-width="180" show-overflow-tooltip />
      <el-table-column label="补贴资格" width="130">
        <template #default="{ row }">
          <el-tag :type="row.subsidy_level === 'full' ? 'success' : row.subsidy_level === 'partial' ? 'warning' : 'info'" size="small">
            {{ subsidyLevels[row.subsidy_level] }} {{ row.subsidy_per_meal }}元/餐
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="饮食禁忌" min-width="120">
        <template #default="{ row }">
          <span class="danger-text">{{ row.dietary_restrictions || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="特殊照护" width="150">
        <template #default="{ row }">
          <el-tag v-if="row.cognitive_impairment" type="danger" size="small">认知障碍</el-tag>
          <el-tag v-if="row.living_alone" type="danger" size="small" style="margin-left:2px">独居</el-tag>
          <el-tag v-if="row.mobility_impaired" type="danger" size="small" style="margin-left:2px">行动不便</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="敲门确认" width="90">
        <template #default="{ row }">{{ row.need_knock_confirm ? '需要' : '—' }}</template>
      </el-table-column>
      <el-table-column label="餐盒回收" width="110">
        <template #default="{ row }">{{ boxMethods[row.box_return_method] }}</template>
      </el-table-column>
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button v-if="canEdit" size="small" @click="openForm(row)">编辑</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="formVisible" :title="form.id ? '编辑老人档案' : '新建老人档案'" width="640px">
      <el-form label-width="110px">
        <el-row :gutter="10">
          <el-col :span="12"><el-form-item label="姓名" required><el-input v-model="form.name" /></el-form-item></el-col>
          <el-col :span="12"><el-form-item label="身份证号" required><el-input v-model="form.id_card" /></el-form-item></el-col>
          <el-col :span="12">
            <el-form-item label="性别">
              <el-radio-group v-model="form.gender">
                <el-radio value="女">女</el-radio><el-radio value="男">男</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="出生日期">
              <el-date-picker v-model="form.birth_date" type="date" value-format="YYYY-MM-DD" style="width:100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12"><el-form-item label="联系电话"><el-input v-model="form.phone" /></el-form-item></el-col>
          <el-col :span="12">
            <el-form-item label="补贴资格">
              <el-select v-model="form.subsidy_level" style="width:100%">
                <el-option label="无补贴" value="none" />
                <el-option label="部分补贴" value="partial" />
                <el-option label="全额补贴" value="full" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="24"><el-form-item label="送餐地址" required><el-input v-model="form.address" /></el-form-item></el-col>
          <el-col :span="12">
            <el-form-item label="每餐补贴(元)">
              <el-input-number v-model="form.subsidy_per_meal" :min="0" :max="100" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="餐盒回收方式">
              <el-select v-model="form.box_return_method" style="width:100%">
                <el-option v-for="(v, k) in boxMethods" :key="k" :label="v" :value="k" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item label="饮食禁忌">
              <el-input v-model="form.dietary_restrictions" placeholder="如：低盐;低糖;软烂;忌海鲜" />
            </el-form-item>
          </el-col>
          <el-col :span="12"><el-form-item label="紧急联系人"><el-input v-model="form.emergency_contact_name" /></el-form-item></el-col>
          <el-col :span="12"><el-form-item label="紧急联系电话"><el-input v-model="form.emergency_contact_phone" /></el-form-item></el-col>
          <el-col :span="24">
            <el-form-item label="照护标记">
              <el-checkbox v-model="form.need_knock_confirm">需敲门确认</el-checkbox>
              <el-checkbox v-model="form.cognitive_impairment">认知障碍</el-checkbox>
              <el-checkbox v-model="form.living_alone">独居</el-checkbox>
              <el-checkbox v-model="form.mobility_impaired">行动不便</el-checkbox>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="绑定家属账号">
              <el-select v-model="form.family_user_id" clearable style="width:100%">
                <el-option v-for="u in familyUsers" :key="u.id" :label="`${u.name}（${u.username}）`" :value="u.id" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="24"><el-form-item label="社区备注"><el-input v-model="form.community_note" type="textarea" /></el-form-item></el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { subsidyLevels, boxMethods } from '../utils'

const list = ref([])
const loading = ref(false)
const acting = ref(false)
const q = ref('')
const formVisible = ref(false)
const familyUsers = ref([])
const emptyForm = {
  id: null, name: '', id_card: '', gender: '女', birth_date: '', phone: '', address: '',
  subsidy_level: 'partial', subsidy_per_meal: 6, dietary_restrictions: '', need_knock_confirm: false,
  box_return_method: 'next_delivery', emergency_contact_name: '', emergency_contact_phone: '',
  cognitive_impairment: false, living_alone: false, mobility_impaired: false,
  family_user_id: null, community_note: ''
}
const form = reactive({ ...emptyForm })

const canEdit = computed(() => ['community', 'admin'].includes(store.role))

async function load() {
  loading.value = true
  try {
    list.value = await api.get('/elders', { params: q.value ? { q: q.value } : {} })
  } finally {
    loading.value = false
  }
}

function openForm(row) {
  Object.assign(form, emptyForm, row || {})
  formVisible.value = true
}

async function save() {
  if (!form.name || !form.id_card || !form.address) {
    ElMessage.warning('请填写姓名、身份证号和送餐地址')
    return
  }
  acting.value = true
  try {
    if (form.id) {
      await api.put(`/elders/${form.id}`, form)
    } else {
      await api.post('/elders', form)
    }
    ElMessage.success('档案已保存')
    formVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

onMounted(async () => {
  await load()
  if (store.role === 'admin') {
    const users = await api.get('/admin/users')
    familyUsers.value = users.filter((u) => u.role === 'family' && u.active)
  }
})
</script>
