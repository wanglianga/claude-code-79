import axios from 'axios'
import { ElMessage } from 'element-plus'

const api = axios.create({ baseURL: '/api', timeout: 15000 })

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

api.interceptors.response.use(
  (res) => res.data.data,
  (err) => {
    const msg = err.response?.data?.error || err.message || '网络错误'
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      if (location.pathname !== '/login') {
        ElMessage.error('登录已过期，请重新登录')
        location.href = '/login'
        return new Promise(() => {})
      }
    }
    ElMessage.error(msg)
    return Promise.reject(new Error(msg))
  }
)

export default api
