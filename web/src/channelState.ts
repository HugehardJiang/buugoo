// 聊天软件的连接情况：侧边栏的状态点和「聊天软件」页共用。
import { ref } from 'vue'
import { api, type Binding, type ChannelInfo, type WeixinUser } from './api'

export const channelState = ref<{ channels: ChannelInfo[]; bindings: Binding[]; weixin: WeixinUser | null } | null>(null)

// connectedCount 是自己已经连上的聊天软件数（扫码连接的微信、用绑定码绑定的 QQ / Telegram）。
export function connectedCount() {
  const s = channelState.value
  if (!s) return 0
  return (s.weixin?.account ? 1 : 0) + s.bindings.filter((b) => b.channel !== 'weixin').length
}

export async function refreshChannels() {
  try {
    channelState.value = await api.channels()
  } catch {
    // 读取失败时保留上一次的结果。
  }
  return channelState.value
}
