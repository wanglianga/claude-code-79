<template>
  <div v-loading="loading">
    <template v-if="e">
      <div class="page-card">
        <div class="flex-row" style="justify-content: space-between">
          <h3 class="page-title" style="margin:0">
            {{ e.name }} 的档案
            <el-tag :type="serviceStatusMap[e.service_status||'active']?.type" size="small" effect="dark" style="margin-left:8px">
              {{ serviceStatusMap[e.service_status||'active']?.text }}
            </el-tag>
          </h3>
          <div class="flex-row">
            <el-tag v-if="e.monthly_quota>0" type="warning" effect="plain">
              本月补贴可享 {{ e.monthly_remaining }}/{{ e.monthly_quota }} 次（已用 {{ e.monthly_used }}）
            </el-tag>
            <el-tag v-else type="info" effect="plain">补贴次数不限</el-tag>
            <el-button v-if="canStatusChange" type="danger" plain @click="goStatus">住院/转院/搬离/去世清算</el-button>
            <el-button v-if="canSubsidy" type="warning" @click="subsidyVisible = true">补贴资格变更</el-button>
          </div>
        </div>
        <el-descriptions :column="3" border class="mt-12">
          <el-descriptions-item label="身份证号">{{ e.id_card }}</el-descriptions-item>
          <el-descriptions-item label="性别">{{ e.gender }}</el-descriptions-item>
          <el-descriptions-item label="出生日期">{{ e.birth_date || '—' }}</el-descriptions-item>
          <el-descriptions-item label="联系电话">{{ e.phone || '—' }}</el-descriptions-item>
          <el-descriptions-item label="送餐地址">{{ e.address }}</el-descriptions-item>
          <el-descriptions-item label="饮食禁忌">
            <span class="danger-text">{{ e.dietary_restrictions || '无' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="补贴资格">
            <el-tag :type="e.subsidy_level === 'full' ? 'success' : e.subsidy_level === 'partial' ? 'warning' : 'info'">
              {{ subsidyLevels[e.subsidy_level] }} {{ e.subsidy_per_meal }} 元/餐
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="敲门确认">{{ e.need_knock_confirm ? '需要' : '不需要' }}</el-descriptions-item>
          <el-descriptions-item label="餐盒回收">{{ boxMethods[e.box_return_method] }}</el-descriptions-item>
          <el-descriptions-item label="紧急联系人">{{ e.emergency_contact_name }} {{ e.emergency_contact_phone }}</el-descriptions-item>
          <el-descriptions-item label="特殊照护">
            <el-tag v-if="e.cognitive_impairment" type="danger" size="small">认知障碍</el-tag>
            <el-tag v-if="e.living_alone" type="danger" size="small" style="margin-left:4px">独居</el-tag>
            <el-tag v-if="e.mobility_impaired" type="danger" size="small" style="margin-left:4px">行动不便</el-tag>
            <span v-if="!e.cognitive_impairment && !e.living_alone && !e.mobility_impaired">无</span>
          </el-descriptions-item>
          <el-descriptions-item label="风险标签">
            <el-tag :type="e.risk_level === 'high' ? 'danger' : e.risk_level === 'attention' ? 'warning' : 'success'"
              :effect="e.risk_level === 'normal' ? 'plain' : 'dark'">
              {{ { normal: '正常', attention: '关注', high: '高风险' }[e.risk_level] }}
            </el-tag>
            <el-tag v-if="e.focus_until" type="danger" size="small" effect="dark" style="margin-left:4px">
              重点关注至 {{ e.focus_until }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="配送方式">
            {{ e.delivery_confirm_mode === 'phone_first' ? '电话确认后再上门' : '直接上门' }}
            <span v-if="e.no_answer_count > 0" class="danger-text">（连续未开门 {{ e.no_answer_count }} 次）</span>
          </el-descriptions-item>
          <el-descriptions-item label="社区备注">{{ e.community_note || '—' }}</el-descriptions-item>
        </el-descriptions>
      </div>

      <el-row :gutter="16">
        <el-col :span="14">
          <div class="page-card">
            <h3 class="page-title">近期餐单</h3>
            <el-table :data="e.recent_orders" size="small" @row-click="(r) => $router.push(`/orders/${r.id}`)"
              row-style="cursor:pointer">
              <el-table-column prop="order_no" label="单号" width="150" />
              <el-table-column prop="meal_date" label="日期" width="110" />
              <el-table-column label="金额/补贴" width="140">
                <template #default="{ row }">{{ fmtMoney(row.total_amount) }} / {{ fmtMoney(row.subsidy_amount) }}</template>
              </el-table-column>
              <el-table-column label="状态">
                <template #default="{ row }">
                  <el-tag :type="orderStatus[row.status]?.type" size="small">{{ orderStatus[row.status]?.text }}</el-tag>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-col>
        <el-col :span="10">
          <div class="page-card">
            <h3 class="page-title">补贴资格变更记录</h3>
            <div v-if="!e.subsidy_changes || e.subsidy_changes.length === 0" class="muted">暂无变更</div>
            <el-timeline v-else>
              <el-timeline-item v-for="ch in e.subsidy_changes" :key="ch.id" :timestamp="fmtTime(ch.created_at)">
                {{ subsidyLevels[ch.old_level] }}({{ ch.old_amount }}元) →
                <b>{{ subsidyLevels[ch.new_level] }}({{ ch.new_amount }}元)</b>
                <div class="muted">{{ ch.reason }} · 操作人：{{ ch.changed_by }}</div>
              </el-timeline-item>
            </el-timeline>
          </div>
        </el-col>
      </el-row>

      <el-dialog v-model="subsidyVisible" title="补贴资格变更（在途餐单将自动重算并通知财政）" width="480px">
        <el-form label-width="100px">
          <el-form-item label="当前资格">
            <el-tag>{{ subsidyLevels[e.subsidy_level] }} {{ e.subsidy_per_meal }} 元/餐</el-tag>
          </el-form-item>
          <el-form-item label="新资格" required>
            <el-select v-model="subsidyForm.new_level" style="width:100%">
              <el-option label="无补贴" value="none" />
              <el-option label="部分补贴" value="partial" />
              <el-option label="全额补贴" value="full" />
            </el-select>
          </el-form-item>
          <el-form-item label="每餐补贴(元)" required>
            <el-input-number v-model="subsidyForm.new_amount" :min="0" :max="100" />
          </el-form-item>
          <el-form-item label="变更原因" required>
            <el-input v-model="subsidyForm.reason" type="textarea" placeholder="如：街道复核调整、低保资格变动" />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="subsidyVisible = false">取消</el-button>
          <el-button type="warning" :loading="acting" @click="doSubsidyChange">确认变更</el-button>
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { subsidyLevels, boxMethods, orderStatus, serviceStatusMap, fmtTime, fmtMoney } from '../utils'

const route = useRoute()
const router = useRouter()
const e = ref(null)
const loading = ref(false)
const acting = ref(false)
const subsidyVisible = ref(false)
const subsidyForm = reactive({ new_level: 'partial', new_amount: 0, reason: '' })

const canSubsidy = computed(() => ['community', 'admin', 'finance'].includes(store.role))
const canStatusChange = computed(() => ['community', 'admin'].includes(store.role) && e.value?.active && e.value?.service_status === 'active')
function goStatus() {
  router.push('/status-changes')
}

async function load() {
  loading.value = true
  try {
    e.value = await api.get(`/elders/${route.params.id}`)
    subsidyForm.new_level = e.value.subsidy_level
    subsidyForm.new_amount = e.value.subsidy_per_meal
  } finally {
    loading.value = false
  }
}

async function doSubsidyChange() {
  if (!subsidyForm.reason) {
    ElMessage.warning('请填写变更原因')
    return
  }
  await ElMessageBox.confirm(
    '变更后，该老人未出餐的在途餐单将按新资格重算补贴，并生成「补贴资格变更」异常工单通知财政与社区。确认变更？',
    '补贴资格变更',
    { type: 'warning' }
  )
  acting.value = true
  try {
    const res = await api.post(`/elders/${e.value.id}/subsidy`, subsidyForm)
    ElMessage.success(`变更成功，${res.affected_orders} 单在途餐单已重算`)
    subsidyVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>
