<template>
  <div>
    <div class="page-card">
      <h3 class="page-title">订餐下单（{{ sourceText }}）</h3>
      <el-form label-width="110px" style="max-width: 760px">
        <el-form-item label="选择老人" required>
          <el-select v-model="form.elder_id" placeholder="请选择老人" style="width: 100%" @change="onElderChange">
            <el-option v-for="e in elders" :key="e.id" :value="e.id"
              :label="`${e.name}（${subsidyLevels[e.subsidy_level]} ${e.subsidy_per_meal}元/餐）`" />
          </el-select>
        </el-form-item>
        <template v-if="elder">
          <el-form-item label="老人档案">
            <el-alert type="warning" :closable="false" show-icon>
              <template #title>
                <div class="flex-row">
                  <span>{{ elder.address }}</span>
                  <el-tag v-if="elder.dietary_restrictions" type="danger" size="small">禁忌：{{ elder.dietary_restrictions }}</el-tag>
                  <el-tag v-if="elder.need_knock_confirm" size="small">需敲门确认</el-tag>
                  <el-tag v-if="elder.strict_mode" type="danger" size="small" effect="dark">严格签收</el-tag>
                </div>
              </template>
              <div class="muted">
                餐盒回收：{{ boxMethods[elder.box_return_method] }} ·
                紧急联系人：{{ elder.emergency_contact_name }} {{ elder.emergency_contact_phone }}
                <span v-if="elder.cognitive_impairment"> · 认知障碍</span>
                <span v-if="elder.living_alone"> · 独居</span>
                <span v-if="elder.mobility_impaired"> · 行动不便</span>
              </div>
            </el-alert>
          </el-form-item>
        </template>
        <el-form-item label="用餐日期" required>
          <el-date-picker v-model="form.meal_date" type="date" value-format="YYYY-MM-DD"
            :disabled-date="(d) => d.getTime() < Date.now() - 86400000" />
        </el-form-item>
        <el-form-item label="餐别">
          <el-radio-group v-model="form.meal_type">
            <el-radio-button value="lunch">午餐</el-radio-button>
            <el-radio-button value="dinner">晚餐</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="取餐方式">
          <el-radio-group v-model="form.delivery_type">
            <el-radio-button value="home">配送到家</el-radio-button>
            <el-radio-button value="community_pickup">社区食堂自取</el-radio-button>
            <el-radio-button value="volunteer">志愿者帮送</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="节日加餐">
          <el-switch v-model="form.is_holiday_special" />
          <el-input v-if="form.is_holiday_special" v-model="form.holiday_name" placeholder="节日名称，如：中秋节"
            style="width: 200px; margin-left: 10px" />
          <span v-if="form.is_holiday_special" class="muted" style="margin-left:10px">节日加餐额外补贴 5 元</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.notes" placeholder="如：饭量减半、门口鞋柜上取餐等" />
        </el-form-item>
      </el-form>
    </div>

    <div class="page-card">
      <h3 class="page-title">选择菜品（营养与适口性已标注）</h3>
      <el-table :data="dishes" v-loading="loadingDishes">
        <el-table-column prop="name" label="菜品" min-width="130" />
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
            <div class="muted">{{ row.nutrition }}</div>
          </template>
        </el-table-column>
        <el-table-column label="数量" width="150">
          <template #default="{ row }">
            <el-input-number v-model="qty[row.id]" :min="0" :max="5" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="个性化要求" min-width="160">
          <template #default="{ row }">
            <el-input v-model="notes[row.id]" size="small" placeholder="如：少盐、剪碎" :disabled="!qty[row.id]" />
          </template>
        </el-table-column>
      </el-table>

      <el-divider />
      <div class="flex-row" style="justify-content: flex-end; font-size: 15px">
        <span>合计：<b>{{ fmtMoney(total) }}</b></span>
        <span>补贴（{{ elder ? subsidyLevels[elder.subsidy_level] : '-' }}）：<b class="danger-text">-{{ fmtMoney(subsidy) }}</b></span>
        <span v-if="form.is_holiday_special">节日加餐补贴：<b class="danger-text">-{{ fmtMoney(holidayExtra) }}</b></span>
        <span>老人自付：<b style="color:#d9702b; font-size:20px">{{ fmtMoney(payable) }}</b></span>
        <el-button type="primary" size="large" :disabled="!canSubmit" :loading="submitting" @click="submit">
          提交订餐
        </el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { subsidyLevels, boxMethods, fmtMoney } from '../utils'

const router = useRouter()
const elders = ref([])
const dishes = ref([])
const loadingDishes = ref(false)
const submitting = ref(false)
const qty = reactive({})
const notes = reactive({})

const form = reactive({
  elder_id: null,
  meal_date: new Date().toISOString().slice(0, 10),
  meal_type: 'lunch',
  delivery_type: 'home',
  is_holiday_special: false,
  holiday_name: '',
  notes: ''
})

const elder = computed(() => elders.value.find((e) => e.id === form.elder_id) || null)
const sourceText = computed(() => ({ family: '家属代订', elder: '老人自订', community: '社区代订', admin: '社区代订' }[store.role] || '订餐'))

const total = computed(() =>
  dishes.value.reduce((sum, d) => sum + (qty[d.id] || 0) * d.price, 0)
)
const subsidy = computed(() => {
  if (!elder.value) return 0
  return Math.min(elder.value.subsidy_per_meal, total.value)
})
const holidayExtra = computed(() => {
  if (!form.is_holiday_special) return 0
  return Math.min(5, total.value - subsidy.value)
})
const payable = computed(() => total.value - subsidy.value - holidayExtra.value)
const canSubmit = computed(() => form.elder_id && form.meal_date && total.value > 0)

function onElderChange() {
  // 切换老人时保留菜品选择
}

async function submit() {
  const items = dishes.value
    .filter((d) => (qty[d.id] || 0) > 0)
    .map((d) => ({ dish_id: d.id, qty: qty[d.id], custom_note: notes[d.id] || '' }))
  if (items.length === 0) return
  if (form.is_holiday_special && !form.holiday_name) {
    ElMessage.warning('请填写节日名称')
    return
  }
  submitting.value = true
  try {
    const res = await api.post('/orders', { ...form, items })
    ElMessageBox.alert(
      `单号 ${res.order_no}，餐费 ${fmtMoney(res.total_amount)}，补贴 ${fmtMoney(res.subsidy_amount)}，自付 ${fmtMoney(res.payable_amount)}`,
      '订餐成功',
      { confirmButtonText: '查看餐单' }
    ).then(() => router.push('/orders'))
  } catch (e) {
    // 已提示
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  loadingDishes.value = true
  try {
    const [es, ds] = await Promise.all([api.get('/elders'), api.get('/dishes')])
    elders.value = es.filter((e) => e.active)
    dishes.value = ds.filter((d) => d.available)
    dishes.value.forEach((d) => { qty[d.id] = 0; notes[d.id] = '' })
    if (elders.value.length === 1) form.elder_id = elders.value[0].id
  } finally {
    loadingDishes.value = false
  }
})
</script>
