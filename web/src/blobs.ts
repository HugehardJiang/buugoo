// 附件需要带登录令牌读取，不能直接用 <img src>：先取回 Blob 再生成本地地址。同一个文件在页面中只取一次。
import { api } from './api'

const cache = new Map<number, Promise<string>>()

export function blobURL(id: number): Promise<string> {
  let p = cache.get(id)
  if (!p) {
    p = api.fetchUpload(id).then((b) => URL.createObjectURL(b))
    cache.set(id, p)
    p.catch(() => cache.delete(id))
  }
  return p
}

// forget 在文件删除后释放本地地址。
export function forget(id: number) {
  const p = cache.get(id)
  if (!p) return
  cache.delete(id)
  p.then((u) => URL.revokeObjectURL(u)).catch(() => {})
}

// download 把附件保存到本地。
export async function download(id: number, filename: string) {
  const link = document.createElement('a')
  link.href = URL.createObjectURL(await api.fetchUpload(id))
  link.download = filename
  link.click()
  setTimeout(() => URL.revokeObjectURL(link.href), 10000)
}
