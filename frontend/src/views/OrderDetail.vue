<template>
  <div v-loading="loading">
    <template v-if="o">
      <div class="page-card">
        <div class="flex-row" style="justify-content: space-between">
          <h3 class="page-title" style="margin:0">餐单 {{ o.order_no }}</h3>
          <div class="flex-row">
            <el-tag :type="orderStatus[o.status]?.type" size="large">{{ orderStatus[o.status]?.text || o.status }}</el-tag>
            <el-tag v-if="o.strict_mode" type="danger" effect="dark">严格签收</el-tag>
            <el-tag v-if="o.is_holiday_special" type="warning">{{ o.holiday_name || '节日加餐' }}</el-tag>
          </div>
        </div>
        <el-descriptions :column="3" border class="mt-12">
          <el-descriptions-item label="老人">{{ o.elder.name }}（{{ o.elder.phone || '无电话' }}）</el-descriptions-item>
          <el-descriptions-item label="用餐日期">{{ o.meal_date }} {{ mealTypes[o.meal_type] }}</el-descriptions-item>
          <el-descriptions-item label="取餐方式">{{ deliveryTypes[o.delivery_type] }}</el-descriptions-item>
          <el-descriptions-item label="送餐地址">{{ o.address }}</el-descriptions-item>
          <el-descriptions-item label="饮食禁忌">
            <span class="danger-text">{{ o.elder.dietary_restrictions || '无' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="紧急联系人">{{ o.elder.emergency_contact_name }} {{ o.elder.emergency_contact_phone }}</el-descriptions-item>
          <el-descriptions-item label="下单来源">{{ orderSources[o.order_source] }}</el-descriptions-item>
          <el-descriptions-item v-if="o.proxy && o.proxy.name" label="家属代订">
            <el-tag type="warning" size="small">代订人：{{ o.proxy.name }}（{{ o.proxy.relation }}·{{ o.proxy.auth_method }}）</el-tag>
            <div class="muted" style="font-size:12px">实际用餐人：{{ o.elder.name }}；{{ o.proxy.contact_phone }}</div>
          </el-descriptions-item>
          <el-descriptions-item v-if="o.sign_basis" label="签收依据">
            <el-tag :type="(signBasisMap[o.sign_basis]||{}).type||'info'" size="small">{{ o.sign_basis_name }}</el-tag>
            <el-tag v-if="o.sign_effectiveness" :type="(effectivenessMap[o.sign_effectiveness]||{}).type||'info'" size="small" style="margin-left:4px">
              {{ o.effectiveness_name }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="敲门确认">{{ o.need_knock_confirm ? '需要' : '不需要' }}</el-descriptions-item>
          <el-descriptions-item label="餐盒回收">
            {{ boxMethods[o.box_return_method] }}（{{ o.boxes_issued }} 个）
            <el-tag v-if="o.elder.box_policy === 'disposable'" type="warning" size="small" style="margin-left:4px">一次性餐盒</el-tag>
            <el-tag v-else-if="o.elder.box_policy === 'paused'" type="danger" size="small" style="margin-left:4px">暂停发放</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="特殊照护">
            <el-tag v-if="o.elder.cognitive_impairment" type="danger" size="small">认知障碍</el-tag>
            <el-tag v-if="o.elder.living_alone" type="danger" size="small" style="margin-left:4px">独居</el-tag>
            <el-tag v-if="o.elder.mobility_impaired" type="danger" size="small" style="margin-left:4px">行动不便</el-tag>
            <span v-if="!o.elder.cognitive_impairment && !o.elder.living_alone && !o.elder.mobility_impaired">无</span>
          </el-descriptions-item>
          <el-descriptions-item label="餐费">{{ fmtMoney(o.total_amount) }}</el-descriptions-item>
          <el-descriptions-item label="补贴">
            <span class="danger-text">{{ fmtMoney(o.subsidy_amount) }}</span>
            <span v-if="o.holiday_extra > 0" class="muted">（含节日加餐 {{ fmtMoney(o.holiday_extra) }}）</span>
          </el-descriptions-item>
          <el-descriptions-item label="老人自付">{{ fmtMoney(o.payable_amount) }}
            <span v-if="o.refund_amount > 0" class="warn-text">（已退 {{ fmtMoney(o.refund_amount) }}）</span>
          </el-descriptions-item>
          <el-descriptions-item v-if="o.notes" label="备注">{{ o.notes }}</el-descriptions-item>
          <el-descriptions-item v-if="o.cancel_reason" label="退餐原因">{{ o.cancel_reason }}</el-descriptions-item>
          <el-descriptions-item v-if="o.settled_in" label="财政档案">已归档于核销单 #{{ o.settled_in }}</el-descriptions-item>
        </el-descriptions>

        <div class="flex-row mt-12">
          <el-button v-if="canModify" type="warning" @click="modifyVisible = true">临时改餐</el-button>
          <el-button v-if="canCancel" type="danger" plain @click="cancelVisible = true">退餐/取消</el-button>
          <el-button v-if="canPickupConfirm" type="primary" @click="pickupVisible = true">现场取餐签收</el-button>
          <el-button v-if="canFeedback" type="success" plain @click="feedbackVisible = true">用餐反馈</el-button>
          <el-button v-if="canConfirm" type="primary" plain @click="confirmVisible = true">回访老人本人/核实签收</el-button>
        </div>
      </div>

      <!-- 志愿者帮送核验与签收效力 -->
      <div v-if="o.delivery && (o.delivery.deliverer_type==='volunteer' || o.delivery.effectiveness)" class="page-card">
        <h3 class="page-title">志愿者帮送与签收效力</h3>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="所属组织">{{ o.delivery.vol_org || '—' }}</el-descriptions-item>
          <el-descriptions-item label="与老人关系">{{ o.delivery.relation_to_elder || '—' }}</el-descriptions-item>
          <el-descriptions-item label="知情同意">{{ o.delivery.informed_consent ? ('已同意（'+o.delivery.consent_by+'）') : '—' }}</el-descriptions-item>
          <el-descriptions-item label="签收效力">
            <el-tag :type="(effectivenessMap[o.delivery.effectiveness]||{}).type||'info'" size="small">
              {{ effectivenessName(o.delivery.effectiveness) }}
            </el-tag>
            <span class="muted" style="margin-left:6px">{{ o.delivery.effectiveness_reason }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </div>

      <!-- 老人本人/同住人回访 -->
      <div v-if="o.elder_confirmations && o.elder_confirmations.length" class="page-card">
        <h3 class="page-title">老人本人/同住人回访（代订不得代老人放弃权益）</h3>
        <el-timeline>
          <el-timeline-item v-for="(c, i) in o.elder_confirmations" :key="i" :timestamp="fmtTime(c.created_at)">
            <el-tag size="small" :type="c.confirms_received?'success':'danger'">{{ c.confirmer_role==='elder'?'老人本人':'同住人' }}·{{ c.confirms_received?'确认收到用餐':'否认/异常' }}</el-tag>
            <el-tag v-if="c.body_discomfort" type="danger" size="small" effect="dark" style="margin-left:4px">身体不适</el-tag>
            <span style="margin-left:6px">{{ c.confirmer_name }}（{{ {phone:'电话',visit:'上门',onsite:'现场'}[c.method] }}）</span>
            <div class="muted">口味：{{ c.taste_feedback || '—' }}；{{ c.note }}</div>
          </el-timeline-item>
        </el-timeline>
      </div>

      <el-row :gutter="16">
        <el-col :span="12">
          <div class="page-card">
            <h3 class="page-title">菜品明细</h3>
            <el-table :data="o.items" size="small">
              <el-table-column prop="dish_name" label="菜品" />
              <el-table-column label="单价" width="90">
                <template #default="{ row }">{{ fmtMoney(row.price) }}</template>
              </el-table-column>
              <el-table-column prop="qty" label="数量" width="70" />
              <el-table-column prop="custom_note" label="个性化" />
            </el-table>
          </div>

          <div class="page-card" v-if="o.delivery">
            <h3 class="page-title">配送信息</h3>
            <el-descriptions :column="2" border size="small">
              <el-descriptions-item label="配送员">{{ o.delivery.deliverer_name || '待接单' }}
                （{{ o.delivery.deliverer_type === 'volunteer' ? '志愿者' : '骑手' }}）</el-descriptions-item>
              <el-descriptions-item label="状态">{{ deliveryStatus[o.delivery.status]?.text || o.delivery.status }}</el-descriptions-item>
              <el-descriptions-item label="保温箱">{{ o.delivery.thermal_box_no || '—' }}</el-descriptions-item>
              <el-descriptions-item label="路线">{{ o.delivery.route_info || '—' }}</el-descriptions-item>
              <el-descriptions-item label="取餐时间">{{ fmtTime(o.delivery.pickup_time) }}</el-descriptions-item>
              <el-descriptions-item label="送达时间">
                {{ fmtTime(o.delivery.delivered_time) }}
                <el-tag v-if="o.delivery.is_timeout" type="danger" size="small">超时</el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="签收人">{{ o.delivery.signed_by_name || '—' }}</el-descriptions-item>
              <el-descriptions-item label="敲门确认">{{ o.delivery.knock_confirmed ? '已确认' : '未确认' }}</el-descriptions-item>
            </el-descriptions>
            <div v-if="o.delivery.sign_photo_url" class="mt-12">
              <div class="muted mb-12">送达照片：</div>
              <el-image :src="o.delivery.sign_photo_url" fit="cover" style="width: 220px; height: 150px; border-radius: 8px"
                :preview-src-list="[o.delivery.sign_photo_url]" />
            </div>
          </div>

          <div class="page-card" v-if="o.contact_attempts && o.contact_attempts.length">
            <h3 class="page-title">未开门联系尝试记录</h3>
            <div v-for="(a, i) in o.contact_attempts" :key="i" class="mb-12">
              <div class="flex-row">
                <el-tag :type="a.knock_done ? 'success' : 'info'" size="small">敲门{{ a.knock_done ? '✓' : '✗' }}</el-tag>
                <el-tag :type="a.phone_done ? 'success' : 'info'" size="small">电话{{ a.phone_done ? '✓' : '✗' }}</el-tag>
                <el-tag :type="a.neighbor_done ? 'success' : 'info'" size="small">邻里询问{{ a.neighbor_done ? '✓' : '✗' }}</el-tag>
                <el-tag :type="a.family_done ? 'success' : 'info'" size="small">家属联系{{ a.family_done ? '✓' : '✗' }}</el-tag>
              </div>
              <div class="muted">{{ a.note }}（{{ fmtTime(a.created_at) }}）</div>
            </div>
          </div>

          <div class="page-card" v-if="o.box_record">
            <h3 class="page-title">餐盒回收</h3>
            <div class="flex-row">
              <el-tag :type="boxStatus[o.box_record.status]?.type" size="large">
                {{ boxStatus[o.box_record.status]?.text }}
              </el-tag>
              <span>已回收 {{ o.box_record.boxes_returned }} / {{ o.box_record.boxes_issued }} 个（{{ boxMethods[o.box_record.return_method] }}）</span>
            </div>
          </div>

          <div class="page-card" v-if="o.feedbacks && o.feedbacks.length">
            <h3 class="page-title">用餐反馈</h3>
            <div v-for="(f, i) in o.feedbacks" :key="i" class="mb-12">
              <el-rate :model-value="f.rating" disabled size="small" />
              <el-tag :type="f.suitable ? 'success' : 'danger'" size="small" style="margin-left:8px">
                {{ f.suitable ? '饭菜适合' : '饭菜不适合' }}
              </el-tag>
              <div class="muted">{{ f.content }}（{{ fmtTime(f.created_at) }}）</div>
            </div>
          </div>
        </el-col>

        <el-col :span="12">
          <div class="page-card">
            <h3 class="page-title">状态时间线（各端一致）</h3>
            <el-timeline>
              <el-timeline-item v-for="(e, i) in o.events" :key="i" :timestamp="fmtTime(e.time)"
                :type="i === o.events.length - 1 ? 'primary' : ''">
                <b>{{ e.action }}</b>
                <div class="muted">{{ e.actor }} · {{ e.detail }}</div>
              </el-timeline-item>
            </el-timeline>
          </div>

          <div class="page-card" v-if="o.anomalies && o.anomalies.length">
            <h3 class="page-title">关联异常工单</h3>
            <div v-for="a in o.anomalies" :key="a.id" class="mb-12">
              <div class="flex-row">
                <el-tag :type="anomalyStatus[a.status]?.type">{{ anomalyStatus[a.status]?.text }}</el-tag>
                <el-tag :type="a.priority === 'high' ? 'danger' : 'info'" effect="plain">{{ a.priority === 'high' ? '高优先级' : '普通' }}</el-tag>
                <b>{{ a.type_name }}</b>
              </div>
              <div class="muted mt-12">{{ a.description }}</div>
              <div v-if="a.resolution" class="muted">处理结果：{{ a.resolution }}</div>
            </div>
          </div>
        </el-col>
      </el-row>

      <!-- 临时改餐 -->
      <el-dialog v-model="modifyVisible" title="临时改餐（将通知厨房与社区）" width="640px">
        <el-input v-model="modifyReason" placeholder="改餐原因（必填），如：老人今天想吃清淡点" class="mb-12" />
        <el-table :data="dishes" size="small" max-height="320">
          <el-table-column prop="name" label="菜品" width="130" />
          <el-table-column label="价格" width="80">
            <template #default="{ row }">{{ fmtMoney(row.price) }}</template>
          </el-table-column>
          <el-table-column label="数量" width="150">
            <template #default="{ row }">
              <el-input-number v-model="modifyQty[row.id]" :min="0" :max="5" size="small" />
            </template>
          </el-table-column>
        </el-table>
        <template #footer>
          <el-button @click="modifyVisible = false">取消</el-button>
          <el-button type="primary" :loading="acting" @click="doModify">确认改餐</el-button>
        </template>
      </el-dialog>

      <!-- 退餐 -->
      <el-dialog v-model="cancelVisible" title="退餐/取消" width="480px">
        <el-input v-model="cancelReason" type="textarea" placeholder="请填写退餐原因（必填）" />
        <template #footer>
          <el-button @click="cancelVisible = false">取消</el-button>
          <el-button type="danger" :loading="acting" @click="doCancel">确认退餐</el-button>
        </template>
      </el-dialog>

      <!-- 现场取餐签收 -->
      <el-dialog v-model="pickupVisible" title="社区食堂现场取餐签收" width="480px">
        <el-form label-width="110px">
          <el-form-item label="签收人姓名" required>
            <el-input v-model="pickupForm.signed_by_name" placeholder="老人或代领人姓名" />
          </el-form-item>
          <el-form-item label="现场回收餐盒">
            <el-input-number v-model="pickupForm.boxes_returned" :min="0" :max="o.boxes_issued" />
            <span class="muted" style="margin-left:8px">共 {{ o.boxes_issued }} 个</span>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="pickupVisible = false">取消</el-button>
          <el-button type="primary" :loading="acting" @click="doPickupConfirm">确认签收</el-button>
        </template>
      </el-dialog>

      <!-- 用餐反馈 -->
      <el-dialog v-model="feedbackVisible" title="用餐反馈" width="480px">
        <el-form label-width="90px">
          <el-form-item label="评分">
            <el-rate v-model="feedbackForm.rating" />
          </el-form-item>
          <el-form-item label="饭菜是否适合">
            <el-switch v-model="feedbackForm.suitable" active-text="适合" inactive-text="不适合" />
          </el-form-item>
          <el-form-item label="反馈内容">
            <el-input v-model="feedbackForm.content" type="textarea" placeholder="如：菜太硬、太咸、份量不够等" />
          </el-form-item>
        </el-form>
        <el-alert v-if="!feedbackForm.suitable" type="warning" :closable="false" show-icon
          title="标记为不适合后将自动生成异常工单，通知厨房与社区跟进" />
        <template #footer>
          <el-button @click="feedbackVisible = false">取消</el-button>
          <el-button type="primary" :loading="acting" @click="doFeedback">提交反馈</el-button>
        </template>
      </el-dialog>

      <!-- 回访老人本人/同住人或核实签收 -->
      <el-dialog v-model="confirmVisible" title="回访老人本人/同住人 · 核实签收" width="520px">
        <el-alert type="warning" :closable="false" show-icon class="mb-12"
          title="家属代订不得代老人放弃权益；口味、身体不适与签收异常须回访老人本人或同住人。待核实签收凭回访结论生效或退餐。" />
        <el-form label-width="110px">
          <el-form-item label="回访对象" required>
            <el-radio-group v-model="confirmForm.confirmer_role">
              <el-radio value="elder">老人本人</el-radio>
              <el-radio value="cohabitant">同住人</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="姓名" required>
            <el-input v-model="confirmForm.confirmer_name" />
          </el-form-item>
          <el-form-item label="回访方式">
            <el-radio-group v-model="confirmForm.method">
              <el-radio value="phone">电话</el-radio>
              <el-radio value="visit">上门</el-radio>
              <el-radio value="onsite">现场</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="实际收到用餐">
            <el-switch v-model="confirmForm.confirms_received" active-text="确认收到并用餐" inactive-text="否认收到" />
          </el-form-item>
          <el-form-item label="口味反馈">
            <el-input v-model="confirmForm.taste_feedback" placeholder="老人本人对口味/软硬度的反馈" />
          </el-form-item>
          <el-form-item label="异常标记">
            <el-checkbox v-model="confirmForm.body_discomfort">用餐后身体不适</el-checkbox>
            <el-checkbox v-model="confirmForm.receipt_dispute">签收异常/否认收到</el-checkbox>
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="confirmForm.note" type="textarea" :rows="2" />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="confirmVisible=false">取消</el-button>
          <el-button type="primary" :loading="acting" @click="doConfirm">提交回访</el-button>
        </template>
      </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'
import {
  orderStatus, mealTypes, deliveryTypes, orderSources, boxMethods, boxStatus,
  anomalyStatus, deliveryStatus, signBasisMap, effectivenessMap, fmtTime, fmtMoney
} from '../utils'
const effectivenessName = (e) => ({ valid: '有效', pending: '待社区核实', invalid: '核实无效' }[e] || '—')

const route = useRoute()
const o = ref(null)
const loading = ref(false)
const acting = ref(false)
const dishes = ref([])

const modifyVisible = ref(false)
const cancelVisible = ref(false)
const pickupVisible = ref(false)
const feedbackVisible = ref(false)
const modifyReason = ref('')
const cancelReason = ref('')
const modifyQty = reactive({})
const pickupForm = reactive({ signed_by_name: '', boxes_returned: 0 })
const feedbackForm = reactive({ rating: 5, suitable: true, content: '' })

const canModify = computed(() =>
  o.value && ['pending', 'confirmed'].includes(o.value.status) &&
  ['family', 'community', 'admin', 'elder'].includes(store.role)
)
const canCancel = computed(() =>
  o.value && ['pending', 'confirmed', 'preparing', 'ready'].includes(o.value.status) &&
  ['family', 'community', 'admin', 'elder'].includes(store.role)
)
const canPickupConfirm = computed(() =>
  o.value && o.value.status === 'ready' && o.value.delivery_type === 'community_pickup' &&
  ['community', 'admin'].includes(store.role)
)
const canFeedback = computed(() =>
  o.value && ['signed', 'completed'].includes(o.value.status) &&
  ['family', 'elder', 'community', 'admin'].includes(store.role)
)
const canConfirm = computed(() => o.value && ['community', 'admin'].includes(store.role))
const confirmVisible = ref(false)
const confirmForm = reactive({
  confirmer_role: 'elder', confirmer_name: '', method: 'phone', confirms_received: true,
  taste_feedback: '', body_discomfort: false, receipt_dispute: false, note: ''
})
function openConfirm() {
  Object.assign(confirmForm, {
    confirmer_role: 'elder', confirmer_name: o.value.elder.name, method: 'phone', confirms_received: true,
    taste_feedback: '', body_discomfort: false, receipt_dispute: false, note: ''
  })
  confirmVisible.value = true
}
async function doConfirm() {
  if (!confirmForm.confirmer_name) {
    ElMessage.warning('请填写回访对象姓名')
    return
  }
  acting.value = true
  try {
    await api.post(`/orders/${o.value.id}/elder-confirmation`, confirmForm)
    ElMessage.success('老人本人/同住人回访已记录')
    confirmVisible.value = false
    await load()
  } finally { acting.value = false }
}

async function load() {
  loading.value = true
  try {
    o.value = await api.get(`/orders/${route.params.id}`)
  } finally {
    loading.value = false
  }
}

async function doModify() {
  if (!modifyReason.value) {
    ElMessage.warning('请填写改餐原因')
    return
  }
  const items = dishes.value
    .filter((d) => (modifyQty[d.id] || 0) > 0)
    .map((d) => ({ dish_id: d.id, qty: modifyQty[d.id], custom_note: '' }))
  if (items.length === 0) {
    ElMessage.warning('请至少选择一道菜')
    return
  }
  acting.value = true
  try {
    await api.put(`/orders/${o.value.id}/items`, { reason: modifyReason.value, items })
    ElMessage.success('改餐成功，已通知厨房与社区')
    modifyVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

async function doCancel() {
  if (!cancelReason.value) {
    ElMessage.warning('请填写退餐原因')
    return
  }
  acting.value = true
  try {
    await api.post(`/orders/${o.value.id}/cancel`, { reason: cancelReason.value })
    ElMessage.success('已退餐')
    cancelVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

async function doPickupConfirm() {
  if (!pickupForm.signed_by_name) {
    ElMessage.warning('请填写签收人姓名')
    return
  }
  acting.value = true
  try {
    await api.post(`/orders/${o.value.id}/pickup-confirm`, pickupForm)
    ElMessage.success('现场签收完成')
    pickupVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

async function doFeedback() {
  acting.value = true
  try {
    await api.post(`/orders/${o.value.id}/feedback`, feedbackForm)
    ElMessage.success('反馈已提交，感谢！')
    feedbackVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

onMounted(async () => {
  await load()
  if (['family', 'community', 'admin', 'elder'].includes(store.role)) {
    dishes.value = await api.get('/dishes')
    dishes.value.forEach((d) => { modifyQty[d.id] = 0 })
  }
})
</script>
