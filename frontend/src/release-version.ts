function parts(value: string) {
  const m = /^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(value)
  if (!m || (m[4] ?? '').split('.').some(s => /^0[0-9]+$/.test(s))) return null
  return { numbers: m.slice(1, 4).map(BigInt), pre: m[4] ?? '' }
}
function compare(a: string, b: string): number | null {
  const x = parts(a), y = parts(b)
  if (!x || !y) return null
  for (let i = 0; i < 3; i++) { if (x.numbers[i] !== y.numbers[i]) return x.numbers[i]! < y.numbers[i]! ? -1 : 1 }
  if (x.pre === y.pre) return 0
  if (!x.pre) return 1
  if (!y.pre) return -1
  const xs = x.pre.split('.'), ys = y.pre.split('.')
  for (let i = 0; i < Math.min(xs.length, ys.length); i++) {
    const a = xs[i]!, b = ys[i]!
    if (a === b) continue
    const an = /^\d+$/.test(a), bn = /^\d+$/.test(b)
    if (an && bn) return BigInt(a) < BigInt(b) ? -1 : 1
    if (an !== bn) return an ? -1 : 1
    return a < b ? -1 : 1
  }
  return xs.length < ys.length ? -1 : 1
}
export function releaseVersionIssue(version: string, history: string[]): string {
  if (!parts(version)) return '请输入语义版本，例如 1.0.0、1.0.1 或 1.1.0-rc.1'
  if (history.some(v => v === version || compare(version, v) === 0)) return '此版本已有发布记录，请使用自动建议的新版本；恢复旧版本请前往发布记录回滚。'
  if (history.some(v => compare(version, v) === -1)) return '待发布版本低于历史版本。请升级版本，或前往发布记录回滚。'
  return ''
}
