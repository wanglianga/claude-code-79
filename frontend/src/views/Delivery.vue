<template>
  <div>
    <!-- 志愿者发车前核验 -->
    <div v-if="isVol" class="page-card">
      <div class="flex-row" style="justify-content: space-between">
        <h3 class="page-title" style="margin:0">发车前核验</h3>
        <div class="flex-row">
          <el-button size="small" @click="openHealth">当日健康打卡</el-button>
          <el-button size="small" @click="loadDispatch">刷新核验</el-button>
        </div>
      </div>
      <el-alert v-if="dispatch" :type="dispatch.dispatch_passed ? 'success' : 'error'" :closable="false" show-icon class="mt-12"
        :title="dispatch.dispatch_passed ? '核验通过，可以开始帮送' : ('核验未通过：' + (dispatch.block_reasons||[]).join('；'))">
        <div class="flex-row" style="gap:14px;font-size:13px">
          <span>身份核验：<b :class="dispatch.id_verified?'':'danger-text'">{{ dispatch.id_verified?'已核验':'未核验' }}</b></span>
          <span>所属：{{ dispatch.org || '—' }}</span>
          <span>助餐培训：<b :class="dispatch.trained?'':'danger-text'">{{ dispatch.trained?('已培训'+(dispatch.training_date?(' '+dispatch.training_date):'')):'未培训' }}</b></span>
          <span>帮送资格：<b :class="dispatch.eligible?'':'danger-text'">{{ dispatch.eligible?'正常':('已暂停 '+dispatch.suspend_reason) }}</b></span>
          <span>今日健康：<b :class="dispatch.health_status==='healthy'?'':'danger-text'">{{ dispatch.health_checked?(dispatch.health_status==='healthy'?'健康':'不适'):'未打卡' }}</b></span>
        </div>
      </el-alert>
    </div>

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
              @click="isVol ? openVolPickup(t) : openPickup(t)">取餐</el-button>
            <el-button v-if="t.status === 'picked'" type="success" @click="isVol ? openVolDeliver(t) : openDeliver(t)">
              {{ isVol ? '送达签收（按效力判定）' : '送达签收' }}
            </el-button>
            <el-button v-if="['assigned','picked'].includes(t.status)" type="danger" plain
              @click="isVol ? openVolFail(t) : openFail(t)">异常上报</el-button>
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

    <!-- 志愿者健康打卡 -->
    <el-dialog v-model="healthVisible" title="当日健康打卡" width="400px">
      <el-form label-width="90px">
        <el-form-item label="健康状态">
          <el-radio-group v-model="healthForm.health_status">
            <el-radio value="healthy">健康可帮送</el-radio>
            <el-radio value="unwell">身体不适（暂停帮送）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="体温">
          <el-input-number v-model="healthForm.temperature" :min="35" :max="42" :precision="1" /> ℃
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="healthForm.note" type="textarea" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="healthVisible=false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doHealth">提交打卡</el-button>
      </template>
    </el-dialog>

    <!-- 志愿者取餐：保温箱/批次/ETA/关系/知情同意 -->
    <el-dialog v-model="volPickupVisible" title="志愿者取餐（发车核验通过后）" width="480px">
      <el-alert type="info" :closable="false" show-icon class="mb-12"
        title="请登记保温箱、出餐批次、预计送达，并取得老人本人或家属知情同意" />
      <el-form label-width="110px">
        <el-form-item label="保温箱编号" required>
          <el-input v-model="volPickup.thermal_box_no" placeholder="如 WBX-V01" />
        </el-form-item>
        <el-form-item label="帮送路线">
          <el-input v-model="volPickup.route_info" placeholder="如 社区食堂→康乐社区沿线" />
        </el-form-item>
        <el-form-item label="预计送达">
          <el-input-number v-model="volPickup.eta_minutes" :min="5" :max="120" /> 分钟
        </el-form-item>
        <el-form-item label="与老人关系" required>
          <el-select v-model="volPickup.relation_to_elder" style="width:100%">
            <el-option label="邻里" value="邻里" />
            <el-option label="同楼栋住户" value="同楼栋住户" />
            <el-option label="社区志愿队" value="社区志愿队" />
            <el-option label="公益组织志愿者" value="公益组织志愿者" />
            <el-option label="其他（备注说明）" value="其他" />
          </el-select>
        </el-form-item>
        <el-form-item label="知情同意" required>
          <el-checkbox v-model="volPickup.informed_consent">老人本人或家属已知情同意本次帮送</el-checkbox>
        </el-form-item>
        <el-form-item label="同意人" required>
          <el-input v-model="volPickup.consent_by" placeholder="老人本人或家属姓名" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="volPickupVisible=false">取消</el-button>
        <el-button type="warning" :loading="acting" @click="doVolPickup">确认取餐帮送</el-button>
      </template>
    </el-dialog>

    <!-- 志愿者四方式签收 -->
    <el-dialog v-model="volDeliverVisible" title="志愿者帮送签收（效力区别于骑手）" width="520px">
      <el-alert type="warning" :closable="false" show-icon class="mb-12"
        title="本人/家属代签即有效；邻里见证须社区授权且知情同意；拍照留证不能仅凭自述核销，须社区核实。认知障碍/独居/行动不便老人的邻里见证或拍照一律先社区核实。" />
      <el-form label-width="100px">
        <el-form-item label="签收方式" required>
          <el-radio-group v-model="volDeliver.sign_type">
            <el-radio value="elder">老人本人签收</el-radio>
            <el-radio value="family">家属代签</el-radio>
            <el-radio value="neighbor">邻里见证</el-radio>
            <el-radio value="photo">志愿者拍照留证</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="volDeliver.sign_type==='photo'" label="送达照片" required>
          <el-upload :show-file-list="false" accept="image/*" :http-request="uploadVolPhoto">
            <el-button :loading="uploading">上传照片</el-button>
          </el-upload>
          <el-image v-if="volDeliver.sign_photo_url" :src="volDeliver.sign_photo_url" fit="cover"
            style="width:120px;height:80px;margin-left:10px;border-radius:6px" />
        </el-form-item>
        <el-form-item :label="volDeliver.sign_type==='neighbor'?'见证人姓名':'签收人'" required>
          <el-input v-model="volDeliver.signed_by_name" :placeholder="volDeliver.sign_type==='neighbor'?'邻里见证人姓名':'签收人姓名'" />
        </el-form-item>
        <el-form-item v-if="volDeliver.sign_type==='neighbor'" label="见证人电话">
          <el-input v-model="volDeliver.witness_phone" />
        </el-form-item>
        <el-form-item label="敲门确认">
          <el-switch v-model="volDeliver.knock_confirmed" active-text="已当面交付" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="volDeliverVisible=false">取消</el-button>
        <el-button type="success" :loading="acting" @click="doVolDeliver">提交签收</el-button>
      </template>
    </el-dialog>

    <!-- 志愿者帮送异常 -->
    <el-dialog v-model="volFailVisible" title="志愿者帮送异常上报" width="480px">
      <el-alert type="warning" :closable="false" show-icon class="mb-12"
        title="社区将核实路线、取餐时间、送达照片与老人反馈，再决定补送/退餐/重新签收；涉及食品安全或老人不适将分别划分责任，不计骑手考核。" />
      <el-form label-width="100px">
        <el-form-item label="异常情形" required>
          <el-radio-group v-model="volFail.subtype">
            <el-radio value="spill">餐品洒漏</el-radio>
            <el-radio value="late">配送迟到</el-radio>
            <el-radio value="wrong_address">送错地址</el-radio>
            <el-radio value="not_received">老人否认收到</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="食品安全">
          <el-checkbox v-model="volFail.food_safety">涉及食品安全或老人用餐后不适</el-checkbox>
        </el-form-item>
        <el-form-item label="情况说明">
          <el-input v-model="volFail.note" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="volFailVisible=false">取消</el-button>
        <el-button type="danger" :loading="acting" @click="doVolFail">确认上报</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { deliveryStatus } from '../utils'

const tasks = ref([])
const acting = ref(false)
const uploading = ref(false)
const current = ref(null)
const isVol = computed(() => store.role === 'volunteer')
const dispatch = ref(null)

const pickupVisible = ref(false)
const deliverVisible = ref(false)
const failVisible = ref(false)
const healthVisible = ref(false)
const volPickupVisible = ref(false)
const volDeliverVisible = ref(false)
const volFailVisible = ref(false)
const pickupForm = reactive({ thermal_box_no: '', route_info: '' })
const deliverForm = reactive({ sign_photo_url: '', sign_type: 'elder', signed_by_name: '', knock_confirmed: false })
const failForm = reactive({ type: 'no_answer', note: '', knock_done: false, phone_done: false, neighbor_done: false, family_done: false })
const healthForm = reactive({ health_status: 'healthy', temperature: 36.5, note: '' })
const volPickup = reactive({ thermal_box_no: '', route_info: '', eta_minutes: 30, relation_to_elder: '', informed_consent: false, consent_by: '' })
const volDeliver = reactive({ sign_photo_url: '', sign_type: 'elder', signed_by_name: '', witness_name: '', witness_phone: '', knock_confirmed: false })
const volFail = reactive({ subtype: 'spill', note: '', food_safety: false })

async function loadDispatch() {
  if (!isVol.value) return
  dispatch.value = await api.get('/volunteer/dispatch-status')
}
function openHealth() {
  Object.assign(healthForm, { health_status: 'healthy', temperature: 36.5, note: '' })
  healthVisible.value = true
}
async function doHealth() {
  acting.value = true
  try {
    await api.post('/volunteer/health-checkin', healthForm)
    ElMessage.success('健康打卡成功')
    healthVisible.value = false
    await loadDispatch()
  } finally { acting.value = false }
}
function openVolPickup(t) {
  current.value = t
  Object.assign(volPickup, { thermal_box_no: 'WBX-V01', route_info: '', eta_minutes: 30, relation_to_elder: '', informed_consent: false, consent_by: '' })
  volPickupVisible.value = true
}
async function doVolPickup() {
  if (!volPickup.relation_to_elder || !volPickup.informed_consent || !volPickup.consent_by) {
    ElMessage.warning('请填写与老人关系、勾选知情同意并登记同意人')
    return
  }
  acting.value = true
  try {
    await api.post(`/delivery/${current.value.id}/volunteer-pickup`, volPickup)
    ElMessage.success('已取餐，开始帮送')
    volPickupVisible.value = false
    await load()
  } finally { acting.value = false }
}
function openVolDeliver(t) {
  current.value = t
  Object.assign(volDeliver, { sign_photo_url: '', sign_type: 'elder', signed_by_name: '', witness_name: '', witness_phone: '', knock_confirmed: false })
  volDeliverVisible.value = true
}
async function uploadVolPhoto({ file }) {
  uploading.value = true
  try {
    const fd = new FormData(); fd.append('file', file)
    const res = await api.post('/uploads', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    volDeliver.sign_photo_url = res.url
  } finally { uploading.value = false }
}
async function doVolDeliver() {
  if (volDeliver.sign_type === 'photo' && !volDeliver.sign_photo_url) {
    ElMessage.warning('拍照留证必须上传照片')
    return
  }
  acting.value = true
  try {
    const payload = {
      sign_type: volDeliver.sign_type, signed_by_name: volDeliver.signed_by_name,
      witness_name: volDeliver.sign_type === 'neighbor' ? volDeliver.signed_by_name : '',
      witness_phone: volDeliver.witness_phone, knock_confirmed: volDeliver.knock_confirmed,
      sign_photo_url: volDeliver.sign_photo_url
    }
    const res = await api.post(`/delivery/${current.value.id}/volunteer-deliver`, payload)
    if (res.effectiveness === 'valid') ElMessage.success('签收有效：' + res.effectiveness_reason)
    else ElMessage.warning('签收待社区核实，核实前不核销：' + res.effectiveness_reason)
    volDeliverVisible.value = false
    await load()
  } finally { acting.value = false }
}
function openVolFail(t) {
  current.value = t
  Object.assign(volFail, { subtype: 'spill', note: '', food_safety: false })
  volFailVisible.value = true
}
async function doVolFail() {
  acting.value = true
  try {
    await api.post(`/delivery/${current.value.id}/volunteer-exception`, volFail)
    ElMessage.warning('帮送异常已上报，等待社区核实')
    volFailVisible.value = false
    await load()
  } finally { acting.value = false }
}

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

onMounted(() => {
  load()
  loadDispatch()
})
</script>

<style scoped>
.risk-bar {
  display: flex; flex-wrap: wrap; gap: 6px;
  margin-top: 8px; padding: 8px 10px;
  background: #fdf0e2; border: 1px solid #f5d9b8; border-radius: 8px;
}
.attempt-grid { display: grid; grid-template-columns: 1fr 1fr; }
</style>
