function cssVar(name: string, fallback: string) {
  if (typeof window === 'undefined') return fallback
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

export function chartTheme() {
  return {
    background: cssVar('--chart-bg', '#ffffff'),
    text: cssVar('--chart-text', '#334155'),
    grid: cssVar('--chart-grid', '#eef2f7'),
    border: cssVar('--chart-border', '#dbe3ee'),
    line: cssVar('--chart-line', '#2563eb'),
    lineTop: cssVar('--chart-line-top', 'rgba(37, 99, 235, 0.24)'),
    lineBottom: cssVar('--chart-line-bottom', 'rgba(37, 99, 235, 0.03)'),
    altLine: cssVar('--chart-alt-line', '#0f766e'),
    altLineTop: cssVar('--chart-alt-line-top', 'rgba(15, 118, 110, 0.22)'),
    altLineBottom: cssVar('--chart-alt-line-bottom', 'rgba(15, 118, 110, 0.03)'),
    volume: cssVar('--chart-volume', 'rgba(100, 116, 139, 0.35)'),
    up: cssVar('--quote-up', '#dc2626'),
    down: cssVar('--quote-down', '#059669'),
    upSoft: cssVar('--quote-up-soft', 'rgba(220, 38, 38, 0.28)'),
    downSoft: cssVar('--quote-down-soft', 'rgba(5, 150, 105, 0.28)')
  }
}
