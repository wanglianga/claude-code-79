<template>
  <div>
    <div class="page-card">
      <div class="flex-row" style="justify-content: space-between">
        <h3 class="page-title" style="margin:0">配送任务（{{ store.role === 'volunteer' ? '志愿者帮送' : '骑手配送' }}）</h3>
        <el-button @click="load" :icon="Refresh">刷新</el-button>
      </div>
    </div>

    <div v-if="tasks.length === 0" class="page-card muted">暂无配送任务</div>

    <el-row :gutter="16">
      <el-col :span="12" v-for="t in tasks" :key="t.id" style="margin-bottom:16px">
        <div class="page-card" style="margin:0">
          <div class="flex-row" style="justify-content: space-between">
            <b>{{ t.order_no }}</b>
            <div class="flex-row">
              <el-tag :type="deliveryStatus[t.status]?.type">{{ deliveryStatus[t.status]?.text }}</el-tag>
              <el-tag v-if="t.strict_mode" type="danger" effect="dark" size="small">严格签收</el-tag>
              <el-tag v-if="t.is_timeout" type="danger" size="small">超时</el-tag>
            </div>
          </div>
          <!-- 未开门风险与重点关注提示：提前告知配送员 -->
          <div v-if="t.focus || t.risk_level !== 'normal' || t.no_answer_count > 0 || t.delivery_confirm_mode === 'phone_first'"
            class="risk-bar">
            <el-tag v-if="t.focus" type="danger" effect="dark" size="small">⭐ 次日重点关注</el-tag>
            <el-tag v-if="t.risk_level === 'high'" type="danger" effect="dark" size="small">高风险老人</el-tag>
            <el-tag v-else-if="t.risk_level === 'attention'" type="warning" size="small">关注老人</el-tag>
            <el-tag v-if="t.no_answer_count > 0" type="warning" size="small">曾未开门×{{ t.no_answer_count }}</el-tag>
            <el-tag v-if="t.delivery_confirm_mode === 'phone_first'" type="primary" size="small">📞 先电话确认再上门</el-tag>
          </div>
          <el-descriptions :column="1" border size="small" class="mt-12">
            <el-descriptions-item label="老人">{{ t.elder_name }}（{{ t.phone || '无电话' }}）</el-descriptions-item>
            <el-descriptions-item label="地址">{{ t.address }}</el-descriptions-item>
            <el-descriptions-item label="紧急联系人">{{ t.emergency_contact_name }} {{ t.emergency_contact_phone }}</el-descriptions-item>
            <el-descriptions-item label="敲门确认">{{ t.need_knock_confirm ? '必须敲门确认' : '不需要' }}</el-descriptions-item>
            <el-descriptions-item label="保温箱" v-if="t.thermal_box_no">{{ t.thermal_box_no }}</el-descriptions-item>
          </el-descriptions>
          <el-alert v-if="t.strict_mode && t.status !== 'delivered'" type="error" :closable="false" show-icon class="mt-12"
            title="严格签收对象：送达必须拍照上传 + 敲门确认 + 记录签收人；异常需立即上报" />
          <div class="flex-row mt-12">
            <el-button v-if="t.status === 'assigned' && !t.mine" type="primary" @click="claim(t)">接单</el-button>
            <el-button v-if="t.status === 'assigned' && (t.mine || true)" type="warning"
              @click="openPickup(t)">取餐</el-button>
            <el-button v-if="t.status === 'picked'" type="success" @click="openDeliver(t)">送达签收</el-button>
            <el-button v-if="['assigned', 'picked'].includes(t.status)" type="danger" plain
              @click="openFail(t)">异常上报</el-button>
            <el-button @click="$router.push(`/orders/${t.order_id}`)">餐单详情</el-button>
          </div>
          <div v-if="t.sign_photo_url" class="mt-12">
            <el-image :src="t.sign_photo_url" fit="cover" style="width: 140px; height: 90px; border-radius: 6px"
              :preview-src-list="[t.sign_photo_url]" />
          </div>
        </div>
      </el-col>
    </el-row>

    <!-- 取餐 -->
    <el-dialog v-model="pickupVisible" title="取餐登记" width="440px">
      <el-form label-width="90px">
        <el-form-item label="保温箱编号" required>
          <el-input v-model="pickupForm.thermal_box_no" placeholder="如 WBX-01" />
        </el-form-item>
        <el-form-item label="配送路线">
          <el-input v-model="pickupForm.route_info" placeholder="如 社区食堂→幸福里沿线" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pickupVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doPickup">确认取餐</el-button>
      </template>
    </el-dialog>

    <!-- 送达签收 -->
    <el-dialog v-model="deliverVisible" title="送达签收" width="480px">
      <el-alert v-if="current?.strict_mode" type="error" :closable="false" show-icon class="mb-12"
        title="严格签收对象：照片、敲门确认、签收人三项缺一不可" />
      <el-form label-width="90px">
        <el-form-item label="送达照片" :required="current?.strict_mode">
          <el-upload :show-file-list="false" accept="image/*" :http-request="uploadPhoto">
            <el-button :loading="uploading">上传照片</el-button>
          </el-upload>
          <el-image v-if="deliverForm.sign_photo_url" :src="deliverForm.sign_photo_url" fit="cover"
            style="width: 120px; height: 80px; margin-left: 10px; border-radius: 6px" />
        </el-form-item>
        <el-form-item label="签收类型" required>
          <el-select v-model="deliverForm.sign_type">
            <el-option label="老人本人" value="elder" />
            <el-option label="家属代收" value="family" />
            <el-option label="社区代收" value="community" />
            <el-option label="志愿者代收" value="volunteer" />
          </el-select>
        </el-form-item>
        <el-form-item label="签收人" required>
          <el-input v-model="deliverForm.signed_by_name" placeholder="签收人姓名" />
        </el-form-item>
        <el-form-item label="敲门确认">
          <el-switch v-model="deliverForm.knock_confirmed" active-text="已敲门确认老人收到" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="deliverVisible = false">取消</el-button>
        <el-button type="success" :loading="acting" @click="doDeliver">确认送达</el-button>
      </template>
    </el-dialog>

    <!-- 异常上报 -->
    <el-dialog v-model="failVisible" title="配送异常上报" width="480px">
      <el-alert type="warning" :closable="false" show-icon class="mb-12"
        title="上报后将生成异常工单并立即通知社区与家属；涉及老人安全请同时电话联系社区" />
      <el-form label-width="90px">
        <el-form-item label="异常类型" required>
          <el-select v-model="failForm.type">
            <el-option label="老人未开门" value="no_answer" />
            <el-option label="其他异常" value="other" />
          </el-select>
        </el-form-item>
        <template v-if="failForm.type === 'no_answer'">
          <el-form-item label="联系尝试" required>
            <div class="attempt-grid">
              <el-checkbox v-model="failForm.knock_done">已敲门</el-checkbox>
              <el-checkbox v-model="failForm.phone_done">已电话联系老人</el-checkbox>
              <el-checkbox v-model="failForm.neighbor_done">已询问邻里</el-checkbox>
              <el-checkbox v-model="failForm.family_done">已联系家属</el-checkbox>
            </div>
          </el-form-item>
          <el-alert type="error" :closable="false" show-icon class="mb-12"
            title="未开门上报必须先完成「敲门」与「电话联系」两项" />
        </template>
        <el-form-item label="情况说明">
          <el-input v-model="failForm.note" type="textarea" placeholder="如：敲门 5 分钟无人应答，电话未接通" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="failVisible = false">取消</el-button>
        <el-button type="danger" :loading="acting" @click="doFail">确认上报</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { deliveryStatus } from '../utils'

const tasks = ref([])
const acting = ref(false)
const uploading = ref(false)
const current = ref(null)

const pickupVisible = ref(false)
const deliverVisible = ref(false)
const failVisible = ref(false)
const pickupForm = reactive({ thermal_box_no: '', route_info: '' })
const deliverForm = reactive({ sign_photo_url: '', sign_type: 'elder', signed_by_name: '', knock_confirmed: false })
const failForm = reactive({ type: 'no_answer', note: '', knock_done: false, phone_done: false, neighbor_done: false, family_done: false })

async function load() {
  tasks.value = await api.get('/delivery/tasks')
}

async function claim(t) {
  await api.post(`/delivery/${t.id}/claim`)
  ElMessage.success('接单成功')
  await load()
}

function openPickup(t) {
  current.value = t
  pickupForm.thermal_box_no = 'WBX-01'
  pickupForm.route_info = ''
  pickupVisible.value = true
}

async function doPickup() {
  if (!pickupForm.thermal_box_no) {
    ElMessage.warning('请填写保温箱编号')
    return
  }
  acting.value = true
  try {
    await api.post(`/delivery/${current.value.id}/pickup`, pickupForm)
    ElMessage.success('已取餐，开始配送')
    pickupVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

function openDeliver(t) {
  current.value = t
  deliverForm.sign_photo_url = ''
  deliverForm.sign_type = 'elder'
  deliverForm.signed_by_name = ''
  deliverForm.knock_confirmed = false
  deliverVisible.value = true
}

async function uploadPhoto({ file }) {
  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', file)
    const res = await api.post('/uploads', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    deliverForm.sign_photo_url = res.url
    ElMessage.success('照片已上传')
  } finally {
    uploading.value = false
  }
}

async function doDeliver() {
  acting.value = true
  try {
    const res = await api.post(`/delivery/${current.value.id}/deliver`, deliverForm)
    ElMessage.success(res.is_timeout ? '已签收（本次配送超时，已自动生成超时工单）' : '送达签收完成')
    deliverVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

function openFail(t) {
  current.value = t
  Object.assign(failForm, { type: 'no_answer', note: '', knock_done: false, phone_done: false, neighbor_done: false, family_done: false })
  failVisible.value = true
}

async function doFail() {
  if (failForm.type === 'no_answer' && (!failForm.knock_done || !failForm.phone_done)) {
    ElMessage.warning('未开门上报必须先完成「敲门」与「电话联系」')
    return
  }
  acting.value = true
  try {
    const res = await api.post(`/delivery/${current.value.id}/fail`, failForm)
    ElMessage.warning(res.no_answer_count >= 2
      ? `异常已上报；该老人已连续 ${res.no_answer_count} 次未开门，社区将发起上门查看`
      : '异常已上报，社区将跟进回访')
    failVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.risk-bar {
  display: flex; flex-wrap: wrap; gap: 6px;
  margin-top: 8px; padding: 8px 10px;
  background: #fdf0e2; border: 1px solid #f5d9b8; border-radius: 8px;
}
.attempt-grid { display: grid; grid-template-columns: 1fr 1fr; }
</style>
