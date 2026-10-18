export function passwordError(value) {
  const length = [...value].length
  return length < 12 || length > 128 || new TextEncoder().encode(value).length > 512 ? '密码须为 12–128 个字符，UTF-8 编码不超过 512 字节；不要求特定字符组合。' : ''
}
