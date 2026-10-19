import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawn, execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { createServer } from 'node:net'
const address = '127.0.0.1', port = 18080
await new Promise((done, reject) => {
  const probe = createServer()
  probe.once('error', reject)
  probe.listen(port, address, () => probe.close(done))
})
const directory = await mkdtemp(join(tmpdir(), 'freshplate-e2e-'))
const env = { ...process.env, GOTOOLCHAIN: 'go1.26.4', GOPROXY: 'https://goproxy.cn', GIN_MODE: 'release' }
delete env.GOROOT
let child
async function stop() {
  if (child && child.exitCode === null && child.signalCode === null) {
    await new Promise(resolveExit => {
      const timeout = setTimeout(() => child.kill('SIGKILL'), 10000)
      child.once('exit', () => { clearTimeout(timeout); resolveExit() })
      child.kill('SIGTERM')
    })
  }
  await rm(directory, { recursive: true, force: true })
}
for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, async () => { await stop(); process.exit(0) })
try {
  const executable = join(directory, 'server')
  const go = env.FRESHPLATE_E2E_GO || 'go'
  await promisify(execFile)(go, ['build', '-o', executable, './cmd/server'], { cwd: resolve('../server'), env, timeout: 180000 })
  const admin = join(directory, 'admin')
  await promisify(execFile)(go, ['build', '-o', admin, './cmd/admin'], { cwd: resolve('../server'), env, timeout: 180000 })
  // 种子现在还会经 go run 编译并写入普通用户，首次编译放宽到 120s。
  await promisify(execFile)('python3', [resolve('e2e/seed-admin.py'), admin, join(directory, 'test.db'), join(directory, 'csrf.key'), join(directory, 'jwt.key')], { env, timeout: 120000 })
  child = spawn(executable, ['-addr', `${address}:${port}`, '-origin', `http://${address}:${port}`, '-db', join(directory, 'test.db'), '-key', join(directory, 'csrf.key'), '-jwt-key', join(directory, 'jwt.key'), '-web', resolve('dist')], { env, stdio: 'inherit' })
  const exitCode = await new Promise((resolveExit, reject) => { child.once('error', reject); child.once('exit', code => resolveExit(code)) })
  await stop()
  process.exit(exitCode ?? 1)
} catch (error) { console.error(error); await stop(); process.exit(1) }
